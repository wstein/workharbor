// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

var promotionUnhealthy = stageConflict("promotion primary healthcheck failed")

func (p *LocalPromotion) Apply(parent context.Context, consent LifecycleConsent) (LifecycleResult, error) {
	if p.applied || p.closed {
		return p.Preview(), stageConflict("promotion transaction already used")
	}
	p.applied = true
	refuse := func(err error) (LifecycleResult, error) { p.result.Status = "refused"; return p.Preview(), err }
	if !consent.Confirmed || !consent.Quiescence {
		return refuse(exitcode.Errorf(exitcode.PathHumanDecisionMissing, "local promote requires pane interruption confirmation and quiescence acknowledgement"))
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
		return refuse(promotionPhaseError(ctx, "preflight", err))
	}
	if err := p.requireDistinctPromotionLookup(ctx); err != nil {
		return refuse(promotionPhaseError(ctx, "preflight", err))
	}
	if len(p.receipt.ActionInventory)+len(p.result.Effects)+4 > 256 {
		return refuse(stageConflict("promotion effect inventory quota"))
	}
	if err := p.bindResultStorage(); err != nil {
		return p.partial("candidate result storage publication failed", err)
	}
	p.stageEffects = append([]StageEffect{}, p.receipt.ActionInventory...)
	p.receipt.ConsentFlags.Volumes = false
	p.receipt.ConsentFlags.Force = false
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
	// Export storage is retained only after an original-repository import is attempted. Every import/ref write
	// has its exact durable target recorded by the callback before execution.

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
	if err = p.requireDistinctPromotionLookup(ctx); err != nil {
		return p.partial("replacement lookup preflight failed", err)
	}
	if err = p.transition("promoting"); err != nil {
		return p.partial("primary replacement intent failed", err)
	}
	old := p.receipt.Primary
	retained := *old
	retained.Name = stageString(p.retainedPrimaryName())
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
	if err = p.resourceEffect(ctx, "rename_primary", &retained, 30*time.Second, []string{"container", "rename", "--", p.primaryObservation.ID, p.retainedPrimaryName()}, func() error {
		got, e := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if e != nil {
			return e
		}
		if got.Running || strings.TrimPrefix(got.Name, "/") != p.retainedPrimaryName() {
			return stageConflict("promotion retained primary rename observation changed")
		}
		got.Name = p.primaryObservation.Name
		if stageContainerBindingDigest(got) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("promotion retained primary binding changed")
		}
		p.originalRetained = true
		return nil
	}); err != nil {
		return p.partial("original primary rename failed or uncertain", err)
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
			if errors.Is(err, promotionUnhealthy) {
				p.rollbackEligible = true
			}
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
		smoke, e := readStageTrustedSmoke(p.receipt.OriginalSource, p.settings.SmokeDigest)
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
	// Only a verified replacement permits removal of the stopped original.
	if err = p.resourceEffect(ctx, "remove_primary", &retained, 30*time.Second, []string{"container", "rm", "--", p.primaryObservation.ID}, func() error { return p.requireContainerAbsent(ctx, p.primaryObservation.ID) }); err != nil {
		return p.partial("verified replacement retained; original primary removal failed or uncertain", err)
	}
	p.originalRemoved = true
	if err = p.checkBeforePhase(ctx, false); err != nil {
		return p.partial("candidate cleanup recheck failed", err)
	}
	if err = p.resourceEffect(ctx, "remove_candidate", p.receipt.Candidate, 30*time.Second, []string{"container", "rm", "--force", "--volumes", "--", p.candidateObservation.ID}, func() error { return p.requireContainerAbsent(ctx, p.candidateObservation.ID) }); err != nil {
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
	disposal := candidateDisposal{store: p.store, runner: p.runner, endpoint: p.endpoint, receipt: &p.receipt, plan: p.homePlan}
	p.disposalStarted = true
	if err = disposal.begin(ctx); err != nil {
		return p.partial("candidate cleanup record binding failed", err)
	}
	if err = p.store.discardCandidateFiles(p.receipt.OperationID); err != nil {
		return p.partial("candidate clone/results or records cleanup incomplete", err)
	}
	p.candidateDisposed = true
	p.result.RecoveryID = nil
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
	var failed *stageCommandFailure
	eligible := (action == "container_hooks" || action == "smoke") && errors.As(err, &failed) && failed.Exit > 0 && (action != "smoke" || failed.Exit < 125) && p.newPrimary != nil
	if eligible {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		current, observationErr := readStageContainer(ctx, p.runner, p.newPrimary.ID)
		cancel()
		if observationErr != nil {
			err = errors.Join(err, stageConflict("promotion failure replacement observation unavailable"), observationErr)
			eligible = false
		} else if !current.Running || stageContainerBindingDigest(current) != stageContainerBindingDigest(*p.newPrimary) {
			err = errors.Join(err, stageConflict("promotion failure replacement not observed running with admitted binding"))
			eligible = false
		}
	}
	if eligible {
		if e := p.recordEffect(action, StageTarget{Resource: target}, "observed"); e == nil {
			p.rollbackEligible = true
			if p.result.Verification != nil {
				p.result.Verification.Status = "failed"
			}
		} else {
			err = errors.Join(err, e)
		}
	} else {
		_ = p.recordEffect(action, StageTarget{Resource: target}, "uncertain")
	}
	return p.partial(reason, err)
}
func (p *LocalPromotion) partial(reason string, cause error) (LifecycleResult, error) {
	if !p.attempted {
		p.result.Status = "refused"
		recovery := "No Docker or Git effects were attempted. Existing primary and candidate resources were not changed by this attempt. Private intent/configuration may have been retained; inspect it before retrying."
		if p.store != nil && p.receipt.OperationID != "" {
			recovery += fmt.Sprintf(" Inspect operation %s at %s.", terminal.Escape(p.receipt.OperationID), terminal.Escape(p.store.operationPath(p.receipt.OperationID)))
		}
		p.result.Recovery = []StageRecoveryStep{{Action: "owner_reconcile", Instruction: recovery}}
		err := promotionPhaseError(context.Background(), p.result.Phase, cause)
		if err == nil {
			err = stageConflict(reason)
		}
		return p.Preview(), err
	}
	if p.rollbackEligible {
		p.rollbackEligible = false
		p.rollbackFailure = p.restoreOriginalResource()
		if p.rollbackFailure == nil && p.rollbackRestored {
			p.rollbackFailure = p.disposeAfterRollback()
		}
	}
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
	recovery := fmt.Sprintf("Inspect operation %s and retained configuration %s/promote/devcontainer.json. Original primary full ID %s, original name %s, recovery name %s. Inspect attempted/uncertain effects by full ID; hooks may have modified shared workspace/home. No further automatic recovery or retry; retain clone, imported objects and refs.", p.receipt.OperationID, terminal.Escape(p.store.operationPath(p.receipt.OperationID)), p.primaryObservation.ID, terminal.Escape(p.primaryConfig.Name), p.retainedPrimaryName())
	if p.originalRemoved {
		recovery += " The replacement passed verification before the original container was removed. Preserve and inspect the replacement; the removed original writable layer cannot be restored by this operation."
	} else if !p.rollbackRestored {
		recovery += " The original container has not been intentionally removed. Inspect its stopped/renamed state and any replacement first. Before the owner renames/starts that exact original, stop any replacement using its home and release the original name. An unconfirmed remove result still requires inspection; do not assume rollback."
	}
	if p.rollbackRestored && !p.candidateDisposed {
		recovery += " Bounded resource recovery: original container restored under its original name and observed running; failed replacement retained stopped as " + p.failedPrimaryName() + ". Shared workspace/home writes, imported objects/refs and terminated sessions were not restored."
	} else if p.rollbackFailure != nil {
		recovery += " Resource recovery stopped: " + terminal.Escape(p.rollbackFailure.Error()) + ". Inspect both full IDs before any manual action."
	}
	if p.candidateDisposed {
		recovery = "Bounded resource recovery: original container restored under its original name and observed running. Failed replacement, candidate container, fresh home, clone/results and candidate records were removed. Shared workspace/home hook writes, imported objects/refs and terminated sessions were not restored."
		p.result.RecoveryID = nil
	}
	p.result.Recovery = []StageRecoveryStep{{Action: "owner_reconcile", Instruction: recovery}}

	if !p.candidateDisposed && !p.disposalStarted {
		_ = p.save()
	}
	suffix := "; retained inventory requires owner reconciliation"
	if p.candidateDisposed {
		suffix = "; original primary restored and candidate artifacts disposed"
	}
	err := exitcode.Errorf(exitcode.PathPartialFailure, "local promote %s: %s%s", reason, terminal.Escape(promotionCause(errors.Join(cause, p.rollbackFailure))), suffix)
	err.Result = p.Preview()
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
	if p.originalRetained && !p.originalRemoved {
		got, e := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if e != nil {
			return e
		}
		if got.Running || strings.TrimPrefix(got.Name, "/") != p.retainedPrimaryName() {
			return stageConflict("promotion retained original primary changed")
		}
		got.Name = p.primaryObservation.Name
		if stageContainerBindingDigest(got) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("promotion retained original primary binding changed")
		}
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
				return promotionUnhealthy
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

func (p *LocalPromotion) retainedPrimaryName() string {
	return "whr-primary-recovery-" + p.receipt.OperationID
}

func promotionCause(err error) string {
	if err == nil {
		return "unknown cause"
	}
	return err.Error()
}

func (p *LocalPromotion) requireDistinctPromotionLookup(ctx context.Context) error {
	args := []string{"ps", "-a", "--format", "{{.ID}}"}
	for _, label := range p.invocation.Labels {
		args = append(args, "--filter", "label="+label)
	}
	out, err := p.runner.Run(ctx, "preflight", "docker", args, nil, 5*time.Second)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out.Stdout)) != 0 {
		return stageConflict("promotion replacement labels already match an existing container")
	}
	metadata, err := readStageMetadata(ctx, p.runner)
	if err != nil {
		return err
	}
	for _, item := range metadata.Containers {
		if strings.TrimPrefix(item.Name, "/") == p.retainedPrimaryName() {
			return stageConflict("promotion recovery name already occupied")
		}
	}
	return nil
}
