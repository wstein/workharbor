//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

func TestResultRedaction(t *testing.T) {
	canaries := []string{"/home/home-canary", "user-canary", "host-canary", "ENV_CANARY=secret", "git-value-canary"}
	p, e := Build(input())
	if e != nil {
		t.Fatal(e)
	}
	s := p.Steps[0]
	raw := ApplyResult{ExitCode: 68, Applied: canaries, Remaining: canaries, Steps: []StepOutcome{{Audit: profile.StepAudit{ID: s.ID, Item: canaries[1], Kind: canaries[2], Result: "blocked", Argv: canaries}, Reason: strings.Join(canaries, " "), Backups: []BackupAudit{{Path: canaries[0], SHA256: canaries[4]}}}}}
	result, e := ProjectResult(input(), p, raw)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range canaries {
		if strings.Contains(string(b), c) {
			t.Fatal("leaked canary")
		}
	}
	assertInternalResultDraft(t, b)
	if result.Steps[0].Reason != "" {
		t.Fatal("raw reason retained")
	}
}

func TestResultRejectsInvalidEngineStatus(t *testing.T) {
	// T006 Apply emits profile.StepAudit's closed protocol result set. A future
	// broken adapter must not be displayed as an ordinary blocked operation.
	p, err := Build(input())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ProjectResult(input(), p, ApplyResult{Steps: []StepOutcome{{Audit: profile.StepAudit{ID: p.Steps[0].ID, Result: "invalid-engine-state"}}}})
	if err == nil {
		t.Fatal("impossible engine status was silently remapped")
	}
}

