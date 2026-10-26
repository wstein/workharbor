// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// RemovalResult is the FR-067 preview and execution inventory. Applied means a
// successful Docker acknowledgement; Attempted without Applied is uncertain.
// Unapplied actions remain explicit recovery work after a partial failure.
type RemovalResult struct {
	DryRun               bool               `json:"dry_run"`
	PreserveHomeVolume   bool               `json:"preserve_home_volume"`
	Containers           []RemovalContainer `json:"containers"`
	Volumes              []RemovalVolume    `json:"volumes"`
	PreservedHomeVolumes []string           `json:"preserved_home_volumes"`
	Actions              []RemovalAction    `json:"actions"`
}
type RemovalContainer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type RemovalVolume struct {
	Name   string         `json:"name"`
	Mounts []RemovalMount `json:"mounts"`
}
type RemovalMount struct {
	ContainerID string `json:"container_id"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
}
type RemovalAction struct {
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Applied   bool   `json:"applied"`
	Attempted bool   `json:"attempted"`
}

// Removal binds one private inspected plan to one admitted daemon. A preview is
// a copy, and Apply consumes the transaction once, including on failed attempts.
// Labels are usability guards, not an authorization boundary against Docker users.
type Removal struct {
	mu        sync.Mutex
	used      bool
	client    Client
	endpoint  string
	options   RemovalOptions
	plan      removalPlan
	candidate *candidateRemoval
}

func (c Client) PrepareRemoval(parent context.Context, ports doctor.Ports, options RemovalOptions) (*Removal, error) {
	if err := validateRemovalOptions(options); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	endpoint, err := ResolveEndpoint(ctx, ports)
	if err != nil {
		return nil, err
	}
	return c.prepareRemovalAtEndpoint(ctx, endpoint, options, "")
}

func (c Client) prepareRemovalAtEndpoint(ctx context.Context, endpoint string, options RemovalOptions, stateRoot string) (*Removal, error) {
	all, err := c.removalSnapshot(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	candidate, err := c.prepareCandidateRemoval(ctx, endpoint, all, options, stateRoot)
	if err != nil {
		return nil, err
	}
	var plan removalPlan
	if candidate != nil {
		plan, err = candidate.removalPlan(ctx, all, options)
	} else {
		plan, err = planRemoval(all, options)
	}
	if err != nil {
		if candidate != nil {
			candidate.Close()
		}
		return nil, err
	}
	return &Removal{client: c, endpoint: endpoint, options: options, plan: plan, candidate: candidate}, nil
}

func (r *Removal) Preview() RemovalResult {
	result := removalResult(r.plan)
	if r.candidate != nil {
		if r.candidate.failed != nil {
			result.Actions = append([]RemovalAction{{Kind: "container", Target: r.candidate.failed.ID}}, result.Actions...)
			result.Containers = append(result.Containers, RemovalContainer{ID: r.candidate.failed.ID, Name: strings.TrimPrefix(r.candidate.failed.Name, "/"), Role: "primary"})
		}
		result.Actions = append(result.Actions, RemovalAction{Kind: "candidate_tree", Target: r.candidate.store.operationPath(r.candidate.plan.OperationID)}, RemovalAction{Kind: "candidate_records", Target: r.candidate.plan.OperationID})
	}
	return result
}

func removalResult(plan removalPlan) RemovalResult {
	result := RemovalResult{DryRun: true, PreserveHomeVolume: plan.PreserveHomeVolume, Containers: []RemovalContainer{}, Volumes: []RemovalVolume{}, PreservedHomeVolumes: []string{}, Actions: []RemovalAction{}}
	preserved := map[string]bool{}
	deletedVolumes := map[string]bool{}
	for _, v := range plan.Volumes {
		deletedVolumes[v.Name] = true
	}
	for _, c := range plan.Containers {
		role, _ := workspaceRole(c.Labels)
		result.Containers = append(result.Containers, RemovalContainer{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Role: role})
		result.Actions = append(result.Actions, RemovalAction{Kind: "container", Target: c.ID})
		{
			for _, m := range c.Mounts {
				if homeMount(m) && !deletedVolumes[m.Name] {
					preserved[m.Name] = true
				}
			}
		}
	}
	for _, v := range plan.Volumes {
		volume := RemovalVolume{Name: v.Name, Mounts: []RemovalMount{}}
		for _, c := range plan.Containers {
			for _, m := range c.Mounts {
				if m.Type == "volume" && m.Name == v.Name {
					volume.Mounts = append(volume.Mounts, RemovalMount{ContainerID: c.ID, Destination: m.Destination, ReadOnly: !m.RW})
				}
			}
		}
		result.Volumes = append(result.Volumes, volume)
		result.Actions = append(result.Actions, RemovalAction{Kind: "volume", Target: v.Name})
	}
	for v := range preserved {
		result.PreservedHomeVolumes = append(result.PreservedHomeVolumes, v)
	}
	sort.Strings(result.PreservedHomeVolumes)
	result.PreserveHomeVolume = plan.PreserveHomeVolume || len(result.PreservedHomeVolumes) > 0
	return result
}

func sameRemovalTargets(a, b removalPlan) bool {
	if len(a.Containers) != len(b.Containers) {
		return false
	}
	for i, c := range a.Containers {
		other := b.Containers[i]
		if !sameRemovalContainer(c, other) {
			return false
		}
	}
	return reflect.DeepEqual(a.Volumes, b.Volumes)
}

func (r *Removal) Apply(parent context.Context) (RemovalResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.Preview()
	result.DryRun = false
	if r.used {
		return result, exitcode.Errorf(exitcode.PathConflict, "removal transaction already consumed; prepare a new preview")
	}
	r.used = true
	if r.candidate != nil {
		return r.applyCandidate(parent, result)
	}
	fail := func(cause error) (RemovalResult, error) {
		for _, action := range result.Actions {
			if action.Attempted {
				e := exitcode.Errorf(exitcode.PathPartialFailure, "local removal incomplete; unresolved targets: %s", unresolvedRemoval(result)).WithRemediation("Inspect the listed targets before retrying; container removal is irreversible and home volumes are not recreated.")
				e.Result = result
				return result, e
			}
		}
		return result, cause
	}
	fresh, err := r.client.removalSnapshot(parent, r.endpoint)
	if err != nil {
		return fail(err)
	}
	plan, err := planRemoval(fresh, r.options)
	if err != nil {
		return fail(err)
	}
	if !sameRemovalTargets(r.plan, plan) {
		return fail(exitcode.Errorf(exitcode.PathConflict, "local removal targets changed since preview; request a new preview"))
	}
	for i, action := range result.Actions {
		if err := parent.Err(); err != nil {
			return fail(err)
		}
		if i > 0 {
			fresh, err = r.client.removalSnapshot(parent, r.endpoint)
			if err != nil {
				return fail(err)
			}
		}
		var args []string
		if action.Kind == "container" {
			current, err := planRemoval(fresh, RemovalOptions{Target: action.Target, Volumes: r.options.Volumes, Force: r.options.Force})
			if err != nil {
				return fail(err)
			}
			if len(current.Containers) != 1 || !sameRemovalContainer(r.plan.Containers[i], current.Containers[0]) {
				return fail(exitcode.Errorf(exitcode.PathConflict, "local container changed before removal"))
			}
			args = []string{"rm", "-f", "--", action.Target}
		} else {
			for _, c := range fresh {
				for _, m := range c.Mounts {
					if m.Type == "volume" && m.Name == action.Target {
						return fail(exitcode.Errorf(exitcode.PathConflict, "home volume %s is still mounted; not removed", action.Target))
					}
				}
			}
			args = []string{"volume", "rm", "--", action.Target}
		}
		result.Actions[i].Attempted = true
		ctx, cancel := context.WithTimeout(parent, doctor.Budget)
		reply, runErr := r.client.mutate(ctx, r.endpoint, args...)
		cancel()
		if runErr != nil || reply.status != 0 || strings.TrimSpace(string(reply.stdout)) != action.Target {
			return fail(exitcode.Errorf(exitcode.PathConflict, "Docker removal could not be confirmed"))
		}
		result.Actions[i].Applied = true
	}
	return result, nil
}

func unresolvedRemoval(result RemovalResult) string {
	var unresolved []string
	for _, a := range result.Actions {
		if !a.Applied {
			unresolved = append(unresolved, a.Kind+":"+a.Target)
		}
	}
	return strings.Join(unresolved, ", ")
}

func (c Client) removalSnapshot(parent context.Context, endpoint string) ([]inspection, error) {
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	all, _, err := c.inspectAllAt(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	for _, record := range all {
		if record.Mounts == nil {
			return nil, exitcode.Errorf(exitcode.PathConflict, "incomplete local mount metadata; no removal admitted")
		}
		for _, m := range record.Mounts {
			if m.Type == "" || !strings.HasPrefix(m.Destination, "/") || (m.Type == "volume" && !volumeName(m.Name)) {
				return nil, exitcode.Errorf(exitcode.PathConflict, "invalid local mount metadata; no removal admitted")
			}
		}
	}
	return all, nil
}

func volumeName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || (i > 0 && (c == '_' || c == '.' || c == '-'))) {
			return false
		}
	}
	return true
}

func (c Client) mutate(ctx context.Context, endpoint string, args ...string) (readResult, error) {
	container := len(args) == 4 && args[0] == "rm" && args[1] == "-f" && args[2] == "--" && fullID(args[3])
	volume := len(args) == 4 && args[0] == "volume" && args[1] == "rm" && args[2] == "--" && volumeName(args[3]) && !strings.HasPrefix(args[3], "bundledock-")
	if !strings.HasPrefix(endpoint, "unix://") || !strings.HasPrefix(strings.TrimPrefix(endpoint, "unix://"), "/") || (!container && !volume) {
		return readResult{}, fmt.Errorf("unsupported local removal operation")
	}
	return c.runFixed(ctx, endpoint, args...)
}

func sameRemovalContainer(a, b inspection) bool {
	return a.ID == b.ID && a.Name == b.Name && reflect.DeepEqual(a.Labels, b.Labels) && reflect.DeepEqual(a.Mounts, b.Mounts)
}

// Close releases the candidate daemon lock on dry-run, refusal, or completion.
func (r *Removal) Close() error {
	if r.candidate != nil {
		return r.candidate.Close()
	}
	return nil
}
