// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

func stageCLIResult() localdocker.LifecycleResult {
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	name := "primary\x1b[31m\nforged"
	id := strings.Repeat("b", 64)
	home := "whr-stage-home-" + strings.Repeat("c", 32)
	return localdocker.LifecycleResult{Status: "preview", Phase: "planned", Fingerprint: &fingerprint, Primary: &localdocker.StageResource{Kind: "container", Name: &name, ID: &id, DaemonID: "daemon"}, Home: &localdocker.StageResource{Kind: "volume", Name: &home, DaemonID: "daemon"}, Effects: []localdocker.StageEffect{}, Recovery: []localdocker.StageRecoveryStep{}, Repositories: []localdocker.StageRepository{}, ResultRefs: []localdocker.StageResultRef{}}
}

func TestLocalStageCommandContractAndAlias(t *testing.T) {
	var entry *commands.Entry
	for _, e := range commands.Table() {
		if e.ID == "workspace.stage" {
			copy := e
			entry = &copy
		}
	}
	want := []string{"--local", "--source", "--smoke", "--dry-run", "--trust", "--yes", "--json"}
	if entry == nil || !reflect.DeepEqual(entry.Flags, want) || len(entry.Short) != 0 || entry.Needs != "none" || entry.Writes != "local" {
		t.Fatalf("stage command entry: %+v", entry)
	}
	if commands.DefaultHandlers()["workspace.stage"] == nil {
		t.Fatal("production stage handler absent")
	}
	for _, group := range []string{"workspace", "ws"} {
		handlers := baseHandlers()
		called := false
		handlers["workspace.stage"] = func(context.Context, commands.Call) (commands.Result, error) {
			called = true
			return commands.Result{Value: stageCLIResult()}, nil
		}
		got := run(t, handlers, group, "stage", "--local", "--source", "/source", "--dry-run", "primary", "--json")
		if got.code != 0 || !called {
			t.Fatalf("dispatch: %+v called=%t", got, called)
		}
		envelope := decodeEnvelope(t, got.stdout)
		if envelope["command"] != "workspace.stage" {
			t.Fatalf("canonical command: %v", envelope)
		}
	}
}

type stageTransactionFixture struct {
	preview, result localdocker.LifecycleResult
	applies, closes int
	consent         localdocker.LifecycleConsent
	err             error
}

