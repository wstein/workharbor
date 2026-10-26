//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"golang.org/x/sys/unix"
)

func (c Client) PrepareStage(ctx context.Context, options StageOptions) (LifecycleTransaction, error) {
	p, e := nativeStagePreparationPorts(c)
	if e != nil {
		return nil, e
	}
	return c.prepareStage(ctx, options, p)
}

func (s *LocalStage) Apply(parent context.Context, consent LifecycleConsent) (result LifecycleResult, failure error) {
	ownedAttempt := false
	defer func() {
		if !ownedAttempt || failure == nil || s.store == nil || s.receipt == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		disposal := candidateDisposal{store: s.store, runner: s.runner, endpoint: s.endpoint, receipt: s.receipt, plan: s.homeIntent.Plan(), observe: func(action string, target *StageResource, state string) {
			recordCleanupEffect(&s.result.Effects, action, target, state)
		}}
		remediation := "Candidate artifacts were removed; start a fresh stage with fresh source trust and confirmation."
		if err := disposal.dispose(cleanup); err != nil {
			s.result.Recovery = []StageRecoveryStep{{Action: "owner_reconcile", Instruction: disposal.residual(err)}}
			remediation = "Finish candidate cleanup with " + disposal.removalCommand() + " before a fresh stage."
		} else {
			s.result.RecoveryID = nil
			s.result.Recovery = []StageRecoveryStep{{Action: "restage_after_cleanup", Instruction: "Candidate container, fresh home, clone/results and operation records were removed. The execution failure remains; hooks may have effects outside these artifacts."}}
		}
		result = s.Preview()
		var coded *exitcode.Error
		if errors.As(failure, &coded) {
			coded.Result = result
			coded.WithRemediation("%s", remediation)
		}
	}()

	if s.applied || s.closed {
		return s.Preview(), stageConflict("transaction already used")
	}
	s.applied = true
	refuse := func(e error) (LifecycleResult, error) {
		s.result.Status = "refused"
		s.result.Phase = "preflight"
		return s.Preview(), e
	}
	if consent.TrustDigest != s.fingerprint || !consent.Confirmed || !consent.Quiescence {
		return refuse(exitcode.Errorf(exitcode.PathHumanDecisionMissing, "local stage source trust and confirmation required"))
	}
	ctx, cancel := context.WithTimeout(parent, 3600*time.Second)
	defer cancel()
	currentEnvironment, e := stageClosedEnvironment(s.runner.Root, s.endpoint.Endpoint)
	if e != nil {
		return refuse(e)
	}
	if stageDigest(stageTrustEnvironment(currentEnvironment)) != stageDigest(stageTrustEnvironment(s.runner.Environment)) {
		return refuse(stageConflict("admitted environment changed since preview"))
	}
	for _, identity := range s.runner.Tools {
		digest, err := stageFileDigest(ctx, identity.Path)
		if err != nil || digest != identity.Digest {
			if ctx.Err() != nil {
				return refuse(ctx.Err())
			}
			return refuse(stageConflict("admitted tool changed since preview"))
		}
	}
	if e := s.capture.Check(ctx); e != nil {
		return refuse(e)
	}
	for _, repo := range s.git.Repositories {
		if e := checkStageOriginalBinding(ctx, repo, nil); e != nil {
			return refuse(e)
		}
	}
	if e := s.checkStageAllocation(ctx); e != nil {
		return refuse(e)
	}
	store, e := openStageStore(s.ports.StateRoot, s.endpoint.DaemonID, s.endpoint.Endpoint)
	if e != nil {
		return refuse(e)
	}
	s.store = store
	if e = s.checkStageAllocation(ctx); e != nil {
		return refuse(e)
	}
	if e = s.archiveRemovedUnverified(ctx); e != nil {
		return refuse(e)
	}
	if e = store.Begin(s.operationID, s.fingerprint); e != nil {
		return refuse(e)
	}
	s.result.RecoveryID = stageString(filepath.Base(store.path) + "/" + s.operationID)
	s.receipt = s.initialStageReceipt(consent)
	ownedAttempt = true
	plan := s.homeIntent.Plan()
	s.settings = stageSettings{HomePlanDigest: stageDigest(plan), SmokeDigest: s.smokeDigest, ExecutionPaths: []string{}}
	s.execution = stageExecutionBinding{SettingsDigest: stageDigest(s.settings), GitInputs: []stageExecutionGitInput{}}
	s.receipt.ExecutionManifestDigest = stageDigest(s.execution)
	for name, value := range map[string]any{"candidate-home.json": plan, "stage-settings.json": s.settings, "execution-binding.json": s.execution} {
		if e = s.writeStageArtifact(name, value); e != nil {
			return s.stagePartial("allocation intent artifact publication failed", e)
		}
	}
	if e = store.saveReceipt(s.receipt, true); e != nil {
		return s.stagePartial("durable intent publication failed", e)
	}
	if e = s.writeStageArtifact("intent.json", s.receipt); e != nil {
		return s.stagePartial("retained intent publication failed", e)
	}
	if e = s.retainStageClone(); e != nil {
		return s.stagePartial("writable clone retention failed", e)
	}
	if e = store.Advance("allocating", "create_volume", "attempted"); e != nil {
		return s.stagePartial("home intent failed", e)
	}
	s.receipt.ActionInventory[0].State = "attempted"
	if e = store.saveReceipt(s.receipt, false); e != nil {
		return s.stagePartial("home intent publication failed", e)
	}
	start := time.Now().UTC()
	_, e = s.runner.Run(ctx, "allocate_home", "docker", []string{"volume", "create", "--driver", "local", "--label", "workharbor.stage.operation=" + s.operationID, "--label", "workharbor.stage.nonce=" + plan.Nonce, plan.Mount.Name}, nil, 30*time.Second)
	if e != nil {
		return s.stagePartial("candidate home creation uncertain", e)
	}
	observed, e := readStageVolume(ctx, s.runner, plan.Mount.Name)
	if e != nil {
		return s.stagePartial("candidate home observation uncertain", e)
	}
	created, e := time.Parse(time.RFC3339Nano, observed.CreatedAt)
	if e != nil || observed.Driver != "local" || observed.Labels["workharbor.stage.operation"] != s.operationID || observed.Labels["workharbor.stage.nonce"] != plan.Nonce || created.Before(start.Truncate(time.Second)) || created.After(time.Now().UTC().Add(time.Second)) {
		return s.stagePartial("candidate home creation correlation failed", stageConflict("fresh home metadata does not match allocation intent"))
	}
	s.result.Home.ID = stageString(plan.Mount.Name)
	s.receipt.Home = s.result.Home
	s.receipt.ActionInventory[0].State = "observed"
	if e = store.Advance("allocating", "create_volume", "observed"); e != nil {
		return s.stagePartial("home observation journal failed", e)
	}
	if e = store.saveReceipt(s.receipt, false); e != nil {
		return s.stagePartial("home observation receipt failed", e)
	}
	if e = s.writeStageArtifact("candidate-home.json", plan); e != nil {
		return s.stagePartial("home plan persistence failed", e)
	}
	if e = s.writeStageArtifact("candidate-home-observation.json", observed); e != nil {
		return s.stagePartial("home observation persistence failed", e)
	}
	s.settings.HomeObservationDigest = stageDigest(observed)
	s.execution.SettingsDigest = stageDigest(s.settings)
	s.receipt.ExecutionManifestDigest = stageDigest(s.execution)
	if e = s.writeStageArtifact("stage-settings.json", s.settings); e != nil {
		return s.stagePartial("home binding settings failed", e)
	}
	if e = s.writeStageArtifact("execution-binding.json", s.execution); e != nil {
		return s.stagePartial("home execution binding failed", e)
	}
	if e = store.saveReceipt(s.receipt, false); e != nil {
		return s.stagePartial("home binding receipt failed", e)
	}
	if e = s.prepareStageRuntime(ctx); e != nil {
		return s.stagePartial("runtime preparation failed", e)
	}
	return s.executeStage(ctx, observed)
}

