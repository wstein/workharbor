// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"io"
	"reflect"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
)

func verifyMetadata(ctx context.Context, parser config.JSONC, expected, actual string) error {
	want, e := ValidateImage(ctx, parser, ImageConfig{Metadata: expected})
	if e != nil || len(want) == 0 {
		return fail("configuration_changed")
	}
	got, e := ValidateImage(ctx, parser, ImageConfig{Metadata: actual})
	if e != nil || !reflect.DeepEqual(want, got) {
		return fail("configuration_changed")
	}
	return nil
}
func validateActualRuntime(ctx context.Context, parser config.JSONC, policy DriverPolicy, c Container) error {
	if c.User != policy.ContainerUser || c.UsernsMode != "" {
		return fail("runtime_conflict")
	}
	return verifyMetadata(ctx, parser, policy.RuntimeMetadata, c.Labels["devcontainer.metadata"])
}
func (p *nativePort) classified(c Call) error {
	p.stateMu.Lock()
	policy := copyPolicy(p.policy)
	p.stateMu.Unlock()
	if c.Scope != p.scope {
		return fail("provider_refused")
	}
	var args []string
	switch c.Kind {
	case "run":
		if c.Run == nil {
			return fail("provider_refused")
		}
		r := c.Run
		args = []string{"run", "--sig-proxy=false", "-a", "STDOUT", "-a", "STDERR", "--mount", r.Mount}
		args = appendPairs(args, "-l", r.Labels)
		args = appendPairs(args, "-e", r.Env)
		args = append(args, "-u", r.User, "--name", r.Name, "--entrypoint", "/bin/sh", r.Image)
		args = append(args, r.Command...)
	case "discovery":
		args = []string{"ps", "-q", "-a"}
		for _, label := range p.scope.LabelList() {
			args = append(args, "--filter", "label="+label)
		}
	case "events":
		args = []string{"events", "--format", "{{json .}}", "--filter", "event=start"}
	default:
		var e error
		args, e = nativeArguments(c, "")
		if e != nil {
			return e
		}
	}
	expected, e := Classify(policy, args)
	if e != nil {
		return e
	}
	expected.Scope = p.scope
	if c.Kind == "run" {
		expected.Run.ImageID = c.Run.ImageID
	}
	if !reflect.DeepEqual(expected, c) {
		return fail("provider_refused")
	}
	return nil
}

