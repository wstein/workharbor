package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

func TestPrefixOfDev(t *testing.T) {
	if got := prefixOf(OffboardEnv{}, true, "/home/u"); got != "/home/u/.local" {
		t.Errorf("dev prefix = %q", got)
	}
	if got := prefixOf(OffboardEnv{Prefix: "/x"}, true, "/home/u"); got != "/x" {
		t.Errorf("an explicit prefix must win, got %q", got)
	}
	if got := prefixOf(OffboardEnv{}, false, "/home/u"); got == "/home/u/.local" {
		t.Errorf("without --dev the default prefix applies, got %q", got)
	}
}

func TestDevIsOneRootFlagOnEverySubcommand(t *testing.T) {
	r := newOffboardRig(t)
	for _, args := range [][]string{{"offboard", "host", "--dev"}, {"--dev", "offboard", "host"}, {"setup", "host", "--dev", "--dry-run"}, {"doctor", "--dev", "--help"}, {"version", "--dev"}} {
		var out, errOut bytes.Buffer
		env := r.env
		env.Stdout, env.Stderr = &out, &errOut
		code := Execute(context.Background(), env, args)
		if strings.Contains(errOut.String(), "unknown flag") || code == exitcode.Usage && strings.Contains(errOut.String(), "--dev") && strings.Contains(errOut.String(), "unknown") {
			t.Errorf("%v: --dev refused: %s", args, errOut.String())
		}
	}
}

func TestDevAloneIsNotAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), Getenv: func(string) string { return "" }}
	code := Execute(context.Background(), env, []string{"--dev"})
	if code != exitcode.OK || !strings.Contains(out.String()+errOut.String(), "Usage") {
		t.Errorf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
}

func TestDevIsDefinedOnce(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), Getenv: func(string) string { return "" }}
	Execute(context.Background(), env, []string{"doctor", "--help"})
	if n := strings.Count(out.String(), "      --dev "); n != 1 {
		t.Errorf("--dev appears %d times in doctor help:\n%s", n, out.String())
	}
	if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "default prefix: $HOME/.local; --prefix, where a command has it, wins") {
		t.Errorf("the --dev help line changed:\n%s", out.String())
	}
}

// offboard host --dev takes a binary under $HOME/.local as installed, refuses a
// relative HOME like setup and doctor do, and without --dev refuses the binary.
func TestOffboardDevAcceptsABinaryUnderHomeLocal(t *testing.T) {
	r := newOffboardRig(t)
	home := t.TempDir()
	exe := filepath.Join(home, ".local", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(exe), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
		t.Fatal(err)
	}
	r.rig.env.Executable = func() (string, error) { return exe, nil }
	r.env.Setup = r.rig.env
	r.env.Offboard.Prefix = ""
	run := func(home string, args ...string) (int, string) {
		var out, errOut bytes.Buffer
		env := r.env
		env.Stdout, env.Stderr = &out, &errOut
		env.Getenv = func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		}
		code := Execute(context.Background(), env, append([]string{"offboard", "host"}, args...))
		return code, errOut.String()
	}
	if code, errOut := run(home, "--dev"); code != exitcode.OK || strings.Contains(errOut, "note (dry run)") {
		t.Errorf("--dev under $HOME/.local: exit %d\n%s", code, errOut)
	}
	if _, errOut := run(home); !strings.Contains(errOut, "note (dry run)") {
		t.Errorf("without --dev the binary is not installed:\n%s", errOut)
	}
	if _, errOut := run("relative", "--dev"); !strings.Contains(errOut, "--dev needs an absolute HOME") {
		t.Errorf("a relative HOME with --dev:\n%s", errOut)
	}
}
