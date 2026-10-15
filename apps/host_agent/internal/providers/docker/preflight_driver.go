// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"sort"
	"strings"
)

// PreflightDriver serves independently validated frozen read snapshots only. Its
// caller must prove candidate absence and installed tools before construction;
// none of these native expected values grants resource mutation authority.
// In particular there is no subprocess or pass-through fallback in this phase.
type PreflightDriver struct {
	Scope                        Scope
	EngineVersion, BuildxVersion string
	CandidateAbsent              bool
	Images                       map[string]map[string]any
}

func (s Scope) LabelList() []string {
	m := s.Labels()
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+m[key])
	}
	return out
}
func exactArgs(args []string, want ...string) bool {
	if len(args) != len(want) {
		return false
	}
	for i := range args {
		if args[i] != want[i] {
			return false
		}
	}
	return true
}
func (d PreflightDriver) Reply(args []string) ([]byte, error) {
	if !d.Scope.valid() || !d.CandidateAbsent || d.EngineVersion != "29.8.2" || d.BuildxVersion != "v0.38.0" {
		return nil, fail("provider_unavailable")
	}
	switch {
	case exactArgs(args, "-v"):
		return []byte("Docker version " + d.EngineVersion + "\n"), nil
	case exactArgs(args, "version", "--format", "{{.Server.Version}}"):
		return []byte(d.EngineVersion + "\n"), nil
	case exactArgs(args, "buildx", "version"):
		return []byte(d.BuildxVersion + "\n"), nil
	case len(args) == 4 && exactArgs(args[:3], "inspect", "--type", "image"):
		image, ok := d.Images[args[3]]
		if !ok {
			return nil, fail("metadata_unavailable")
		}
		raw, e := json.Marshal([]any{image})
		if e != nil {
			return nil, fail("invalid_configuration")
		}
		return append(raw, '\n'), nil
	case len(args) >= 3 && exactArgs(args[:3], "ps", "-q", "-a"):
		labels := map[string]bool{}
		expected := d.Scope.LabelList()
		if len(args) != 3+2*len(expected) {
			return nil, fail("provider_refused")
		}
		for i := 3; i < len(args); i += 2 {
			if args[i] != "--filter" || !strings.HasPrefix(args[i+1], "label=") {
				return nil, fail("provider_refused")
			}
			label := strings.TrimPrefix(args[i+1], "label=")
			if labels[label] {
				return nil, fail("provider_refused")
			}
			labels[label] = true
		}
		for _, label := range expected {
			if !labels[label] {
				return nil, fail("provider_refused")
			}
		}
		return []byte{}, nil
	default:
		return nil, fail("provider_refused")
	}
}
