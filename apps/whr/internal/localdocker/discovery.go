// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// Workspace is the FR-066 discovery record. Labels establish usability scope,
// not an authentication boundary. No container environment is requested.
type Workspace struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Role             string            `json:"role"`
	Image            string            `json:"image"`
	State            string            `json:"state"`
	Status           string            `json:"status"`
	WorkspaceFolder  string            `json:"workspace_folder"`
	Labels           map[string]string `json:"labels"`
	HomeVolume       *string           `json:"home_volume"`
	ProtectedVolumes []string          `json:"protected_volumes"`
}
type mount struct {
	Type, Name, Source, Destination string
	RW                              bool
	readOnlyUnknown                 bool // missing/null RW is not evidence of a read-only mount
}
type inspection struct {
	ID                 string `json:"Id"`
	Name, Image, State string
	Labels             map[string]string
	Mounts             []mount
}

// Discover shares one total five-second wall-clock deadline across socket
// selection and both read-only Docker calls. It includes stopped containers.
type Snapshot struct {
	Endpoint   string
	Workspaces []Workspace
}

// Snapshot retains the admitted endpoint so shell preflight and exec cannot
// rediscover a different daemon. Discovery itself keeps its five-second cap.
func (c Client) Snapshot(parent context.Context, p doctor.Ports) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	endpoint, err := ResolveEndpoint(ctx, p)
	if err != nil {
		return Snapshot{}, err
	}
	workspaces, err := c.discoverAt(ctx, endpoint)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Endpoint: endpoint, Workspaces: workspaces}, nil
}
func (c Client) Discover(parent context.Context, p doctor.Ports) ([]Workspace, error) {
	snapshot, err := c.Snapshot(parent, p)
	return snapshot.Workspaces, err
}
func (c Client) discoverAt(ctx context.Context, endpoint string) ([]Workspace, error) {
	all, statuses, err := c.inspectAllAt(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	out := []Workspace{}
	for _, item := range all {
		role, recognized := workspaceRole(item.Labels)
		if !recognized {
			continue
		}
		w := Workspace{ID: item.ID, Name: strings.TrimPrefix(item.Name, "/"), Role: role, Image: item.Image, State: item.State, Status: statuses[item.ID], Labels: item.Labels, WorkspaceFolder: item.Labels["devcontainer.local_folder"], ProtectedVolumes: []string{}}
		protected := map[string]bool{}
		binds := map[string]bool{}
		for _, m := range item.Mounts {
			toolMount := strings.HasPrefix(m.Destination, "/opt/bdock/") || m.Destination == "/opt/bdock"
			if m.Type == "bind" && !toolMount && filepath.IsAbs(m.Source) && (m.Destination == "/workspace" || m.Destination == "/workspaces" || strings.HasPrefix(m.Destination, "/workspaces/")) {
				binds[m.Source] = true
			}
			if m.Type != "volume" || m.Name == "" {
				continue
			}
			if m.Destination == "/home/captain" || m.Destination == "/home/vscode" {
				if w.HomeVolume == nil || m.Destination == "/home/captain" {
					name := m.Name
					w.HomeVolume = &name
				}
			}
			if !m.RW || toolMount || strings.HasPrefix(m.Name, "bundledock-") {
				protected[m.Name] = true
			}
		}
		if w.WorkspaceFolder == "" && len(binds) == 1 {
			for source := range binds {
				w.WorkspaceFolder = source
			}
		}
		for name := range protected {
			w.ProtectedVolumes = append(w.ProtectedVolumes, name)
		}
		sort.Strings(w.ProtectedVolumes)
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (c Client) inspectAllAt(ctx context.Context, endpoint string) ([]inspection, map[string]string, error) {
	fail := func(message string) ([]inspection, map[string]string, error) {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, exitcode.Errorf(exitcode.PathConflict, "%s", message).WithRemediation("Check whr doctor and the local Docker CLI; inspection changed no container.")
	}
	raw, err := c.read(ctx, endpoint, "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return fail("local Docker container listing failed")
	}
	statuses := map[string]string{}
	var ids []string
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var item struct{ ID, Status string }
		err := decodeDockerRecord(dec, &item)
		if err == io.EOF {
			break
		}
		if err != nil || !fullID(item.ID) {
			return fail("invalid local Docker container list")
		}
		if _, ok := statuses[item.ID]; ok {
			return fail("duplicate local Docker container ID")
		}
		statuses[item.ID] = item.Status
		ids = append(ids, item.ID)
	}
	all := []inspection{}
	if len(ids) == 0 {
		return all, statuses, nil
	}
	sort.Strings(ids)
	args := append([]string{"container", "inspect", "--format", inspectFormat}, ids...)
	raw, err = c.read(ctx, endpoint, args...)
	if err != nil {
		return fail("local Docker container inspection failed")
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	seen := map[string]bool{}
	for {
		var item inspection
		err := decodeDockerRecord(dec, &item)
		if err == io.EOF {
			break
		}
		if err != nil || !fullID(item.ID) || item.Name == "" {
			return fail("invalid local Docker container metadata")
		}
		if _, ok := statuses[item.ID]; !ok || seen[item.ID] {
			return fail("unexpected local Docker container ID")
		}
		seen[item.ID] = true
		all = append(all, item)
	}
	if len(seen) != len(ids) {
		return fail("incomplete local Docker container inspection")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, statuses, nil
}

func workspaceRole(labels map[string]string) (string, bool) {
	if _, ok := labels["anchorage-staging"]; ok {
		return "staging", true
	}
	if _, ok := labels["anchorage-temp"]; ok {
		return "temp", true
	}
	// workharbor.workspace is already emitted by the host-agent Docker provider.
	for _, key := range []string{"devcontainer.local_folder", "devcontainer.config_file", "workharbor.workspace"} {
		if _, ok := labels[key]; ok {
			return "primary", true
		}
	}
	return "", false
}

// Human renders each untrusted value with the existing FR-022 terminal escaper.
func Human(workspaces []Workspace) string {
	var b strings.Builder
	fmt.Fprintln(&b, "ID\tNAME\tROLE\tIMAGE\tSTATE\tSTATUS\tWORKSPACE FOLDER\tHOME VOLUME\tPROTECTED VOLUMES\tLABELS")
	for _, w := range workspaces {
		labels, _ := json.Marshal(w.Labels)
		home := "-"
		if w.HomeVolume != nil {
			home = *w.HomeVolume
		}
		id := w.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fields := []string{id, w.Name, w.Role, w.Image, w.State, w.Status, w.WorkspaceFolder, home, strings.Join(w.ProtectedVolumes, ","), string(labels)}
		for i, s := range fields {
			fields[i] = terminal.Escape(s)
		}
		fmt.Fprintln(&b, strings.Join(fields, "\t"))
	}
	return b.String()
}
