// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
)

// phaseResult validates only a fully waited normal CLI exit's stdout. This
// function alone grants no receipt: final native target inspection is required.
func phaseResult(raw []byte, phase Phase, containerID string) (map[string]any, error) {
	value, e := wirejson.Parse(raw)
	if e != nil {
		return nil, fail("phase_unknown")
	}
	m, ok := value.(map[string]any)
	if !ok || m["outcome"] != "success" {
		return nil, fail("phase_unknown")
	}
	if phase == Hooks {
		if !exactKeys(m, []string{"outcome", "result"}) || m["result"] != "done" {
			return nil, fail("phase_unknown")
		}
		return m, nil
	}
	if phase != Resources || !exactKeys(m, []string{"outcome", "containerId", "remoteUser", "remoteWorkspaceFolder", "configuration", "mergedConfiguration"}, "ociAuthDiagnostics") || m["containerId"] != containerID || !hexID.MatchString(containerID) || m["remoteWorkspaceFolder"] != "/workspace" {
		return nil, fail("phase_unknown")
	}
	if user, ok := m["remoteUser"].(string); !ok || user == "" {
		return nil, fail("phase_unknown")
	}
	for _, key := range []string{"configuration", "mergedConfiguration"} {
		if _, ok := m[key].(map[string]any); !ok {
			return nil, fail("phase_unknown")
		}
	}
	return m, nil
}
func inspectPhaseTarget(ctx context.Context, p *nativePort) (Target, error) {
	all, e := p.freshState(ctx)
	if e != nil {
		return Target{}, e
	}
	target, e := p.ObserveAllocation(ctx, all)
	if e != nil {
		return Target{}, e
	}
	if target == nil {
		return Target{}, fail("phase_unknown")
	}
	actual, e := VerifyTarget(*target, all)
	if e != nil {
		return Target{}, e
	}
	if !actual.Running {
		return Target{}, fail("phase_unknown")
	}
	if e = p.authorize(ctx); e != nil {
		return Target{}, e
	}
	return *target, nil
}
