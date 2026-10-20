// SPDX-License-Identifier: EUPL-1.2

// Package plan builds and seals typed host setup steps; it never applies them.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
)

// ConfigObservation is an input seam for a caller's read-only dotfile preview.
// State is absent, identical, differs or unsafe_target. No FS engine is imported.
type ConfigObservation struct {
	State  string
	SHA256 string
}
type Input struct {
	Platform  string
	Catalogue *catalogue.Catalogue
	Results   []detect.Result
	Config    map[string]ConfigObservation
	Selected  map[string]bool
	OptedOut  map[string]bool
}
type Step struct {
	ID           string
	Item         string
	Kind         string
	Class        string
	DependsOn    []string
	Target       string
	BeforeSHA256 string
	Blocked      string
}
type Plan struct {
	Hash         string
	Steps        []Step
	sourceDigest string
}

func Build(input Input) (Plan, error) {
	invalid := func() (Plan, error) { return Plan{}, errors.New("host setup plan input is invalid") }
	if input.Catalogue == nil || input.Catalogue.SchemaVersion != 1 || input.Platform == "" {
		return invalid()
	}
	ids := map[string]int{}
	for i, item := range input.Catalogue.Items {
		if _, exists := ids[item.ID]; exists || item.ID == "" {
			return invalid()
		}
		ids[item.ID] = i
	}
	results := map[string]detect.Result{}
	for _, result := range input.Results {
		if _, exists := ids[result.Item]; !exists {
			return invalid()
		}
		if _, exists := results[result.Item]; exists {
			return invalid()
		}
		switch result.State {
		case "absent", "present_ok", "present_outdated", "present_other_source", "present_unverifiable", "unknown":
		default:
			return invalid()
		}
		results[result.Item] = result
	}
	for id := range ids {
		if _, exists := results[id]; !exists {
			return invalid()
		}
	}
	for id := range input.Selected {
		if _, exists := ids[id]; !exists && id != "git-config" {
			return invalid()
		}
	}
	for id := range input.OptedOut {
		if _, exists := ids[id]; !exists && id != "git-config" {
			return invalid()
		}
	}
	selected := map[string]bool{}
	for _, item := range input.Catalogue.Items {
		selected[item.ID] = item.SelectedByDefault
		if value, exists := input.Selected[item.ID]; exists {
			selected[item.ID] = value
		}
		if input.OptedOut[item.ID] {
			selected[item.ID] = false
		}
	}
	// A reserved profile item is selection metadata, never a fourteenth tool or a
	// git-move implementation. T005/T006 supply its typed steps later.
	selected["git-config"] = input.Selected["git-config"] && !input.OptedOut["git-config"]
	order := []int{}
	state := make([]uint8, len(input.Catalogue.Items))
	var visit func(int) bool
	visit = func(i int) bool {
		if state[i] == 1 {
			return false
		}
		if state[i] == 2 {
			return true
		}
		state[i] = 1
		for _, id := range input.Catalogue.Items[i].Requires {
			j, exists := ids[id]
			if !exists || !visit(j) {
				return false
			}
		}
		state[i] = 2
		order = append(order, i)
		return true
	}
	for i := range input.Catalogue.Items {
		if !visit(i) {
			return invalid()
		}
	}
	plan := Plan{Steps: []Step{}}
	last := map[string]string{}
	for _, i := range order {
		item := input.Catalogue.Items[i]
		if !selected[item.ID] {
			continue
		}
		result := results[item.ID]
		dependencies := []string{}
		blocked := ""
		for _, required := range item.Requires {
			if step, exists := last[required]; exists {
				dependencies = append(dependencies, step)
			} else {
				observed := results[required]
				if observed.State != "present_ok" {
					blocked = "dependency_unavailable"
				}
			}
		}
		add := func(kind, class string, config *catalogue.Config) {
			step := Step{ID: item.ID + "." + kind, Item: item.ID, Kind: kind, Class: class, DependsOn: append([]string(nil), dependencies...), Blocked: blocked}
			if own, exists := last[item.ID]; exists {
				step.DependsOn = append(step.DependsOn, own)
			}
			if config != nil {
				step.Target = config.Target
				step.BeforeSHA256 = input.Config[item.ID].SHA256
			}
			plan.Steps = append(plan.Steps, step)
			last[item.ID] = step.ID
		}
		switch result.State {
		case "absent":
			add("brew_install", "create", nil)
		case "present_outdated":
			if !result.Managed {
				return invalid()
			}
			add("brew_upgrade", "modify", nil)
		}
		if item.Config != nil {
			observation, exists := input.Config[item.ID]
			if !exists {
				return invalid()
			}
			if result.State == "unknown" || result.State == "present_unverifiable" {
				continue
			}
			switch observation.State {
			case "absent":
				add("dotfile_create", "create", item.Config)
			case "differs":
				add("dotfile_replace", "modify", item.Config)
			case "unsafe_target":
				add("dotfile_replace", "modify", item.Config)
				plan.Steps[len(plan.Steps)-1].Blocked = "target_unsafe"
			case "identical":
			default:
				return invalid()
			}
		}
	}
	// Normalize observed order and effective selection before hashing. Equivalent
	// map insertion/result order and explicit versus default selection hash equally.
	ordered := make([]detect.Result, 0, len(input.Catalogue.Items))
	for _, item := range input.Catalogue.Items {
		ordered = append(ordered, results[item.ID])
	}
	configs := map[string]ConfigObservation{}
	for id, observation := range input.Config {
		if _, exists := ids[id]; !exists {
			return invalid()
		}
		configs[id] = observation
	}
	snapshot := struct {
		Platform  string
		Catalogue *catalogue.Catalogue
		Results   []detect.Result
		Config    map[string]ConfigObservation
		Selected  map[string]bool
	}{input.Platform, input.Catalogue, ordered, configs, selected}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return invalid()
	}
	digest := sha256.Sum256(raw)
	plan.sourceDigest = hex.EncodeToString(digest[:])
	plan.Hash = planHash(plan)
	return plan, nil
}
func planHash(plan Plan) string {
	raw, err := json.Marshal(struct {
		Source string
		Steps  []Step
	}{plan.sourceDigest, plan.Steps})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Recheck validates the shown hash against the current sealed steps. A caller
// rebuilds from fresh observations before applying; stale state or step mutation
// therefore cannot inherit an earlier confirmation.
func Recheck(plan Plan, expected string) error {
	if plan.sourceDigest == "" || plan.Hash != expected || planHash(plan) != expected {
		return errors.New("host setup plan changed")
	}
	return nil
}
func ValidKind(kind string) bool {
	switch kind {
	case "detect", "brew_install", "brew_upgrade", "dotfile_create", "dotfile_replace", "git_config_move", "socket_verify", "privileged_exec", "profile_write":
		return true
	default:
		return false
	}
}
