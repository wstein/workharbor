// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func assertNoCandidateRecords(t *testing.T, root string) {
	t.Helper()
	daemons, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, daemon := range daemons {
		entries, err := os.ReadDir(filepath.Join(root, daemon.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Name() != "lock" {
				t.Errorf("candidate artifact remains: %s/%s", daemon.Name(), entry.Name())
			}
		}
	}
}

func TestThrowawayPromoteNeedsOnlyNormalConfirmation(t *testing.T) {
	p, path, ports := fixtureVerifiedPromotion(t)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("normal confirmed promote: %+v %v", result, err)
	}
	state := readFakePromotionState(t, path)
	if state.Candidate != nil || state.Volume != nil || state.Retained != nil {
		t.Fatalf("candidate resources remain: %+v", state)
	}
	if state.Primary == nil || state.Primary["Running"] != true {
		t.Fatal("replacement primary unavailable")
	}
	assertNoCandidateRecords(t, ports.StateRoot)
	if _, err := os.Lstat(p.snapshots[0].ClonePath); !os.IsNotExist(err) {
		t.Fatalf("clone remains: %v", err)
	}
}

func TestThrowawayPromoteRollbackDisposesCandidate(t *testing.T) {
	p, path, ports := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	state.Failure = "hooks"
	writeFakePromotionState(t, path, state)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "primary container hooks failed") {
		t.Fatalf("hook failure lost: %+v %v", result, err)
	}
	state = readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true {
		t.Fatal("original not restored")
	}
	if state.Candidate != nil || state.Volume != nil || state.Failed != nil {
		t.Fatalf("rollback retained candidate artifacts: %+v", state)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
}

func TestThrowawayFailedStageDisposesArtifacts(t *testing.T) {
	for _, failure := range []string{"missing_smoke", "failed_smoke", "failed_health"} {
		t.Run(failure, func(t *testing.T) {
			c, opts, ports, path := fixtureStageLifecycle(t, failure == "failed_health")
			opts.Smoke = failure == "failed_smoke"
			state := readFakeStageState(t, path)
			if failure == "failed_smoke" {
				state.SmokeExit = 7
			}
			if failure == "failed_health" {
				state.HealthStatus = "unhealthy"
			}
			raw, err := stageArtifactBytes(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), failure) {
				t.Fatalf("stage failure lost: %+v %v", result, err)
			}
			state = readFakeStageState(t, path)
			if state.Candidate != nil || state.Volume != nil {
				t.Fatalf("failed stage retained candidate resources: %+v", state)
			}
			if state.Primary["Running"] != true {
				t.Fatal("primary stopped")
			}
			assertNoCandidateRecords(t, ports.StateRoot)
		})
	}
}

func TestThrowawayRemoveDisposesBoundCandidateWithoutVolumeFlags(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ports.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *staged.Candidate.ID}, ports.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	preview := tx.Preview()
	if preview.PreserveHomeVolume || len(preview.Volumes) != 1 || len(preview.Actions) != 4 {
		t.Fatalf("disposal preview: %+v", preview)
	}
	result, err := tx.Apply(context.Background())
	if err != nil {
		t.Fatalf("remove: %+v %v", result, err)
	}
	state := readFakeStageState(t, path)
	if state.Candidate != nil || state.Volume != nil || state.Primary["Running"] != true {
		t.Fatalf("remove did not isolate candidate resources")
	}
	assertNoCandidateRecords(t, ports.StateRoot)
}

func TestThrowawayRemoveFinishesResidualAfterContainerDeletion(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	state := readFakeStageState(t, path)
	state.Candidate = nil
	data, err := stageArtifactBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ports.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
	if err != nil {
		t.Fatalf("receipt-bound residual removal: %v", err)
	}
	defer tx.Close()
	if _, err = tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state = readFakeStageState(t, path); state.Volume != nil || state.Primary["Running"] != true {
		t.Fatal("residual cleanup did not isolate the candidate")
	}
	assertNoCandidateRecords(t, ports.StateRoot)
}

func TestThrowawayTreeCleanupHandlesReadOnlyCloneAndSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := openStageStore(root, "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	op := strings.Repeat("b", 32)
	if err = store.Begin(op, "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	base := store.operationPath(op)
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, []byte("owner data"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "file"), []byte("work"), 0400); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(workspace, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(workspace, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workspace, 0700) })
	if err = os.WriteFile(filepath.Join(base, "receipt.json"), []byte("private receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.discardCandidateFiles(op); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "owner data" {
		t.Fatalf("outside candidate changed: %q %v", data, err)
	}
	assertNoCandidateRecords(t, root)
}

func TestThrowawayRemovalFailureKeepsRecordsAndReportsAttempts(t *testing.T) {
	for _, point := range []string{"container", "home", "records"} {
		t.Run(point, func(t *testing.T) {
			c, opts, ports, path := fixtureStageLifecycle(t, false)
			opts.Smoke = true
			s, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if err != nil {
				t.Fatal(err)
			}
			op := s.store.operationPath(s.operationID)
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			state := readFakeStageState(t, path)
			state.CleanupFailure = point
			state.StateRoot = ports.StateRoot
			state.RequireCleanupRecords = true
			data, err := stageArtifactBytes(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			endpoint, err := ports.Endpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
			if err != nil {
				t.Fatal(err)
			}
			if point == "records" {
				if err = os.Chmod(op, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(op, 0700) })
			}
			removed, err := tx.Apply(context.Background())
			reason := "phase command failed: remove_candidate"
			if point == "home" {
				reason = "phase command failed: remove_home"
			}
			if point == "records" {
				reason = "receipt directory ownership or mode"
			}
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), reason) {
				t.Fatalf("cleanup cause: %v want 73/%s", err, reason)
			}
			if point == "container" {
				for _, action := range removed.Actions {
					if action.Kind == "volume" && action.Attempted {
						t.Fatal("home reported attempted before container was removed")
					}
				}
			}
			if _, err = os.Stat(filepath.Join(op, "receipt.json")); err != nil {
				t.Fatalf("receipt removed before resources/tree: %v", err)
			}
			if _, err = os.Stat(filepath.Join(filepath.Dir(op), "journal.json")); err != nil {
				t.Fatalf("journal lost: %v", err)
			}
			if err = os.Chmod(op, 0700); err != nil {
				t.Fatal(err)
			}
			state = readFakeStageState(t, path)
			state.CleanupFailure = ""
			data, err = stageArtifactBytes(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			retry, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
			if err != nil {
				t.Fatalf("cannot finish residual: %v", err)
			}
			defer retry.Close()
			if _, err = retry.Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertNoCandidateRecords(t, ports.StateRoot)
		})
	}
}

