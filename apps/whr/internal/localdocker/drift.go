// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type BundleDrift struct {
	Bundle          string  `json:"bundle"`
	PinnedReference *string `json:"pinned_reference"`
	MountedIdentity *string `json:"mounted_identity"`
	Status          string  `json:"status"`
	Reason          string  `json:"reason"`
}
type BundleDriftResult struct {
	ContainerID *string       `json:"container_id"`
	Status      string        `json:"status"`
	Reason      string        `json:"reason"`
	Bundles     []BundleDrift `json:"bundles"`
}

var canonicalBundleVolume = regexp.MustCompile(`^bundledock-([a-z][a-z0-9-]{0,31})-([0-9a-f]{64})$`)
var driftBundleName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Drift reads the selected daemon and explicit local source within one shared
// budget. Only projected list/inspect vectors are admitted; no exec or mutations.
func (c Client) Drift(parent context.Context, ports doctor.Ports, source, selector string) (BundleDriftResult, error) {
	result := BundleDriftResult{Status: "cannot_determine", Reason: "runtime_unavailable", Bundles: []BundleDrift{}}
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	finish := func(err error) (BundleDriftResult, error) {
		if parent.Err() != nil {
			return result, parent.Err()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			if exitcode.FromError(err) == exitcode.Timeout || exitcode.FromError(err) == exitcode.Interrupted {
				return result, err
			}
		}
		if result.Status == "in_sync" {
			return result, nil
		}
		e := exitcode.Errorf(exitcode.PathConflict, "bundle references: %s", result.Status)
		e.Result = result
		return result, e
	}
	if selector == "" || source == "" {
		return result, exitcode.Errorf(exitcode.PathBadArguments, "drift --local requires --source <dir> and an exact container name or full ID")
	}
	endpoint, err := ResolveEndpoint(ctx, ports)
	if err != nil {
		return finish(err)
	}
	all, _, err := c.inspectAllAt(ctx, endpoint)
	if err != nil {
		return finish(err)
	}
	var selected *inspection
	for i := range all {
		if all[i].ID == selector || strings.TrimPrefix(all[i].Name, "/") == selector {
			if selected != nil {
				result.Reason = "selection_undetermined"
				return finish(nil)
			}
			selected = &all[i]
		}
	}
	if selected == nil {
		result.Reason = "selection_undetermined"
		return finish(nil)
	}
	result.ContainerID = &selected.ID
	role, ok := workspaceRole(selected.Labels)
	if !ok || role != "primary" || selected.State != "running" {
		result.Reason = "selection_undetermined"
		return finish(nil)
	}
	sourceAbs, sourceErr := filepath.Abs(source)
	local, err := filepath.EvalSymlinks(sourceAbs)
	declared, derr := filepath.EvalSymlinks(selected.Labels["devcontainer.local_folder"])
	if sourceErr != nil || err != nil || derr != nil || selected.Labels["devcontainer.local_folder"] == "" || local != declared {
		result.Reason = "source_mismatch"
		return finish(nil)
	}
	pins, err := workspacecfg.ReadBundlePins(ctx, source)
	if err != nil {
		result.Reason = "pins_unavailable"
		return finish(err)
	}
	result = compareBundlePins(selected.ID, pins, selected.Mounts)
	if err := ctx.Err(); err != nil {
		return finish(err)
	}
	return finish(nil)
}

// compareBundlePins compares declared references, not volume payloads. Canonical
// names encode a digest reference; Docker mount metadata cannot attest content.
func compareBundlePins(id string, pins []workspacecfg.BundlePin, mounts []mount) BundleDriftResult {
	result := BundleDriftResult{ContainerID: &id, Status: "in_sync", Reason: "reference_match", Bundles: []BundleDrift{}}
	byTarget := map[string][]mount{}
	extra := map[string]bool{}
	known := map[string]bool{}
	for _, p := range pins {
		known[p.Target] = true
	}
	for _, m := range mounts {
		byTarget[m.Destination] = append(byTarget[m.Destination], m)
		relevant := strings.HasPrefix(m.Destination, "/opt/bdock") || strings.HasPrefix(m.Name, "bundledock-")
		if relevant && !known[m.Destination] {
			name := strings.TrimPrefix(m.Destination, "/opt/bdock/")
			if !strings.HasPrefix(m.Destination, "/opt/bdock/") || !driftBundleName.MatchString(name) {
				name = "unknown"
			}
			extra[name] = true
		}
	}
	for _, p := range pins {
		ref := p.Reference
		row := BundleDrift{Bundle: p.Bundle, PinnedReference: &ref, Status: "cannot_determine", Reason: "mount_unavailable"}
		ms := byTarget[p.Target]
		if len(ms) == 1 {
			m := ms[0]
			identity := canonicalBundleVolume.FindStringSubmatch(m.Name)
			if m.Type == "volume" && !m.RW && !m.readOnlyUnknown && identity != nil && identity[1] == p.Bundle {
				v := m.Name
				row.MountedIdentity = &v
				row.Status = "drift"
				row.Reason = "reference_mismatch"
				if v == p.Volume {
					row.Status = "in_sync"
					row.Reason = "reference_match"
				}
			}
		}
		result.Bundles = append(result.Bundles, row)
	}
	for name := range extra {
		result.Bundles = append(result.Bundles, BundleDrift{Bundle: name, Status: "cannot_determine", Reason: "undeclared_bundle"})
	}
	if mounts == nil {
		result.Status = "cannot_determine"
		result.Reason = "mount_unavailable"
	}
	for _, row := range result.Bundles {
		if row.Status == "cannot_determine" {
			result.Status = "cannot_determine"
			result.Reason = "incomplete_evidence"
		} else if row.Status == "drift" && result.Status == "in_sync" {
			result.Status = "drift"
			result.Reason = "reference_mismatch"
		}
	}
	sort.Slice(result.Bundles, func(i, j int) bool {
		a, b := result.Bundles[i], result.Bundles[j]
		if a.Bundle == b.Bundle {
			return a.Reason < b.Reason
		}
		return a.Bundle < b.Bundle
	})
	return result
}

func (r BundleDriftResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bundle reference status: %s (%s); volume contents are not attested.\n", terminal.Escape(r.Status), terminal.Escape(r.Reason))
	fmt.Fprintln(&b, "BUNDLE\tPINNED REFERENCE\tMOUNTED VOLUME IDENTITY\tSTATUS\tREASON")
	for _, row := range r.Bundles {
		pin, mounted := "-", "-"
		if row.PinnedReference != nil {
			pin = *row.PinnedReference
		}
		if row.MountedIdentity != nil {
			mounted = *row.MountedIdentity
		}
		fields := []string{row.Bundle, pin, mounted, row.Status, row.Reason}
		for i := range fields {
			fields[i] = terminal.Escape(fields[i])
		}
		fmt.Fprintln(&b, strings.Join(fields, "\t"))
	}
	return b.String()
}
