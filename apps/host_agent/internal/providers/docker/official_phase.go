// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
)

// PhaseEvidence requires a full normal official CLI process exit and fresh exact
// target inspection. T010 must persist its fenced durable receipt before ack;
// stdout, CID, IID, hook markers and this in-memory value are not durable receipts.
type PhaseEvidence struct {
	Phase      Phase
	Target     Target
	Descriptor config.Descriptor
}

func (p *Prepared) Resources(ctx context.Context) (PhaseEvidence, error) {
	if e := p.Check(ctx); e != nil {
		return PhaseEvidence{}, e
	}
	p.mu.Lock()
	if p.resourcesStarted {
		p.mu.Unlock()
		return PhaseEvidence{}, fail("phase_unknown")
	}
	p.resourcesStarted = true
	p.mu.Unlock()
	// Explicit digest pulls happen only after Prepare completed ALL capability,
	// trust, user/daemon and initial-data gates. No inspection silently pulls.
	p.port.stateMu.Lock()
	refs := []string{}
	for ref := range p.port.images {
		refs = append(refs, ref)
	}
	p.port.stateMu.Unlock()
	for _, ref := range refs {
		if e := p.port.InvokeStream(ctx, Call{Scope: p.admission.Scope(), Kind: "pull", Reference: ref}, nil, discardOutput{}); e != nil {
			return PhaseEvidence{}, e
		}
	}
	return p.runPhase(ctx, Resources, "")
}
func (p *Prepared) Hooks(ctx context.Context) (PhaseEvidence, error) {
	if e := p.Check(ctx); e != nil {
		return PhaseEvidence{}, e
	}
	p.mu.Lock()
	if p.resources == nil || p.hooksStarted {
		p.mu.Unlock()
		return PhaseEvidence{}, fail("phase_unknown")
	}
	target := *p.resources
	p.hooksStarted = true
	p.mu.Unlock()
	return p.runPhase(ctx, Hooks, target.ContainerID)
}

type discardOutput struct{}

func (discardOutput) Write(p []byte) (int, error) { return len(p), nil }
func (p *Prepared) runPhase(ctx context.Context, phase Phase, id string) (PhaseEvidence, error) {
	if p.port == nil || p.port.lifetime == nil {
		return PhaseEvidence{}, fail("provider_unavailable")
	}
	phaseCtx, done, e := p.port.lifetime.enter(ctx)
	if e != nil {
		return PhaseEvidence{}, e
	}
	defer done()
	ctx = phaseCtx

	if e := p.Check(ctx); e != nil {
		return PhaseEvidence{}, e
	}
	p.port.stateMu.Lock()
	policy := copyPolicy(p.port.policy)
	policy.Phase = phase
	p.port.policy = copyPolicy(policy)
	images := copyImages(p.port.images)
	var target *Target
	if p.port.target != nil {
		copy := *p.port.target
		target = &copy
	}
	p.port.stateMu.Unlock()
	if phase == Hooks && (target == nil || target.ContainerID != id) {
		return PhaseEvidence{}, fail("runtime_conflict")
	}
	gateway := &Gateway{Parser: p.port.parser, Policy: policy, Port: p.port, Authorize: p.Check, Target: target, Images: images, CheckBuild: p.port.checkBuild}
	driver, e := StartDriver(ctx, gateway, p.root)
	if e != nil {
		return PhaseEvidence{}, e
	}
	input := p.invocation
	input.Phase = phase
	input.ContainerID = id
	input.Driver, e = driver.Executable(ctx, p.agent)
	if e != nil {
		driver.Close()
		return PhaseEvidence{}, e
	}
	args, e := input.Arguments()
	if e != nil {
		driver.Close()
		return PhaseEvidence{}, e
	}
	if e = p.tools.Verify(ctx, true); e != nil {
		driver.Close()
		return PhaseEvidence{}, e
	}
	// Fresh native checks precede even internal CLI exec/start commands.
	if phase == Hooks {
		current, e := inspectPhaseTarget(ctx, p.port)
		if e != nil || current.ContainerID != id {
			driver.Close()
			if e != nil {
				return PhaseEvidence{}, e
			}
			return PhaseEvidence{}, fail("runtime_conflict")
		}
	}
	if e = p.Check(ctx); e != nil {
		driver.Close()
		return PhaseEvidence{}, e
	}
	env := append(p.tools.Environment(), registryEnvironment(p.root, p.tools.NodePath)...)
	env = append(env, "TMPDIR="+filepath.Join(p.root, "tmp"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	raw, processError := captureMetadata(ctx, p.tools.NodePath, append([]string{filepath.Join(p.tools.Root, "node_modules/@devcontainers/cli/dist/spec-node/devContainersSpecCLI.js")}, args...), p.root, env, metadataLimit)
	closeError := driver.Close() // Cancels/reaps remaining native CLI clients first.
	if processError != nil || closeError != nil {
		return PhaseEvidence{}, fail("phase_unknown")
	}
	observed, e := inspectPhaseTarget(ctx, p.port)
	if e != nil {
		return PhaseEvidence{}, e
	}
	result, e := phaseResult(ctx, config.JSONC{NodePath: p.tools.NodePath, CompanionDir: p.tools.Root}, raw, phase, observed.ContainerID)
	if e != nil {
		return PhaseEvidence{}, e
	}
	if phase == Resources {
		if e := validateResourceUser(result, policy.RemoteUser); e != nil {
			return PhaseEvidence{}, e
		}
		effective := map[string]any{"configuration": result["configuration"], "mergedConfiguration": result["mergedConfiguration"], "workspace": p.effective["workspace"], "ociAuthDiagnostics": result["ociAuthDiagnostics"]}
		if e = ValidateEffective(effective, p.source.Source, p.metadata, input); e != nil {
			return PhaseEvidence{}, e
		}
	}
	if phase == Hooks && observed.ContainerID != id {
		return PhaseEvidence{}, fail("runtime_conflict")
	}
	if e = p.Check(ctx); e != nil {
		return PhaseEvidence{}, e
	}
	if phase == Resources {
		p.mu.Lock()
		copy := observed
		p.resources = &copy
		p.mu.Unlock()
	}
	descriptor, _ := config.DecodeDescriptor(p.source.Descriptor.Map())
	return PhaseEvidence{Phase: phase, Target: observed, Descriptor: descriptor}, nil
}

// The CLI display user is checked separately from the authoritative numeric pair.
func validateResourceUser(result map[string]any, remotePair string) error {
	uid, _, ok := parseUser(remotePair)
	if !ok {
		return fail("configuration_changed")
	}
	expected := strings.SplitN(remotePair, ":", 2)[0]
	// Pinned CLI0.89.0 createContainerProperties displays UID0 as root.
	// This does not resolve names or alter the enforced UID:GID pair.
	if uid == 0 {
		expected = "root"
	}
	if result["remoteUser"] != expected {
		return fail("configuration_changed")
	}
	return nil
}
