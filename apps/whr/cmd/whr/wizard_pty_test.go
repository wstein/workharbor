// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
)

func TestMain(m *testing.M) {
	ptytest.Main(m, map[string]func([]string) int{"wizard": func(args []string) int {
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
