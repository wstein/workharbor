// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type acceptanceResultDiagnostics struct {
	bytes.Buffer
	first  func()
	fail   bool
	called bool
}

func (w *acceptanceResultDiagnostics) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *acceptanceResultDiagnostics) Write(p []byte) (int, error) {
	if !w.called {
		w.called = true
		if w.first != nil {
			w.first()
		}
		if w.fail {
			return 0, errors.New("injected preview write failure")
		}
	}
	return w.Buffer.Write(p)
}

func acceptanceResultFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"debian:bookworm"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func acceptanceResultErrorLines(diagnostics string) []string {
	var found []string
	for _, line := range strings.Split(diagnostics, "\n") {
		if strings.HasPrefix(line, "whr: ") {
			found = append(found, line)
		}
	}
	return found
}

func acceptanceResultExecute(t *testing.T, ctx context.Context, args []string, diagnostics *acceptanceResultDiagnostics, handlers map[string]Handler, want int, jsonMode bool) output.Envelope {
	t.Helper()
	var out bytes.Buffer
	if jsonMode {
		args = append(args, "--json")
	}
	code := Execute(ctx, args, Env{Stdout: &out, Stderr: diagnostics, Getenv: func(string) string { return "" }, IsTerminal: func(io.Writer) bool { return false }}, handlers)
	if code != want {
		t.Fatalf("init exit = %d, want %d; stdout=%s stderr=%s", code, want, terminal.EscapeClip(out.String(), 1024), terminal.EscapeClip(diagnostics.String(), 1024))
	}
	if !jsonMode {
		if want != 0 && out.Len() != 0 {
			t.Fatalf("human failure polluted stdout: %q", out.String())
		}
		if want == 0 && !strings.Contains(out.String(), "Initialized workspace configuration") {
			t.Fatalf("successful init lacks human result: %q", out.String())
		}
		if want != 0 && len(acceptanceResultErrorLines(diagnostics.String())) != 1 {
			t.Fatalf("human failure needs exactly one CLI diagnostic: %q", acceptanceResultErrorLines(diagnostics.String()))
		}
		return output.Envelope{}
	}
	decoder := json.NewDecoder(&out)
	var envelope output.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout carries more than one JSON document: %v", err)
	}
	if envelope.Command != "workspace.init" || envelope.SchemaVersion != 1 || envelope.ExitCode != want || envelope.OK != (want == 0) {
		t.Fatalf("init envelope disagrees with process result: command=%q schema=%d code=%d ok=%t", envelope.Command, envelope.SchemaVersion, envelope.ExitCode, envelope.OK)
	}
	if want == 0 {
		if envelope.Error != nil || envelope.Result == nil || len(acceptanceResultErrorLines(diagnostics.String())) != 0 {
			t.Fatalf("successful init represented as failure: error=%v, result_present=%t, diagnostic_lines=%v", envelope.Error, envelope.Result != nil, acceptanceResultErrorLines(diagnostics.String()))
		}
	} else {
		name, _ := exitcode.Name(want)
		if envelope.Error == nil || envelope.Error.Code != name || envelope.Error.Message == "" {
			t.Fatalf("JSON error does not match exit table: %+v", envelope)
		}
		line := "whr: " + terminal.EscapeClip(envelope.Error.Message, 1024) + "\n"
		if len(acceptanceResultErrorLines(diagnostics.String())) != 1 || !strings.Contains(diagnostics.String(), line) {
			t.Fatalf("JSON/stderr messages disagree: %+v / %q", envelope.Error, acceptanceResultErrorLines(diagnostics.String()))
		}
	}
	return envelope
}

