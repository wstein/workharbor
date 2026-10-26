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
