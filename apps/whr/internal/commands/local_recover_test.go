// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

type recoverFixture struct {
	starts int
	status int
	err    error
	prep   localdocker.RecoverOptions
}

func (f *recoverFixture) Preview() localdocker.RecoverResult {
	return localdocker.RecoverResult{Status: "preview", Source: localdocker.HomeCopySource{ContainerName: "ws1", Role: "primary", Volume: "ws1-home"},
		Image: "docker.io/library/debian@sha256:abc", Toolbox: "quay.io/bundledock/toolbox:latest", Nixbox: "quay.io/bundledock/nixbox:latest", ReadOnly: true,
		Actions: []localdocker.HomeCopyAction{{Kind: "recover_image_pull", Target: "docker.io/library/debian@sha256:abc"}, {Kind: "nixbox_pull", Target: "quay.io/bundledock/nixbox:latest"}}, Residuals: []string{}}
}

func (f *recoverFixture) Start(context.Context, localdocker.RecoverStreams) (localdocker.RecoverResult, error) {
	f.starts++
	r := f.Preview()
	r.Status, r.ExitStatus = "exited", f.status
	return r, f.err
}

func recoverHandlers(f *recoverFixture) map[string]commands.Handler {
	return map[string]commands.Handler{"workspace.recover": commands.LocalRecoverHandler(func(_ context.Context, o localdocker.RecoverOptions) (commands.RecoverSession, error) {
		f.prep = o
		return f, nil
	})}
}

func runRecover(t *testing.T, f *recoverFixture, terminal bool, stdin string, args ...string) (int, string, string) {
	t.Helper()
	tty, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	var diag bytes.Buffer
	env := commands.Env{Stdout: tty, Stderr: &diag, Stdin: strings.NewReader(stdin), TerminalInput: tty, InputTerminal: terminal,
		Getenv: func(k string) string { return map[string]string{"TERM": "xterm"}[k] }, IsTerminal: func(io.Writer) bool { return terminal }}
	code := commands.Execute(context.Background(), append([]string{"workspace", "recover"}, args...), env, recoverHandlers(f))
	return code, diag.String(), ""
}

func TestRecoverNeedsLocalAndOneSelector(t *testing.T) {
	for _, args := range [][]string{{"ws1"}, {"--local"}, {"--local", "a", "b"}, {"--local", "ws1", "--json"}} {
		f := &recoverFixture{}
		if code, diag, _ := runRecover(t, f, true, "", args...); code != 64 || f.starts != 0 {
			t.Errorf("%v: code=%d starts=%d %s", args, code, f.starts, diag)
		}
	}
}

func TestRecoverDryRunPreviewsWithoutTerminalOrStart(t *testing.T) {
	f := &recoverFixture{}
	var out, diag bytes.Buffer
	env := commands.Env{Stdout: &out, Stderr: &diag, Getenv: func(string) string { return "" }, IsTerminal: func(io.Writer) bool { return false }}
	code := commands.Execute(context.Background(), []string{"workspace", "recover", "--local", "ws1", "--toolbox", "current", "--nixbox", "2026.10.30-b2", "--rw", "--net", "--dry-run"}, env, recoverHandlers(f))
	if code != 0 || f.starts != 0 || !strings.Contains(out.String(), "ws1-home") || !strings.Contains(out.String(), "read-only") || !strings.Contains(out.String(), "quay.io/bundledock/toolbox:latest") ||
		!strings.Contains(out.String(), "nixbox quay.io/bundledock/nixbox:latest") || !strings.Contains(out.String(), "action: recover_image_pull docker.io/library/debian@sha256:abc attempted=false") || strings.Contains(out.String(), "built once") {
		t.Fatalf("code=%d starts=%d out=%q diag=%q", code, f.starts, out.String(), diag.String())
	}
	if f.prep.Toolbox != "current" || f.prep.Nixbox != "2026.10.30-b2" || !f.prep.RW || !f.prep.Net || f.prep.Target != "ws1" {
		t.Fatalf("options %+v", f.prep)
	}
}

func TestRecoverNeedsATerminal(t *testing.T) {
	f := &recoverFixture{}
	if code, _, _ := runRecover(t, f, false, "", "--local", "ws1"); code != 64 || f.starts != 0 {
		t.Fatalf("code=%d starts=%d", code, f.starts)
	}
}

func TestRecoverStartsAndPassesTheShellStatus(t *testing.T) {
	f := &recoverFixture{status: 5}
	code, diag, _ := runRecover(t, f, true, "", "--local", "ws1", "--toolbox", "2026.10.24-b1")
	if code != 5 || f.starts != 1 || f.prep.Toolbox != "2026.10.24-b1" || f.prep.Term != "xterm" || f.prep.RW || f.prep.Net {
		t.Fatalf("code=%d starts=%d opts=%+v diag=%q", code, f.starts, f.prep, diag)
	}
	if !strings.Contains(diag, "ws1-home") || !strings.Contains(diag, "read-only") {
		t.Fatalf("no start notice: %q", diag)
	}
}

// Without --toolbox a terminal user is asked; Enter takes latest.
func TestRecoverAsksForTheToolboxOnATerminal(t *testing.T) {
	for in, want := range map[string]string{"2026.10.24-b1\n": "2026.10.24-b1", "\n": "", "current\n": "current"} {
		f := &recoverFixture{}
		code, diag, _ := runRecover(t, f, true, in, "--local", "ws1")
		if code != 0 || f.prep.Toolbox != want || !strings.Contains(diag, "Toolbox") {
			t.Errorf("%q: code=%d toolbox=%q diag=%q", in, code, f.prep.Toolbox, diag)
		}
	}
}

// Setting only --nixbox does not ask for the toolbox either way; the default stays latest.
func TestRecoverNixboxDefaultsToLatest(t *testing.T) {
	f := &recoverFixture{}
	if code, _, _ := runRecover(t, f, true, "\n", "--local", "ws1"); code != 0 || f.prep.Nixbox != "" {
		t.Fatalf("code=%d nixbox=%q", code, f.prep.Nixbox)
	}
}
