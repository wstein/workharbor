//go:build linux

// SPDX-License-Identifier: EUPL-1.2
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type signalCapture struct{ buffer bytes.Buffer }

func (b *signalCapture) String() string { return b.buffer.String() }
func (b *signalCapture) Bytes() []byte  { return b.buffer.Bytes() }
func (b *signalCapture) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 4<<20 {
		return 0, fmt.Errorf("test child output exceeds bound")
	}
	return b.buffer.Write(p)
}

// The child is this test compiler's own executable. Its helper branch calls
// actual main, so the unchanged NotifyContext boundary handles both signals.
func TestInitEntrypointSignalsCancelAndCleanUp(t *testing.T) {
	if os.Getenv("WHR_INIT_SIGNAL_HELPER") == "1" {
		os.Args = []string{"whr", "init", os.Getenv("WHR_INIT_SIGNAL_TARGET"), "--non-interactive", "--yes", "--name=changed", "--overwrite=workharbor.json", "--json"}
		main()
		return
	}
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
				t.Fatal(err)
			}
			dc := []byte(`{"image":"debian:bookworm"}`)
			if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), dc, 0644); err != nil {
				t.Fatal(err)
			}
			// A valid large no-edit input yields a >pipe-capacity overwrite preview.
			// The parent reads one line, signals, then drains: writes cannot begin
			// until after signal delivery; cancellation and rollback must leave the
			// original authored files unchanged even if context delivery races drain.
			old := append([]byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`), bytes.Repeat([]byte(" "), 60000)...)
			manifest := filepath.Join(dir, "workharbor.json")
			if err := os.WriteFile(manifest, old, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
			rootBefore, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			readEnd, writeEnd, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer readEnd.Close()
			defer writeEnd.Close()
			if _, err = unix.FcntlInt(writeEnd.Fd(), unix.F_SETPIPE_SZ, 4096); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestInitEntrypointSignalsCancelAndCleanUp$")
			privateHome := t.TempDir()
			cmd.Env = []string{"WHR_INIT_SIGNAL_HELPER=1", "WHR_INIT_SIGNAL_TARGET=" + dir, "HOME=" + privateHome, "PATH=" + privateHome, "TMPDIR=" + privateHome, "TERM=dumb"}
			cmd.Dir = dir
			var stdout, diagnostics signalCapture
			cmd.Stdout = &stdout
			cmd.Stderr = writeEnd
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			// From this point, Process is exclusively this test's started child.
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			reaped := false
			defer func() {
				if !reaped {
					_ = cmd.Process.Kill()
					select {
					case <-waited:
					case <-time.After(3 * time.Second):
						t.Error("owned child was not reaped")
					}
				}
			}()
			_ = writeEnd.Close()
			reader := bufio.NewReader(readEnd)
			first := make(chan string, 1)
			go func() { line, _ := reader.ReadString('\n'); first <- line }()
			select {
			case line := <-first:
				if !strings.HasPrefix(line, "reuse directory ") {
					t.Fatalf("child did not reach init preview: %q", line)
				}
				_, _ = diagnostics.Write([]byte(line))
			case <-ctx.Done():
				t.Fatal("child did not reach bounded preview")
			}
			// Signal exactly the process handle returned by our own successful Start.
			if err = cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			drained := make(chan error, 1)
			go func() { _, e := io.Copy(&diagnostics, reader); drained <- e }()
			select {
			case err = <-waited:
				reaped = true
			case <-ctx.Done():
				t.Fatal("child did not exit after signal")
			}
			select {
			case e := <-drained:
				if e != nil {
					t.Fatal(e)
				}
			case <-ctx.Done():
				t.Fatal("child diagnostics did not close")
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 72 {
				t.Fatalf("signal exit=%v, stdout=%q", err, stdout.String())
			}
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			var envelope struct {
				Command  string `json:"command"`
				ExitCode int    `json:"exit_code"`
				OK       bool   `json:"ok"`
			}
			if err = decoder.Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Command != "workspace.init" || envelope.ExitCode != 72 || envelope.OK {
				t.Fatalf("wrong interrupt envelope: %+v", envelope)
			}
			var second any
			if err = decoder.Decode(&second); err != io.EOF {
				t.Fatal("child wrote more than one JSON envelope")
			}
			if !strings.Contains(diagnostics.String(), "whr: init interrupted") {
				t.Fatal("missing signal diagnostic")
			}
			after, err := os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
			rootAfter, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			names, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(rootBefore, rootAfter) || rootBefore.Mode() != rootAfter.Mode() || after.Mode() != before.Mode() || !bytes.Equal(raw, old) || len(names) != 2 {
				t.Fatal("signal cleanup changed manifest bytes/mode or directory ownership/names")
			}
			preserved, err := os.ReadFile(filepath.Join(dir, ".devcontainer/devcontainer.json"))
			if err != nil || !bytes.Equal(preserved, dc) {
				t.Fatal("signal changed existing devcontainer")
			}
		})
	}
}
