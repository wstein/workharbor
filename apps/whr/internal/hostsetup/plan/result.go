//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"errors"
	"strings"
)

// Result is an internal projection, not a public JSON contract. AP-6 remains open.
// Raw audit argv, paths, backup data and error text are deliberately excluded.
type Result struct {
	SchemaVersion      int `json:"schema_version"`
	ExitCode           int
	Steps              []ResultStep
	Applied, Remaining []string
}
type ResultStep struct{ ID, Item, Kind, Status, Reason string }

// ProjectResult takes display identities from the sealed plan, never receipts.
// The engine alone determines ExitCode; journal success and a mutation receipt
// are not interchangeable. Unknown receipt identities and reasons are omitted.
func ProjectResult(input Input, p Plan, r ApplyResult) (Result, error) {
	code, err := ResultExit(r)
	if err != nil {
		return Result{}, err
	}
	if err = Recheck(p, p.Hash); err != nil {
		return Result{}, err
	}
	rebuilt, err := Build(input)
	if err != nil || rebuilt.Hash != p.Hash {
		return Result{}, errors.New("result plan input changed")
	}
	out := Result{SchemaVersion: 1, ExitCode: int(code), Steps: []ResultStep{}, Applied: []string{}, Remaining: []string{}}
	known := map[string]Step{}
	listIDs := map[string]bool{}
	for _, item := range input.Catalogue.Items {
		listIDs[item.ID+".profile_write"] = true
	}
	listIDs["git-config.profile_write"] = true
	for _, s := range p.Steps {
		known[s.ID] = s
		listIDs[s.ID] = true
	}
	for _, receipt := range r.Steps {
		s, ok := known[receipt.Audit.ID]
		if !ok {
			continue
		}
		status := receipt.Audit.Result
		switch status {
		case "applied", "unchanged", "blocked", "failed", "rolled_back", "skipped", "declined", "opted_out":
		default:
			return Result{}, errors.New("invalid host setup engine step status")
		}
		reason := receipt.Reason
		if !strings.HasPrefix(reason, "host_setup.") {
			reason = "host_setup." + reason
		}
		if _, ok := RefusalCodes()[reason]; !ok {
			switch receipt.Reason {
			case "interrupted", "step_failed", "confirmation_required", "dependency_unavailable":
				reason = receipt.Reason
			default:
				reason = ""
			}
		}
		out.Steps = append(out.Steps, ResultStep{ID: s.ID, Item: s.Item, Kind: s.Kind, Status: status, Reason: reason})
	}
	for _, id := range r.Applied {
		if listIDs[id] {
			out.Applied = append(out.Applied, id)
		}
	}
	for _, id := range r.Remaining {
		if listIDs[id] {
			out.Remaining = append(out.Remaining, id)
		}
	}
	return out, nil
}
