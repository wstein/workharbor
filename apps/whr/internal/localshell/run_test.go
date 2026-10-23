// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"bytes"
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"strings"
	"testing"
	"time"
)

func runFixture(t *testing.T, status int, reachable bool) (Ports, *[]string) {
	calls := []string{}
	p := Ports{
		Discover: func(ctx context.Context) (localdocker.Snapshot, error) {
			calls = append(calls, "discover")
			return localdocker.Snapshot{Endpoint: "unix:///test/socket", Workspaces: []localdocker.Workspace{selected()}}, nil
		},
		Inspect: func(ctx context.Context, endpoint, id string) ([]byte, error) {
			calls = append(calls, "inspect")
			if endpoint != "unix:///test/socket" || id != selected().ID {
				t.Fatalf("inspect unpinned: %s %s", endpoint, id)
			}
			return inspected(t, nil), nil
		},
		Manifest: func(context.Context, string) (string, error) { calls = append(calls, "manifest"); return "fish", nil },
		Exec: func(ctx context.Context, plan ExecPlan, streams Streams) (int, error) {
			calls = append(calls, "exec")
			if _, ok := ctx.Deadline(); ok {
				t.Fatal("preflight deadline leaked into interactive session")
			}
			return status, nil
		},
		Reachable: func(ctx context.Context, endpoint, id string) bool {
			calls = append(calls, "alive")
			if ctx.Err() != nil {
				t.Fatal("postcheck inherited cancelled context")
			}
			d, ok := ctx.Deadline()
			if !ok || time.Until(d) > 5*time.Second {
				t.Fatal("postcheck lacks separate5s budget")
			}
			if endpoint != "unix:///test/socket" || id != selected().ID {
				t.Fatal("liveness unpinned")
			}
			return reachable
		},
	}
	return p, &calls
}
func TestLocalShellExitStatusesPassThrough(t *testing.T) {
	for _, status := range []int{0, 1, 7, 64, 68, 72, 126, 127, 137, 255} {
		p, calls := runFixture(t, status, true)
		var out bytes.Buffer
		got, err := Run(context.Background(), "sample", Options{Stderr: &out}, p)
		if err != nil || got != status {
			t.Fatalf("status%d ->%d %v", status, got, err)
		}
		want := "discover,inspect,manifest,exec"
		if status != 0 {
			want += ",alive"
		}
		if strings.Join(*calls, ",") != want {
			t.Fatalf("%v", *calls)
		}
	}
}
func TestLocalShellNonzeroRunsOneLivenessInspect(t *testing.T) {
	p, calls := runFixture(t, 127, true)
	got, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p)
	if got != 127 || err != nil || strings.Count(strings.Join(*calls, ","), "alive") != 1 {
		t.Fatalf("%d %v %v", got, err, *calls)
	}
}
func TestLocalShellDaemonLossExits72(t *testing.T) {
	p, _ := runFixture(t, 255, false)
	_, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p)
	if exitcode.FromError(err) != 72 {
		t.Fatalf("%v", err)
	}
}
func TestLocalShellReachableDaemonMissingContainerPreservesStatus(t *testing.T) {
	p, _ := runFixture(t, 137, true)
	got, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p)
	if got != 137 || err != nil {
		t.Fatalf("%d %v", got, err)
	}
}
func TestLocalShellPreflightSharesBudgetAndInterruption(t *testing.T) {
	p, calls := runFixture(t, 0, true)
	var deadline time.Time
	originalD := p.Discover
	p.Discover = func(ctx context.Context) (localdocker.Snapshot, error) {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("no shared budget")
		}
		return originalD(ctx)
	}
	originalI := p.Inspect
	p.Inspect = func(ctx context.Context, e, id string) ([]byte, error) {
		d, _ := ctx.Deadline()
		if !d.Equal(deadline) {
			t.Fatal("inspect restarted preflight budget")
		}
		return originalI(ctx, e, id)
	}
	if _, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	*calls = nil
	if _, err := Run(ctx, "sample", Options{}, p); exitcode.FromError(err) != 72 || len(*calls) != 0 {
		t.Fatalf("%v %v", err, *calls)
	}
	p, _ = runFixture(t, 0, true)
	p.Inspect = func(context.Context, string, string) ([]byte, error) { return nil, errors.New("failed inspect") }
	if _, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p); exitcode.FromError(err) != 68 {
		t.Fatalf("failed inspect: %v", err)
	}
}

func TestLocalShellMissingClientNamesRuntimeGuide(t *testing.T) {
	p, _ := runFixture(t, 0, true)
	p.Discover = func(ctx context.Context) (localdocker.Snapshot, error) {
		return (localdocker.Client{Executable: "/nonexistent/whr-test-docker"}).Snapshot(ctx, doctor.Ports{DialUnix: func(context.Context, string) error { return nil }})
	}
	_, err := Run(context.Background(), "sample", Options{Stderr: &bytes.Buffer{}}, p)
	var cli *exitcode.Error
	if !errors.As(err, &cli) || cli.Code() != 68 || !strings.Contains(cli.Remediation, "docs/container-runtime.md") {
		t.Fatalf("missing runtime remediation: %v", err)
	}
}

func TestLocalShellLivenessSurvivesSessionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, calls := runFixture(t, 143, true)
	original := p.Exec
	p.Exec = func(ctx context.Context, plan ExecPlan, streams Streams) (int, error) {
		status, err := original(ctx, plan, streams)
		cancel()
		return status, err
	}
	got, err := Run(ctx, "sample", Options{Stderr: &bytes.Buffer{}}, p)
	if err != nil || got != 143 || strings.Join(*calls, ",") != "discover,inspect,manifest,exec,alive" {
		t.Fatalf("status=%d error=%v calls=%v", got, err, *calls)
	}
}
