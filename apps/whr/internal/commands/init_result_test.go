// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPublicPartialRecoverySurvivesMessageClip(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
			t.Fatal(err)
		}
		remaining := []string{strings.Repeat("nested/", 200) + "file\u202e", ".whr-init-tmp-journal"}
		handlers := DefaultHandlers()
		handlers["workspace.init"] = func(ctx context.Context, call Call) (Result, error) {
			return initWithApply(ctx, call, func(ctx context.Context, p *scaffold.Proposal) (scaffold.Preview, error) {
				result := p.Preview()
				result.Remaining = remaining
				err := exitcode.Errorf(exitcode.PathPartialFailure, "%s", strings.Repeat("long failure message ", 100))
				err.Result = result
				return result, err
			})
		}
		var out, diagnostics bytes.Buffer
		args := []string{"init", dir, "--non-interactive", "--yes"}
		if jsonMode {
			args = append(args, "--json")
		}
		code := Execute(context.Background(), args, Env{Stdout: &out, Stderr: &diagnostics}, handlers)
		if code != 73 {
			t.Fatalf("partial precedence lost: %d", code)
		}
		for _, name := range remaining {
			if !strings.Contains(diagnostics.String(), terminal.Escape(name)) {
				t.Errorf("recovery path missing from human diagnostics: length=%d", len(name))
			}
		}
		if strings.ContainsRune(diagnostics.String(), '\u202e') {
			t.Fatal("raw unsafe text in recovery diagnostic")
		}
		if jsonMode {
			var env struct {
				Result struct {
					Remaining []string `json:"remaining"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if len(env.Result.Remaining) != 2 || env.Result.Remaining[0] != remaining[0] {
				t.Fatal("JSON recovery list lost bytes")
			}
		} else if out.Len() != 0 {
			t.Fatal("human failure polluted stdout")
		}
	}
}
