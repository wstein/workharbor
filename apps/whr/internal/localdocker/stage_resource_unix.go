//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

func (s *LocalStage) writeStageArtifact(name string, value any) error {
	b, e := stageArtifactBytes(value)
	if e != nil {
		return stageConflict("private artifact encoding failed")
	}
	if s.artifacts == nil {
		s.artifacts = map[string]string{}
	}
	digest := stageDigest(value)
	if previous, known := s.artifacts[name]; known {
		current, e := s.store.readArtifact(s.operationID, name, stageMaxArtifactBytes)
		if e != nil || stageBytesDigest(current) != previous {
			return stageConflict("private artifact changed after publication")
		}
		if digest == previous {
			return nil
		}
	}
	// Retain each referenced manifest version, so the initial intent remains
	// inspectable after later phase receipts legitimately replace active aliases.
	version := strings.TrimSuffix(name, ".json") + "-" + strings.TrimPrefix(digest, "sha256:") + ".json"
	if e = s.store.writeArtifact(s.operationID, version, b, true); e != nil {
		return e
	}
	_, replace := s.artifacts[name]
	if e = s.store.writeArtifact(s.operationID, name, b, !replace); e != nil {
		return e
	}
	s.artifacts[name] = digest
	return nil
}

func (s *LocalStage) prepareStageRuntime(ctx context.Context) error {
	paths := map[string]string{}
	for name, tool := range s.runner.Tools {
		if name != "docker_driver" {
			paths[name] = tool.Path
		}
	}
	root := filepath.Join(s.store.operationPath(s.operationID), "runtime")
	if e := os.Mkdir(root, 0700); e != nil {
		return stageConflict("private runtime directory unavailable")
	}
	r, e := newStageRunner(ctx, root, s.endpoint.Endpoint, paths)
	if e != nil {
		return e
	}
	if stageDigest(stageTrustEnvironment(r.Environment)) != stageDigest(stageTrustEnvironment(s.runner.Environment)) {
		return stageConflict("admitted environment changed since preview")
	}
	for name, old := range stageTrustTools(s.runner.Tools) {
		fresh := r.Tools[name]
		if fresh.Path != old.Path || fresh.Digest != old.Digest {
			return stageConflict("admitted tool changed since preview")
		}
		fresh.Version = old.Version
		r.Tools[name] = fresh
	}
	r.Policy = s.runner.Policy
	driver, e := r.DockerDriver("stage", "")
	if e != nil {
		return e
	}
	s.runner = r
	s.invocation.Driver = driver
	s.invocation.UserData = filepath.Join(root, "user-data")
	s.receipt.ToolManifest = stageTrustTools(r.Tools)
	s.primaryObserved, e = readStageContainer(ctx, r, s.primary.ID)
	if e != nil {
		return e
	}
	return s.stagePhaseInputs(ctx)
}

func (s *LocalStage) stagePhaseInputs(ctx context.Context) error {
	if e := s.capture.Check(ctx); e != nil {
		return e
	}
	entries, _, e := scanStageFiles(ctx, s.capture.Path, "")
	if e != nil {
		return e
	}
	project := func(records []map[string]any) []map[string]any {
		out := []map[string]any{}
		for _, record := range records {
			if record["path"] != "." {
				out = append(out, record)
			}
		}
		return out
	}
	if stageDigest(project(entries)) != stageDigest(project(s.capture.Entries)) {
		return stageConflict("captured execution inputs changed")
	}
	for _, repo := range s.git.Repositories {
		if e = checkStageOriginalBinding(ctx, repo, nil); e != nil {
			return e
		}
	}
	primary, e := readStageContainer(ctx, s.runner, s.primary.ID)
	if e != nil {
		return e
	}
	if !primary.Running || stageContainerBindingDigest(primary) != stageContainerBindingDigest(s.primaryObserved) {
		return stageConflict("primary changed during stage")
	}
	return nil
}