func TestInitExitCodes(t *testing.T) {
	for _, kind := range []string{"warning-success", "usage", "absent", "conflict", "needs-human", "interrupted", "partial", "internal"} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", kind, jsonMode), func(t *testing.T) {
				dir := acceptanceResultFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				diagnostics := &acceptanceResultDiagnostics{}
				handlers := DefaultHandlers()
				args := []string{"init", dir, "--non-interactive", "--yes"}
				want := 0
				switch kind {
				case "usage":
					want = 64
					args = append(args, "--human-shell=unsupported")
				case "absent":
					want = 65
					args[1] = filepath.Join(dir, "absent")
				case "conflict":
					want = 68
					if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte(`{"schema_version":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				case "needs-human":
					want = 70
					args = []string{"init", dir, "--non-interactive"}
				case "interrupted":
					want = 72
					diagnostics.first = cancel
				case "partial":
					want = 73
					// Inject an apply outcome through the existing private seam.
					// This checks reporting; it does not claim real rollback failure.
					handlers["workspace.init"] = func(ctx context.Context, call Call) (Result, error) {
						return initWithApply(ctx, call, func(_ context.Context, p *scaffold.Proposal) (scaffold.Preview, error) {
							view := p.Preview()
							view.Remaining = []string{"workharbor.json", ".whr-init-tmp-journal"}
							return view, exitcode.Errorf(exitcode.PathPartialFailure, "injected incomplete init rollback")
						})
					}
				case "internal":
					want = 74
					diagnostics.fail = true
				}
				envelope := acceptanceResultExecute(t, ctx, args, diagnostics, handlers, want, jsonMode)
				if kind == "partial" {
					for _, path := range []string{"workharbor.json", ".whr-init-tmp-journal"} {
						if !strings.Contains(diagnostics.String(), "remaining: "+path) {
							t.Fatalf("partial result lost recovery diagnostic %s", path)
						}
					}
					if jsonMode {
						view, ok := envelope.Result.(map[string]any)
						if !ok || fmt.Sprint(view["remaining"]) != "[workharbor.json .whr-init-tmp-journal]" {
							t.Fatalf("partial JSON lost structured recovery paths: %+v", envelope.Result)
						}
					}
				}
				if want != 0 {
					if _, err := os.Lstat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
						t.Fatalf("failed command created agent workflow: %v", err)
					}
				}
			})
		}
	}
}

func TestInitExitCodeExistingFileVsReplaced(t *testing.T) {
	for _, kind := range []string{"existing-unapproved", "changed-after-preview", "replaced-after-preview"} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", kind, jsonMode), func(t *testing.T) {
				dir := acceptanceResultFixture(t)
				target := filepath.Join(dir, "workharbor.json")
				retained := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
				if err := os.WriteFile(target, retained, 0600); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"init", dir, "--non-interactive", "--yes", "--name=replacement"}
				diagnostics := &acceptanceResultDiagnostics{}
				want := 70
				if kind != "existing-unapproved" {
					want = 68
					args = append(args, "--overwrite=workharbor.json")
					diagnostics.first = func() {
						retained = []byte("foreign bytes introduced after preview\n")
						path := target
						if kind == "replaced-after-preview" {
							path = filepath.Join(dir, "incoming")
						}
						if err := os.WriteFile(path, retained, 0600); err != nil {
							t.Fatal(err)
						}
						if kind == "replaced-after-preview" {
							if err := os.Rename(path, target); err != nil {
								t.Fatal(err)
							}
						}
						info, err = os.Stat(target)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				acceptanceResultExecute(t, context.Background(), args, diagnostics, DefaultHandlers(), want, jsonMode)
				if kind != "existing-unapproved" && !diagnostics.called {
					t.Fatal("preview mutation never ran")
				}
				raw, err := os.ReadFile(target)
				after, statErr := os.Stat(target)
				if err != nil || statErr != nil || !bytes.Equal(raw, retained) || !os.SameFile(info, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
					t.Fatalf("refusal altered retained or foreign manifest: %q, %v, %v", raw, err, statErr)
				}
				if kind == "existing-unapproved" && !strings.Contains(diagnostics.String(), "--overwrite workharbor.json") {
					t.Fatal("needs-human diagnostic omitted the consent path")
				}
				for _, path := range []string{"projects", "AGENTS.md", ".claude", ".whr-init-tmp-journal"} {
					if _, err := os.Lstat(filepath.Join(dir, path)); !os.IsNotExist(err) {
						t.Fatalf("refusal left newly created path %s: %v", path, err)
					}
				}
			})
		}
	}
}