func (f *stageTransactionFixture) Preview() localdocker.LifecycleResult { return f.preview }
func (f *stageTransactionFixture) Apply(_ context.Context, c localdocker.LifecycleConsent) (localdocker.LifecycleResult, error) {
	f.applies++
	f.consent = c
	return f.result, f.err
}
func (f *stageTransactionFixture) Close() error { f.closes++; return nil }
func stageCLIHandlers(f *stageTransactionFixture, prepares *int, options *localdocker.StageOptions) map[string]commands.Handler {
	h := baseHandlers()
	h["workspace.stage"] = commands.LocalStageHandler(func(_ context.Context, o localdocker.StageOptions) (localdocker.LifecycleTransaction, error) {
		*prepares++
		*options = o
		return f, nil
	})
	return h
}
func TestLocalStageCLIRefusalReasonsBeforePrepare(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		code   int
		reason string
	}{
		{"no-local", []string{"--source", "/src", "--dry-run", "primary"}, 64, "stage --local"},
		{"no-source", []string{"--local", "--dry-run", "primary"}, 64, "--source"},
		{"no-selector", []string{"--local", "--source", "/src", "--dry-run"}, 64, "exact"},
		{"two-selectors", []string{"--local", "--source", "/src", "--dry-run", "a", "b"}, 64, "exact"},
		{"no-yes", []string{"--local", "--source", "/src", "--trust", "digest", "primary"}, 70, "--yes"},
		{"no-trust", []string{"--local", "--source", "/src", "--yes", "primary"}, 70, "--trust"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			var o localdocker.StageOptions
			f := &stageTransactionFixture{preview: stageCLIResult()}
			g := run(t, stageCLIHandlers(f, &n, &o), append([]string{"workspace", "stage"}, tc.args...)...)
			if g.code != tc.code || !strings.Contains(g.err, tc.reason) || n != 0 || f.applies != 0 {
				t.Fatalf("wrong refusal or backend call: %+v prepares=%d applies=%d", g, n, f.applies)
			}
		})
	}
}
func TestLocalStageCLIPreviewAndConsent(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "preview", false: "apply"}[dry], func(t *testing.T) {
			p := stageCLIResult()
			f := &stageTransactionFixture{preview: p, result: p}
			n := 0
			var o localdocker.StageOptions
			args := []string{"ws", "stage", "--local", "--source", "/source with spaces", "--smoke", "primary", "--json"}
			if dry {
				args = append(args, "--dry-run")
			} else {
				args = append(args, "--trust", *p.Fingerprint, "--yes")
				f.result.Status = "staged_verified"
				f.result.Phase = "staged_verified"
			}
			g := run(t, stageCLIHandlers(f, &n, &o), args...)
			if g.code != 0 || n != 1 || f.closes != 1 || o.Target != "primary" || o.Source != "/source with spaces" || !o.Smoke {
				t.Fatalf("wiring: %+v prepare=%d close=%d options=%+v", g, n, f.closes, o)
			}
			if dry && f.applies != 0 || !dry && (f.applies != 1 || f.consent.TrustDigest != *p.Fingerprint || !f.consent.Confirmed || !f.consent.Quiescence || f.consent.Volumes || f.consent.Force) {
				t.Fatalf("wrong consent: %+v applies=%d", f.consent, f.applies)
			}
			decodeEnvelope(t, g.stdout)
		})
	}
}
func TestLocalStageCLIWrongTrustRefusesAndCloses(t *testing.T) {
	n := 0
	var o localdocker.StageOptions
	f := &stageTransactionFixture{preview: stageCLIResult()}
	g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--yes", "--trust", "wrong", "primary", "--json")
	if g.code != 68 || !strings.Contains(g.err, "fingerprint") || f.applies != 0 || f.closes != 1 {
		t.Fatalf("wrong trust: %+v fixture=%+v", g, f)
	}
}
func TestLocalStageCLIPartialEnvelopePreservesRecovery(t *testing.T) {
	n := 0
	var o localdocker.StageOptions
	p := stageCLIResult()
	r := p
	r.Status = "staged_unverified"
	r.Phase = "staged_unverified"
	reason := "missing_smoke"
	rid := "daemon/attempt"
	r.UnverifiedReason = &reason
	r.RecoveryID = &rid
	f := &stageTransactionFixture{preview: p, result: r, err: exitcode.Errorf(exitcode.PathPartialFailure, "verification missing").WithRemediation("inspect retained candidate")}
	g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--yes", "--trust", *p.Fingerprint, "primary", "--json")
	if g.code != 73 || f.applies != 1 || f.closes != 1 {
		t.Fatalf("partial: %+v", g)
	}
	env := decodeEnvelope(t, g.stdout)
	result := env["result"].(map[string]any)
	if result["status"] != "staged_unverified" || result["recovery_id"] != rid || result["unverified_reason"] != reason || env["operation_id"] != nil {
		t.Fatalf("lost local partial inventory: %v", env)
	}
}
func TestLocalStageCLIInteractiveTrustAndConfirmation(t *testing.T) {
	for _, answer := range []string{"DECLINE\n", "MATCH\nno\n", "MATCH\nyes\n"} {
		t.Run(strings.ReplaceAll(answer, "\n", "-"), func(t *testing.T) {
			p := stageCLIResult()
			input := strings.ReplaceAll(answer, "MATCH", *p.Fingerprint)
			n := 0
			var o localdocker.StageOptions
			f := &stageTransactionFixture{preview: p, result: p}
			var out, diag bytes.Buffer
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(input), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
			code := commands.Execute(context.Background(), []string{"workspace", "stage", "--local", "--source", "/src", "primary"}, env, stageCLIHandlers(f, &n, &o))
			want := 70
			if strings.HasSuffix(answer, "yes\n") {
				want = 0
			}
			if code != want || f.closes != 1 || f.applies != map[bool]int{true: 1, false: 0}[want == 0] {
				t.Fatalf("code=%d out=%q diag=%q applies=%d", code, out.String(), diag.String(), f.applies)
			}
			for _, text := range []string{"fingerprint", "Pause input writers"} {
				if !strings.Contains(diag.String(), text) {
					t.Fatalf("missing disclosure %q: %q", text, diag.String())
				}
			}
			if strings.ContainsAny(out.String(), "\x1b\r") || strings.Contains(out.String(), "\nforged") {
				t.Fatalf("unsafe output %q", out.String())
			}
		})
	}
}

func TestLocalStageJSONClosedResultSchema(t *testing.T) {
	p := stageCLIResult()
	n := 0
	var o localdocker.StageOptions
	f := &stageTransactionFixture{preview: p, result: p}
	g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--dry-run", "primary", "--json")
	envelope, err := jsonUnmarshal(g.stdout)
	if err != nil {
		t.Fatal(err)
	}
	schema := compileSchema(t)
	if err = schema.Validate(envelope); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"unknown-result-field", "invalid-status", "missing-nullable", "null-array", "local-operation-id", "invalid-resource"} {
		t.Run(kind, func(t *testing.T) {
			env, e := jsonUnmarshal(g.stdout)
			if e != nil {
				t.Fatal(e)
			}
			m := env.(map[string]any)
			r := m["result"].(map[string]any)
			switch kind {
			case "unknown-result-field":
				r["script_body"] = "not public"
			case "invalid-status":
				r["status"] = "done"
			case "missing-nullable":
				delete(r, "verification")
			case "null-array":
				r["effects"] = nil
			case "local-operation-id":
				m["operation_id"] = "fictional"
			case "invalid-resource":
				r["primary"].(map[string]any)["kind"] = "workspace"
			}
			if schema.Validate(env) == nil {
				t.Fatalf("open stage schema admits %s", kind)
			}
		})
	}
}
