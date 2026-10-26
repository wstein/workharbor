// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// LocalPromotion retains the existing daemon lock and one immutable result plan.
// A failed or uncertain application is never retried or adopted by this object.
type LocalPromotion struct {
	options                                  PromoteOptions
	ports                                    stagePreparationPorts
	root                                     string
	rootIdentity                             os.FileInfo
	runner                                   *stageRunner
	store                                    *stageStore
	endpoint                                 stageEndpointIdentity
	receipt                                  stageReceipt
	settings                                 stageSettings
	execution                                stageExecutionBinding
	sourceConfig                             map[string]any
	sourceMetadata                           map[string]any
	primaryConfig                            stagePrimaryConfiguration
	homePlan                                 CandidateHomePlan
	primaryObservation, candidateObservation stageContainerDetails
	homeObservation                          stageVolumeDetails
	image                                    stageImageDetails
	snapshots                                []stageGitSnapshot
	results                                  *stageResultTransfer
	invocation                               stageCLIInvocation
	configuration                            map[string]any
	configurationBytes                       []byte
	configurationDigest                      string
	configurationPath                        string
	result                                   LifecycleResult
	applied, closed, attempted               bool
	stageEffects                             []StageEffect
	newPrimary                               *stageContainerDetails
	persistentConfiguration                  bool
	primaryVerified                          bool
	originalRetained, originalRemoved        bool
	rollbackEligible, rollbackRestored       bool
	rollbackFailure                          error
	candidateDisposed                        bool
	disposalStarted                          bool
}

