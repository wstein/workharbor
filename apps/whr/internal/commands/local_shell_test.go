// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localshell"
	"io"
	"os"
	"strings"
	"testing"
)

func shellPorts(t *testing.T, status int, calls *int) localshell.Ports {
	id := strings.Repeat("a", 64)
	return localshell.Ports{
		Discover: func(context.Context) (localdocker.Snapshot, error) {
			*calls++
			return localdocker.Snapshot{Endpoint: "unix:///test/socket", Workspaces: []localdocker.Workspace{{ID: id, Name: "sample", Role: "primary", State: "running", WorkspaceFolder: "/host/project", Labels: map[string]string{"workharbor.workspace": "x"}}}}, nil
		},
		Inspect: func(context.Context, string, string) ([]byte, error) {
			return []byte(fmt.Sprintf(`{"Id":%q,"Name":"/sample","State":{"Status":"running","Running":true},"Config":{"User":"captain","WorkingDir":"/workspace"},"Mounts":[{"Source":"/host/project","Destination":"/workspace"}]}`, id)), nil
		},
		Manifest: func(context.Context, string) (string, error) { return "fish", nil }, Exec: func(context.Context, localshell.ExecPlan, localshell.Streams) (int, error) { return status, nil }, Reachable: func(context.Context, string, string) bool { return true },
	}
}
func localShellRun(t *testing.T, h map[string]commands.Handler, stdinTTY, stdoutTTY bool, args ...string) result {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	in, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var diag bytes.Buffer
	code := commands.Execute(context.Background(), args, commands.Env{Stdout: out, Stderr: &diag, Stdin: strings.NewReader(""), TerminalInput: in, InputTerminal: stdinTTY, Getenv: func(k string) string {
		if k == "TERM" {
			return "xterm-256color"
		}
		return ""
	}, IsTerminal: func(w io.Writer) bool { return stdoutTTY && w == out }}, h)
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return result{code, string(raw), diag.String()}
}
func TestLocalShellCanonicalAndAlias(t *testing.T) {
	for _, status := range []int{0, 7, 64, 68, 72, 126, 127, 137, 255} {
		calls := 0
		h := baseHandlers()
		h["workspace.shell"] = commands.LocalShellHandler(shellPorts(t, status, &calls))
		for _, sp := range [][]string{{"workspace", "shell"}, {"ws", "shell"}, {"shell"}} {
			args := append(append([]string{}, sp...), "--local", "sample")
			got := localShellRun(t, h, true, true, args...)
			if got.code != status || got.stdout != "" || got.err != "" {
				t.Fatalf("status%d %v -> %+v", status, args, got)
			}
		}
		if calls != 3 {
			t.Fatalf("calls=%d", calls)
		}
	}
}
func TestLocalShellRejectsJSONOrTrailingCommandBeforeDocker(t *testing.T) {
	calls := 0
	h := baseHandlers()
	h["workspace.shell"] = commands.LocalShellHandler(shellPorts(t, 0, &calls))
	for _, args := range [][]string{{"shell", "--local", "sample", "--json"}, {"--json", "ws", "shell", "--local", "sample"}, {"shell", "--local", "sample", "echo", "hi"}, {"shell", "--local"}, {"shell", "--local", "sample", "--shell", "bash"}, {"shell", "--local", "sample", "--host", "ssh://remote"}} {
		got := localShellRun(t, h, true, true, args...)
		if got.code != 64 {
			t.Fatalf("%v: %+v", args, got)
		}

		for _, arg := range args {
			if arg == "--json" {
				env := decodeEnvelope(t, got.stdout)
				if env["command"] != "workspace.shell" || env["ok"] != false || env["exit_code"] != float64(64) {
					t.Fatalf("wrong local JSON refusal: %v", env)
				}
				if err := compileSchema(t).Validate(env); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, term := range [][2]bool{{false, true}, {true, false}} {
		got := localShellRun(t, h, term[0], term[1], "shell", "--local", "sample")
		if got.code != 64 {
			t.Fatalf("tty%v: %+v", term, got)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid usage contacted Docker %d times", calls)
	}
}
func TestConsoleLocalRefusesWithHostSessionGuidance(t *testing.T) {
	for _, sp := range [][]string{{"workspace", "console"}, {"ws", "console"}, {"console"}} {
		args := append(append([]string{}, sp...), "sample", "--local")
		got := localShellRun(t, baseHandlers(), true, true, args...)
		if got.code != 64 || !strings.Contains(got.err, "whr shell --local <ws>") || !strings.Contains(got.err, "host multiplexer session") {
			t.Fatalf("%v: %+v", args, got)
		}
	}
}
func TestLocalFlagsNeverEnableSSHOrAgentAttach(t *testing.T) {
	calls := 0
	h := baseHandlers()
	spy := func(context.Context, commands.Call) (commands.Result, error) { calls++; return commands.Result{}, nil }
	h["workspace.ssh"], h["agent.attach"] = spy, spy
	for _, args := range [][]string{{"ssh", "--local", "sample"}, {"workspace", "ssh", "sample", "--local"}, {"agent", "attach", "sample", "--local"}} {
		if got := localShellRun(t, h, true, true, args...); got.code != 64 {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	if calls != 0 {
		t.Fatal("local flag reached remote handler")
	}
}
func TestRemoteShellWithoutLocalStillRefuses(t *testing.T) {
	calls := 0
	h := baseHandlers()
	h["workspace.shell"] = commands.LocalShellHandler(shellPorts(t, 0, &calls))
	for _, args := range [][]string{{"shell", "sample"}, {"ws", "shell", "sample", "--json"}, {"workspace", "shell"}} {
		got := localShellRun(t, h, true, true, args...)
		if got.code != 64 || !strings.Contains(got.err, "owner_api") {
			t.Fatalf("%v: %+v", args, got)
		}
	}

	for _, flag := range []string{"--bogus", "--cpus"} {
		args := []string{"shell", flag, "sample"}
		if flag == "--cpus" {
			args = []string{"shell", flag, "2", "sample"}
		}
		got := localShellRun(t, h, true, true, args...)
		if got.code != 64 || !strings.Contains(got.err, "unknown flag: "+flag) || !strings.Contains(got.err, "whr shell --help") || strings.Contains(got.err, "owner_api") {
			t.Fatalf("remote unknown flag %s: %+v", flag, got)
		}
	}
	if calls != 0 {
		t.Fatal("remote shell contacted Docker")
	}
}
func TestLocalShellHelpExplainsDetachAndOrphanLimits(t *testing.T) {
	got := run(t, nil, "shell", "--help")
	for _, text := range []string{"--local", "ctrl-] ctrl-^", "does not guarantee", "direct terminal", "not filtered", "pane title", "tab label"} {
		if got.code != 0 || !strings.Contains(got.stdout, text) {
			t.Fatalf("help lacks %q: %+v", text, got)
		}
	}
}
