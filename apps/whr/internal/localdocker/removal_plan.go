// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"sort"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// RemovalOptions selects one explicit target or one scoped group. Consent and
// execution are separate from this pure, side-effect-free safety calculation.
type RemovalOptions struct {
	Target, Role, Label string
	Volumes, Force      bool
}

type removalVolume struct {
	Name   string
	Mounts []mount
}
type removalPlan struct {
	Containers         []inspection
	Volumes            []removalVolume
	PreserveHomeVolume bool
}

func planRemoval(all []inspection, o RemovalOptions) (removalPlan, error) {
	fail := func(path exitcode.Path, message string) (removalPlan, error) {
		return removalPlan{}, exitcode.Errorf(path, "%s", message)
	}
	if err := validateRemovalOptions(o); err != nil {
		return removalPlan{}, err
	}
	key, value, hasValue := strings.Cut(o.Label, "=")
	selected := []inspection{}
	for _, c := range all {
		role, recognized := workspaceRole(c.Labels)
		match := o.Target != "" && (c.ID == o.Target || strings.TrimPrefix(c.Name, "/") == o.Target)
		if o.Role != "" {
			match = recognized && role == o.Role
		}
		if o.Label != "" {
			v, exists := c.Labels[key]
			match = exists && (!hasValue || v == value)
		}
		if !match {
			continue
		}
		if !fullID(c.ID) || !recognized {
			return fail(exitcode.PathConflict, "refusing container without verified workspace ownership")
		}
		selected = append(selected, c)
	}
	if o.Target != "" && len(selected) != 1 {
		return fail(exitcode.PathConflict, "local workspace target is missing or ambiguous")
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	protected := map[string]bool{}
	primary := map[string]bool{}
	for _, c := range all {
		role, recognized := workspaceRole(c.Labels)
		for _, m := range c.Mounts {
			if m.Type != "volume" || m.Name == "" {
				continue
			}
			if !m.RW || strings.HasPrefix(m.Name, "bundledock-") || m.Destination == "/opt/bdock" || strings.HasPrefix(m.Destination, "/opt/bdock/") {
				protected[m.Name] = true
			}
			if recognized && role == "primary" && homeMount(m) {
				primary[m.Name] = true
			}
		}
	}
	plan := removalPlan{Containers: selected, Volumes: []removalVolume{}, PreserveHomeVolume: !o.Volumes}
	volumes := map[string]bool{}
	for _, c := range selected {
		role, _ := workspaceRole(c.Labels)
		for _, m := range c.Mounts {
			if !homeMount(m) {
				continue
			}
			if role != "primary" && (strings.HasPrefix(m.Name, "anchorage-home-") || primary[m.Name]) {
				return fail(exitcode.PathConflict, "staging or temp cleanup would touch a primary home volume")
			}
			if !o.Volumes {
				continue
			}
			if protected[m.Name] {
				return fail(exitcode.PathConflict, "home volume is protected")
			}
			mounts := []mount{}
			for _, other := range all {
				for _, om := range other.Mounts {
					if om.Type == "volume" && om.Name == m.Name {
						if other.ID != c.ID {
							return fail(exitcode.PathConflict, "home volume is mounted by another container")
						}
						mounts = append(mounts, om)
					}
				}
			}
			if !volumes[m.Name] {
				plan.Volumes = append(plan.Volumes, removalVolume{Name: m.Name, Mounts: mounts})
				volumes[m.Name] = true
			}
		}
	}
	sort.Slice(plan.Volumes, func(i, j int) bool { return plan.Volumes[i].Name < plan.Volumes[j].Name })
	return plan, nil
}

func homeMount(m mount) bool {
	return m.Type == "volume" && m.Name != "" && (m.Destination == "/home/captain" || m.Destination == "/home/vscode")
}

func validateRemovalOptions(o RemovalOptions) error {
	selectors := 0
	for _, v := range []string{o.Target, o.Role, o.Label} {
		if v != "" {
			selectors++
		}
	}
	if selectors != 1 || (o.Role != "" && o.Role != "staging" && o.Role != "temp") {
		return exitcode.Errorf(exitcode.PathBadArguments, "rm --local requires one container, --role staging|temp, or --label selector")
	}
	key, _, _ := strings.Cut(o.Label, "=")
	if o.Label != "" && key == "" {
		return exitcode.Errorf(exitcode.PathBadArguments, "--label requires a key or key=value")
	}
	if o.Volumes && !o.Force {
		return exitcode.Errorf(exitcode.PathHumanDecisionMissing, "home volume deletion requires --volumes and --force")
	}

	return nil
}
