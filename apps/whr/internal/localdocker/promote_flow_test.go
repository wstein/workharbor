// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureVerifiedPromotion(t *testing.T) (*LocalPromotion, string, stagePreparationPorts) {
	return fixtureVerifiedPromotionWithEdit(t, nil)
}
func fixtureVerifiedPromotionWithEdit(t *testing.T, edit func(string)) (*LocalPromotion, string, stagePreparationPorts) {
	return fixtureVerifiedPromotionPolicy(t, false, true, edit)
}
func fixtureVerifiedPromotionPolicy(t *testing.T, health, smoke bool, edit func(string)) (*LocalPromotion, string, stagePreparationPorts) {
	t.Helper()
	c, opts, ports, path := fixturePromotionLifecycle(t, health)
	opts.Smoke = smoke
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("stage producer: %+v %v", result, err)
	}
	if edit != nil {
		edit(result.Repositories[0].ClonePath)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	state := readFakePromotionState(t, path)
	state.Promotion = true
	if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	writeFakePromotionState(t, path, state)
	var diagnostic bytes.Buffer
	p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64), Diagnostics: &diagnostic}, ports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p, path, ports
}
func TestLocalPromoteUsesVerifiedImage(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	before := readFakePromotionState(t, path)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("promote: %+v %v", result, err)
	}
	state := readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["ImageID"] != "sha256:"+strings.Repeat("d", 64) || state.Primary["ConfigImage"] != "sha256:"+strings.Repeat("d", 64) || state.Primary["Id"] == before.Primary["Id"] || state.Candidate != nil || state.Volume != nil {
		t.Fatalf("native promotion resources: %+v", state)
	}
	for _, phase := range state.Phases {
		var c, o string
		for i, v := range phase {
			if i+1 < len(phase) {
				if v == "--config" {
					c = phase[i+1]
				}
				if v == "--override-config" {
					o = phase[i+1]
				}
			}
		}
		if c != o || filepath.Base(c) != "devcontainer.json" {
			t.Fatalf("C=P=O: %q", phase)
		}
	}
	if _, err = os.Stat(p.snapshots[0].ClonePath); err != nil {
		t.Fatalf("retained clone: %v", err)
	}
}

