// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

func TestLocalPromoteRegistryDispatch(t *testing.T) {
	found := false
	for _, e := range commands.Table() {
		if e.ID == "workspace.promote" {
			found = true
			if len(e.Short) != 0 {
				t.Fatal("promote must have no bare alias")
			}
		}
	}
	if !found || commands.DefaultHandlers()["workspace.promote"] == nil {
		t.Fatal("promote registry/default handler absent")
	}
	for _, group := range []string{"workspace", "ws"} {
		h := baseHandlers()
		called := false
		h["workspace.promote"] = func(context.Context, commands.Call) (commands.Result, error) {
			called = true
			return commands.Result{Value: stageCLIResult()}, nil
		}
		g := run(t, h, group, "promote", "--local", "--dry-run", "primary", "--json")
		if g.code != 0 || !called {
			t.Fatalf("promote dispatch: %+v", g)
		}
		if decodeEnvelope(t, g.stdout)["command"] != "workspace.promote" {
			t.Fatal(g.stdout)
		}
	}
}

func promoteCLIHandlers(f *stageTransactionFixture, n *int, o *localdocker.PromoteOptions) map[string]commands.Handler {
	h := baseHandlers()
	h["workspace.promote"] = commands.LocalPromoteHandler(func(_ context.Context, options localdocker.PromoteOptions) (localdocker.LifecycleTransaction, error) {
		*n++
		*o = options
		return f, nil
	})
	return h
}
func TestLocalPromoteCLIConsentAndPreview(t *testing.T) {
	t.Run("json-terminal-no-prompt", func(t *testing.T) {
		p := stageCLIResult()
		f := &stageTransactionFixture{preview: p, result: p}
		n := 0
		var o localdocker.PromoteOptions
		var out, diag bytes.Buffer
		env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("yes\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
		code := commands.Execute(context.Background(), []string{"workspace", "promote", "--local", "--volumes", "--force", "--json", "primary"}, env, promoteCLIHandlers(f, &n, &o))
		if code != 70 || !strings.Contains(diag.String(), "--yes") || strings.Contains(diag.String(), "[y/N]") || n != 0 || f.applies != 0 {
			t.Fatalf("code=%d diag=%q prepare=%d", code, diag.String(), n)
		}
	})
	for _, mode := range []string{"preview", "success", "partial", "terminal-yes"} {
		t.Run("human-"+mode, func(t *testing.T) {
			p := stageCLIResult()
			r := p
			r.Status = "promoted"
			r.Phase = "complete"
			f := &stageTransactionFixture{preview: p, result: r}
			n := 0
			var o localdocker.PromoteOptions
			var out, diag bytes.Buffer
			args := []string{"workspace", "promote", "--local", "--no-color", "primary"}
			want := 0
			if mode == "preview" {
				args = append(args, "--dry-run")
			} else {
				args = append(args, "--volumes", "--force", "--yes")
			}
			if mode == "partial" {
				f.result.Status = "partial"
				f.result.Phase = "promoting"
				rid := "daemon/attempt"
				f.result.RecoveryID = &rid
				f.err = exitcode.Errorf(exitcode.PathPartialFailure, "replacement failed; original recovered").WithRemediation("inspect retained candidate")
				want = 73
			}
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("no\n"), InputTerminal: mode == "terminal-yes", IsTerminal: func(io.Writer) bool { return true }}
			code := commands.Execute(context.Background(), args, env, promoteCLIHandlers(f, &n, &o))
			if code != want || n != 1 || f.closes != 1 || o.Diagnostics != &diag {
				t.Fatalf("code=%d out=%q diag=%q fixture=%+v", code, out.String(), diag.String(), f)
			}
			if _, err := io.WriteString(o.Diagnostics, "backend diagnostic\n"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(diag.String(), "backend diagnostic") || strings.Contains(out.String(), "backend diagnostic") {
				t.Fatal("diagnostic stream lost")
			}
			for _, part := range []string{"status: ", "exec sessions", "The original primary home is never a cleanup target", "Trusted container hooks may write the original workspace"} {
				if !strings.Contains(out.String(), part) {
					t.Fatalf("missing %q: %q", part, out.String())
				}
			}
			if mode == "preview" && (!strings.Contains(out.String(), "Apply requires explicit --volumes and --force, plus confirmation or --yes") || f.applies != 0) {
				t.Fatal(out.String())
			}
			if mode != "preview" && (f.applies != 1 || strings.Contains(diag.String(), "[y/N]")) {
				t.Fatalf("unexpected prompt or no apply: %q", diag.String())
			}
			if mode == "partial" && (!strings.Contains(out.String(), "recovery: daemon/attempt") || !strings.Contains(diag.String(), "replacement failed; original recovered")) {
				t.Fatalf("partial output lost: out=%q diag=%q", out.String(), diag.String())
			}
		})
	}

	for _, tc := range []struct {
		name           string
		flags          []string
		code           int
		reason         string
		prepare, apply bool
	}{
		{"preview", []string{"--dry-run"}, 0, "", true, false},
		{"preview-force-alone", []string{"--dry-run", "--force"}, 0, "", true, false},
		{"preview-full-flags", []string{"--dry-run", "--volumes", "--force"}, 0, "", true, false},
		{"preview-volumes-alone", []string{"--dry-run", "--volumes"}, 70, "--force", false, false},
		{"yes-not-volumes", []string{"--yes"}, 70, "--volumes", false, false},
		{"yes-not-force", []string{"--yes", "--volumes"}, 70, "--force", false, false},
		{"missing-confirmation", []string{"--volumes", "--force"}, 70, "--yes", false, false},
		{"apply", []string{"--volumes", "--force", "--yes"}, 0, "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := stageCLIResult()
			r := p
			r.Status = "promoted"
			r.Phase = "complete"
			f := &stageTransactionFixture{preview: p, result: r}
			n := 0
			var o localdocker.PromoteOptions
			args := append([]string{"workspace", "promote", "--local", "primary", "--json"}, tc.flags...)
			g := run(t, promoteCLIHandlers(f, &n, &o), args...)
			wantCount := func(v bool) int {
				if v {
					return 1
				}
				return 0
			}
			if g.code != tc.code || !strings.Contains(g.err, tc.reason) || n != wantCount(tc.prepare) || f.applies != wantCount(tc.apply) || f.closes != wantCount(tc.prepare) {
				t.Fatalf("wrong consent/preview/reason: %+v prepare=%d fixture=%+v", g, n, f)
			}
			if tc.apply && (!f.consent.Volumes || !f.consent.Force || !f.consent.Confirmed || !f.consent.Quiescence || f.consent.TrustDigest != "") {
				t.Fatalf("wrong deletion consent: %+v", f.consent)
			}
			if tc.prepare && o.Target != "primary" {
				t.Fatalf("selector=%q", o.Target)
			}
		})
	}
}
func TestLocalPromoteCLIRefusesBadArgumentsBeforePrepare(t *testing.T) {
	for _, args := range [][]string{{"--local", "--dry-run", ""}, {"--local", "--dry-run", "  "}, {"primary", "--dry-run"}, {"--local", "--dry-run"}, {"--local", "--dry-run", "a", "b"}, {"--local", "--dry-run", "primary", "--trust", "digest"}} {
		f := &stageTransactionFixture{preview: stageCLIResult()}
		n := 0
		var o localdocker.PromoteOptions
		g := run(t, promoteCLIHandlers(f, &n, &o), append([]string{"workspace", "promote"}, args...)...)
		reason := "use workspace promote --local"
		if strings.Contains(strings.Join(args, " "), "--trust") {
			reason = "unknown flag: --trust"
		}
		if g.code != 64 || !strings.Contains(g.err, reason) || n != 0 || f.applies != 0 {
			t.Fatalf("bad selector/flag reached backend: %+v prepare=%d", g, n)
		}
	}
}
func TestLocalPromoteCLIInteractivePaneWarningAndDecline(t *testing.T) {
	for _, answer := range []string{"yes\n", " Y \r\n", " YES \n", "no\n", "yes", "", strings.Repeat("y", 1025) + "\n"} {
		t.Run(fmt.Sprintf("%d-%t", len(answer), answer == "yes\n"), func(t *testing.T) {
			p := stageCLIResult()
			f := &stageTransactionFixture{preview: p, result: p}
			n := 0
			var o localdocker.PromoteOptions
			var out, diag bytes.Buffer
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(answer), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
			code := commands.Execute(context.Background(), []string{"ws", "promote", "--local", "--volumes", "--force", "--no-color", "primary"}, env, promoteCLIHandlers(f, &n, &o))
			want := 70
			if strings.ToLower(strings.TrimSpace(answer)) == "yes" && strings.HasSuffix(answer, "\n") || strings.TrimSpace(answer) == "Y" {
				want = 0
			}
			if code != want || f.closes != 1 || f.applies != map[bool]int{true: 1, false: 0}[want == 0] {
				t.Fatalf("code=%d diag=%q applies=%d", code, diag.String(), f.applies)
			}
			for _, part := range []string{"shell", "exec sessions", "The original primary home is never a cleanup target", "Trusted container hooks may write the original workspace", "Pause input writers"} {
				if !strings.Contains(diag.String(), part) {
					t.Fatalf("missing preview warning %q: %q", part, diag.String())
				}
			}
			if strings.ContainsAny(diag.String()+out.String(), "\x1b\r") || strings.Contains(diag.String(), "\nforged") {
				t.Fatal("unescaped metadata")
			}
		})
	}
}
func TestLocalPromoteCLIPartialResultAndErrorReason(t *testing.T) {
	p := stageCLIResult()
	r := p
	r.Status = "partial"
	r.Phase = "promoting"
	rid := "daemon/attempt"
	r.RecoveryID = &rid
	target := localdocker.StageTarget{Resource: r.Home}
	r.Effects = []localdocker.StageEffect{{Action: "remove_home", Target: target, State: "remaining"}}
	r.Recovery = []localdocker.StageRecoveryStep{{Action: "inspect_retained", Target: &target, Instruction: "inspect retained candidate"}}
	f := &stageTransactionFixture{preview: p, result: r, err: exitcode.Errorf(exitcode.PathPartialFailure, "replacement failed; original recovered").WithRemediation("inspect retained candidate")}
	n := 0
	var o localdocker.PromoteOptions
	g := run(t, promoteCLIHandlers(f, &n, &o), "workspace", "promote", "--local", "--volumes", "--force", "--yes", "primary", "--json")
	env := decodeEnvelope(t, g.stdout)
	failure := env["error"].(map[string]any)
	result, present := env["result"].(map[string]any)
	if !present {
		t.Fatalf("partial result missing: %v", env)
	}
	if g.code != 73 || failure["message"] != "replacement failed; original recovered" || failure["code"] != "partial_failure" || failure["remediation"] != "inspect retained candidate" || result["recovery_id"] != rid || f.applies != 1 || f.closes != 1 {
		t.Fatalf("lost partial result or cause: %+v", env)
	}

	effects := result["effects"].([]any)
	recovery := result["recovery"].([]any)
	if len(effects) != 1 || len(recovery) != 1 {
		t.Fatalf("inventory lost: %v", result)
	}
	effect := effects[0].(map[string]any)
	step := recovery[0].(map[string]any)
	if effect["action"] != "remove_home" || effect["state"] != "remaining" || step["action"] != "inspect_retained" || step["instruction"] != "inspect retained candidate" {
		t.Fatalf("inventory changed: %v", result)
	}
	for _, target := range []any{effect["target"], step["target"]} {
		m := target.(map[string]any)
		if m["kind"] != "volume" || m["name"] != *r.Home.Name || m["daemon_id"] != "daemon" {
			t.Fatalf("target changed: %v", m)
		}
	}
	if err := compileSchema(t).Validate(env); err != nil {
		t.Fatal(err)
	}
}
func TestLocalPromoteJSONSchemaClosed(t *testing.T) {
	f := &stageTransactionFixture{preview: stageCLIResult()}
	n := 0
	var o localdocker.PromoteOptions
	g := run(t, promoteCLIHandlers(f, &n, &o), "workspace", "promote", "--local", "--dry-run", "primary", "--json")
	env := decodeEnvelope(t, g.stdout)
	schema := compileSchema(t)
	if err := schema.Validate(env); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"extra", "missing", "operation"} {
		t.Run(kind, func(t *testing.T) {
			m := decodeEnvelope(t, g.stdout)
			r := m["result"].(map[string]any)
			switch kind {
			case "extra":
				r["raw_output"] = "private"
			case "missing":
				delete(r, "home")
			case "operation":
				m["operation_id"] = "fictional"
			}
			if schema.Validate(m) == nil {
				t.Fatal("open promote schema", kind)
			}
		})
	}
}
