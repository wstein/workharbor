// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"io"
	"os"
	"strings"
	"time"
)

// Streams are native terminal/file descriptors, never a whr stream relay.
type Streams struct{ Stdin, Stdout, Stderr *os.File }
type Options struct {
	Term           string
	Stderr         io.Writer
	StderrTerminal bool
	Streams        Streams
}

// Ports keep all process/socket boundaries replaceable for offline evidence.
type Ports struct {
	Discover  func(context.Context) (localdocker.Snapshot, error)
	Inspect   func(context.Context, string, string) ([]byte, error)
	Manifest  func(context.Context, string) (string, error)
	Exec      func(context.Context, ExecPlan, Streams) (int, error)
	Reachable func(context.Context, string, string) bool
}

func preflightError(parent context.Context, err error) error {
	if errors.Is(parent.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return exitcode.Errorf(exitcode.PathUserInterrupt, "local shell interrupted before handover")
	}
	var cli *exitcode.Error
	if errors.As(err, &cli) && cli.Code() != exitcode.Timeout {
		if cli.Code() == exitcode.Conflict && !strings.Contains(cli.Remediation, "docs/container-runtime.md") {
			copy := *cli
			copy.Remediation = strings.TrimSpace(cli.Remediation + " See docs/container-runtime.md.")
			return &copy
		}
		return cli
	}
	return exitcode.Errorf(exitcode.PathConflict, "local Docker preflight failed or exceeded its 5 s budget").WithRemediation("Check whr doctor, the Docker CLI and docs/container-runtime.md; whr never starts a runtime.")
}

// Run performs one bounded preflight, one direct interactive child, then only
// for a nonzero child status one independently bounded reachability inspect.
func Run(parent context.Context, selector string, o Options, p Ports) (int, error) {
	if err := parent.Err(); err != nil {
		return 0, preflightError(parent, err)
	}
	if p.Discover == nil || p.Inspect == nil || p.Exec == nil || p.Reachable == nil {
		return 0, exitcode.Errorf(exitcode.PathUnclassified, "local shell process boundary is unavailable")
	}
	if p.Manifest == nil {
		p.Manifest = ManifestShell
	}
	ctx, cancel := context.WithTimeout(parent, doctor.Budget)
	defer cancel()
	snapshot, err := p.Discover(ctx)
	if err != nil {
		return 0, preflightError(parent, err)
	}
	target, err := Select(snapshot.Workspaces, selector)
	if err != nil {
		return 0, err
	}
	raw, err := p.Inspect(ctx, snapshot.Endpoint, target.ID)
	if err != nil {
		return 0, preflightError(parent, err)
	}
	metadata, err := ParseMetadata(raw, target, o.Term)
	if err != nil {
		return 0, err
	}
	shell, err := p.Manifest(ctx, target.WorkspaceFolder)
	if err != nil {
		return 0, preflightError(parent, err)
	}
	plan, err := NewExecPlan(snapshot.Endpoint, metadata, shell)
	if err != nil {
		return 0, err
	}
	if err = ctx.Err(); err != nil {
		return 0, preflightError(parent, err)
	}
	cancel() // the interactive child has no preflight deadline
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	if err = emitStart(o.Stderr, o.StderrTerminal, metadata); err != nil {
		return 0, exitcode.Errorf(exitcode.PathUnclassified, "writing the local shell start notice failed")
	}
	status, err := p.Exec(parent, plan, o.Streams)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0, preflightError(parent, err)
		}
		var cli *exitcode.Error
		if errors.As(err, &cli) {
			return 0, cli
		}
		return 0, exitcode.Errorf(exitcode.PathUnclassified, "starting local Docker exec failed")
	}
	if status < 0 || status > 255 {
		return 0, exitcode.Errorf(exitcode.PathUnclassified, "Docker client returned an invalid process status")
	}
	if status != 0 {
		live, cancelLive := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer cancelLive()
		if !p.Reachable(live, snapshot.Endpoint, target.ID) {
			return 0, exitcode.Errorf(exitcode.PathTransportLost, "local Docker daemon is unreachable after the shell exited")
		}
	}
	return status, nil
}