func TestThrowawayStageFailureBeforeCandidateCanBeRemoved(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	state := readFakeStageState(t, path)
	state.ResourceExit = 19
	state.CleanupFailure = "home"
	data, err := stageArtifactBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "candidate resource phase failed") || result.RecoveryID == nil {
		t.Fatalf("failed allocation: %+v %v", result, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	state = readFakeStageState(t, path)
	state.CleanupFailure = ""
	data, err = stageArtifactBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ports.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: "whr-anchorage-staging"}, ports.StateRoot)
	if err != nil {
		t.Fatalf("allocation residual by receipt name: %v", err)
	}
	defer tx.Close()
	if _, err = tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
	state = readFakeStageState(t, path)
	if state.Volume != nil || state.Primary["Running"] != true {
		t.Fatal("allocation residual remains or primary changed")
	}
}

func TestThrowawayRemovalResumesAcrossFinalRecordDeletion(t *testing.T) {
	for _, boundary := range []string{"binding_artifacts", "receipt", "operation_directory"} {
		t.Run(boundary, func(t *testing.T) {
			c, opts, ports, path := fixtureStageLifecycle(t, false)
			opts.Smoke = true
			s, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if err != nil {
				t.Fatal(err)
			}
			op := s.store.operationPath(s.operationID)
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			endpoint, err := ports.Endpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.candidate.begin(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err = tx.candidate.removeResources(context.Background()); err != nil {
				t.Fatal(err)
			}
			switch boundary {
			case "binding_artifacts":
				err = os.Remove(filepath.Join(op, "execution-binding.json"))
			case "receipt":
				err = os.Remove(filepath.Join(op, "receipt.json"))
			case "operation_directory":
				err = os.RemoveAll(op)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Close(); err != nil {
				t.Fatal(err)
			}
			resumed, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
			if err != nil {
				t.Fatalf("cannot resume %s deletion: %v", boundary, err)
			}
			defer resumed.Close()
			if _, err = resumed.Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertNoCandidateRecords(t, ports.StateRoot)
			state := readFakeStageState(t, path)
			if state.Volume != nil || state.Candidate != nil || state.Primary["Running"] != true {
				t.Fatal("final cleanup retained artifacts or changed primary")
			}
		})
	}
}

func TestThrowawayEarlyCreatedCandidateResidual(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	state := readFakeStageState(t, path)
	state.UpReturnsPrimary = true
	state.CleanupFailure = "container"
	data, err := stageArtifactBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "official up identity or configuration response invalid") || result.Candidate == nil || result.Candidate.ID == nil || !fullID(*result.Candidate.ID) {
		t.Fatalf("invalid creation response must retain its cause and bind the observed cleanup full ID: result=%+v err=%v", result, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	state = readFakeStageState(t, path)
	if state.Candidate == nil {
		t.Fatal("probe did not leave the created candidate")
	}
	state.CleanupFailure = ""
	data, err = stageArtifactBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ports.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: "whr-anchorage-staging"}, ports.StateRoot)
	if err != nil {
		t.Fatalf("normal confirmed removal cannot resume receipt-bound created candidate: %v", err)
	}
	defer tx.Close()
	if _, err = tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
}

func TestThrowawayPromotionCleanupIntentFailureStaysResumable(t *testing.T) {
	p, _, ports := fixtureVerifiedPromotion(t)
	storePath := p.store.path
	injected := false
	ctx := context.WithValue(context.Background(), stageCaptureControlKey{}, stageCaptureControl{Phase: func(phase, path string) {
		if phase == "candidate-cleanup-intent" {
			injected = true
			if err := os.Chmod(path, 0500); err != nil {
				t.Fatal(err)
			}
		}
	}})
	t.Cleanup(func() { _ = os.Chmod(storePath, 0700) })
	result, err := p.Apply(ctx, LifecycleConsent{Confirmed: true, Quiescence: true})
	if !injected || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "candidate cleanup record binding failed") {
		t.Fatalf("cleanup-intent storage failure: %+v %v injected=%t", result, err, injected)
	}
	rows, err := p.store.Records()
	if err != nil || len(rows) == 0 {
		t.Fatalf("cleanup journal lost: %v", err)
	}
	if err = p.store.Begin(strings.Repeat("f", 32), "sha256:"+strings.Repeat("a", 64)); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "retained attempt requires owner reconciliation") {
		t.Fatalf("fresh stage adopted unfinished disposal: %v", err)
	}
	rows, err = p.store.Records()
	if err != nil || len(rows) == 0 {
		t.Fatalf("fresh stage removed cleanup journal: %v", err)
	}
	if err = os.Chmod(p.store.path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ports.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := (Client{Executable: ports.Tools["docker"]}).prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: p.candidateObservation.ID}, ports.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if _, err = tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
}
