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
	for _, tc := range []struct {
		name           string
		flags          []string
		code           int
		reason         string
		prepare, apply bool
	}{
		{"preview", []string{"--dry-run"}, 0, "", true, false},
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
	for _, args := range [][]string{{"primary", "--dry-run"}, {"--local", "--dry-run"}, {"--local", "--dry-run", "a", "b"}, {"--local", "--dry-run", "primary", "--trust", "digest"}} {
		f := &stageTransactionFixture{preview: stageCLIResult()}
		n := 0
		var o localdocker.PromoteOptions
		g := run(t, promoteCLIHandlers(f, &n, &o), append([]string{"workspace", "promote"}, args...)...)
		if g.code != 64 || g.err == "" || n != 0 || f.applies != 0 {
			t.Fatalf("bad selector/flag reached backend: %+v prepare=%d", g, n)
		}
	}
}
func TestLocalPromoteCLIInteractivePaneWarningAndDecline(t *testing.T) {
	for _, answer := range []string{"yes\n", "no\n", "yes", "", strings.Repeat("y", 1025) + "\n"} {
		t.Run(fmt.Sprintf("%d-%t", len(answer), answer == "yes\n"), func(t *testing.T) {
			p := stageCLIResult()
			f := &stageTransactionFixture{preview: p, result: p}
			n := 0
			var o localdocker.PromoteOptions
			var out, diag bytes.Buffer
			env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(answer), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
			code := commands.Execute(context.Background(), []string{"ws", "promote", "--local", "--volumes", "--force", "primary"}, env, promoteCLIHandlers(f, &n, &o))
			want := 70
			if answer == "yes\n" {
				want = 0
			}
			if code != want || f.closes != 1 || f.applies != map[bool]int{true: 1, false: 0}[want == 0] {
				t.Fatalf("code=%d diag=%q applies=%d", code, diag.String(), f.applies)
			}
			for _, part := range []string{"shell", "exec sessions", "original primary home", "Pause input writers"} {
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
	f := &stageTransactionFixture{preview: p, result: r, err: exitcode.Errorf(exitcode.PathPartialFailure, "replacement failed; original recovered").WithRemediation("inspect retained candidate")}
	n := 0
	var o localdocker.PromoteOptions
	g := run(t, promoteCLIHandlers(f, &n, &o), "workspace", "promote", "--local", "--volumes", "--force", "--yes", "primary", "--json")
	env := decodeEnvelope(t, g.stdout)
	failure := env["error"].(map[string]any)
	result := env["result"].(map[string]any)
	if g.code != 73 || failure["message"] != "replacement failed; original recovered" || failure["code"] != "partial_failure" || result["recovery_id"] != rid || f.applies != 1 || f.closes != 1 {
		t.Fatalf("lost partial result or cause: %+v", env)
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
