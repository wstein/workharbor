// SPDX-License-Identifier: EUPL-1.2

package localshell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const shellManifest = `{"schema_version":2,"devcontainer_path":"missing.json","projects_path":"missing","bundles":[]}`

func writeShellManifest(t *testing.T, raw string) string {
	t.Helper()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, workspacecfg.ManifestFile), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestLocalShellManifestUsesWorkspacecfg(t *testing.T) {
	// A manifest alone suffices; neither Git nor devcontainer/projects is read.
	t.Setenv("PATH", t.TempDir())
	for _, folder := range []string{"", t.TempDir(), writeShellManifest(t, shellManifest), writeShellManifest(t, strings.Replace(shellManifest, `"schema_version":2`, `"schema_version":1`, 1))} {
		got, err := ManifestShell(context.Background(), folder)
		if got != "fish" || err != nil {
			t.Fatalf("folder %q: got %q, %v", folder, got, err)
		}
	}
	for _, raw := range []string{"{", shellManifest + `{}`, strings.Replace(shellManifest, `"bundles":[]`, `"bundles":[],"bundles":[]`, 1), strings.Replace(shellManifest, `"projects_path":"missing"`, `"projects_path":"../escape"`, 1), `{}`, `null`} {
		folder := writeShellManifest(t, raw)
		result := workspacecfg.CheckManifest([]byte(raw))
		if len(result.Findings) == 0 {
			t.Fatal("fixture must fail workspacecfg")
		}
		requireManifestConflict(t, folder)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, folder := range []string{"", t.TempDir()} {
		if _, err := ManifestShell(ctx, folder); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled folder %q: %v", folder, err)
		}
	}
}

func requireManifestConflict(t *testing.T, folder string) error {
	t.Helper()
	got, err := ManifestShell(context.Background(), folder)
	var cli *exitcode.Error
	if got != "" || !errors.As(err, &cli) || cli.Path != exitcode.PathConflict || cli.Code() != 68 {
		t.Fatalf("got %q, %v; want conflict 68", got, err)
	}
	if terminal.Escape(err.Error()) != err.Error() {
		t.Fatalf("unsafe diagnostic: %q", err.Error())
	}
	return err
}

func TestLocalShellManifestRefusesSymlinkOrOversize(t *testing.T) {
	for _, n := range []int{workspacecfg.MaxManifestBytes - 1, workspacecfg.MaxManifestBytes, workspacecfg.MaxManifestBytes + 1} {
		folder := writeShellManifest(t, shellManifest+strings.Repeat(" ", n-len(shellManifest)))
		if n > workspacecfg.MaxManifestBytes {
			requireManifestConflict(t, folder)
			continue
		}
		got, err := ManifestShell(context.Background(), folder)
		if got != "fish" || err != nil {
			t.Fatalf("%d bytes: got %q, %v", n, got, err)
		}
	}
	for _, kind := range []string{"symlink", "dangling", "directory", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			folder := t.TempDir()
			path := filepath.Join(folder, workspacecfg.ManifestFile)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(filepath.Join(writeShellManifest(t, shellManifest), workspacecfg.ManifestFile), path)
			case "dangling":
				err = os.Symlink(filepath.Join(folder, "missing"), path)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			requireManifestConflict(t, folder)
		})
	}
	folder := t.TempDir()
	link := filepath.Join(t.TempDir(), "folder")
	if err := os.Symlink(folder, link); err != nil {
		t.Fatal(err)
	}
	requireManifestConflict(t, link)
	requireManifestConflict(t, filepath.Join(folder, "missing"))
	file := filepath.Join(writeShellManifest(t, shellManifest), workspacecfg.ManifestFile)
	requireManifestConflict(t, file)
}

func TestLocalShellManifestValidatesHumanShell(t *testing.T) {
	for _, shell := range []string{"fish", "bash", "zsh", "nu"} {
		folder := writeShellManifest(t, strings.TrimSuffix(shellManifest, "}")+`,"human_shell":"`+shell+`"}`)
		got, err := ManifestShell(context.Background(), folder)
		if got != shell || err != nil {
			t.Fatalf("shell %q: got %q, %v", shell, got, err)
		}
	}
	for _, value := range []string{`""`, `"sh"`, `"Fish"`, `"fish -l"`, `"/bin/bash"`, `null`, `7`, `false`, `[]`, `{}`, `"fish\n\u001b\u202e"`} {
		folder := writeShellManifest(t, strings.TrimSuffix(shellManifest, "}")+`,"human_shell":`+value+`}`)
		err := requireManifestConflict(t, folder)
		if !strings.Contains(err.Error(), "human_shell") {
			t.Fatalf("missing field name: %v", err)
		}
		if value == `"sh"` && !strings.Contains(err.Error(), `"sh"`) {
			t.Fatalf("missing rejected value: %v", err)
		}
	}
	// Duplicate shell keys must not select either value or fall back.
	requireManifestConflict(t, writeShellManifest(t, strings.TrimSuffix(shellManifest, "}")+`,"human_shell":"fish","human_shell":"bash"}`))
}
