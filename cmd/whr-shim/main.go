package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: whr-shim <run|kill> [flags] [cmd...]\n")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "kill":
		killCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	pidfile := fs.String("pidfile", "", "path to write the child PGID/PID")
	_ = fs.Parse(args)
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "run requires a command to execute\n")
		os.Exit(2)
	}
	if *pidfile == "" {
		fmt.Fprintf(os.Stderr, "run requires -pidfile\n")
		os.Exit(2)
	}

	cmd := exec.CommandContext(context.Background(), cmdArgs[0], cmdArgs[1:]...) //nolint:gosec // launcher runs the commanded child process
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start failed: %v\n", err)
		os.Exit(1)
	}

	pid := cmd.Process.Pid
	if err := os.WriteFile(*pidfile, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write pidfile failed: %v\n", err)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(*pidfile) }()

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(-pid, s)
			}
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if ws.Signaled() {
					os.Exit(128 + int(ws.Signal()))
				}
				os.Exit(ws.ExitStatus())
			}
		}
		os.Exit(1)
	}
}

func killCmd(args []string) {
	fs := flag.NewFlagSet("kill", flag.ExitOnError)
	pidfile := fs.String("pidfile", "", "path to read the child PGID/PID")
	grace := fs.Duration("grace", 500*time.Millisecond, "grace period before SIGKILL")
	_ = fs.Parse(args)

	if *pidfile == "" {
		fmt.Fprintf(os.Stderr, "kill requires -pidfile\n")
		os.Exit(2)
	}

	data, err := os.ReadFile(*pidfile)
	if err != nil {
		if os.IsNotExist(err) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "read pidfile: %v\n", err)
		os.Exit(1)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		fmt.Fprintf(os.Stderr, "invalid pid in pidfile: %q\n", string(data))
		os.Exit(1)
	}

	t0 := time.Now()
	// Send SIGINT to the entire process group
	_ = syscall.Kill(-pid, syscall.SIGINT)

	dead := false
	deadline := time.Now().Add(*grace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); err != nil {
			dead = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !dead {
		// Send SIGKILL to the entire process group
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		killDeadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(killDeadline) {
			if err := syscall.Kill(-pid, 0); err != nil {
				dead = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	elapsed := time.Since(t0)
	_ = os.Remove(*pidfile)
	if !dead {
		fmt.Fprintf(os.Stderr, "process group %d still alive after %v\n", pid, elapsed)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "killed process group %d in %v\n", pid, elapsed)
}
