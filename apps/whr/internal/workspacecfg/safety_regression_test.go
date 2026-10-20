// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func TestValidateRefusesInternalSymlinkComponents(t *testing.T) {
	for _, tc := range []struct{ name, path, target, code string }{
		{"devcontainer", ".devcontainer", "real", "devcontainer.unsafe_path"},
		{"projects", "p", "realp", "projects.unsafe_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := validDir(t)
			if tc.name == "projects" {
				write(t, dir, "workharbor.json", strings.Replace(manifestV1, `"projects_path":"projects"`, `"projects_path":"p/projects"`, 1))
				write(t, dir, "p/projects/.gitkeep", "")
			}
			if r := validate(t, dir, workspacecfg.Options{GitFree: true}); !r.Valid {
				t.Fatalf("fixture before symlink: %+v", r.Findings)
			}
			if err := os.Rename(filepath.Join(dir, tc.path), filepath.Join(dir, tc.target)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tc.target, filepath.Join(dir, tc.path)); err != nil {
				t.Fatal(err)
			}
			r := validate(t, dir, workspacecfg.Options{GitFree: true})
			if r.Valid || errorCodes(r) != tc.code {
				t.Fatalf("internal symlink accepted or wrong finding: valid=%v findings=%+v", r.Valid, r.Findings)
			}
		})
	}
}

// The child records completed writes so the parent can distinguish stopping the
// producer from retaining only a prefix after draining its entire 64 MiB output.
func TestExecRunnerOutputCapWriter(t *testing.T) {
	progress := os.Getenv("WHR_TEST_OUTPUT_CAP_PROGRESS")
	if progress == "" {
		return
	}
	chunk := make([]byte, 64<<10)
	for written := 0; written < 64<<20; {
		n, err := os.Stdout.Write(chunk)
		if err != nil {
			os.Exit(2)
		}
		written += n
		if err := os.WriteFile(progress+".next", []byte(strconv.Itoa(written)), 0o600); err != nil {
			os.Exit(3)
		}
		if err := os.Rename(progress+".next", progress); err != nil {
			os.Exit(4)
		}
	}
	os.Exit(0)
}

func TestExecRunnerOutputCapStopsWriter(t *testing.T) {
	const capBytes = 1 << 20
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	progress := filepath.Join(t.TempDir(), "progress")
	// Atomic receipt replacement preserves the last complete count if killed.
	if err := os.WriteFile(progress, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := (workspacecfg.ExecRunner{}).Run(context.Background(), workspacecfg.GitCmd{
		Path: executable, Args: []string{"-test.run=^TestExecRunnerOutputCapWriter$"},
		Env: []string{"WHR_TEST_OUTPUT_CAP_PROGRESS=" + progress}, Dir: "/", MaxOutput: capBytes, Timeout: 10 * time.Second,
	})
	if err != nil || !out.Truncated || len(out.Stdout) != capBytes || out.Exit != -1 {
		t.Fatalf("cap result: len=%d truncated=%v exit=%d err=%v stderr=%q", len(out.Stdout), out.Truncated, out.Exit, err, out.Stderr)
	}
	raw, err := os.ReadFile(progress)
	if err != nil {
		t.Fatal(err)
	}
	written, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("bad writer receipt %q: %v", raw, err)
	}
	if written >= 64<<20 {
		t.Fatal("the capped reader drained all 64 MiB instead of stopping the writer")
	}
	t.Logf("retained=%d bytes; last completed writer receipt=%d bytes (<64 MiB)", len(out.Stdout), written)
}
