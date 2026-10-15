// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"io"
	"sort"
	"strings"
	"sync"
)

type nativePort struct {
	stateMu      sync.Mutex
	buildStarted bool
	policy       DriverPolicy
	images       map[string]AdmittedImage
	registry     *Registry
	parser       config.JSONC
	target       *Target
	allocation   *allocation
	checkBuild   func(context.Context, BuildCall) error
	lifetime     *nativeLifetime
	stream       func(context.Context, []string, io.Reader, io.Writer) error
	scope        Scope
	check        func(context.Context) error
	query        func(context.Context, []string) ([]byte, error)
}

func (p *nativePort) authorize(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if p.check == nil || p.query == nil || !p.scope.valid() {
		return fail("forbidden")
	}
	return p.check(ctx)
}
func (p *nativePort) read(ctx context.Context, args []string) ([]byte, error) {
	if e := p.authorize(ctx); e != nil {
		return nil, e
	}
	return p.query(ctx, args)
}
func candidateIDs(raw []byte) ([]string, error) {
	text := strings.TrimSuffix(string(raw), "\n")
	if text == "" {
		return []string{}, nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, id := range strings.Split(text, "\n") {
		if !hexID.MatchString(id) || seen[id] {
			return nil, fail("runtime_unknown")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// Candidates independently queries the union, not complete-label intersections:
// partial and foreign deterministic-name/runtime-label collisions must be visible.
func (p *nativePort) Candidates(ctx context.Context, scope Scope) ([]Container, error) {
	if e := p.authorize(ctx); e != nil {
		return nil, e
	}
	if scope != p.scope {
		return nil, fail("runtime_conflict")
	}
	union := map[string]bool{}
	for _, filter := range []string{"name=^/" + scope.Name() + "$", "label=workharbor.runtime=" + scope.RuntimeID} {
		raw, e := p.read(ctx, []string{"ps", "-aq", "--no-trunc", "--filter", filter})
		if e != nil {
			return nil, e
		}
		ids, e := candidateIDs(raw)
		if e != nil {
			return nil, e
		}
		for _, id := range ids {
			union[id] = true
		}
	}
	ids := make([]string, 0, len(union))
	for id := range union {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return []Container{}, nil
	}
	raw, e := p.read(ctx, append([]string{"inspect", "--type", "container"}, ids...))
	if e != nil {
		return nil, e
	}
	all, e := nativeContainers(raw)
	if e != nil || len(all) != len(ids) {
		return nil, fail("runtime_unknown")
	}
	seen := map[string]bool{}
	for _, c := range all {
		if !union[c.ID] || seen[c.ID] {
			return nil, fail("runtime_unknown")
		}
		seen[c.ID] = true
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}
func (p *nativePort) Information(ctx context.Context) (EngineInfo, error) {
	raw, e := p.read(ctx, []string{"info", "--format", "{{json .}}"})
	if e != nil {
		return EngineInfo{}, e
	}
	return nativeInformation(raw)
}

func (p *nativePort) Invoke(ctx context.Context, c Call) ([]byte, error) {
	if e := p.authorize(ctx); e != nil {
		return nil, e
	}
	if c.Scope != p.scope {
		return nil, fail("provider_refused")
	}
	switch c.Kind {
	case "version", "engine_version", "buildx_version":
		args, e := nativeArguments(c, "")
		if e != nil {
			return nil, e
		}
		return p.read(ctx, args)
	case "image_inspect":
		if e := p.classified(c); e != nil {
			return nil, e
		}
		expected, e := p.Image(ctx, c.Reference)
		if e != nil {
			return nil, e
		}
		return p.requireLocal(ctx, c.Reference, expected)
	case "discovery", "container_inspect":
		if e := p.classified(c); e != nil {
			return nil, e
		}
		all, e := p.freshState(ctx)
		if e != nil {
			return nil, e
		}
		target, e := p.ObserveAllocation(ctx, all)
		if e != nil {
			return nil, e
		}
		if c.Kind == "discovery" {
			if target == nil {
				if len(all) != 0 {
					return nil, fail("runtime_conflict")
				}
				return []byte{}, nil
			}
			return []byte(target.ContainerID + "\n"), nil
		}
		if target == nil || len(c.IDs) != 1 || c.IDs[0] != target.ContainerID {
			return nil, fail("runtime_conflict")
		}
		raw, e := p.read(ctx, []string{"inspect", "--type", "container", target.ContainerID})
		if e != nil {
			return nil, e
		}
		fresh, e := nativeContainers(raw)
		if e != nil {
			return nil, e
		}
		actual, e := VerifyTarget(*target, fresh)
		if e != nil {
			return nil, e
		}
		p.stateMu.Lock()
		policy := copyPolicy(p.policy)
		p.stateMu.Unlock()
		if e = validateActualRuntime(ctx, p.parser, policy, actual); e != nil {
			return nil, e
		}
		return raw, nil
	default:
		return nil, fail("provider_unavailable")
	}
}
