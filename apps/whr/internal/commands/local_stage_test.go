// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"io"
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
	for _, tc := range []struct{ flag, reason string }{{"--trust", "--yes"}, {"--yes", "--trust"}} {
		t.Run("json-terminal-"+tc.flag, func(t *testing.T) {
			f := &stageTransactionFixture{preview: stageCLIResult()}
			n := 0
			var o localdocker.StageOptions
			var out, diag bytes.Buffer
			args := []string{"workspace", "stage", "--local", "--source", "/src", "primary", "--json", tc.flag}
			if tc.flag == "--trust" {
				args = append(args, "digest")
			}
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("yes\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
			code := commands.Execute(context.Background(), args, env, stageCLIHandlers(f, &n, &o))
			if code != 70 || !strings.Contains(diag.String(), tc.reason) || strings.Contains(diag.String(), "[y/N]") || strings.Contains(diag.String(), "Enter the exact fingerprint") || n != 0 || f.applies != 0 {
				t.Fatalf("code=%d diag=%q prepare=%d fixture=%+v", code, diag.String(), n, f)
			}
		})
	}

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
	t.Run("human-preview", func(t *testing.T) {
		f := &stageTransactionFixture{preview: stageCLIResult()}
		n := 0
		var o localdocker.StageOptions
		g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--dry-run", "primary")
		for _, text := range []string{"Pause input writers", "Hashes cannot detect a writer that changes and restores bytes during a phase"} {
			if g.code != 0 || !strings.Contains(g.stdout, text) || f.applies != 0 {
				t.Fatalf("missing %q: %+v", text, g)
			}
		}
	})
	for _, fp := range []*string{nil, new(string)} {
		f := &stageTransactionFixture{preview: stageCLIResult()}
		f.preview.Fingerprint = fp
		n := 0
		var o localdocker.StageOptions
		g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--yes", "--trust", "digest", "primary")
		if g.code != 68 || !strings.Contains(g.err, "stage preview has no source fingerprint") || f.applies != 0 || f.closes != 1 {
			t.Fatalf("missing fingerprint: %+v fixture=%+v", g, f)
		}
	}

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

type cancelStageReader struct {
	cancel context.CancelFunc
	text   string
}

func (r *cancelStageReader) Read(p []byte) (int, error) {
	r.cancel()
	return strings.NewReader(r.text).Read(p)
}

func TestLocalStageCLIInteractiveTrustAndConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, answer, reason string
		code                 int
		supplied             bool
	}{
		{"trust-declined", "DECLINE\n", "source fingerprint trust declined", 70, false},
		{"confirmation-declined", "MATCH\nno\n", "stage confirmation declined", 70, false},
		{"accepted", "MATCH\nyes\n", "", 0, false},
		{"upper", "MATCH\nY\n", "", 0, false},
		{"padded", "MATCH\n YES \r\n", "", 0, false},
		{"supplied-trust", "yes\n", "", 0, true},
		{"padded-digest", " MATCH\nyes\n", "source fingerprint trust declined", 70, false},
		{"changed-digest-case", "SHA256:" + strings.Repeat("a", 64) + "\nyes\n", "source fingerprint trust declined", 70, false},
		{"over-bound-with-newline", strings.Repeat("x", 1024) + "\n", "confirmation declined: a complete bounded answer is required", 70, false},
		{"oversized", strings.Repeat("x", 1100) + "\n", "confirmation declined: a complete bounded answer is required", 70, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := stageCLIResult()
			n := 0
			var o localdocker.StageOptions
			f := &stageTransactionFixture{preview: p, result: p}
			var out, diag bytes.Buffer
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(strings.ReplaceAll(tc.answer, "MATCH", *p.Fingerprint)), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
			args := []string{"workspace", "stage", "--local", "--source", "/src", "--no-color", "primary"}
			if tc.supplied {
				args = append(args, "--trust", *p.Fingerprint)
			}
			code := commands.Execute(context.Background(), args, env, stageCLIHandlers(f, &n, &o))
			wantApply := 0
			if tc.code == 0 {
				wantApply = 1
			}
			if code != tc.code || f.applies != wantApply || f.closes != 1 || !strings.Contains(diag.String(), tc.reason) {
				t.Fatalf("code=%d diag=%q fixture=%+v", code, diag.String(), f)
			}
			for _, text := range []string{"fingerprint", "Pause input writers", "Hashes cannot detect a writer that changes and restores bytes during a phase"} {
				if !strings.Contains(diag.String(), text) {
					t.Fatalf("missing %q: %q", text, diag.String())
				}
			}
			if tc.supplied && (strings.Contains(diag.String(), "Enter the exact fingerprint") || strings.Count(diag.String(), "[y/N]") != 1) {
				t.Fatal(diag.String())
			}
			if strings.ContainsAny(out.String(), "\x1b\r") || strings.Contains(out.String(), "\nforged") {
				t.Fatal(out.String())
			}
		})
	}
	t.Run("canceled-during-answer", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p := stageCLIResult()
		f := &stageTransactionFixture{preview: p, result: p}
		n := 0
		var o localdocker.StageOptions
		var out, diag bytes.Buffer
		env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: &cancelStageReader{cancel: cancel, text: "yes\n"}, InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
		code := commands.Execute(ctx, []string{"workspace", "stage", "--local", "--source", "/src", "primary", "--no-color", "--trust", *p.Fingerprint}, env, stageCLIHandlers(f, &n, &o))
		if code != 72 || !strings.Contains(diag.String(), "interrupted") || f.applies != 0 || f.closes != 1 {
			t.Fatalf("code=%d diag=%q fixture=%+v", code, diag.String(), f)
		}
	})
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

func TestLocalStageCLIApplyErrorCodesAndReasons(t *testing.T) {
	for _, tc := range []struct {
		path exitcode.Path
		code int
		name string
	}{
		{exitcode.PathTargetAbsent, 65, "not_found"}, {exitcode.PathConflict, 68, "conflict"}, {exitcode.PathHumanDecisionMissing, 70, "needs_human"}, {exitcode.PathDeadlineExceeded, 71, "timeout"}, {exitcode.PathUserInterrupt, 72, "interrupted"}, {exitcode.PathPartialFailure, 73, "partial_failure"}, {exitcode.PathUnclassified, 74, "internal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := stageCLIResult()
			r := p
			r.Status = "refused"
			r.Phase = "preflight"
			message := "library refusal " + tc.name
			n := 0
			var o localdocker.StageOptions
			f := &stageTransactionFixture{preview: p, result: r, err: exitcode.Errorf(tc.path, "%s", message).WithRemediation("inspect the reported phase")}
			g := run(t, stageCLIHandlers(f, &n, &o), "workspace", "stage", "--local", "--source", "/src", "--yes", "--trust", *p.Fingerprint, "primary", "--json")
			env := decodeEnvelope(t, g.stdout)
			failure := env["error"].(map[string]any)
			if g.code != tc.code || failure["code"] != tc.name || failure["message"] != message || failure["remediation"] != "inspect the reported phase" || f.applies != 1 || f.closes != 1 {
				t.Fatalf("lost code/reason: %+v env=%v applies=%d closes=%d", g, env, f.applies, f.closes)
			}
			if err := compileSchema(t).Validate(env); err != nil {
				t.Fatal(err)
			}
		})
	}
}
