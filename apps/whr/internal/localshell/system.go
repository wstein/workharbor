// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"os/exec"
	"time"
)

// SystemPorts constructs lazy boundaries: ordinary commands/help perform no
// Docker lookup or socket access merely by constructing the handler map.
func SystemPorts() Ports {
	c := localdocker.Client{}
	return Ports{Discover: func(ctx context.Context) (localdocker.Snapshot, error) {
		return c.Snapshot(ctx, doctor.SystemPorts(nil))
	}, Inspect: c.PreflightInspect, Manifest: ManifestShell, Exec: runInteractive, Reachable: c.Reachable}
}
func runInteractive(ctx context.Context, p ExecPlan, streams Streams) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if streams.Stdin == nil || streams.Stdout == nil || streams.Stderr == nil {
		return 0, exitcode.Errorf(exitcode.PathBadArguments, "local shell requires direct terminal/file descriptors")
	}
	if _, err := NewExecPlan(p.endpoint, p.metadata, p.shell); err != nil {
		return 0, err
	}
	exe, err := exec.LookPath("docker")
	if err != nil {
		return 0, exitcode.Errorf(exitcode.PathConflict, "Docker CLI unavailable").WithRemediation("Install the Docker CLI for the existing runtime; see docs/container-runtime.md and whr doctor.")
	}
	env, cleanup, err := localdocker.ClientEnvironment()
	if err != nil {
		return 0, exitcode.Errorf(exitcode.PathConflict, "cannot isolate Docker client configuration")
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, exe, p.Args()...)
	cmd.Env = env
	// Concrete *os.File values give Docker the owner's descriptors directly.
	// No Setpgid: Docker shares the foreground terminal group and handles resize.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = streams.Stdin, streams.Stdout, streams.Stderr
	cmd.WaitDelay = 200 * time.Millisecond
	superviseInteractive(cmd)
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, exitcode.Errorf(exitcode.PathConflict, "cannot start Docker CLI").WithRemediation("Check whr doctor and docs/container-runtime.md.")
	}
	err = cmd.Wait()
	if err == nil {
		return 0, nil
	}
	if cmd.ProcessState == nil {
		return 0, exitcode.Errorf(exitcode.PathUnclassified, "Docker client process status unavailable")
	}
	return interactiveStatus(cmd), nil
}
