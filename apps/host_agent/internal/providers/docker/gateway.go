// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"reflect"
	"strings"
	"sync"
)

// AdmittedImage is frozen read-only preflight evidence. It is compared with a
// fresh image/registry inspection; CLI output is never its authority.
type AdmittedImage struct {
	ID       string
	Config   ImageConfig
	Document map[string]any
}

// DockerPort accepts only the closed typed union. Candidates must return the
// union of deterministic-name and reserved runtime-label matches, including
// partial/foreign matches. Implementations must not return a first match.
// Image may query registry metadata, but may never pull as an inspection fallback.
type DockerPort interface {
	Candidates(context.Context, Scope) ([]Container, error)
	Information(context.Context) (EngineInfo, error)
	Image(context.Context, string) (AdmittedImage, error)
	Invoke(context.Context, Call) ([]byte, error)
}

// Gateway is an internal native boundary. Authorize must check the current
// operation, owner approval and held host fence; policy/IDs alone grant no effect.
// Durable phase receipts and takeover/recovery belong to the lifecycle caller.
type Gateway struct {
	mu         sync.Mutex
	Parser     config.JSONC
	Policy     DriverPolicy
	Port       DockerPort
	Authorize  func(context.Context) error
	Target     *Target
	Images     map[string]AdmittedImage
	CheckBuild func(context.Context, BuildCall) error
}