func (c Client) PreparePromotion(ctx context.Context, options PromoteOptions) (LifecycleTransaction, error) {
	ports, err := nativeStagePreparationPorts(c)
	if err != nil {
		return nil, err
	}
	p, err := c.preparePromotion(ctx, options, ports)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (c Client) preparePromotion(parent context.Context, options PromoteOptions, ports stagePreparationPorts) (promotion *LocalPromotion, err error) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, promotionPhaseError(ctx, "preflight", ctx.Err())
	}
	if options.Target == "" {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "local promote requires exactly one primary container id/name")
	}
	if ports.Endpoint == nil || ports.MountAdmission == nil || !filepath.IsAbs(ports.StateRoot) {
		return nil, stageConflict("promotion preparation capability unavailable")
	}
	p := &LocalPromotion{options: options, ports: ports}
	defer func() {
		if err != nil {
			if closeErr := p.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	p.root, err = os.MkdirTemp("", "whr-local-promote-")
	if err != nil {
		return nil, stageConflict("promotion private storage unavailable")
	}
	if err = os.Chmod(p.root, 0700); err != nil {
		return nil, stageConflict("promotion private storage mode")
	}
	p.rootIdentity, err = os.Lstat(p.root)
	if err != nil {
		return nil, stageConflict("promotion private storage identity unavailable")
	}
	physicalRoot, rootErr := filepath.EvalSymlinks(p.root)
	if rootErr != nil {
		return nil, stageConflict("promotion private storage physical path unavailable")
	}
	physicalInfo, rootErr := os.Lstat(physicalRoot)
	if rootErr != nil || !os.SameFile(p.rootIdentity, physicalInfo) || !physicalInfo.IsDir() || physicalInfo.Mode().Perm() != 0700 {
		return nil, stageConflict("promotion private storage physical identity changed")
	}
	p.root = physicalRoot
	phase := filepath.Join(p.root, "phase")
	if err = os.Mkdir(phase, 0700); err != nil {
		return nil, stageConflict("promotion phase storage unavailable")
	}
	endpoint, err := ports.Endpoint(ctx)
	if err != nil {
		return nil, err
	}
	p.endpoint, err = stageSocketIdentity(endpoint)
	if err != nil {
		return nil, err
	}
	p.runner, err = newStageRunner(ctx, phase, p.endpoint.Endpoint, ports.Tools)
	if err != nil {
		return nil, err
	}
	if err = admitStageToolVersions(ctx, p.runner); err != nil {
		return nil, promotionPhaseError(ctx, "preflight", err)
	}
	metadata, err := readStageMetadata(ctx, p.runner)
	if err != nil {
		return nil, promotionPhaseError(ctx, "preflight", err)
	}
	p.endpoint.DaemonID = metadata.Daemon.ID
	primary, err := selectStagePrimary(metadata.Containers, options.Target)
	if err != nil {
		return nil, err
	}
	p.store, err = openPromotionStore(ports.StateRoot, p.endpoint.DaemonID, p.endpoint.Endpoint)
	if err != nil {
		return nil, err
	}
	records, err := p.store.Records()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, stageConflict("promotion verified receipt is absent; retained resources require owner reconciliation")
	}
	op := records[len(records)-1].OperationID
	p.receipt, err = p.store.loadReceipt(op)
	if err != nil {
		return nil, err
	}
	if p.receipt.State != "staged_verified" {
		if p.receipt.UnverifiedReason != nil {
			return nil, stageConflict("promotion verification is " + *p.receipt.UnverifiedReason + "; guarded cleanup and fresh stage required")
		}
		return nil, stageConflict("promotion requires a staged_verified receipt; retained attempt requires owner reconciliation")
	}
	if p.receipt.EndpointIdentity != p.endpoint || p.receipt.Primary == nil || p.receipt.Primary.ID == nil || *p.receipt.Primary.ID != primary.ID {
		return nil, stageConflict("promotion receipt endpoint or primary binding changed")
	}
	if p.receipt.TrustDigest != records[len(records)-1].Fingerprint || !stageImageID(p.receipt.TrustDigest) || !p.receipt.ConsentFlags.Trust || !p.receipt.ConsentFlags.Confirmation || !p.receipt.ConsentFlags.Quiescence {
		return nil, stageConflict("promotion receipt lacks exact source trust and phase acknowledgement")
	}
	if p.receipt.Verification == nil || p.receipt.Verification.Status != "passed" || p.receipt.Verification.BindingMode != "cooperative_quiescence" || p.receipt.Verification.Quiescence != "caller_acknowledged" || !stageImageID(p.receipt.Verification.ImageID) {
		return nil, stageConflict("promotion receipt verification is missing or stale")
	}
	if stageDigest(stageTrustTools(p.runner.Tools)) != stageDigest(stageTrustTools(p.receipt.ToolManifest)) {
		return nil, stageConflict("promotion trusted tool identity changed")
	}
	if err = p.loadArtifacts(); err != nil {
		return nil, err
	}
	if p.primaryConfig.Source != p.receipt.OriginalSource {
		return nil, stageConflict("promotion original source binding changed")
	}
	physical, e := filepath.EvalSymlinks(p.receipt.OriginalSource)
	if e != nil || physical != p.receipt.OriginalSource {
		return nil, stageConflict("promotion original source path changed")
	}
	if err = ports.MountAdmission(ctx, metadata.Daemon, primary, p.receipt.OriginalSource); err != nil {
		return nil, err
	}
	if err = p.checkArtifactsAndBindings(ctx, metadata, true); err != nil {
		return nil, promotionPhaseError(ctx, "preflight", err)
	}
	p.results, err = prepareStageResults(ctx, p.receipt.OperationID, p.snapshots)
	if err != nil {
		return nil, promotionPhaseError(ctx, "preflight", err)
	}
	// Preview names the eventual operation-owned export targets without writing
	// them. The temporary reader root is used only for pre-apply inspection.
	for i := range p.results.repositories {
		p.results.repositories[i].Objects.ExportPath = filepath.Join(p.store.operationPath(p.receipt.OperationID), "results", filepath.Base(p.results.repositories[i].Objects.ExportPath))
	}
	p.result = p.previewResult()
	if err = p.prepareConfiguration(ctx); err != nil {
		return nil, promotionPhaseError(ctx, "preflight", err)
	}
	p.disclose()
	return p, nil
}
func (p *LocalPromotion) loadArtifacts() error {
	op := p.receipt.OperationID
	for _, item := range []struct {
		name, digest string
		value        any
	}{
		{"execution-binding.json", p.receipt.ExecutionManifestDigest, &p.execution},
	} {
		if err := readPromotionArtifact(p.store, op, item.name, item.digest, item.value); err != nil {
			return err
		}
	}
	if err := readPromotionArtifact(p.store, op, "stage-settings.json", p.execution.SettingsDigest, &p.settings); err != nil {
		return err
	}
	for _, item := range []struct {
		name, digest string
		value        any
	}{
		{"source-config.json", p.receipt.ConfigDigest, &p.sourceConfig},
		{"source-metadata.json", p.settings.SourceMetadataDigest, &p.sourceMetadata},
		{"primary-config.json", p.settings.PrimaryConfigDigest, &p.primaryConfig},
		{"git-snapshots.json", p.settings.GitSnapshotsDigest, &p.snapshots},
		{"candidate-home.json", p.settings.HomePlanDigest, &p.homePlan},
		{"candidate-home-observation.json", p.settings.HomeObservationDigest, &p.homeObservation},
		{"primary-observation.json", p.settings.PrimaryObservationDigest, &p.primaryObservation},
		{"candidate-observation.json", p.settings.CandidateObservationDigest, &p.candidateObservation},
	} {
		if err := readPromotionArtifact(p.store, op, item.name, item.digest, item.value); err != nil {
			return err
		}
	}
	if !stageGitRelativePath(p.settings.ConfigRelativePath) || len(p.settings.ExecutionPaths) == 0 || len(p.settings.ExecutionPaths) > stageMaxEntries || p.sourceConfig == nil || p.sourceMetadata == nil {
		return stageConflict("promotion execution inventory is invalid")
	}
	if len(p.snapshots) == 0 || len(p.snapshots) > 33 || len(p.snapshots) != len(p.receipt.OriginBinding) || len(p.snapshots) != len(p.receipt.CloneRepositories) {
		return stageConflict("promotion repository inventory is invalid")
	}
	seen := map[string]bool{}
	for i, s := range p.snapshots {
		b := p.receipt.OriginBinding[i]
		r := p.receipt.CloneRepositories[i]
		if seen[s.Source] || s.Source != b.SourcePath || s.GitDir != b.GitDir || s.CommonDir != b.CommonDir || s.ObjectFormat != b.ObjectFormat || s.Head != b.OriginalHeadOID || s.BindingDigest != b.BindingDigest || r.SourcePath != s.Source || r.ClonePath != s.ClonePath || r.ObjectFormat != s.ObjectFormat || r.CloneRootMode != 0700 || r.CloneOwnerUID != os.Getuid() {
			return stageConflict("promotion repository provenance changed")
		}
		physical, err := filepath.EvalSymlinks(s.ClonePath)
		if err != nil || physical != s.ClonePath {
			return stageConflict("promotion retained clone path changed")
		}
		info, err := os.Lstat(s.ClonePath)
		owner, owned := promotionInfoOwner(info)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !owned || owner != os.Getuid() {
			return stageConflict("promotion retained clone mode changed")
		}
		seen[s.Source] = true
	}
	if p.snapshots[0].Source != p.receipt.OriginalSource || p.primaryConfig.Source != p.receipt.OriginalSource || p.primaryConfig.Home != p.homePlan.PrimaryHome || p.primaryConfig.HomeDestination != p.homePlan.Mount.Destination {
		return stageConflict("promotion original home or source binding changed")
	}
	if p.receipt.Primary == nil || p.receipt.Primary.ID == nil || p.receipt.Candidate == nil || p.receipt.Candidate.ID == nil || p.receipt.Home == nil || p.receipt.Home.Name == nil || p.homePlan.OperationID != p.receipt.OperationID || p.homePlan.DaemonID != p.endpoint.DaemonID || p.homePlan.Endpoint != p.endpoint.Endpoint || p.homePlan.PrimaryID != *p.receipt.Primary.ID || p.homePlan.Mount.Name != *p.receipt.Home.Name || !lowerHex32(p.homePlan.Nonce) || p.homePlan.Nonce == p.homePlan.OperationID || p.homePlan.Mount.Name != "whr-stage-home-"+p.homePlan.Nonce {
		return stageConflict("promotion private home allocation binding changed")
	}
	if p.primaryObservation.ID != *p.receipt.Primary.ID || p.candidateObservation.ID != *p.receipt.Candidate.ID || p.primaryConfig.Name != strings.TrimPrefix(p.primaryObservation.Name, "/") || p.homeObservation.Name != p.homePlan.Mount.Name || p.homeObservation.Driver != "local" || p.homeObservation.Labels["workharbor.stage.operation"] != p.receipt.OperationID || p.homeObservation.Labels["workharbor.stage.nonce"] != p.homePlan.Nonce {
		return stageConflict("promotion recorded resource observation changed")
	}
	return nil
}
func (p *LocalPromotion) previewResult() LifecycleResult {
	r := LifecycleResult{Status: "preview", Phase: "preflight", RecoveryID: stageString(filepath.Base(p.store.path) + "/" + p.receipt.OperationID), Fingerprint: stageString(p.receipt.TrustDigest), Primary: p.receipt.Primary, Candidate: p.receipt.Candidate, Home: p.receipt.Home, Verification: p.receipt.Verification, Effects: []StageEffect{}, Recovery: []StageRecoveryStep{}, Repositories: append([]StageRepository{}, p.receipt.CloneRepositories...), ResultRefs: p.results.Preview()}
	for _, objects := range p.results.Targets() {
		copy := objects
		r.Effects = append(r.Effects, StageEffect{Action: "import_objects", State: "planned", Target: StageTarget{Objects: &copy}})
	}
	creates := map[string][]StageRefCreate{}
	for _, ref := range r.ResultRefs {
		creates[ref.RepositoryPath] = append(creates[ref.RepositoryPath], StageRefCreate{Name: ref.TargetRef, OID: ref.OID, ExpectedAbsent: true})
	}
	for _, repo := range p.snapshots {
		if refs := creates[repo.Source]; len(refs) > 0 {
			binding := StageRepositoryBinding{SourcePath: repo.Source, GitDir: repo.GitDir, CommonDir: repo.CommonDir, ObjectFormat: repo.ObjectFormat, OriginalHeadOID: repo.Head, BindingDigest: repo.BindingDigest}
			r.Effects = append(r.Effects, StageEffect{Action: "create_result_refs", State: "planned", Target: StageTarget{Refs: &StageGitRefsTarget{Kind: "git_refs", Repository: binding, Creates: refs}}})
		}
	}
	retained := *r.Primary
	retained.Name = stageString(p.retainedPrimaryName())
	for _, item := range []struct {
		action string
		target *StageResource
	}{{"stop_primary", r.Primary}, {"rename_primary", &retained}, {"remove_primary", &retained}, {"create_primary", &StageResource{Kind: "container", Name: p.receipt.Primary.Name, DaemonID: p.endpoint.DaemonID}}, {"container_hooks", &StageResource{Kind: "container", Name: p.receipt.Primary.Name, DaemonID: p.endpoint.DaemonID}}, {"remove_candidate", r.Candidate}, {"remove_home", r.Home}} {
		r.Effects = append(r.Effects, StageEffect{Action: item.action, State: "planned", Target: StageTarget{Resource: item.target}})
	}
	if p.settings.SmokeRequested {
		r.Effects = append(r.Effects, StageEffect{Action: "smoke", State: "planned", Target: StageTarget{Resource: &StageResource{Kind: "container", Name: p.receipt.Primary.Name, DaemonID: p.endpoint.DaemonID}}})
	}
	return r
}
func (p *LocalPromotion) Preview() LifecycleResult {
	data, _ := json.Marshal(p.result)
	var result LifecycleResult
	_ = json.Unmarshal(data, &result)
	return result
}
func (p *LocalPromotion) Close() error {
	if p == nil || p.closed {
		return nil
	}
	if p.results != nil {
		p.results.Close()
	}
	if p.store != nil {
		p.store.Close()
		p.store = nil
	}
	if p.root != "" {
		info, err := os.Lstat(p.root)
		if err != nil || p.rootIdentity == nil || !os.SameFile(info, p.rootIdentity) || info.Mode()&os.ModeSymlink != 0 {
			return exitcode.Errorf(exitcode.PathConflict, "local promote temporary preparation identity changed; retained for inspection").WithRemediation("Inspect retained temporary preparation %s before owner cleanup.", terminal.Escape(p.root))
		}
		if err = removeStageTemporaryRoot(p.root); err != nil {
			return exitcode.Errorf(exitcode.PathConflict, "local promote temporary preparation retained after cleanup failure").WithRemediation("Inspect retained temporary preparation %s before owner cleanup.", terminal.Escape(p.root))
		}
	}
	p.closed = true
	return nil
}
func (p *LocalPromotion) disclose() {
	if p.options.Diagnostics == nil {
		return
	}
	fmt.Fprintln(p.options.Diagnostics, "Promotion stops and renames the original primary, terminating all its exec sessions including shell panes. Its writable layer is retained until the replacement passes verification; a known hook/smoke failure permits bounded resource recovery only after fresh safety checks; uncertain effects require owner recovery. After successful rollback, remove the failed replacement and all candidate artifacts. The original primary home is preserved; successful promotion disposes of the candidate container, fresh home, clone/results and records last.")
	fmt.Fprintf(p.options.Diagnostics, "Pause ordinary input writers throughout every phase. Trusted container hooks have read-write access to original workspace %s, admitted mounts and container network; hashes do not freeze paths or inventory all hook writes.\n", terminal.Escape(p.receipt.OriginalSource))
	fmt.Fprintln(p.options.Diagnostics, "Promotion runs no host initialization and uses only the verified immutable image; source container hooks retain their original origins/order, with postAttach suppressed because this command does not attach.")
	fmt.Fprintf(p.options.Diagnostics, "Original container recovery name: %s; original full ID: %s.\n", p.retainedPrimaryName(), p.primaryObservation.ID)
	fmt.Fprintln(p.options.Diagnostics, "Apply requires fresh confirmation/--yes. Result refs are fetched into the original before clone disposal, for owner integration rather than automatic working-tree activation.")
	for _, key := range stageLifecycleHooks {
		if _, ok := p.sourceConfig[key]; ok {
			fmt.Fprintf(p.options.Diagnostics, "container hook %s: trusted source origin; contents remain private.\n", key)
		}
	}
}
func promotionPhaseError(ctx context.Context, phase string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return exitcode.Errorf(exitcode.PathDeadlineExceeded, "local promote %s deadline exceeded", phase)
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return exitcode.Errorf(exitcode.PathUserInterrupt, "local promote %s interrupted", phase)
	}
	return err
}

func promotionInfoOwner(info os.FileInfo) (int, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