func (s *LocalStage) executeStage(ctx context.Context, homeObservation stageVolumeDetails) (LifecycleResult, error) {
	fail := func(reason string, e error) (LifecycleResult, error) { return s.stagePartial(reason, e) }
	imageTarget := &StageResource{Kind: "image", DaemonID: s.endpoint.DaemonID}
	if _, ok := s.config["initializeCommand"]; ok {
		s.receipt.ActionInventory = append(s.receipt.ActionInventory, StageEffect{Action: "host_initialize", Target: StageTarget{Resource: s.result.Candidate}, State: "attempted"})
	}
	// The build/UID-extension target is a durable conditional intent. Native
	// image observation does not by itself claim a build command was performed.
	s.receipt.ActionInventory = append(s.receipt.ActionInventory, StageEffect{Action: "build", Target: StageTarget{Resource: imageTarget}, State: "planned"}, StageEffect{Action: "create_container", Target: StageTarget{Resource: s.result.Candidate}, State: "attempted"})
	if e := s.store.Advance("candidate_resources", "official_up", "attempted"); e != nil {
		return fail("resource phase intent failed", e)
	}
	if e := s.store.saveReceipt(s.receipt, false); e != nil {
		return fail("resource phase intent publication failed", e)
	}
	argv, e := s.invocation.Arguments("up")
	if e != nil {
		return fail("resource phase arguments failed", e)
	}
	s.runner.InitializationLabels = s.initializeLabels
	s.runner.InitializationBudget = 600 * time.Second
	out, e := s.runner.Run(ctx, "candidate_resources", "devcontainer", argv, nil, 1800*time.Second)
	if e != nil {
		return fail("candidate resource phase failed", e)
	}
	var up struct {
		Outcome, ContainerID, RemoteUser, RemoteWorkspaceFolder string
		Configuration, MergedConfiguration                      map[string]any
	}
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	d.UseNumber()
	if e = decodeDockerRecord(d, &up); e != nil || d.Decode(new(any)) != io.EOF || up.Outcome != "success" || !fullID(up.ContainerID) || up.ContainerID == s.primary.ID || up.RemoteWorkspaceFolder != s.workspace || up.RemoteUser == "" || up.MergedConfiguration == nil {
		return fail("official resource outcome invalid", stageConflict("official up identity or configuration response invalid"))
	}
	if e = s.stagePhaseInputs(ctx); e != nil {
		return fail("inputs changed during resource creation", e)
	}
	candidate, e := readStageContainer(ctx, s.runner, up.ContainerID)
	if e != nil {
		return fail("candidate observation failed", e)
	}
	if e = s.bindStageCandidate(candidate); e != nil {
		return fail("candidate resource binding failed", e)
	}
	image, e := readStageImage(ctx, s.runner, candidate.ImageID)
	if e != nil {
		return fail("candidate immutable image observation failed", e)
	}
	s.result.Candidate = stageContainerResource(inspection{ID: candidate.ID, Name: candidate.Name}, s.endpoint.DaemonID)
	s.receipt.Candidate = s.result.Candidate
	imageTarget.ID = stageString(image.ID)
	for i := range s.receipt.ActionInventory {
		effect := &s.receipt.ActionInventory[i]
		if effect.Action == "create_container" {
			effect.Target.Resource = s.result.Candidate
		}
		if effect.State == "attempted" {
			effect.State = "observed"
		}
	}
	s.effectiveConfig = up.MergedConfiguration
	s.receipt.EffectiveConfigDigest = stageDigest(up.MergedConfiguration)
	s.receipt.ImageMetadataDigest = stageDigest(image.Metadata)
	if e = s.store.Advance("candidate_resources", "official_up", "observed"); e != nil {
		return fail("resource observation journal failed", e)
	}
	if e = s.store.saveReceipt(s.receipt, false); e != nil {
		return fail("resource observation receipt failed", e)
	}
	if e = checkStageImageMounts(image, s.primaryObserved.Mounts); e != nil {
		return fail("replacement mount preflight failed", e)
	}
	if e = checkStageCandidateVolumes(candidate.Mounts, s.runner.Policy); e != nil {
		return fail("candidate mount preflight failed", e)
	}
	if e = s.admitMergedDependencies(ctx, up.MergedConfiguration); e != nil {
		return fail("lifecycle execution dependency capability unsupported", e)
	}
	if stageAnchorageHostInitializer(s.config) {
		facts, digest, e := readStageHostFacts(filepath.Join(s.capture.Path, ".devcontainer", "host.env"))
		if e != nil {
			return fail("fresh initializer host facts invalid", e)
		}
		generated := StageOutputEntry{Path: filepath.Join(s.capture.Path, ".devcontainer", "host.env"), Origin: "generated_E", Size: int64(len(facts)), Digest: digest, ProducerOperationID: stageString(s.operationID), ProducerInputDigest: stageString(s.fingerprint), AdmissionOperationID: s.operationID, AdmissionInputDigest: s.fingerprint}
		target := &StageCloneOutputTarget{Kind: "clone_output", ClonePath: s.clonePath, RelativePath: ".devcontainer/host.env", Digest: digest, ProducerOperationID: s.operationID}
		s.receipt.ActionInventory = append(s.receipt.ActionInventory, StageEffect{Action: "publish_candidate_output", Target: StageTarget{Output: target}, State: "attempted"})
		s.receipt.OutputInventory = append(s.receipt.OutputInventory, generated)
		if e = s.store.Advance("candidate_resources", "publish_candidate_output", "attempted"); e != nil {
			return fail("candidate facts intent failed", e)
		}
		if e = s.store.saveReceipt(s.receipt, false); e != nil {
			return fail("candidate facts intent publication failed", e)
		}
		if e = publishStageHostFacts(s.clonePath, facts); e != nil {
			return fail("candidate facts publication failed", e)
		}
		published := generated
		published.Path = filepath.Join(s.clonePath, ".devcontainer", "host.env")
		published.Origin = "published_W"
		s.receipt.OutputInventory = append(s.receipt.OutputInventory, published)
		s.receipt.ActionInventory[len(s.receipt.ActionInventory)-1].State = "observed"
		if e = s.store.Advance("candidate_resources", "publish_candidate_output", "observed"); e != nil {
			return fail("candidate facts observation failed", e)
		}
		if e = s.store.saveReceipt(s.receipt, false); e != nil {
			return fail("candidate facts receipt failed", e)
		}
	}
	if s.originalFacts != nil {
		s.receipt.OutputInventory = append(s.receipt.OutputInventory, StageOutputEntry{Path: filepath.Join(s.capture.Source, ".devcontainer", "host.env"), Origin: "admitted_A", Size: int64(len(s.originalFacts)), Digest: s.originalFactsDigest, AdmissionOperationID: s.operationID, AdmissionInputDigest: s.fingerprint})
	}
	paths := s.executionPaths
	before, e := stageExecutionDigest(ctx, s.clonePath, paths)
	if e != nil {
		return fail("candidate execution inventory failed", e)
	}
	s.receipt.ActionInventory = append(s.receipt.ActionInventory, StageEffect{Action: "container_hooks", Target: StageTarget{Resource: s.result.Candidate}, State: "attempted"})
	if e = s.store.Advance("candidate_hooks", "container_hooks", "attempted"); e != nil {
		return fail("hook intent failed", e)
	}
	if e = s.store.saveReceipt(s.receipt, false); e != nil {
		return fail("hook intent publication failed", e)
	}
	s.invocation.ContainerID = candidate.ID
	argv, e = s.invocation.Arguments("run-user-commands")
	if e != nil {
		return fail("hook arguments failed", e)
	}
	_, e = s.runner.Run(ctx, "container_hooks", "devcontainer", argv, nil, 600*time.Second)
	if e != nil {
		return fail("candidate container hooks failed", e)
	}
	if e = s.stagePhaseInputs(ctx); e != nil {
		return s.stageUnverified(ctx, "stale_candidate_inputs", candidate.ImageID, before, e)
	}
	after, e := stageExecutionDigest(ctx, s.clonePath, paths)
	if e != nil || before != after {
		return s.stageUnverified(ctx, "stale_candidate_inputs", candidate.ImageID, before, e)
	}
	s.receipt.ActionInventory[len(s.receipt.ActionInventory)-1].State = "observed"
	if e = s.store.Advance("candidate_hooks", "container_hooks", "observed"); e != nil {
		return fail("hook observation journal failed", e)
	}
	if e = s.store.saveReceipt(s.receipt, false); e != nil {
		return fail("hook observation receipt failed", e)
	}
	if e = s.store.Advance("verifying", "", "observed"); e != nil {
		return fail("verification transition failed", e)
	}
	policy := "smoke"
	if candidate.Healthcheck {
		policy = "health"
		if s.options.Smoke {
			policy = "health_and_smoke"
		}
	}
	s.result.Verification = &StageVerification{Policy: policy, Status: "not_requested", BindingMode: "cooperative_quiescence", Quiescence: "caller_acknowledged", PreInputDigest: before, PostInputDigest: stageString(after), ImageID: candidate.ImageID, InputDigest: before, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	s.receipt.Verification = s.result.Verification
	if candidate.Healthcheck {
		if e = s.awaitStageHealth(ctx, candidate.ID); e != nil {
			return s.stageUnverified(ctx, "failed_health", candidate.ImageID, before, e)
		}
	}
	if !candidate.Healthcheck && !s.options.Smoke {
		return s.stageUnverified(ctx, "missing_smoke", candidate.ImageID, before, nil)
	}
	if s.options.Smoke {
		smoke, e := readStageTrustedSmoke(s.capture.Path, s.smokeDigest)
		if e != nil {
			return s.stageUnverified(ctx, "failed_smoke", candidate.ImageID, before, e)
		}
		s.receipt.ActionInventory = append(s.receipt.ActionInventory, StageEffect{Action: "smoke", Target: StageTarget{Resource: s.result.Candidate}, State: "attempted"})
		if e = s.store.Advance("verifying", "smoke", "attempted"); e != nil {
			return fail("smoke intent failed", e)
		}
		if e = s.store.saveReceipt(s.receipt, false); e != nil {
			return fail("smoke intent publication failed", e)
		}
		out, e = s.runner.Run(ctx, "smoke", "docker", []string{"exec", "--interactive", "--user", up.RemoteUser, "--workdir", s.workspace, candidate.ID, "bash", "-s", "--", s.workspace}, bytes.NewReader(smoke), 600*time.Second)
		if s.options.Diagnostics != nil {
			fmt.Fprintf(s.options.Diagnostics, "%s%s", terminal.Escape(string(out.Stdout)), terminal.Escape(string(out.Stderr)))
		}
		if e != nil {
			return s.stageUnverified(ctx, "failed_smoke", candidate.ImageID, before, e)
		}
		s.result.Verification.OutputDigest = stageString(out.OutputDigest)
		s.receipt.ActionInventory[len(s.receipt.ActionInventory)-1].State = "observed"
		if e = s.store.Advance("verifying", "smoke", "observed"); e != nil {
			return fail("smoke observation journal failed", e)
		}
	}
	if e = s.stagePhaseInputs(ctx); e != nil {
		return s.stageUnverified(ctx, "stale_candidate_inputs", candidate.ImageID, before, e)
	}
	after, e = stageExecutionDigest(ctx, s.clonePath, paths)
	if e != nil || after != before {
		return s.stageUnverified(ctx, "stale_candidate_inputs", candidate.ImageID, before, e)
	}
	s.result.Verification.Status = "passed"
	s.result.Verification.PostInputDigest = stageString(after)
	s.result.Verification.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if e = s.persistStageVerification(ctx, candidate, image, homeObservation, paths, up.RemoteUser, after); e != nil {
		return fail("verified input persistence failed", e)
	}
	if e = s.store.Advance("staged_verified", "", "observed"); e != nil {
		return fail("verified transition failed", e)
	}
	s.result.Status = "staged_verified"
	s.result.Phase = "staged_verified"
	s.result.Effects = s.receipt.ActionInventory
	s.receipt.CloneRepositories = s.result.Repositories
	if e = s.store.saveReceipt(s.receipt, false); e != nil {
		return fail("verified receipt publication failed", e)
	}
	return s.Preview(), nil
}

func (s *LocalStage) bindStageCandidate(candidate stageContainerDetails) error {
	if !candidate.Running || candidate.Name != "/whr-anchorage-staging" || candidate.Labels["anchorage-staging"] != "1" || candidate.Labels["workharbor.stage.operation"] != s.operationID {
		return stageConflict("candidate identity does not match durable intent")
	}
	observation := CandidateHomeObservation{DaemonID: s.endpoint.DaemonID, Endpoint: s.endpoint.Endpoint, PrimaryID: s.primary.ID, CandidateID: candidate.ID, CandidateName: "whr-anchorage-staging", Labels: candidate.Labels, Mounts: []CandidateHomeMount{}}
	workspace := 0
	for _, m := range candidate.Mounts {
		if m.Type == "bind" && m.Source == s.clonePath && m.Destination == s.workspace && m.RW {
			workspace++
		}
		if m.Type == "volume" {
			observation.Mounts = append(observation.Mounts, CandidateHomeMount{Type: m.Type, Name: m.Name, Destination: m.Destination, ReadOnly: !m.RW})
		}
	}
	if workspace != 1 {
		return stageConflict("candidate workspace mount mismatch")
	}
	_, e := s.homeIntent.Bind(observation)
	return e
}

func (s *LocalStage) awaitStageHealth(parent context.Context, id string) error {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	for {
		candidate, e := readStageContainer(ctx, s.runner, id)
		if e != nil {
			return e
		}
		if !candidate.Running {
			return stageConflict("candidate stopped before health verification")
		}
		if candidate.Health != nil {
			if *candidate.Health == "healthy" {
				return nil
			}
			if *candidate.Health == "unhealthy" {
				return stageConflict("candidate healthcheck failed")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *LocalStage) stageUnverified(ctx context.Context, reason, image, digest string, cause error) (LifecycleResult, error) {
	if s.result.Verification == nil {
		s.result.Verification = &StageVerification{Policy: "smoke", Status: "stale", BindingMode: "cooperative_quiescence", Quiescence: "caller_acknowledged", PreInputDigest: digest, ImageID: image, InputDigest: digest, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	}
	s.result.Verification.Status = "failed"
	if reason == "missing_smoke" {
		s.result.Verification.Status = "missing"
	}
	if reason == "stale_candidate_inputs" {
		s.result.Verification.Status = "stale"
	}
	s.receipt.Verification = s.result.Verification
	if ctx.Err() != nil {
		cause = ctx.Err()
	}
	if cause != nil {
		for _, effect := range s.receipt.ActionInventory {
			if effect.State == "attempted" || effect.State == "uncertain" {
				return s.stagePartial(reason, cause)
			}
		}
	}
	s.result.Status = "staged_unverified"
	s.result.UnverifiedReason = stageString(reason)
	s.result.Phase = "staged_unverified"
	s.receipt.UnverifiedReason = s.result.UnverifiedReason
	rows, e := s.store.Records()
	if e == nil && len(rows) > 0 {
		last := rows[len(rows)-1]
		if last.Observation == "attempted" {
			for i := range s.receipt.ActionInventory {
				if s.receipt.ActionInventory[i].State == "attempted" {
					s.receipt.ActionInventory[i].State = "observed"
				}
			}
			_ = s.store.Advance(last.State, last.Action, "observed")
		}
		if last.State == "candidate_hooks" {
			_ = s.store.Advance("verifying", "", "observed")
		}
	}
	if e = s.store.Advance("staged_unverified", "", "observed"); e != nil {
		return s.stagePartial("unverified transition failed", e)
	}
	s.result.Effects = s.receipt.ActionInventory
	s.result.Recovery = []StageRecoveryStep{{Action: "remove_candidate_with_guarded_rm", Instruction: "Remove the candidate and its fresh home with guarded local rm; preserve the retained clone."}, {Action: "run_stage_with_smoke", Instruction: "Then start a fresh stage --smoke with fresh source trust and confirmation."}}
	if e = s.store.saveReceipt(s.receipt, false); e != nil {
		return s.stagePartial("unverified receipt publication failed", e)
	}
	err := exitcode.Errorf(exitcode.PathPartialFailure, "local stage %s", stageFailureMessage(reason, cause))
	err.Result = s.Preview()
	return s.Preview(), err
}

func (s *LocalStage) persistStageVerification(ctx context.Context, candidate stageContainerDetails, image stageImageDetails, home stageVolumeDetails, paths []string, user, executionDigest string) error {
	primary := stagePrimaryConfiguration{Source: s.capture.Source, Workspace: s.workspace, Home: s.home, HomeDestination: s.homeDestination, Name: strings.TrimPrefix(s.primary.Name, "/"), Hostname: s.primaryObserved.Hostname, RemoteUser: user, ContainerUser: s.primaryObserved.User, Mounts: []any{}, RunArgs: []any{}, ContainerEnv: map[string]any{}, RemoteEnv: map[string]any{}, OverrideCommand: true}
	for _, m := range s.primaryObserved.Mounts {
		if m.Type == "bind" && m.Source == s.capture.Source && m.Destination == s.workspace {
			continue
		}
		fields := []string{"type=" + m.Type, "source=" + m.Source, "target=" + m.Destination}
		if m.Type == "volume" {
			fields[1] = "source=" + m.Name
		}
		if !m.RW {
			fields = append(fields, "readonly")
		}
		primary.Mounts = append(primary.Mounts, renderStageMount(fields...))
	}
	for _, raw := range s.primaryObserved.Environment {
		k, v, ok := strings.Cut(raw, "=")
		if !ok {
			return stageConflict("primary native environment entry unsupported")
		}
		if _, duplicate := primary.ContainerEnv[k]; duplicate {
			return stageConflict("primary duplicate native environment unsupported")
		}
		primary.ContainerEnv[k] = v
	}
	if remote, ok := s.config["remoteEnv"].(map[string]any); ok {
		primary.RemoteEnv = remote
	}
	if override, ok := s.config["overrideCommand"].(bool); ok {
		primary.OverrideCommand = override
	}
	primary.RunArgs = []any{"--name", primary.Name, "--hostname", primary.Hostname}
	persistedSnapshots := append([]stageGitSnapshot{}, s.git.Repositories...)
	for i := range persistedSnapshots {
		persistedSnapshots[i].Index = nil
	}
	s.settings = stageSettings{SourceMetadataDigest: stageDigest(s.sourceMetadata), ConfigRelativePath: s.configRelative, PrimaryConfigDigest: stageDigest(primary), GitSnapshotsDigest: stageDigest(persistedSnapshots), HomePlanDigest: stageDigest(s.homeIntent.Plan()), HomeObservationDigest: stageDigest(home), PrimaryObservationDigest: stageDigest(s.primaryObserved), CandidateObservationDigest: stageDigest(candidate), AnchorageFacts: stageAnchorageHostInitializer(s.config), SmokeRequested: s.options.Smoke, SmokeDigest: s.smokeDigest, ExecutionPaths: paths}
	original, e := stageExecutionDigest(ctx, s.capture.Source, paths)
	if e != nil {
		return e
	}
	s.execution = stageExecutionBinding{SettingsDigest: stageDigest(s.settings), CandidateDigest: executionDigest, OriginalDigest: original, GitInputs: []stageExecutionGitInput{}}
	for i, repository := range s.git.Repositories {
		current, e := readStageGitSnapshotContext(ctx, repository.ClonePath, true)
		if e != nil {
			return e
		}
		s.receipt.CandidateGitVersion = append(s.receipt.CandidateGitVersion, stageCandidateGitVersion{RepositoryPath: repository.Source, HeadOID: current.Head, Refs: current.Refs, WorkingDigest: executionDigest})
		s.result.Repositories[i].HeadOID = current.Head
		s.result.Repositories[i].Dirty = current.Dirty
		s.result.Repositories[i].UncommittedRetained = current.UncommittedRetained
		input := stageExecutionGitInput{RepositoryPath: repository.Source, Predicate: "status_success", InputDigest: stageDigest("git status succeeds")}
		if s.gitInputMode != "none" {
			input.Predicate = "literal_status_success"
			if s.gitInputMode == "known_identity" {
				input.Predicate = "known_git_identity"
			}
			input.InputDigest, e = stageConsumedGitInputDigest(ctx, repository, current, s.gitInputMode == "known_identity")
			if e != nil {
				return e
			}
		}
		s.execution.GitInputs = append(s.execution.GitInputs, input)
	}
	if e = stageCheckExecutionGit(ctx, s.execution.GitInputs, s.git.Repositories); e != nil {
		return e
	}
	for name, value := range map[string]any{"source-metadata.json": s.sourceMetadata, "source-config.json": s.config, "primary-config.json": primary, "git-snapshots.json": persistedSnapshots, "primary-observation.json": s.primaryObserved, "candidate-observation.json": candidate, "effective-config.json": s.effectiveConfig, "stage-settings.json": s.settings, "execution-binding.json": s.execution} {
		if e = s.writeStageArtifact(name, value); e != nil {
			return e
		}
	}
	s.receipt.ExecutionManifestDigest = stageDigest(s.execution)
	return nil
}