func assertPromotionRefusal(t *testing.T, p *LocalPromotion, path string, c LifecycleConsent, code exitcode.Code, reason string) {
	t.Helper()
	before := readFakePromotionState(t, path)
	journal, _ := os.ReadFile(filepath.Join(p.store.path, "journal.json"))
	result, err := p.Apply(context.Background(), c)
	if exitcode.FromError(err) != code || err == nil || !strings.Contains(err.Error(), reason) || result.Status != "refused" {
		t.Fatalf("refusal: status=%s error=%v want=%d/%s", result.Status, err, code, reason)
	}
	after := readFakePromotionState(t, path)
	if stageDigest(before.Primary) != stageDigest(after.Primary) || stageDigest(before.Candidate) != stageDigest(after.Candidate) || stageDigest(before.Volume) != stageDigest(after.Volume) {
		t.Fatal("refusal mutated native resources")
	}
	current, _ := os.ReadFile(filepath.Join(p.store.path, "journal.json"))
	if !bytes.Equal(journal, current) {
		t.Fatal("refusal mutated journal")
	}
}
func TestLocalPromoteRequiresExplicitHomeDeletionConsent(t *testing.T) {
	for _, missing := range []string{"volumes", "force"} {
		t.Run(missing, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotion(t)
			c := LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}
			if missing == "volumes" {
				c.Volumes = false
			} else {
				c.Force = false
			}
			assertPromotionRefusal(t, p, path, c, exitcode.NeedsHuman, "explicit home deletion consent")
		})
	}
}
func TestLocalPromoteConfirmsPaneInterruption(t *testing.T) {
	for _, missing := range []string{"confirmation", "quiescence"} {
		t.Run(missing, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotion(t)
			c := LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}
			if missing == "confirmation" {
				c.Confirmed = false
			} else {
				c.Quiescence = false
			}
			assertPromotionRefusal(t, p, path, c, exitcode.NeedsHuman, "pane interruption confirmation and quiescence acknowledgement")
		})
	}
}
func TestLocalPromoteNeverBypassesProtectedHome(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	for _, raw := range state.Candidate["Mounts"].([]any) {
		m := raw.(map[string]any)
		if m["Type"] == "volume" {
			m["Name"] = "anchorage-home-protected"
		}
	}
	writeFakePromotionState(t, path, state)
	assertPromotionRefusal(t, p, path, LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}, exitcode.Conflict, "staging or temp cleanup would touch a primary home volume")
}
func TestLocalPromoteStaleReceiptRefuses(t *testing.T) {
	for _, which := range []string{"original_execution", "candidate_execution", "artifact", "wrong_trust", "git_owner_ref", "candidate_native"} {
		t.Run(which, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotion(t)
			reason := ""
			consent := LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}
			switch which {
			case "original_execution":
				if err := os.WriteFile(filepath.Join(p.receipt.OriginalSource, ".devcontainer/setup.sh"), []byte("changed original\n"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "promotion original execution inputs changed"
			case "candidate_execution":
				if err := os.WriteFile(filepath.Join(p.snapshots[0].ClonePath, ".devcontainer/setup.sh"), []byte("changed candidate\n"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "promotion candidate execution inputs changed"
			case "artifact":
				if err := os.WriteFile(filepath.Join(p.store.operationPath(p.receipt.OperationID), "source-config.json"), []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "promotion private artifact digest changed: source-config.json"
			case "wrong_trust":
				consent.TrustDigest = "sha256:" + strings.Repeat("f", 64)
				reason = "promotion source trust differs from verified receipt"
			case "git_owner_ref":
				fixtureGit(t, p.receipt.OriginalSource, "branch", "owner-added")
				reason = "original Git binding changed outside exact operation refs"
			case "candidate_native":
				s := readFakePromotionState(t, path)
				s.Candidate["User"] = "root"
				writeFakePromotionState(t, path, s)
				reason = "promotion candidate resource binding changed"
			}
			assertPromotionRefusal(t, p, path, consent, exitcode.Conflict, reason)
		})
	}
}
func TestLocalPromotePartialFailureInventory(t *testing.T) {
	for _, tc := range []struct {
		failure, reason, action string
		candidate, home         bool
	}{
		{"up", "primary resource phase failed", "create_primary", true, true},
		{"wrong_image", "primary native binding failed", "create_primary", true, true},
		{"hooks", "primary container hooks failed", "container_hooks", true, true},
		{"smoke", "primary smoke verification failed", "smoke", true, true},
		{"candidate_rm", "candidate removal failed or uncertain", "remove_candidate", true, true},
		{"home_rm", "candidate home removal failed or uncertain", "remove_home", false, true},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			state.Failure = tc.failure
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.PartialFailure || err == nil || !strings.Contains(err.Error(), tc.reason) || result.Status != "partial" || len(result.Recovery) == 0 {
				t.Fatalf("partial: %+v %v", result, err)
			}
			if tc.failure == "smoke" && (result.Verification == nil || result.Verification.Status != "failed") {
				t.Fatalf("failed primary smoke reported candidate success: %+v", result.Verification)
			}
			found := false
			for _, e := range result.Effects {
				if e.Action == tc.action && e.State == "uncertain" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing exact uncertain action %s: %+v", tc.action, result.Effects)
			}
			state = readFakePromotionState(t, path)
			if (state.Candidate != nil) != tc.candidate || (state.Volume != nil) != tc.home {
				t.Fatalf("retained resources: %+v", state)
			}
			for _, a := range state.Calls {
				if len(a) > 1 && a[0] == "volume" && a[1] == "rm" && a[len(a)-1] == "anchorage-home-owner" {
					t.Fatal("removed original primary home")
				}
			}
			if _, err = os.Stat(p.snapshots[0].ClonePath); err != nil {
				t.Fatal("lost retained clone")
			}
			if _, err = p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "promotion transaction already used") {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}
func TestLocalPromotePreservesPrimaryHome(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil {
		t.Fatal(err)
	}
	state := readFakePromotionState(t, path)
	found := false
	for _, raw := range state.Primary["Mounts"].([]any) {
		m := raw.(map[string]any)
		if m["Name"] == "anchorage-home-owner" && m["Destination"] == "/home/vscode" && m["RW"] == true {
			found = true
		}
	}
	if !found || result.Primary == nil || result.Primary.ID == nil || *result.Primary.ID != strings.Repeat("e", 64) {
		t.Fatalf("original home not preserved: %+v", state)
	}
	order := map[string]int{}
	for i, a := range state.Calls {
		if len(a) < 2 {
			continue
		}
		if a[0] == "exec" && a[len(a)-1] == p.primaryConfig.Workspace {
			order["smoke"] = i
		}
		if a[0] == "container" && a[1] == "rm" && a[len(a)-1] == p.candidateObservation.ID {
			order["candidate"] = i
		}
		if a[0] == "volume" && a[1] == "rm" {
			if a[len(a)-1] == "anchorage-home-owner" {
				t.Fatal("original home removal")
			}
			order["home"] = i
		}
	}
	if !(order["smoke"] < order["candidate"] && order["candidate"] < order["home"]) {
		t.Fatalf("verification then candidate then home cleanup: %v", order)
	}
}

func TestLocalPromoteTransfersResultsBeforeReplacingPrimary(t *testing.T) {
	var oid string
	p, path, _ := fixtureVerifiedPromotionWithEdit(t, func(clone string) {
		if err := os.WriteFile(filepath.Join(clone, "result"), []byte("candidate result\n"), 0600); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, clone, "add", "result")
		fixtureGit(t, clone, "commit", "-m", "candidate result")
		oid = strings.TrimSpace(fixtureGit(t, clone, "rev-parse", "HEAD"))
	})
	original := p.receipt.OriginalSource
	head := fixtureGit(t, original, "rev-parse", "HEAD")
	index, _ := os.ReadFile(filepath.Join(original, ".git", "index"))
	if len(p.Preview().ResultRefs) == 0 {
		t.Fatal("missing planned results")
	}
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil {
		t.Fatal(err)
	}
	if fixtureGit(t, original, "rev-parse", "HEAD") != head {
		t.Fatal("moved owner HEAD")
	}
	current, _ := os.ReadFile(filepath.Join(original, ".git", "index"))
	if !bytes.Equal(index, current) {
		t.Fatal("changed original index")
	}
	if _, err = os.Stat(filepath.Join(original, "result")); !os.IsNotExist(err) {
		t.Fatal("activated result in original working tree")
	}
	for _, ref := range result.ResultRefs {
		if ref.State != "created" || ref.OID != oid || strings.TrimSpace(fixtureGit(t, original, "rev-parse", ref.TargetRef)) != oid {
			t.Fatalf("result ref: %+v", ref)
		}
	}
	rows, err := p.store.Records()
	if err != nil {
		t.Fatal(err)
	}
	refsObserved, stopAttempt := 0, 0
	for _, row := range rows {
		if row.Action == "create_result_refs" && row.Observation == "observed" {
			refsObserved = row.Sequence
		}
		if row.Action == "stop_primary" && row.Observation == "attempted" {
			stopAttempt = row.Sequence
		}
	}
	if refsObserved == 0 || stopAttempt <= refsObserved {
		t.Fatalf("refs must precede primary stop: %d %d", refsObserved, stopAttempt)
	}
	state := readFakePromotionState(t, path)
	if state.Primary == nil || state.Candidate != nil {
		t.Fatal("promotion lifecycle incomplete")
	}
}

func TestLocalPromoteEnvironmentCannotRebindTrust(t *testing.T) {
	c, opts, ports, path := fixturePromotionLifecycle(t)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("stage: %v", err)
	}
	journalPath := filepath.Join(s.store.path, "journal.json")
	before, _ := os.ReadFile(journalPath)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANG", "whr-changed-admitted-language")
	p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: "primary"}, ports)
	if p != nil {
		p.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "promotion admitted substitution environment or source trust changed") {
		t.Fatalf("environment trust: %v", err)
	}
	after, _ := os.ReadFile(journalPath)
	if !bytes.Equal(before, after) {
		t.Fatal("refusal changed durable state")
	}
	state := readFakePromotionState(t, path)
	if state.Primary["Id"] != strings.Repeat("a", 64) || state.Primary["Running"] != true || state.Candidate == nil || state.Volume == nil {
		t.Fatal("trust refusal altered resources")
	}
}

