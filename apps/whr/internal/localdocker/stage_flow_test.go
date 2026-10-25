//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageRequiresTrustAndConsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		consent LifecycleConsent
	}{
		{"no_trust", LifecycleConsent{Confirmed: true, Quiescence: true}},
		{"no_consent", LifecycleConsent{TrustDigest: "MATCH", Quiescence: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, opts, p, _ := fixtureStageLifecycle(t, false)
			s, e := c.prepareStage(context.Background(), opts, p)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			consent := tc.consent
			if consent.TrustDigest == "MATCH" {
				consent.TrustDigest = *s.Preview().Fingerprint
			}
			result, e := s.Apply(context.Background(), consent)
			if exitcode.FromError(e) != exitcode.NeedsHuman || !strings.Contains(e.Error(), "source trust and confirmation required") || result.Status != "refused" {
				t.Fatalf("missing decision: %+v %v", result, e)
			}
			if _, e = os.Stat(p.StateRoot); !os.IsNotExist(e) {
				t.Fatalf("decision refusal wrote persistent state: %v", e)
			}
		})
	}
}

func TestLocalStageLeavesPrimaryRunning(t *testing.T) {
	c, opts, p, statePath := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	var diagnostics bytes.Buffer
	opts.Diagnostics = &diagnostics
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result, e := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if e != nil || result.Status != "staged_verified" {
		t.Fatalf("stage: %+v %v", result, e)
	}
	state := readFakeStageState(t, statePath)
	if state.Primary["State"] != "running" || state.Candidate == nil || state.Volume == nil {
		t.Fatalf("resource state: %+v", state)
	}
	for _, args := range state.Calls {
		for _, word := range args {
			if word == "stop" || word == "rm" {
				t.Fatalf("stage altered primary lifecycle: %q", args)
			}
		}
	}
	if result.Candidate == nil || result.Candidate.ID == nil || *result.Candidate.ID == *result.Primary.ID {
		t.Fatalf("candidate identity: %+v", result)
	}
	if *result.Home.Name == "anchorage-home-owner" {
		t.Fatal("candidate exposed primary home")
	}
	if _, e = s.Apply(context.Background(), LifecycleConsent{TrustDigest: *result.Fingerprint, Confirmed: true, Quiescence: true}); exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), "transaction already used") {
		t.Fatalf("Apply replay: %v", e)
	}
}

func TestLocalStageMissingSmokeRetainsUnverifiedCandidate(t *testing.T) {
	c, opts, p, statePath := fixtureStageLifecycle(t, false)
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result, e := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(e) != exitcode.PartialFailure || !strings.Contains(e.Error(), "missing_smoke") || result.Status != "staged_unverified" || result.UnverifiedReason == nil || *result.UnverifiedReason != "missing_smoke" {
		t.Fatalf("missing smoke: %+v %v", result, e)
	}
	state := readFakeStageState(t, statePath)
	if state.Candidate == nil || state.Volume == nil || state.Primary["State"] != "running" || state.SmokeStdin != "" {
		t.Fatalf("unverified resources or implicit smoke: %+v", state)
	}
	if _, e = os.Stat(result.Repositories[0].ClonePath); e != nil {
		t.Fatalf("retained writable clone missing: %v", e)
	}
}

func TestLocalStageSmokeUsesTrustedStdin(t *testing.T) {
	c, opts, p, statePath := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if e != nil {
		t.Fatal(e)
	}
	state := readFakeStageState(t, statePath)
	if state.SmokeStdin != "printf smoke\n" {
		t.Fatalf("trusted stdin: %q", state.SmokeStdin)
	}
	want := []string{"exec", "--interactive", "--user", "vscode", "--workdir", "/workspaces/Anchorage", strings.Repeat("c", 64), "bash", "-s", "--", "/workspaces/Anchorage"}
	found := false
	for _, call := range state.Calls {
		if strings.Join(call, "\x00") == strings.Join(want, "\x00") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing exact smoke vector: %q", state.Calls)
	}
}

func TestLocalStageEscapesSmokeOutput(t *testing.T) {
	c, opts, p, _ := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	var diagnostics bytes.Buffer
	opts.Diagnostics = &diagnostics
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if e != nil {
		t.Fatal(e)
	}
	if strings.ContainsRune(diagnostics.String(), '\x1b') || !strings.Contains(diagnostics.String(), "<U+001B>[31msmoke-marker<U+000A>") {
		t.Fatalf("escaped diagnostic missing: %q", diagnostics.String())
	}
}