func (s *LocalStage) initialStageReceipt(consent LifecycleConsent) *stageReceipt {
	bindings := []StageRepositoryBinding{}
	for _, r := range s.git.Repositories {
		bindings = append(bindings, StageRepositoryBinding{SourcePath: r.Source, GitDir: r.GitDir, CommonDir: r.CommonDir, ObjectFormat: r.ObjectFormat, OriginalHeadOID: r.Head, BindingDigest: r.BindingDigest})
	}
	return &stageReceipt{OperationID: s.operationID, EndpointIdentity: s.endpoint, OriginalSource: s.capture.Source, InputManifestDigest: s.capture.Digest, ToolManifest: stageTrustTools(s.runner.Tools), ConfigDigest: stageDigest(s.config), ImageMetadataDigest: stageDigest([]map[string]any{}), EffectiveConfigDigest: stageDigest(map[string]any{}), OutputInventory: []StageOutputEntry{}, OriginBinding: bindings, CandidateGitVersion: []stageCandidateGitVersion{}, ExecutionManifestDigest: stageDigest(map[string]any{}), PhaseBindingMode: "cooperative_quiescence", CloneRepositories: s.result.Repositories, ExportedRefs: []StageResultRef{}, ImportedObjects: []StageGitObjectsTarget{}, ResultRefTransactions: []StageGitRefsTarget{}, Primary: s.result.Primary, Candidate: s.result.Candidate, Home: s.result.Home, TrustDigest: s.fingerprint, ConsentFlags: stageConsentFlags{Trust: true, Confirmation: consent.Confirmed, Quiescence: consent.Quiescence}, ActionInventory: []StageEffect{{Action: "create_volume", Target: StageTarget{Resource: s.result.Home}, State: "planned"}}}
}

