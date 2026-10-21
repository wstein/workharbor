// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

// Only the Go test executable is reused; no CLI artifact is built or installed.
func TestFirstIncrementCommandProcess(t *testing.T) {
	if os.Getenv("WHR_ACCEPTANCE_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(commands.Execute(context.Background(), os.Args[i+1:], commands.Env{Stdout: os.Stdout, Stderr: os.Stderr}, commands.DefaultHandlers()))
		}
	}
	os.Exit(99)
}

func TestCompletionExecutesInShells(t *testing.T) {
	drivers := map[string][]string{
		// Supply editor state at bash-completion's initialization boundary.
		"bash": {"--noprofile", "--norc", "-c", `source "$1"
_init_completion() { words=(whr completion ""); cur=""; prev=completion; cword=2; }
__start_whr
printf '%s\n' "${COMPREPLY[@]}"`, "completion-test"},
		// Capture _describe's candidates without requiring a live ZLE editor.
		"zsh": {"-f", "-c", `compdef() { :; }
_describe() { print -rl -- "${completions[@]}"; }
source "$1"
words=(whr completion ""); CURRENT=3; PREFIX=""; _whr`, "completion-test"},
		"fish": {"--no-config", "-c", `source $argv[1]; complete -C 'whr completion '`},
	}
	for shell, driver := range drivers {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("acceptance gap: %s unavailable: %v", shell, err)
			}
			dir := t.TempDir()
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(bin, "'", "'\"'\"'") + "' -test.run=^TestFirstIncrementCommandProcess$ -- \"$@\"\n"
			if err := os.WriteFile(filepath.Join(dir, "whr"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			generated := run(t, nil, "completion", shell)
			if generated.code != 0 {
				t.Fatalf("generation: %+v", generated)
			}
			script := filepath.Join(dir, "completion")
			if err := os.WriteFile(script, []byte(generated.stdout), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, path, append(driver, script)...)
			cmd.Env = []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "TERM=dumb", "WHR_ACCEPTANCE_PROCESS=1"}
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s execution: %v\n%s", shell, err, out)
			}
			for _, want := range []string{"bash", "zsh", "fish"} {
				found := false
				for _, candidate := range strings.Fields(string(out)) {
					if strings.SplitN(candidate, ":", 2)[0] == want {
						found = true
					}
				}
				if !found {
					t.Errorf("%s did not offer %s: %q", shell, want, out)
				}
			}
		})
	}
}

// Source-only available-command coverage. The parent has no init handler;
// binary, init-flow and real network-namespace acceptance remain separate.
func TestFirstIncrementAvailableCommandsOffline(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, home)
	}
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "WORKHARBOR_PROFILE", "WORKHARBOR_TOKEN", "GITHUB_TOKEN", "SSH_AUTH_SOCK"} {
		t.Setenv(key, "")
	}
	workspace := goodDir(t)
	workspaceBefore := doctorTreeSnapshot(t, workspace)
	p := fakePorts()
	calls := 0
	p.DialUnix = func(context.Context, string) error { calls++; return os.ErrNotExist }
	h := withDoctor(p)
	before := doctorTreeSnapshot(t, home)
	for _, args := range [][]string{{"version", "--json"}, {"validate", workspace, "--json"}, {"help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}} {
		if got := run(t, h, args...); got.code != 0 {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	if calls != 0 {
		t.Fatalf("non-doctor commands dialed %d times", calls)
	}
	got := run(t, h, "doctor", "--json")
	if got.code != 68 || !strings.Contains(got.stdout, "container.socket") || calls != 5 {
		t.Fatalf("denied doctor: calls=%d result=%+v", calls, got)
	}
	tested := 0
	for _, entry := range commands.Table() {
		if entry.BlockedOn != "owner_api" {
			continue
		}
		tested++
		for _, spelling := range spellings(entry) {
			got := run(t, h, append(append([]string{}, spelling...), "--json")...)
			if got.code != 64 || decodeEnvelope(t, got.stdout)["command"] != entry.ID {
				t.Errorf("unavailable %v: %+v", spelling, got)
			}
		}
	}
	if tested == 0 {
		t.Fatal("no owner-API commands exercised")
	}
	if calls != 5 {
		t.Errorf("unavailable commands dialed: %d", calls)
	}
	if after := doctorTreeSnapshot(t, workspace); !reflect.DeepEqual(workspaceBefore, after) {
		t.Fatal("offline validation changed the workspace")
	}
	if after := doctorTreeSnapshot(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("profile/credential tree changed: before=%v after=%v", before, after)
	}
}
