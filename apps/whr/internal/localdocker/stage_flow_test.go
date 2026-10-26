//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageHomeIntentDurableBeforeNativeCreation(t *testing.T) {
	c, opts, p, statePath := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	state := readFakeStageState(t, statePath)
	state.StateRoot = p.StateRoot
	state.RequireDurableHomeIntent = true
	b, _ := json.Marshal(state)
	if e := os.WriteFile(statePath, b, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result, e := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if e != nil || result.Status != "staged_verified" {
		t.Fatalf("native create lacked bound retained nonce intent: %+v %v", result, e)
	}
}

func TestLocalStageRequiresTrustAndConsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		consent LifecycleConsent
	}{
		{"no_trust", LifecycleConsent{Confirmed: true, Quiescence: true}},
		{"no_consent", LifecycleConsent{TrustDigest: "MATCH", Quiescence: true}},
		{"no_quiescence", LifecycleConsent{TrustDigest: "MATCH", Confirmed: true}},
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

func TestLocalStageMissingSmokeDisposesUnverifiedCandidate(t *testing.T) {
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
	if state.Candidate != nil || state.Volume != nil || state.Primary["State"] != "running" || state.SmokeStdin != "" {
		t.Fatalf("unverified resources or implicit smoke: %+v", state)
	}
	if _, e = os.Stat(result.Repositories[0].ClonePath); !os.IsNotExist(e) {
		t.Fatalf("unverified clone remains: %v", e)
	}
	if result.RecoveryID != nil {
		t.Fatal("disposed attempt reports retained receipt")
	}
	assertNoCandidateRecords(t, p.StateRoot)
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

func TestLocalStageBindsRawOverrideMetadataOrigin(t *testing.T) {
	c, opts, p, _ := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw, err := os.ReadFile(s.invocation.Override)
	if err != nil {
		t.Fatal(err)
	}
	var override map[string]any
	if err = json.Unmarshal(raw, &override); err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := s.store.readArtifact(s.operationID, "source-metadata.json", 256<<10)
	if err != nil {
		t.Fatalf("missing raw source-origin evidence: %v", err)
	}
	var contribution map[string]any
	if err = json.Unmarshal(artifact, &contribution); err != nil {
		t.Fatal(err)
	}
	if stageDigest(contribution) != stageDigest(stageSourceMetadata(override)) {
		t.Fatal("metadata origin differs from raw complete stage override")
	}
	settingsRaw, err := s.store.readArtifact(s.operationID, "stage-settings.json", 256<<10)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err = json.Unmarshal(settingsRaw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["source_metadata_digest"] != stageDigest(contribution) {
		t.Fatal("source origin is not transitively receipt bound")
	}
}

func TestLocalStageRejectsChangedToolsAndSubstitutionsBeforeEffects(t *testing.T) {
	for _, kind := range []string{"environment", "tool"} {
		t.Run(kind, func(t *testing.T) {
			c, opts, p, path := fixtureStageLifecycle(t, false)
			s, err := c.prepareStage(context.Background(), opts, p)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			reason := "admitted environment changed since preview"
			if kind == "environment" {
				t.Setenv("ANCHORAGE_HOST_LABEL", "changed-after-preview")
			} else {
				reason = "admitted tool changed since preview"
				f, err := os.OpenFile(p.Tools["devcontainer"], os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.WriteString("\n// substituted after preview\n"); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), reason) || result.Status != "refused" {
				t.Fatalf("changed %s: %+v %v", kind, result, err)
			}
			if _, err = os.Lstat(p.StateRoot); !os.IsNotExist(err) {
				t.Fatal("changed trusted input created persistent state")
			}
			state := readFakeStageState(t, path)
			if state.Candidate != nil || state.Volume != nil {
				t.Fatal("changed trusted input caused allocation")
			}
		})
	}
}

func TestLocalStageFreshAfterDisposedUnverifiedRequiresFreshConsent(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	first, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := first.Apply(context.Background(), LifecycleConsent{TrustDigest: *first.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "missing_smoke") || result.Status != "staged_unverified" {
		t.Fatalf("first stage: %+v %v", result, err)
	}
	oldClone, oldHome, oldOperation := first.clonePath, *result.Home.Name, first.operationID
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	state := readFakeStageState(t, path)
	if state.Candidate != nil || state.Volume != nil || state.Primary["State"] != "running" {
		t.Fatalf("failed stage was not disposed: %+v", state)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
	opts.Smoke = true
	preview, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
	_, err = preview.Apply(context.Background(), LifecycleConsent{})
	if exitcode.FromError(err) != exitcode.NeedsHuman || err == nil || !strings.Contains(err.Error(), "source trust and confirmation required") {
		t.Fatalf("missing fresh consent: %v", err)
	}
	assertNoCandidateRecords(t, ports.StateRoot)
	preview.Close()
	next, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	result, err = next.Apply(context.Background(), LifecycleConsent{TrustDigest: *next.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("fresh stage after disposal: %+v %v", result, err)
	}
	if next.operationID == oldOperation || *result.Home.Name == oldHome || next.clonePath == oldClone {
		t.Fatal("fresh operation reused old allocation")
	}
	if _, err = os.Stat(oldClone); !os.IsNotExist(err) {
		t.Fatalf("old clone remains: %v", err)
	}
}
