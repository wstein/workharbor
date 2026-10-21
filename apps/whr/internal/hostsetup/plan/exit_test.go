//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExitMappingMatchesADR0013(t *testing.T) {
	raw, e := os.ReadFile("../../../../../contracts/cli/exit-codes.json")
	if e != nil {
		t.Fatal(e)
	}
	var table struct {
		Codes []struct {
			Code int
			Name string
		}
	}
	if e = json.Unmarshal(raw, &table); e != nil {
		t.Fatal(e)
	}
	names := map[int]string{}
	for _, r := range table.Codes {
		names[r.Code] = r.Name
	}
	raw, e = os.ReadFile("../../../../../specs/009-host-setup-wizard/contracts/refusal-codes.json")
	if e != nil {
		t.Fatal(e)
	}
	var c struct {
		Codes []struct {
			Code string
			Exit int `json:"exit_alone"`
		}
	}
	if e = json.Unmarshal(raw, &c); e != nil {
		t.Fatal(e)
	}
	got := RefusalCodes()
	if len(got) != len(c.Codes) {
		t.Fatal("count mismatch")
	}
	seen := map[string]bool{}
	for _, r := range c.Codes {
		if seen[r.Code] {
			t.Fatal("duplicate reason")
		}
		seen[r.Code] = true
		code, ok := got[r.Code]
		name, known := exitcode.Name(int(code))
		if !ok || int(code) != r.Exit || !known || name != names[int(code)] {
			t.Fatal(r)
		}
	}
	got["host_setup.running_as_root"] = 74
	if RefusalCodes()["host_setup.running_as_root"] != 68 {
		t.Fatal("mutable table")
	}
	if _, e = ResultExit(ApplyResult{ExitCode: 255}); e == nil {
		t.Fatal("remote status accepted")
	}
}
func TestPrecedence73(t *testing.T) {
	for _, code := range []int{0, 64, 68, 70, 71, 72, 73, 74} {
		got, e := ResultExit(ApplyResult{ExitCode: code})
		if e != nil || int(got) != code {
			t.Fatalf("%d %v", got, e)
		}
	}
	e, r, runner, _ := applyFixture(t)
	r.Confirmed = false
	out, err := e.Apply(context.Background(), r)
	if err == nil || out.ExitCode != 70 {
		t.Fatalf("%+v %v", out, err)
	}
	hint := ConfirmationHint()
	if !strings.Contains(hint, "--yes") || !strings.Contains(hint, "--confirm <id>") {
		t.Fatal(hint)
	}
	r.Confirmed = true
	r.Input.Results[0].State = "present_outdated"
	r.Input.Results[0].Managed = true
	resealApply(t, &r)
	out, err = e.Apply(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range runner.calls {
		if s.Class == "modify" {
			t.Fatal("blanket consent approved modify")
		}
	}
	_ = out
}

func TestPrecedence73EngineEvidence(t *testing.T) {
	for _, cause := range []string{"failed", "timeout", "interrupt", "declined", "opted_out", "blocked-dependent"} {
		t.Run(cause, func(t *testing.T) {
			e, r, runner, _ := applyFixture(t)
			addBat(&r)
			if cause == "declined" || cause == "blocked-dependent" {
				r.Input.Results[1].State = "present_outdated"
				r.Input.Results[1].Managed = true
			}
			if cause == "opted_out" {
				r.Input.OptedOut["bat"] = true
			}
			if cause == "blocked-dependent" {
				r.Input.Catalogue.Items[0].Requires = []string{"bat"}
			}
			resealApply(t, &r)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration, _ StepGuard) (StepOutcome, error) {
				if cause == "interrupt" {
					cancel()
				}
				if s.Item == "bat" && (cause == "failed" || cause == "timeout") {
					status := 1
					out := StepOutcome{Audit: profile.StepAudit{Result: "failed", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}, ExitStatus: &status}}
					if cause == "timeout" {
						return out, context.DeadlineExceeded
					}
					return out, nil
				}
				return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}, Changed: true}, nil
			}
			out, err := e.Apply(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ResultExit(out)
			want := exitcode.PartialFailure
			if cause == "declined" || cause == "opted_out" {
				want = exitcode.OK
			}
			if err != nil || got != want {
				t.Fatalf("%+v %v want %d", out, err, want)
			}
			p, err := Build(r.Input)
			if err != nil {
				t.Fatal(err)
			}
			view, err := ProjectResult(r.Input, p, out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(view.Applied, out.Applied) || !reflect.DeepEqual(view.Remaining, out.Remaining) {
				t.Fatalf("projection lost completion evidence: %+v %+v", view, out)
			}
		})
	}
}
func TestReservedGitConfigIDAccepted(t *testing.T) {
	for _, id := range []string{"jq", "git-config", "unknown-item"} {
		t.Run(id, func(t *testing.T) {
			e, r, runner, _ := applyFixture(t)
			r.DryRun = true
			r.ConfirmedItems = map[string]bool{id: true}
			out, err := e.Apply(context.Background(), r)
			want := 0
			if id == "unknown-item" {
				want = 64
			}
			if out.ExitCode != want || (err != nil) != (want != 0) || len(runner.calls) != 0 {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
}
