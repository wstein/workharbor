// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

func (p *LocalPromotion) Apply(parent context.Context, consent LifecycleConsent) (LifecycleResult, error) {
	if p.applied || p.closed {
		return p.Preview(), stageConflict("promotion transaction already used")
	}
	p.applied = true
	refuse := func(err error) (LifecycleResult, error) { p.result.Status = "refused"; return p.Preview(), err }
	if !consent.Confirmed || !consent.Volumes || !consent.Force || !consent.Quiescence {
		return refuse(exitcode.Errorf(exitcode.PathHumanDecisionMissing, "local promote requires explicit home deletion consent, force, pane interruption confirmation and quiescence acknowledgement"))
	}
	if consent.TrustDigest != "" && consent.TrustDigest != p.receipt.TrustDigest {
		return refuse(stageConflict("promotion source trust differs from verified receipt"))
	}
	ctx, cancel := context.WithTimeout(parent, 1800*time.Second)
	defer cancel()
	if err := p.checkBeforePhase(ctx, true); err != nil {
		return refuse(promotionPhaseError(ctx, "preflight", err))
	}
	if err := p.results.Check(ctx); err != nil {
		return refuse(err)
	}
	if len(p.receipt.ActionInventory)+len(p.result.Effects) > 256 {
		return refuse(stageConflict("promotion effect inventory quota"))
	}
	p.stageEffects = append([]StageEffect{}, p.receipt.ActionInventory...)
	p.receipt.ConsentFlags.Volumes = true
	p.receipt.ConsentFlags.Force = true
	p.receipt.ConsentFlags.Confirmation = true
	p.receipt.ConsentFlags.Quiescence = true
	p.receipt.ExportedRefs = p.result.ResultRefs
	p.receipt.EffectiveConfigDigest = p.configurationDigest
	if err := p.transition("exporting_results"); err != nil {
		return p.partial("result export intent publication failed", err)
	}
	if err := p.persistConfiguration(); err != nil {
		return p.partial("promotion configuration publication failed", err)
	}
	driver, err := p.activatePromotionRunner(ctx)
	if err != nil {
		return p.partial("promotion driver admission failed", err)
	}
	p.invocation.Driver = driver
	if err = p.transition("importing_results"); err != nil {
		return p.partial("result import intent publication failed", err)
	}
	// Export storage becomes retained once transfer begins. Every import/ref write
	// has its exact durable target recorded by the callback before execution.
	if len(p.result.ResultRefs) > 0 {
		p.attempted = true
	}
	refs, err := p.results.Apply(ctx, p.recordEffect)
	p.result.ResultRefs = refs
	p.receipt.ExportedRefs = refs
	if err != nil {
		return p.partial("result transfer failed", err)
	}
	if err = p.save(); err != nil {
		return p.partial("result transfer inventory publication failed", err)
	}
	if err = p.checkBeforePhase(ctx, true); err != nil {
		return p.partial("pre-replacement binding changed", err)
	}
	if err = p.transition("promoting"); err != nil {
		return p.partial("primary replacement intent failed", err)
	}
	old := p.receipt.Primary
	if err = p.resourceEffect(ctx, "stop_primary", old, 30*time.Second, []string{"container", "stop", "--time", "10", "--", p.primaryObservation.ID}, func() error {
		got, e := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if e != nil {
			return e
		}
		if got.Running || stageContainerBindingDigest(got) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("promotion stopped primary observation changed")
		}
		return nil
	}); err != nil {
		return p.partial("original primary stop failed or uncertain", err)
	}
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.partial("post-stop binding changed", err)
	}
	if err = p.resourceEffect(ctx, "remove_primary", old, 30*time.Second, []string{"container", "rm", "--", p.primaryObservation.ID}, func() error { return p.requireContainerAbsent(ctx, p.primaryObservation.ID) }); err != nil {
		return p.partial("original primary removal failed or uncertain", err)
	}
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.partial("pre-resource binding changed", err)
	}
	if err = p.transition("primary_resources"); err != nil {
		return p.partial("primary resource phase intent failed", err)
	}
	target := &StageResource{Kind: "container", Name: p.receipt.Primary.Name, DaemonID: p.endpoint.DaemonID}
	if err = p.recordEffect("create_primary", StageTarget{Resource: target}, "attempted"); err != nil {
		return p.partial("primary creation intent failed", err)
	}
	args, err := p.invocation.Arguments("up")
	if err != nil {
		return p.partial("primary resource arguments invalid", err)
	}
	out, err := p.runner.Run(ctx, "primary_resources", "devcontainer", args, nil, 1800*time.Second)
	if err != nil {
		return p.effectFailure("create_primary", target, "primary resource phase failed", err)
	}
	var up struct {
		Outcome, ContainerID, RemoteUser, RemoteWorkspaceFolder string
		Configuration, MergedConfiguration                      map[string]any
	}
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	d.UseNumber()
	if decodeDockerRecord(d, &up) != nil || d.Decode(new(any)) != io.EOF || up.Outcome != "success" || !fullID(up.ContainerID) || up.ContainerID == p.primaryObservation.ID || up.ContainerID == p.candidateObservation.ID || up.RemoteUser != p.primaryConfig.RemoteUser || up.RemoteWorkspaceFolder != p.primaryConfig.Workspace || up.Configuration == nil || up.MergedConfiguration == nil || up.Configuration["image"] != p.receipt.Verification.ImageID || up.MergedConfiguration["image"] != p.receipt.Verification.ImageID {
		return p.effectFailure("create_primary", target, "primary resource outcome invalid", stageConflict("official promotion up identity or configuration response invalid"))
	}
	target.ID = stageString(up.ContainerID)
	actual, err := readStageContainer(ctx, p.runner, up.ContainerID)
	if err != nil {
		return p.effectFailure("create_primary", target, "primary observation failed", err)
	}
	if err = p.bindNewPrimary(actual); err != nil {
		return p.effectFailure("create_primary", target, "primary native binding failed", err)
	}
	p.newPrimary = &actual
	p.result.Primary = target
	policy := "smoke"
	if actual.Healthcheck {
		policy = "health"
		if p.settings.SmokeRequested {
			policy = "health_and_smoke"
		}
	}
	p.result.Verification = &StageVerification{Policy: policy, Status: "not_requested", BindingMode: "cooperative_quiescence", Quiescence: "caller_acknowledged", PreInputDigest: p.execution.OriginalDigest, InputDigest: p.execution.OriginalDigest, ImageID: p.receipt.Verification.ImageID, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.effectFailure("create_primary", target, "inputs changed during primary resources", err)
	}
	if err = p.recordEffect("create_primary", StageTarget{Resource: target}, "observed"); err != nil {
		return p.partial("primary resource observation publication failed", err)
	}
	p.invocation.ContainerID = actual.ID
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.partial("pre-hook binding changed", err)
	}
	if err = p.recordEffect("container_hooks", StageTarget{Resource: target}, "attempted"); err != nil {
		return p.partial("primary hook intent failed", err)
	}
	args, err = p.invocation.Arguments("run-user-commands")
	if err != nil {
		return p.effectFailure("container_hooks", target, "primary hook arguments invalid", err)
	}
	if _, err = p.runner.Run(ctx, "container_hooks", "devcontainer", args, nil, 600*time.Second); err != nil {
		return p.effectFailure("container_hooks", target, "primary container hooks failed", err)
	}
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.effectFailure("container_hooks", target, "inputs changed during primary hooks", err)
	}
	if err = p.recordEffect("container_hooks", StageTarget{Resource: target}, "observed"); err != nil {
		return p.partial("primary hook observation publication failed", err)
	}
	verification := p.result.Verification
	if actual.Healthcheck {
		verification.Policy = "health"
		if p.settings.SmokeRequested {
			verification.Policy = "health_and_smoke"
		}
		if err = p.awaitHealth(ctx, actual.ID); err != nil {
			verification.Status = "failed"
			return p.partial("primary health verification failed", err)
		}
	} else if !p.settings.SmokeRequested {
		verification.Status = "missing"
		return p.partial("primary has no healthcheck and smoke was not requested", stageConflict("promotion missing_smoke"))
	}
	if p.settings.SmokeRequested {
		if err = p.checkBeforePhase(ctx, false); err != nil {
			return p.partial("pre-smoke binding changed", err)
		}
		verification.Status = "failed"
		if err = p.recordEffect("smoke", StageTarget{Resource: target}, "attempted"); err != nil {
			return p.partial("primary smoke intent failed", err)
		}
		smoke, e := readStageInput(filepath.Join(p.receipt.OriginalSource, ".devcontainer", "smoke-container.sh"), stageMaxFileBytes)
		if e != nil {
			return p.effectFailure("smoke", target, "primary smoke input unavailable", e)
		}
		out, e = p.runner.Run(ctx, "smoke", "docker", []string{"exec", "--interactive", "--user", p.primaryConfig.RemoteUser, "--workdir", p.primaryConfig.Workspace, actual.ID, "bash", "-s", "--", p.primaryConfig.Workspace}, bytes.NewReader(smoke), 600*time.Second)
		if p.options.Diagnostics != nil {
			fmt.Fprintf(p.options.Diagnostics, "%s%s", terminal.Escape(string(out.Stdout)), terminal.Escape(string(out.Stderr)))
		}
		if e != nil {
			return p.effectFailure("smoke", target, "primary smoke verification failed", e)
		}
		verification.OutputDigest = stageString(out.OutputDigest)
		if err = p.checkBeforePhase(ctx, false); err != nil {
			return p.effectFailure("smoke", target, "inputs changed during primary smoke", err)
		}
		if err = p.recordEffect("smoke", StageTarget{Resource: target}, "observed"); err != nil {
			return p.partial("primary smoke observation publication failed", err)
		}
	}
	verification.Status = "passed"
	verification.PostInputDigest = stageString(p.execution.OriginalDigest)
	verification.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	p.result.Verification = verification
	p.primaryVerified = true
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.partial("pre-cleanup binding changed", err)
	}
	if err = p.transition("primary_verified"); err != nil {
		return p.partial("primary verification publication failed", err)
	}
	if err = p.resourceEffect(ctx, "remove_candidate", p.receipt.Candidate, 30*time.Second, []string{"container", "rm", "--force", "--", p.candidateObservation.ID}, func() error { return p.requireContainerAbsent(ctx, p.candidateObservation.ID) }); err != nil {
		return p.partial("candidate removal failed or uncertain", err)
	}
	if err = p.transition("candidate_removed"); err != nil {
		return p.partial("candidate removal publication failed", err)
	}
	if err = p.checkHomeCleanup(ctx); err != nil {
		return p.partial("home cleanup preflight failed", err)
	}
	if err = p.resourceEffect(ctx, "remove_home", p.receipt.Home, 30*time.Second, []string{"volume", "rm", "--", p.homePlan.Mount.Name}, func() error {
		names, e := readStageVolumeNames(ctx, p.runner)
		if e != nil {
			return e
		}
		for _, name := range names {
			if name == p.homePlan.Mount.Name {
				return stageConflict("promotion candidate home still exists")
			}
		}
		return nil
	}); err != nil {
		return p.partial("candidate home removal failed or uncertain", err)
	}
	if err = p.transition("home_removed"); err != nil {
		return p.partial("home removal publication failed", err)
	}
	if err = p.checkFinalBindings(ctx); err != nil {
		return p.partial("final promotion binding changed", err)
	}
	p.receipt.Primary = p.result.Primary
	p.receipt.Verification = verification
	if err = p.transition("complete"); err != nil {
		return p.partial("promotion completion publication failed", err)
	}
	p.result.Status = "promoted"
	p.result.Phase = "complete"
	return p.Preview(), nil
}
func (p *LocalPromotion) save() error {
	effects := append(append([]StageEffect{}, p.stageEffects...), p.result.Effects...)
	data, err := json.Marshal(effects)
	if err != nil {
		return stageConflict("promotion effect encoding invalid")
	}
	if err = json.Unmarshal(data, &p.receipt.ActionInventory); err != nil {
		return stageConflict("promotion effect copying invalid")
	}
	p.receipt.ExportedRefs = p.result.ResultRefs
	return p.store.saveReceipt(&p.receipt, false)
}
func (p *LocalPromotion) transition(phase string) error {
	if err := p.store.Advance(phase, "", "observed"); err != nil {
		return err
	}
	p.result.Phase = phase
	return p.save()
}
func (p *LocalPromotion) recordEffect(action string, target StageTarget, state string) error {
	index := -1
	for i, e := range p.result.Effects {
		if e.Action == action && (stageDigest(e.Target) == stageDigest(target) || (e.Target.Resource != nil && target.Resource != nil && e.Target.Resource.Name != nil && target.Resource.Name != nil && *e.Target.Resource.Name == *target.Resource.Name)) {
			index = i
			break
		}
	}
	if index < 0 {
		return stageConflict("promotion unplanned effect target")
	}
	if state == "attempted" {
		p.attempted = true
	}
	p.result.Effects[index] = StageEffect{Action: action, Target: target, State: state}
	if action == "import_objects" && state == "observed" {
		p.receipt.ImportedObjects = append(p.receipt.ImportedObjects, *target.Objects)
	}
	if action == "create_result_refs" && state == "observed" {
		p.receipt.ResultRefTransactions = append(p.receipt.ResultRefTransactions, *target.Refs)
		for i := range p.result.ResultRefs {
			for _, c := range target.Refs.Creates {
				if p.result.ResultRefs[i].RepositoryPath == target.Refs.Repository.SourcePath && p.result.ResultRefs[i].TargetRef == c.Name && p.result.ResultRefs[i].OID == c.OID {
					p.result.ResultRefs[i].State = "created"
				}
			}
		}
	}
	if err := p.store.Advance(p.result.Phase, action, state); err != nil {
		return err
	}
	return p.save()
}
func (p *LocalPromotion) resourceEffect(ctx context.Context, action string, target *StageResource, budget time.Duration, args []string, observe func() error) error {
	if err := p.recordEffect(action, StageTarget{Resource: target}, "attempted"); err != nil {
		return err
	}
	if _, err := p.runner.Run(ctx, action, "docker", args, nil, budget); err != nil {
		_ = p.recordEffect(action, StageTarget{Resource: target}, "uncertain")
		return err
	}
	if err := observe(); err != nil {
		_ = p.recordEffect(action, StageTarget{Resource: target}, "uncertain")
		return err
	}
	return p.recordEffect(action, StageTarget{Resource: target}, "observed")
}
func (p *LocalPromotion) effectFailure(action string, target *StageResource, reason string, err error) (LifecycleResult, error) {
	_ = p.recordEffect(action, StageTarget{Resource: target}, "uncertain")
	return p.partial(reason, err)
}
func (p *LocalPromotion) partial(reason string, cause error) (LifecycleResult, error) {
	p.result.Status = "partial"
	for i := range p.result.Effects {
		if p.result.Effects[i].State == "planned" {
			p.result.Effects[i].State = "remaining"
		} else if p.result.Effects[i].State == "attempted" {
			p.result.Effects[i].State = "uncertain"
		}
	}
	for i := range p.result.ResultRefs {
		if p.result.ResultRefs[i].State == "planned" {
			p.result.ResultRefs[i].State = "remaining"
		}
	}
	p.result.Recovery = []StageRecoveryStep{{Action: "owner_reconcile", Instruction: "Inspect the exact retained operation receipt and effects before any owner reconciliation; do not rerun promotion or assume rollback. Preserve original primary home and writable clones."}}
	_ = p.save()
	err := exitcode.Errorf(exitcode.PathPartialFailure, "local promote %s; retained inventory requires owner reconciliation", reason)
	err.Result = p.Preview()
	_ = cause
	return p.Preview(), err
}
func (p *LocalPromotion) checkBeforePhase(ctx context.Context, original bool) (failure error) {
	defer func() {
		if failure != nil {
			p.invalidatePublicVerification()
		}
	}()
	if err := p.checkConfiguration(); err != nil {
		return err
	}
	if p.persistentConfiguration {
		if err := p.checkPersistedConfiguration(); err != nil {
			return err
		}
	}
	metadata, err := readStageMetadata(ctx, p.runner)
	if err != nil {
		return err
	}
	if err = p.checkArtifactsAndBindings(ctx, metadata, original); err != nil {
		return err
	}
	if p.newPrimary != nil {
		return p.checkFinalPrimary(ctx)
	}
	return nil
}
func (p *LocalPromotion) requireContainerAbsent(ctx context.Context, id string) error {
	m, e := readStageMetadata(ctx, p.runner)
	if e != nil {
		return e
	}
	for _, c := range m.Containers {
		if c.ID == id {
			return stageConflict("promotion removed container still exists")
		}
	}
	return nil
}
func (p *LocalPromotion) awaitHealth(parent context.Context, id string) error {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	for {
		d, e := readStageContainer(ctx, p.runner, id)
		if e != nil {
			return e
		}
		if !d.Running {
			return stageConflict("promotion primary stopped before verification")
		}
		if d.Health != nil {
			if *d.Health == "healthy" {
				return nil
			}
			if *d.Health == "unhealthy" {
				return stageConflict("promotion primary healthcheck failed")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (p *LocalPromotion) invalidatePublicVerification() {
	if p.result.Verification != nil {
		copy := *p.result.Verification
		copy.Status = "stale"
		p.result.Verification = &copy
	}
}