func supportedEngine(e EngineInfo) bool {
	if e.OSType != "linux" || (e.Architecture != "arm64" && e.Architecture != "amd64" && e.Architecture != "aarch64" && e.Architecture != "x86_64") || e.ServerVersion != "29.8.2" || e.SecurityOptions == nil {
		return false
	}
	for _, option := range e.SecurityOptions {
		switch strings.SplitN(option, ",", 2)[0] {
		case "name=seccomp", "name=apparmor", "name=selinux", "name=cgroupns":
		default:
			return false
		}
	}
	return true
}
func (g *Gateway) image(ctx context.Context, ref string) error {
	expected, ok := g.Images[ref]
	if !ok || !hexID.MatchString(strings.TrimPrefix(expected.ID, "sha256:")) {
		return fail("configuration_changed")
	}
	actual, e := g.Port.Image(ctx, ref)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(actual, expected) {
		return fail("configuration_changed")
	}
	return nil
}
func (g *Gateway) target(ctx context.Context, candidates []Container, id string) error {
	if g.Target == nil || g.Target.Scope != g.Policy.Scope || g.Target.DataPath != g.Policy.DataPath || id != g.Target.ContainerID {
		return fail("runtime_conflict")
	}
	c, e := VerifyTarget(*g.Target, candidates)
	if e != nil {
		return e
	}
	return validateActualRuntime(ctx, g.Parser, g.Policy, c)
}
func (g *Gateway) Forward(ctx context.Context, args []string) ([]byte, error) {
	c, e := g.prepareCall(ctx, args)
	if e != nil {
		return nil, e
	}
	return g.Port.Invoke(ctx, c)
}
func (g *Gateway) prepareCall(ctx context.Context, args []string) (Call, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Port == nil || g.Authorize == nil {
		return Call{}, fail("forbidden")
	}
	if e := g.Authorize(ctx); e != nil {
		return Call{}, e
	}
	// Only the native same-live-CID port can supply a first target. Labels/names
	// alone never grant adoption, and the full union is verified before Classify.
	if observer, ok := g.Port.(interface {
		ObserveAllocation(context.Context, []Container) (*Target, error)
	}); ok && len(args) > 0 && (args[0] == "ps" || args[0] == "inspect" && len(args) > 2 && args[2] == "container" || args[0] == "exec" || args[0] == "start" || args[0] == "events") {
		info, err := g.Port.Information(ctx)
		if err != nil {
			return Call{}, err
		}
		if !supportedEngine(info) {
			return Call{}, fail("unsupported_configuration")
		}
		all, err := g.Port.Candidates(ctx, g.Policy.Scope)
		if err != nil {
			return Call{}, err
		}
		target, err := observer.ObserveAllocation(ctx, all)
		if err != nil {
			return Call{}, err
		}
		if target != nil {
			if g.Target != nil && *g.Target != *target {
				return Call{}, fail("runtime_conflict")
			}
			g.Target = target
			if g.Policy.KnownIDs == nil {
				g.Policy.KnownIDs = map[string]bool{}
			}
			g.Policy.KnownIDs[target.ContainerID] = true
		}
	}
	if len(args) == 4 && exactArgs(args[:3], "inspect", "--type", "image") && g.Policy.Build != nil && args[3] == g.Policy.Build.Tag {
		if source, ok := g.Port.(interface {
			BuiltImage(context.Context, string) (AdmittedImage, bool, error)
		}); ok {
			image, found, err := source.BuiltImage(ctx, args[3])
			if err != nil {
				return Call{}, err
			}
			if found {
				if g.Images == nil {
					g.Images = map[string]AdmittedImage{}
				}
				if prior, exists := g.Images[args[3]]; exists && !reflect.DeepEqual(prior, image) {
					return Call{}, fail("configuration_changed")
				}
				g.Images[args[3]] = image
				if !g.Policy.OverrideCommand {
					g.Policy.StartupTail = imageCommand(image)
				}
				if g.Policy.ImageRefs == nil {
					g.Policy.ImageRefs = map[string]bool{}
				}
				g.Policy.ImageRefs[args[3]] = true
			}
		}
	}
	c, e := Classify(g.Policy, args)
	if e != nil {
		return Call{}, e
	}
	c.Scope = g.Policy.Scope
	// Version/image reads remain closed typed reads. Every container read validates
	// the whole candidate set, so official CLI first-match discovery cannot escape.
	switch c.Kind {
	case "version", "engine_version", "buildx_version":
		return c, nil
	case "image_inspect":
		if e = g.image(ctx, c.Reference); e != nil {
			return Call{}, e
		}
		return c, nil
	}
	info, e := g.Port.Information(ctx)
	if e != nil {
		return Call{}, e
	}
	if !supportedEngine(info) {
		return Call{}, fail("unsupported_configuration")
	}
	candidates, e := g.Port.Candidates(ctx, g.Policy.Scope)
	if e != nil {
		return Call{}, e
	}
	switch c.Kind {
	case "discovery":
		if len(candidates) != 0 {
			if g.Target == nil {
				return Call{}, fail("runtime_conflict")
			}
			if e = g.target(ctx, candidates, g.Target.ContainerID); e != nil {
				return Call{}, e
			}
		}
	case "container_inspect":
		if len(c.IDs) != 1 {
			return Call{}, fail("runtime_conflict")
		}
		if e = g.target(ctx, candidates, c.IDs[0]); e != nil {
			return Call{}, e
		}
	case "exec":
		if e = g.target(ctx, candidates, c.Exec.ContainerID); e != nil {
			return Call{}, e
		}
	case "start":
		if e = g.target(ctx, candidates, c.IDs[0]); e != nil {
			return Call{}, e
		}
	case "run":
		if len(candidates) != 0 || g.Target != nil {
			return Call{}, fail("runtime_conflict")
		}
		if e = g.image(ctx, c.Run.Image); e != nil {
			return Call{}, e
		}
		metadata, present := c.Run.Labels["devcontainer.metadata"]
		if !present {
			metadata = g.Images[c.Run.Image].Config.Metadata
		}
		expected, parseErr := ValidateImage(ctx, g.Parser, ImageConfig{Metadata: g.Policy.RuntimeMetadata})
		if parseErr != nil || len(expected) == 0 {
			return Call{}, fail("configuration_changed")
		}
		actual, parseErr := ValidateImage(ctx, g.Parser, ImageConfig{Metadata: metadata})
		if parseErr != nil || !reflect.DeepEqual(actual, expected) {
			return Call{}, fail("configuration_changed")
		}
		c.Run.ImageID = g.Images[c.Run.Image].ID
	case "pull":
		if len(candidates) != 0 || g.Target != nil {
			return Call{}, fail("runtime_conflict")
		}
		if e = g.image(ctx, c.Reference); e != nil {
			return Call{}, e
		}
	case "build":
		if len(candidates) != 0 || g.Target != nil {
			return Call{}, fail("runtime_conflict")
		}
		if g.CheckBuild == nil {
			return Call{}, fail("configuration_changed")
		}
		for ref := range g.Images {
			if e = g.image(ctx, ref); e != nil {
				return Call{}, e
			}
		}
		if len(g.Images) == 0 {
			return Call{}, fail("configuration_changed")
		}
		if e = g.CheckBuild(ctx, *c.Build); e != nil {
			return Call{}, e
		}
	case "events":
		if len(candidates) != 0 {
			if g.Target == nil {
				return Call{}, fail("runtime_conflict")
			}
			if e = g.target(ctx, candidates, g.Target.ContainerID); e != nil {
				return Call{}, e
			}
		}
	default:
		return Call{}, fail("provider_refused")
	}
	// Recheck immediately before forwarding. The concrete port additionally owns
	// the fixed daemon binding; it must not use ambient Docker contexts/options.
	if e = g.Authorize(ctx); e != nil {
		return Call{}, e
	}
	c.Scope = g.Policy.Scope
	return c, nil
}
