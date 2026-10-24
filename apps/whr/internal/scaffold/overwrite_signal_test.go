//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// This writer bounds child output and announces only its first complete line.
type writerSignalCapture struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan string
	sent  bool
}

func (b *writerSignalCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(p) > 8192 {
		return 0, fmt.Errorf("owned signal child exceeded output bound")
	}
	n, err := b.data.Write(p)
	if b.ready != nil && !b.sent {
		if i := strings.IndexByte(b.data.String(), '\n'); i >= 0 {
			b.sent = true
			b.ready <- b.data.String()[:i]
		}
	}
	return n, err
}

func (b *writerSignalCapture) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func writerSignalProposal(t *testing.T, dir, mode string) *Proposal {
	t.Helper()
	name := "changed"
	options := Options{Name: &name, NonInteractive: true, Yes: true}
	if mode == "overwrite" {
		options.Overwrite = []string{"workharbor.json"}
	}
	return applyProposal(t, dir, options)
}

// Only this test helper reads these private test-process arguments. It calls
// the writer directly; no product flag, environment hook or CLI is added.
func TestInitWriterSignalProcess(t *testing.T) {
	var args []string
	for i, value := range os.Args {
		if value == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return
	}
	if len(args) != 3 || (args[0] != "create" && args[0] != "overwrite") {
		t.Fatal("invalid private signal-helper arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	proposal := writerSignalProposal(t, args[2], args[0])
	observed := false
	result, err := proposal.apply(ctx, func(event string) error {
		if event != args[1] || observed {
			return nil
		}
		fmt.Println("ready:" + event)
		select {
		case <-ctx.Done():
			observed = ctx.Err() == context.Canceled
			return nil
		case <-time.After(5 * time.Second):
			return fmt.Errorf("owned signal helper did not receive a signal")
		}
	})
	if !observed || len(result.Remaining) != 0 {
		fmt.Fprintln(os.Stderr, "signal was not observed or rollback left recovery paths")
		os.Exit(74)
	}
	code := int(exitcode.FromError(err))
	fmt.Printf("exit:%d\n", code)
	os.Exit(code)
}

func TestInitWriterSignalsEveryStep(t *testing.T) {
	setup := func(t *testing.T, mode string) (string, *Proposal) {
		t.Helper()
		dir := fixture(t)
		if err := os.Mkdir(filepath.Join(dir, "projects"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "projects/owner.txt"), []byte("original project data\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if mode == "overwrite" {
			old := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
			if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), old, 0640); err != nil {
				t.Fatal(err)
			}
		}
		return dir, writerSignalProposal(t, dir, mode)
	}
	type entry struct {
		info fs.FileInfo
		data []byte
	}
	snapshot := func(t *testing.T, dir string) map[string]entry {
		t.Helper()
		out := map[string]entry{}
		err := filepath.WalkDir(dir, func(name string, item fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, name)
			if err != nil {
				return err
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			var data []byte
			if !info.IsDir() {
				data, err = os.ReadFile(name)
				if err != nil {
					return err
				}
			}
			out[filepath.ToSlash(rel)] = entry{info, data}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	suiteDeadline := time.Now().Add(90 * time.Second)
	for _, mode := range []string{"create", "overwrite"} {
		_, probe := setup(t, mode)
		var events []string
		seen := map[string]bool{}
		if _, err := probe.apply(context.Background(), func(event string) error {
			if seen[event] {
				t.Fatalf("duplicate success-path event: %s", event)
			}
			seen[event] = true
			events = append(events, event)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		want := 27
		if mode == "overwrite" {
			want = 28
		}
		if len(events) != want {
			t.Fatalf("%s matrix has %d events, want %d: %v", mode, len(events), want, events)
		}
		for _, event := range events {
			for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
				t.Run(mode+"/"+event+"/"+sig.String(), func(t *testing.T) {
					dir, _ := setup(t, mode)
					before := snapshot(t, dir)
					private := t.TempDir()
					cmd := exec.Command(executable, "-test.run=^TestInitWriterSignalProcess$", "--", mode, event, dir)
					cmd.Dir = dir
					cmd.Env = []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "TERM=dumb"}
					cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
					stdout := &writerSignalCapture{ready: make(chan string, 1)}
					stderr := &writerSignalCapture{}
					cmd.Stdout, cmd.Stderr = stdout, stderr
					budget := min(5*time.Second, time.Until(suiteDeadline))
					if budget <= 0 {
						t.Fatal("writer signal matrix exceeded its 90-second bound")
					}
					deadline := time.NewTimer(budget)
					defer deadline.Stop()
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					waited := make(chan error, 1)
					go func() { waited <- cmd.Wait() }()
					reaped := false
					defer func() {
						if !reaped {
							// Only the process group of this successfully started
							// owned child is eligible for timeout cleanup.
							_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
							select {
							case <-waited:
							case <-time.After(2 * time.Second):
								t.Error("owned signal child was not reaped")
							}
						}
					}()
					select {
					case line := <-stdout.ready:
						if line != "ready:"+event {
							t.Fatalf("unexpected helper handshake: %q", line)
						}
					case err := <-waited:
						reaped = true
						t.Fatalf("helper exited before handshake: %v, %s", err, stderr.String())
					case <-deadline.C:
						t.Fatal("helper did not reach selected writer event")
					}
					if err := cmd.Process.Signal(sig); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-waited:
						reaped = true
						exit, ok := err.(*exec.ExitError)
						if !ok || exit.ExitCode() != 72 {
							t.Fatalf("signal must exit 72: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
						}
					case <-deadline.C:
						t.Fatal("helper did not finish rollback after signal")
					}
					if stdout.String() != "ready:"+event+"\nexit:72\n" || stderr.String() != "" {
						t.Fatalf("unexpected helper outcome: stdout=%q stderr=%q", stdout.String(), stderr.String())
					}
					after := snapshot(t, dir)
					if len(before) != len(after) {
						t.Fatalf("signal rollback changed inventory: before=%v after=%v", before, after)
					}
					for name, old := range before {
						current, found := after[name]
						if !found || old.info.Mode() != current.info.Mode() || !bytes.Equal(old.data, current.data) {
							t.Fatalf("signal rollback did not restore bytes/mode of %s", name)
						}
						if old.info.IsDir() || name != "workharbor.json" {
							if !os.SameFile(old.info, current.info) {
								t.Fatalf("signal rollback replaced pre-existing %s", name)
							}
							if !old.info.IsDir() && old.info.ModTime() != current.info.ModTime() {
								t.Fatalf("signal rollback changed pre-existing file time: %s", name)
							}
						}
					}
				})
			}
		}
	}
}