// ObserveAllocation never adopts from a label/name match. The initial target
// comes only from the same live native run's CID plus complete fresh inspection.
func (p *nativePort) ObserveAllocation(ctx context.Context, candidates []Container) (*Target, error) {
	if e := p.authorize(ctx); e != nil {
		return nil, e
	}
	p.stateMu.Lock()
	a, target, policy := p.allocation, p.target, copyPolicy(p.policy)
	p.stateMu.Unlock()
	if target != nil {
		actual, e := VerifyTarget(*target, candidates)
		if e != nil {
			return nil, e
		}
		if e = validateActualRuntime(ctx, p.parser, policy, actual); e != nil {
			return nil, e
		}
		copy := *target
		return &copy, nil
	}
	if a == nil || len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) != 1 {
		return nil, fail("runtime_conflict")
	}
	if e := validateActualRuntime(ctx, p.parser, policy, candidates[0]); e != nil {
		return nil, e
	}
	observed, e := a.Observe(ctx, candidates)
	if e != nil {
		return nil, e
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.target != nil && *p.target != observed {
		return nil, fail("runtime_conflict")
	}
	if p.target == nil {
		p.target = &observed
	}
	if p.policy.KnownIDs == nil {
		p.policy.KnownIDs = map[string]bool{}
	}
	p.policy.KnownIDs[observed.ContainerID] = true
	copy := observed
	return &copy, nil
}
func (p *nativePort) freshState(ctx context.Context) ([]Container, error) {
	info, e := p.Information(ctx)
	if e != nil {
		return nil, e
	}
	if !supportedEngine(info) {
		return nil, fail("unsupported_configuration")
	}
	return p.Candidates(ctx, p.scope)
}
func (p *nativePort) InvokeStream(ctx context.Context, c Call, in io.Reader, out io.Writer) error {
	if e := p.authorize(ctx); e != nil {
		return e
	}
	if e := p.classified(c); e != nil {
		return e
	}
	if p.stream == nil {
		return fail("provider_unavailable")
	}
	candidates, e := p.freshState(ctx)
	if e != nil {
		return e
	}
	var cid string
	switch c.Kind {
	case "run":
		if len(candidates) != 0 {
			return fail("runtime_conflict")
		}
		p.stateMu.Lock()
		expected, ok := p.images[c.Run.Image]
		existing := p.allocation != nil || p.target != nil
		p.stateMu.Unlock()
		if !ok || expected.ID != c.Run.ImageID {
			return fail("configuration_changed")
		}
		if existing {
			return fail("runtime_unknown")
		}
		if _, e = p.requireLocal(ctx, c.Run.Image, expected); e != nil {
			return e
		}
		metadata, present := c.Run.Labels["devcontainer.metadata"]
		if !present {
			metadata = expected.Config.Metadata
		}
		if e = verifyMetadata(ctx, p.parser, p.policy.RuntimeMetadata, metadata); e != nil {
			return e
		}
		if p.lifetime == nil {
			return fail("provider_unavailable")
		}
		p.stateMu.Lock()
		if p.allocation != nil || p.target != nil {
			p.stateMu.Unlock()
			return fail("runtime_unknown")
		}
		a, err := reserveAllocation(ctx, p.lifetime.root, p.scope, p.policy.DataPath, expected.ID, p.check)
		if err == nil {
			err = a.Begin(ctx)
		}
		if err != nil {
			if a != nil {
				a.Close()
			}
			p.stateMu.Unlock()
			return err
		}
		p.allocation = a
		cid = a.cid
		p.stateMu.Unlock()
		defer a.End()
	case "exec", "start", "container_inspect":
		target, err := p.ObserveAllocation(ctx, candidates)
		if err != nil {
			return err
		}
		if target == nil {
			return fail("runtime_unknown")
		}
		id := ""
		if c.Exec != nil {
			id = c.Exec.ContainerID
		} else if len(c.IDs) == 1 {
			id = c.IDs[0]
		}
		if id != target.ContainerID {
			return fail("runtime_conflict")
		}
	case "events":
		if len(candidates) != 0 {
			if target, err := p.ObserveAllocation(ctx, candidates); err != nil || target == nil {
				if err != nil {
					return err
				}
				return fail("runtime_unknown")
			}
		}
	case "pull":
		if len(candidates) != 0 {
			return fail("runtime_conflict")
		}
		if _, e = p.Image(ctx, c.Reference); e != nil {
			return e
		}
	case "build":
		if len(candidates) != 0 {
			return fail("runtime_conflict")
		}
		return p.build(ctx, c, in, out)
	default:
		raw, err := p.Invoke(ctx, c)
		if err != nil {
			return err
		}
		_, err = out.Write(raw)
		return err
	}
	args, e := nativeArguments(c, cid)
	if e != nil {
		return e
	}
	if e = p.authorize(ctx); e != nil {
		return e
	}
	if e = p.stream(ctx, args, in, out); e != nil {
		return e
	}
	if c.Kind == "pull" {
		p.stateMu.Lock()
		expected := p.images[c.Reference]
		p.stateMu.Unlock()
		_, e = p.requireLocal(ctx, c.Reference, expected)
		return e
	}
	return nil
}
func (p *nativePort) Close() error {
	p.stateMu.Lock()
	a := p.allocation
	p.stateMu.Unlock()
	if a != nil {
		a.End()
	}
	if p.lifetime != nil {
		return p.lifetime.Close()
	}
	return nil
}