func (s *LocalStage) checkStageAllocation(ctx context.Context) error {
	identity, e := stageSocketIdentity(s.endpoint.Endpoint)
	if e != nil || identity.SocketDevice != s.endpoint.SocketDevice || identity.SocketInode != s.endpoint.SocketInode {
		return stageConflict("socket identity changed")
	}
	metadata, e := readStageMetadata(ctx, s.runner)
	if e != nil {
		return e
	}
	if metadata.Daemon.ID != s.endpoint.DaemonID {
		return stageConflict("daemon identity changed")
	}
	primary, e := selectStagePrimary(metadata.Containers, s.primary.ID)
	if e != nil {
		return e
	}
	if stageDigest(primary) != stageDigest(s.primary) {
		return stageConflict("primary binding changed before allocation")
	}
	for _, item := range metadata.Containers {
		_, staging := item.Labels["anchorage-staging"]
		if strings.TrimPrefix(item.Name, "/") == "whr-anchorage-staging" || staging {
			return stageConflict("candidate reservation collision")
		}
	}
	names, e := readStageVolumeNames(ctx, s.runner)
	if e != nil {
		return e
	}
	for _, name := range names {
		if name == s.homeIntent.Plan().Mount.Name {
			return stageConflict("candidate home name collision")
		}
	}
	return nil
}

func readStageVolumeNames(ctx context.Context, r *stageRunner) ([]string, error) {
	out, e := r.Run(ctx, "metadata", "docker", []string{"volume", "ls", "--format", "{{json .Name}}"}, nil, 5*time.Second)
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(strings.NewReader(string(out.Stdout)))
	names := []string{}
	seen := map[string]bool{}
	for {
		var name string
		e = decodeDockerRecord(d, &name)
		if e == io.EOF {
			break
		}
		if e != nil || !volumeName(name) || seen[name] || len(names) >= stageMaxEntries {
			return nil, stageConflict("volume name inventory invalid")
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

func (s *LocalStage) retainStageClone() error {
	op, e := s.store.operationDirectory(s.operationID)
	if e != nil {
		return e
	}
	defer op.Close()
	if e = unix.Mkdirat(int(op.Fd()), "workspace", 0700); e != nil {
		return stageConflict("retained workspace directory unavailable")
	}
	fd, e := unix.Openat(int(op.Fd()), "workspace", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("retained workspace directory changed")
	}
	defer unix.Close(fd)
	oldParent, e := os.Open(filepath.Dir(s.git.Clone))
	if e != nil {
		return stageConflict("preview clone parent unavailable")
	}
	defer oldParent.Close()
	if e = unix.Renameat(int(oldParent.Fd()), filepath.Base(s.git.Clone), fd, filepath.Base(s.clonePath)); e != nil {
		return stageConflict("retained clone publication failed")
	}
	if e = unix.Fsync(fd); e != nil {
		return stageConflict("retained clone durability uncertain")
	}
	if e = op.Sync(); e != nil {
		return stageConflict("retained workspace durability uncertain")
	}
	s.git.Clone = s.clonePath
	s.git.ClonePath = s.clonePath
	for i := range s.git.Repositories {
		s.git.Repositories[i].ClonePath = filepath.Join(s.clonePath, s.git.Repositories[i].RelativePath)
	}
	return nil
}

func (s *LocalStage) stagePartial(reason string, cause error) (LifecycleResult, error) {
	s.result.Status = "partial"
	s.result.Phase = "allocating"
	if s.receipt != nil {
		for i := range s.receipt.ActionInventory {
			if s.receipt.ActionInventory[i].State == "attempted" {
				s.receipt.ActionInventory[i].State = "uncertain"
			}
		}
		s.result.Effects = s.receipt.ActionInventory
		if s.store != nil {
			rows, e := s.store.Records()
			if e == nil && len(rows) > 0 {
				last := rows[len(rows)-1]
				s.result.Phase = last.State
				if last.Observation == "attempted" {
					_ = s.store.Advance(last.State, last.Action, "uncertain")
				}
				_ = s.store.saveReceipt(s.receipt, false)
			}
		}
	}
	s.result.Recovery = []StageRecoveryStep{{Action: "owner_reconcile", Instruction: "Inspect attempted/uncertain effects and the final candidate cleanup result; do not adopt an uncertain operation."}}
	e := exitcode.Errorf(exitcode.PathPartialFailure, "local stage %s", stageFailureMessage(reason, cause)).WithRemediation("Follow the final cleanup result before starting a fresh stage.")
	e.Result = s.Preview()
	return s.Preview(), e
}

// Partial effects keep exit 73 while retaining the failure category and phase.
func stageFailureMessage(reason string, cause error) string {
	message := terminal.Escape(reason)
	if cause == nil {
		return message
	}
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		return message + ": deadline exceeded"
	case errors.Is(cause, context.Canceled):
		return message + ": interrupted"
	default:
		return message + ": " + terminal.Escape(cause.Error())
	}
}
