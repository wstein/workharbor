// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestThrowawayRollbackResidualReportsObservedResources(t *testing.T) {
	for _, point := range []string{"container", "failed"} {
		t.Run(point, func(t *testing.T) {
			p, path, ports := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			state.Failure = "hooks"
			state.CleanupFailure = point
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "primary container hooks failed") {
				t.Fatalf("cause lost: %+v %v", result, err)
			}
			state = readFakePromotionState(t, path)
			if state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true {
				t.Fatal("original not restored")
			}
			if (state.Failed != nil) != (point == "failed") {
				t.Fatalf("failed replacement presence: %+v", state.Failed)
			}
			command := "whr workspace rm --local " + p.candidateObservation.ID
			instructions := ""
			for _, step := range result.Recovery {
				instructions += step.Instruction + "\n"
			}
			var coded *exitcode.Error
			if !errors.As(err, &coded) || !strings.Contains(instructions, "Candidate cleanup incomplete") || !strings.Contains(instructions, command) || !strings.Contains(coded.Remediation, command) || strings.Contains(instructions, "failed replacement retained stopped") || strings.Contains(instructions, "retain clone") {
				t.Fatalf("stale/incomplete remediation: recovery=%q error=%+v", instructions, coded)
			}
			failedID := p.newPrimary.ID
			if err = p.Close(); err != nil {
				t.Fatal(err)
			}
			endpoint, err := ports.Endpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			c := Client{Executable: ports.Tools["docker"]}
			tx, err := c.prepareRemovalAtEndpoint(context.Background(), endpoint, RemovalOptions{Target: *result.Candidate.ID}, ports.StateRoot)
			if err != nil {
				t.Fatal(err)
			}
			preview := tx.Preview()
			listed := false
			for _, item := range preview.Containers {
				if item.ID == failedID {
					listed = true
				}
			}
			if listed != (point == "failed") {
				t.Fatalf("preview invents/omits failed replacement: %+v", preview)
			}
			for _, action := range preview.Actions {
				if point == "container" && action.Target == failedID {
					t.Fatalf("preview invents absent removal: %+v", action)
				}
			}
			state.CleanupFailure = ""
			writeFakePromotionState(t, path, state)
			removed, err := tx.Apply(context.Background())
			if err != nil {
				t.Fatalf("residual retry: %+v %v", removed, err)
			}
			for _, action := range removed.Actions {
				if point == "container" && action.Target == failedID {
					t.Fatalf("result claims absent removal: %+v", action)
				}
			}
			assertNoCandidateRecords(t, ports.StateRoot)
			state = readFakePromotionState(t, path)
			if state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true || state.Candidate != nil || state.Volume != nil || state.Failed != nil {
				t.Fatalf("residual retry changed the original or left candidate resources: %+v", state)
			}
		})
	}
}

func TestThrowawayStageRemediationMatchesDisposalOutcome(t *testing.T) {
	for _, cleanupFailure := range []string{"", "home"} {
		t.Run("cleanup-"+cleanupFailure, func(t *testing.T) {
			c, opts, ports, path := fixtureStageLifecycle(t, false)
			state := readFakeStageState(t, path)
			state.CleanupFailure = cleanupFailure
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
			result, err := s.Apply(context.Background(), LifecycleConsent{Confirmed: true, TrustDigest: *s.Preview().Fingerprint, Quiescence: true})
			var coded *exitcode.Error
			if !errors.As(err, &coded) || coded.Code() != exitcode.PartialFailure || !strings.Contains(err.Error(), "missing_smoke") {
				t.Fatalf("cause lost: %+v %v", result, err)
			}
			instructions := coded.Remediation
			for _, step := range result.Recovery {
				instructions += "\n" + step.Instruction
			}
			for _, stale := range []string{"preserve the retained clone", "retained recovery ID", "writable clone; do not retry"} {
				if strings.Contains(instructions, stale) {
					t.Fatalf("stale %q after disposal: %s", stale, instructions)
				}
			}
			if cleanupFailure == "" {
				if result.RecoveryID != nil || !strings.Contains(coded.Remediation, "fresh stage") {
					t.Fatalf("completed disposal remediation: %+v %+v", result, coded)
				}
				assertNoCandidateRecords(t, ports.StateRoot)
			} else if !strings.Contains(coded.Remediation, "whr workspace rm --local ") || !strings.Contains(instructions, "Candidate cleanup incomplete") {
				t.Fatalf("unfinished disposal remediation: %+v %+v", result, coded)
			}
		})
	}
}
