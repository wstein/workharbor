// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

type noRead struct{ t *testing.T }

func (r noRead) Read([]byte) (int, error) {
	r.t.Fatal("suppressed wizard read stdin")
	return 0, io.EOF
}
func TestWizardNeverPromptsWhenNonTerminalOrJSON(t *testing.T) {
	for _, tc := range []struct {
		in, errTTY bool
		flags      []string
	}{{false, true, nil}, {true, false, nil}, {true, true, []string{"--json"}}, {true, true, []string{"--non-interactive"}}, {true, true, []string{"--dry-run"}}} {
		var out, diag bytes.Buffer
		dir := initFixture(t)
		code := commands.Execute(context.Background(), append([]string{"init", dir, "--yes"}, tc.flags...), commands.Env{Stdout: &out, Stderr: &diag, Stdin: noRead{t}, InputTerminal: tc.in, IsTerminal: func(io.Writer) bool { return tc.errTTY }}, commands.DefaultHandlers())
		if code != 0 {
			t.Fatalf("%+v code=%d: %s", tc, code, diag.String())
		}
		if strings.Contains(diag.String(), "Review:") || strings.Contains(diag.String(), "Workspace name (optional)") {
			t.Fatal("suppressed wizard emitted prompt")
		}
	}
}
func TestWizardPromptsOnStderrOnly(t *testing.T) {
	dir := initFixture(t)
	var out, diag bytes.Buffer
	code := commands.Execute(context.Background(), []string{"init", dir}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("demo\n\n\n\n\n\n\nwrite\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, commands.DefaultHandlers())
	if code != 0 {
		t.Fatalf("%d: %s", code, diag.String())
	}
	if strings.Contains(out.String(), "Review:") || strings.Contains(out.String(), "Workspace name") {
		t.Fatal("prompt on stdout")
	}
	if !strings.Contains(diag.String(), "Review:") || !strings.Contains(diag.String(), "not creation-ready") {
		t.Fatal("missing stderr preview")
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); err != nil {
		t.Fatal(err)
	}
}
func TestWizardDeclineDoesNotWrite(t *testing.T) {
	dir := initFixture(t)
	var out, diag bytes.Buffer
	code := commands.Execute(context.Background(), []string{"init", dir, "--yes"}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("review\ncancel\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, commands.DefaultHandlers())
	if code != 72 {
		t.Fatalf("%d: %s", code, diag.String())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("cancel wrote files")
	}
}

type previewMutation struct {
	bytes.Buffer
	mutate func()
	done   bool
}

func (w *previewMutation) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *previewMutation) Write(p []byte) (int, error) {
	if !w.done && bytes.Contains(p, []byte("Review:")) {
		w.done = true
		w.mutate()
	}
	return w.Buffer.Write(p)
}
func TestWizardStalePreviewConflict(t *testing.T) {
	dir := initFixture(t)
	sentinel := []byte("changed while preview was displayed\n")
	diag := &previewMutation{mutate: func() {
		if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), sentinel, 0644); err != nil {
			t.Fatal(err)
		}
	}}
	var out bytes.Buffer
	code := commands.Execute(context.Background(), []string{"init", dir}, commands.Env{Stdout: &out, Stderr: diag, Stdin: strings.NewReader("review\nwrite\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, commands.DefaultHandlers())
	if code != 68 {
		t.Fatalf("%d: %s", code, diag.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err != nil || !bytes.Equal(data, sentinel) {
		t.Fatal("stale proposal overwrote appearing file")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects")); !os.IsNotExist(err) {
		t.Fatal("conflict left a created directory")
	}
}
