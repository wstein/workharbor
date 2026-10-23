// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
)

func TestMain(m *testing.M) {
	if os.Getenv("WHR_TEST_DOCKER") == "1" {
		os.Exit(localShellFakeDocker(os.Args[1:]))
	}
	ptytest.Main(m, map[string]func([]string) int{"local-shell": localShellPTYEntry, "local-shell-nonterminal": localShellPTYNonterminal, "local-shell-redirect": localShellPTYRedirect, "wizard": func(args []string) int {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return run(ctx, args, os.Stdout, os.Stderr)
	}})
}
func wizardFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestWizardPTYHappyPath(t *testing.T) {
	dir := wizardFixture(t)
	s := ptytest.StartFunc(t, "wizard", ptytest.Options{Args: []string{"init", dir}})
	s.Expect("Workspace name (optional) [")
	s.Send("demo\n")
	s.Expect("Project URL (optional) [")
	s.Send("\n")
	s.Expect("Starting directory [.")
	s.Send("\n")
	s.Expect("Reuse existing devcontainer [")
	s.Send("\n")
	s.Expect("Human shell (fish/bash/zsh/nu) [fish")
	s.Send("\n")
	s.Expect("Console mode (herdr/plain) [herdr")
	s.Send("\n")
	s.Expect("Shared shortcuts (on/off) [on")
	s.Send("\n")
	s.Expect("not creation-ready")
	s.Expect("Review:")
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
		t.Fatal("write before consent")
	}
	s.Send("write\n")
	s.ExpectExit(0)
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); err != nil {
		t.Fatal(err)
	}
}
func TestWizardPTYInterruptRollsBack(t *testing.T) {
	for _, sig := range []string{"ctrl-c", "sigterm"} {
		t.Run(sig, func(t *testing.T) {
			dir := wizardFixture(t)
			s := ptytest.StartFunc(t, "wizard", ptytest.Options{Args: []string{"init", dir}})
			s.Expect("Workspace name (optional) [")
			if sig == "ctrl-c" {
				s.SendKey(ptytest.KeyCtrlC)
			} else {
				if err := syscall.Kill(s.Pid(), syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			s.ExpectExit(72)
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("interrupted prompt changed files")
			}
		})
	}
}

func TestWizardPTYDevcontainerSelection(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("{ // custom\n \"image\":\"debian:bookworm\", }\n")
	if err := os.WriteFile(filepath.Join(dir, "chosen.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := ptytest.StartFunc(t, "wizard", ptytest.Options{Args: []string{"init", dir}})
	s.Expect("Workspace name (optional) [")
	s.Send("demo\n")
	s.Expect("Project URL (optional) [")
	s.Send("\n")
	s.Expect("Starting directory [.")
	s.Send("\n")
	s.Expect("Reuse existing devcontainer [")
	s.Send("../escape\n")
	s.Expect("safe relative file path")
	s.Send("chosen.json\n")
	s.Expect("Human shell (fish/bash/zsh/nu) [fish")
	s.Send("\n")
	s.Expect("Console mode (herdr/plain) [herdr")
	s.Send("\n")
	s.Expect("Shared shortcuts (on/off) [on")
	s.Send("\n")
	s.Expect("reuse file chosen.json")
	s.Expect("Review:")
	s.Send("write\n")
	s.ExpectExit(0)
	got, err := os.ReadFile(filepath.Join(dir, "chosen.json"))
	if err != nil || string(got) != string(raw) {
		t.Fatal("selected devcontainer changed", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"devcontainer_path": "chosen.json"`) {
		t.Fatal("wizard selection missing")
	}
}

func TestWizardPTYAgentsConsentAndRefusal(t *testing.T) {
	for _, kind := range []string{"decline", "refused", "flagged refusal"} {
		t.Run(kind, func(t *testing.T) {
			dir := wizardFixture(t)
			original := []byte("owner\r\n")
			if kind != "decline" {
				original = []byte("<!-- whr:agents-block end -->\n")
			}
			target := filepath.Join(dir, "AGENTS.md")
			if err := os.WriteFile(target, original, 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"init", dir}
			if kind == "flagged refusal" {
				args = append(args, "--overwrite=AGENTS.md")
			}
			s := ptytest.StartFunc(t, "wizard", ptytest.Options{Args: args})
			if kind == "flagged refusal" {
				s.ExpectExit(68)
			} else {
				s.Expect("Workspace name (optional) [")
				s.Send("review\n")
				s.Expect("Review:")
				s.Send("write\n")
				if kind == "decline" {
					s.Expect("Modify AGENTS.md? [y/N]: ")
					s.Send("no\n")
				}
				s.ExpectExit(0)
			}
			raw, err := os.ReadFile(target)
			if err != nil || string(raw) != string(original) {
				t.Fatalf("declined/refused path changed: %v", err)
			}
			if kind != "decline" && strings.Contains(string(s.Transcript()), "Modify AGENTS.md?") {
				t.Fatal("refused edit offered")
			}
			if kind == "flagged refusal" {
				if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
					t.Fatal("refusal wrote manifest")
				}
			}
		})
	}
}