func TestResultRequiresSealedPlanAndMatchingInput(t *testing.T) {
	// ProjectResult must bind both the displayed sealed steps and the original
	// inputs; either rejection guard alone is insufficient.
	for _, tc := range []string{"valid", "tampered_plan", "changed_input"} {
		t.Run(tc, func(t *testing.T) {
			in := input()
			p, err := Build(in)
			if err != nil {
				t.Fatal(err)
			}
			switch tc {
			case "tampered_plan":
				p.Steps[0].Item = "forged-item"
			case "changed_input":
				in.Selected["bat"] = true
			}
			result, err := ProjectResult(in, p, ApplyResult{})
			if tc == "valid" {
				if err != nil || result.SchemaVersion != 1 || result.Steps == nil {
					t.Fatalf("valid sealed projection rejected: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("%s projected without its original binding", tc)
			}
		})
	}
}

func TestResultPreservesSafeEvidence(t *testing.T) {
	in := input()
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	s := p.Steps[0]
	for _, status := range []string{"applied", "unchanged", "blocked", "failed", "rolled_back", "skipped", "declined", "opted_out"} {
		for _, reason := range []struct{ raw, want string }{
			{"step_timeout", "host_setup.step_timeout"}, {"host_setup.dependency_failed", "host_setup.dependency_failed"},
			{"interrupted", "interrupted"}, {"step_failed", "step_failed"}, {"confirmation_required", "confirmation_required"}, {"dependency_unavailable", "dependency_unavailable"},
			{"private-unknown-reason", ""},
		} {
			t.Run(status+"/"+reason.raw, func(t *testing.T) {
				// Receipt metadata is untrusted display input; only the sealed identity is
				// safe. Recognized statuses/reasons still need exact positive assertions.
				out, err := ProjectResult(in, p, ApplyResult{Steps: []StepOutcome{{Audit: profile.StepAudit{ID: s.ID, Item: "forged-item", Kind: "forged-kind", Result: status}, Reason: reason.raw}}})
				if err != nil {
					t.Fatal(err)
				}
				want := []ResultStep{{ID: s.ID, Item: s.Item, Kind: s.Kind, Status: status, Reason: reason.want}}
				if !reflect.DeepEqual(out.Steps, want) {
					t.Fatalf("safe evidence lost or changed: got %+v want %+v", out.Steps, want)
				}
			})
		}
	}
}

func TestResultOmitsUnknownReceiptIdentity(t *testing.T) {
	in := input()
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	s := p.Steps[0]
	unknown := "/home/private-receipt-canary"
	result, err := ProjectResult(in, p, ApplyResult{ExitCode: 68, Steps: []StepOutcome{
		{Audit: profile.StepAudit{ID: unknown, Item: unknown, Kind: "brew_install", Result: "blocked"}, Reason: "dependency_failed"},
		{Audit: profile.StepAudit{ID: s.ID, Item: s.Item, Kind: s.Kind, Result: "blocked"}, Reason: "dependency_failed"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []ResultStep{{ID: s.ID, Item: s.Item, Kind: s.Kind, Status: "blocked", Reason: "host_setup.dependency_failed"}}
	if !reflect.DeepEqual(result.Steps, want) {
		t.Fatalf("unknown receipt admitted or known receipt lost: %+v", result.Steps)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), unknown) {
		t.Fatal("unknown receipt identity leaked")
	}
}

// assertInternalResultDraft checks only the existing internal draft; it does not
// register a public envelope or resolve AP-6.
func assertInternalResultDraft(t *testing.T, b []byte) {
	t.Helper()
	var e error
	var value any
	if e = json.Unmarshal(b, &value); e != nil {
		t.Fatal(e)
	}
	// Draft internal shape only; no command envelope or AP-6 failure placement.
	var draft any
	e = json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"required":["schema_version","ExitCode","Steps","Applied","Remaining"],"properties":{"schema_version":{"const":1},"ExitCode":{"enum":[0,64,68,70,71,72,73,74]},"Steps":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["ID","Item","Kind","Status","Reason"],"properties":{"ID":{"type":"string"},"Item":{"type":"string"},"Kind":{"type":"string"},"Status":{"enum":["applied","unchanged","blocked","failed","rolled_back","skipped","declined","opted_out"]},"Reason":{"type":"string"}}}},"Applied":{"type":"array","items":{"type":"string"}},"Remaining":{"type":"array","items":{"type":"string"}}}}`), &draft)
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	if e = c.AddResource("draft.json", draft); e != nil {
		t.Fatal(e)
	}
	schema, e := c.Compile("draft.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = schema.Validate(value); e != nil {
		t.Fatal(e)
	}
}

func TestResultCurrentProductionCatalogueContext(t *testing.T) {
	// Bind the current production loader and concrete parent's ApplyResult API to
	// the reviewed internal projection. Only injected actions run in a temp HOME;
	// selection here is test input, not a change to production default choices.
	current, err := catalogue.LoadInitial()
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Platform: "darwin-arm64", Catalogue: current, Config: map[string]ConfigObservation{}, Selected: map[string]bool{}, OptedOut: map[string]bool{}}
	for _, item := range current.Items {
		in.Selected[item.ID] = item.ID == "jq"
		state := "present_ok"
		if item.ID == "jq" {
			state = "absent"
		}
		in.Results = append(in.Results, detect.Result{Item: item.ID, State: state})
		if item.Config != nil {
			in.Config[item.ID] = ConfigObservation{State: "identical"}
		}
	}
	sealed, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed.Steps) != 1 || sealed.Steps[0].ID != "jq.brew_install" {
		t.Fatalf("unexpected current catalogue plan: %+v", sealed.Steps)
	}
	engine, request, runner, _ := applyFixture(t)
	request.Input = in
	request.ExpectedHash = sealed.Hash
	applied, err := engine.Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if applied.ExitCode != 0 || !applied.ProfileChanged || len(runner.calls) != 1 {
		t.Fatalf("current concrete result mismatch: %+v calls=%+v", applied, runner.calls)
	}
	view, err := ProjectResult(in, sealed, applied)
	if err != nil {
		t.Fatal(err)
	}
	if view.ExitCode != applied.ExitCode || !reflect.DeepEqual(view.Applied, applied.Applied) || !reflect.DeepEqual(view.Remaining, applied.Remaining) {
		t.Fatalf("current result evidence changed: %+v versus %+v", view, applied)
	}
	want := []ResultStep{{ID: "jq.brew_install", Item: "jq", Kind: "brew_install", Status: "applied"}}
	if !reflect.DeepEqual(view.Steps, want) {
		t.Fatalf("current receipt identity lost: %+v", view.Steps)
	}
	profileIDs := 0
	for _, id := range view.Applied {
		if strings.HasSuffix(id, ".profile_write") {
			profileIDs++
		}
	}
	if profileIDs != len(current.Items) {
		t.Fatalf("profile receipts=%d catalogue items=%d", profileIDs, len(current.Items))
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	assertInternalResultDraft(t, encoded)
}