func TestLocalPromoteRechecksInputsAfterHomeCleanup(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	s := readFakePromotionState(t, path)
	s.Failure = "cleanup_input_drift"
	writeFakePromotionState(t, path, s)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || err == nil || !strings.Contains(err.Error(), "final promotion binding changed") || result.Status != "partial" {
		t.Fatalf("post-cleanup drift: status=%s error=%v", result.Status, err)
	}
	if result.Verification == nil || result.Verification.Status != "stale" {
		t.Fatalf("drift retained a passing verification: %+v", result.Verification)
	}
	s = readFakePromotionState(t, path)
	if s.Primary == nil || s.Candidate != nil || s.Volume != nil {
		t.Fatal("completed effects missing from inventory")
	}
	for _, action := range []string{"create_primary", "remove_candidate", "remove_home"} {
		observed := false
		for _, e := range result.Effects {
			if e.Action == action && e.State == "observed" {
				observed = true
			}
		}
		if !observed {
			t.Fatalf("lost observed %s effect", action)
		}
	}
}

func TestLocalPromoteOriginalMustMatchVerifiedExecution(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	if err := os.WriteFile(filepath.Join(p.snapshots[0].ClonePath, ".devcontainer", "setup.sh"), []byte("verified different hook\n"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := stageExecutionDigest(context.Background(), p.snapshots[0].ClonePath, p.settings.ExecutionPaths)
	if err != nil {
		t.Fatal(err)
	}
	// Model a coherent stage ticket for a candidate execution version that is not
	// present in the original. Updating its private hash chain must not activate it.
	p.execution.CandidateDigest = digest
	p.receipt.Verification.PreInputDigest = digest
	p.receipt.Verification.PostInputDigest = stageString(digest)
	p.receipt.Verification.InputDigest = digest
	data, err := stageArtifactBytes(p.execution)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.store.writeArtifact(p.receipt.OperationID, "execution-binding.json", data, false); err != nil {
		t.Fatal(err)
	}
	p.receipt.ExecutionManifestDigest = promotionBytesDigest(data)
	if err = p.store.saveReceipt(&p.receipt, false); err != nil {
		t.Fatal(err)
	}
	assertPromotionRefusal(t, p, path, LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}, exitcode.Conflict, "promotion original execution inputs differ from verified candidate")
}

func TestLocalPromoteHonorsVerifiedHealthAndSmokePolicy(t *testing.T) {
	for _, smoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "health_only", true: "health_and_smoke"}[smoke], func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotionPolicy(t, true, smoke, nil)
			result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
			if err != nil || result.Status != "promoted" {
				t.Fatalf("health promotion: %s %v", result.Status, err)
			}
			want := "health"
			if smoke {
				want = "health_and_smoke"
			}
			if result.Verification == nil || result.Verification.Policy != want || result.Verification.Status != "passed" {
				t.Fatalf("verification policy: %+v", result.Verification)
			}
			state := readFakePromotionState(t, path)
			runs := 0
			for _, a := range state.Calls {
				if len(a) > 0 && a[0] == "exec" && strings.Contains(strings.Join(a, " "), strings.Repeat("e", 64)+" bash -s") {
					runs++
				}
			}
			if (runs > 0) != smoke {
				t.Fatalf("implicit or missing primary smoke: %d requested=%v", runs, smoke)
			}
		})
	}
}
