// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
	"golang.org/x/sys/unix"
)

const localShellFixtureID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const localShellRaw = "\x1b[35mRAW\x1b[0m\a\u202e"

type localShellFixture struct {
	Mode, Folder string
	Liveness     string
	Fallback     bool
	Exit         int
	Signal       syscall.Signal
}

func localShellPTYEntry(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, args, os.Stdout, os.Stderr)
}
func localShellPTYNonterminal(args []string) int {
	file, err := os.OpenFile(os.DevNull, os.O_RDWR, 0600)
	if err != nil {
		return 90
	}
	defer file.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Getenv("WHR_TEST_AXIS") == "input" {
		os.Stdin = file
		return run(ctx, args, os.Stdout, os.Stderr)
	}
	return run(ctx, args, file, os.Stderr)
}
func localShellPTYRedirect(args []string) int {
	file, err := os.OpenFile(os.Getenv("WHR_TEST_STDERR"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return 90
	}
	defer file.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, args, os.Stdout, file)
}
func shellTestQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func localPTYFixture(t *testing.T, f localShellFixture) (ptytest.Options, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "whr010-pty-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	f.Folder = filepath.Join(root, "workspace")
	if err = os.Mkdir(f.Folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.Folder, "workharbor.json"), []byte(`{"schema_version":2,"devcontainer_path":"d.json","projects_path":"projects","bundles":[],"console_mode":"herdr"}`), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(f)
	if err = os.WriteFile(filepath.Join(root, "fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf x >> " + shellTestQuote(filepath.Join(root, "called")) + "\nGORACE=atexit_sleep_ms=0 WHR_TEST_DOCKER=1 exec " + shellTestQuote(exe) + " \"$@\"\n"
	if err = os.WriteFile(filepath.Join(root, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"herdr", "tmux"} {
		trap := "#!/bin/sh\nprintf forbidden > " + shellTestQuote(filepath.Join(root, "multiplexer-called")) + "\nexit 99\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(trap), 0700); err != nil {
			t.Fatal(err)
		}
	}
	opts := ptytest.Options{Args: []string{"shell", "--local", "sample"}, Env: []string{"PATH=" + root + ":/usr/bin:/bin", "DOCKER_HOST=unix://" + socket}}
	return opts, root
}

// Only the test wrapper sets this dispatcher flag; product argv/env stay fixed.
func localShellFakeDocker(args []string) int {
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		switch key {
		case "PATH", "LC_ALL", "DOCKER_CONFIG", "PWD", "GORACE", "WHR_TEST_DOCKER":
		case "SHLVL":
			// Bash (/bin/sh on Darwin) adds this in the test wrapper after
			// the product has already isolated the Docker environment.
			if env != "SHLVL=0" {
				return 91
			}
		default:
			fmt.Fprintln(os.Stderr, "unexpected host environment key:", key)
			return 91
		}
	}
	cfg := os.Getenv("DOCKER_CONFIG")
	if cfg == "" {
		return 92
	}
	if _, err := os.Stat(filepath.Join(cfg, "config.json")); !os.IsNotExist(err) {
		return 92
	}
	if len(args) < 3 || !strings.HasPrefix(args[0], "--host=unix://") {
		return 93
	}
	root := filepath.Dir(strings.TrimPrefix(args[0], "--host=unix://"))
	raw, err := os.ReadFile(filepath.Join(root, "fixture.json"))
	if err != nil {
		return 94
	}
	var f localShellFixture
	if json.Unmarshal(raw, &f) != nil {
		return 94
	}
	host := args[0]
	if len(args) > 2 && args[1] == "container" && args[2] == "ls" {
		if !reflect.DeepEqual(args, []string{host, "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}"}) {
			return 95
		}
		if f.Mode == "block-discovery" {
			time.Sleep(time.Minute)
			return 89
		}
		fmt.Printf("{\"ID\":%q,\"Status\":\"Up\"}\n", localShellFixtureID)
		return 0
	}
	if len(args) == 6 && args[1] == "container" && args[2] == "inspect" && args[3] == "--format" && args[5] == localShellFixtureID {
		if args[4] == "{{.State.Running}}" {
			if f.Liveness == "lost" {
				fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon")
				return 1
			}
			if f.Liveness == "missing" {
				fmt.Fprintln(os.Stderr, "Error response from daemon: No such container: "+localShellFixtureID)
				return 1
			}
			fmt.Println("true")
			return 0
		}
		mounts := []any{}
		if !f.Fallback {
			mounts = append(mounts, map[string]any{"Type": "bind", "Source": f.Folder, "Destination": "/workspace", "RW": true})
		}
		switch args[4] {
		case `{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Config.Image}},"State":{{json .State.Status}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}}}`:
			raw, _ := json.Marshal(map[string]any{"Id": localShellFixtureID, "Name": "/sample", "Image": "fake:image", "State": "running", "Labels": map[string]string{"devcontainer.local_folder": f.Folder}, "Mounts": mounts})
			fmt.Println(string(raw))
			return 0
		case "{{json .}}":
			raw, _ := json.Marshal(map[string]any{"Id": localShellFixtureID, "Name": "/sample", "State": map[string]any{"Status": "running", "Running": true}, "Config": map[string]any{"User": "captain", "WorkingDir": "/fallback", "Labels": map[string]string{}, "Env": []string{"IGNORED_TOKEN=container-canary"}}, "Mounts": mounts})
			fmt.Println(string(raw))
			return 0
		}
	}
	directory := "/workspace"
	if f.Fallback {
		directory = "/fallback"
	}
	want := []string{host, "exec", "--interactive", "--tty", "--detach-keys=ctrl-],ctrl-^", "--user=captain", "--workdir=" + directory, "--env=TERM=xterm-256color", "--env=WHR_LOCAL_WORKSPACE=sample", "--env=WHR_LOCAL_MODE=1", localShellFixtureID, "fish", "-l"}
	if !reflect.DeepEqual(args, want) {
		fmt.Fprintln(os.Stderr, "unexpected Docker argv")
		return 95
	}
	if !wizard.IsTerminal(os.Stdin) || !wizard.IsTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "Docker did not receive direct terminal descriptors")
		return 96
	}
	fmt.Fprintln(os.Stdout, "ENV_OK")
	if f.Signal != 0 {
		if err := syscall.Kill(os.Getpid(), f.Signal); err != nil {
			return 99
		}
		time.Sleep(time.Minute)
		return 99
	}
	if f.Mode == "quick" {
		return f.Exit
	}
	restore, err := ptytest.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return 97
	}
	defer restore()
	// Real Docker makes its terminal raw. The generic harness preserves ISIG
	// for other tests; this fake must deliver ctrl-\ as a byte, not SIGQUIT.
	termios, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), localTTYGet)
	if err != nil {
		return 97
	}
	termios.Lflag &^= unix.ISIG
	if unix.IoctlSetTermios(int(os.Stdin.Fd()), localTTYSet, termios) != nil {
		return 97
	}
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer func() { signal.Stop(resize); close(resize) }()
	go func() {
		for range resize {
			rows, cols, err := ptytest.WindowSize(os.Stdin.Fd())
			if err == nil {
				fmt.Fprintf(os.Stdout, "SIZE=%dx%d\n", rows, cols)
			}
		}
	}()
	fmt.Fprintln(os.Stdout, "EXEC_READY")
	fmt.Fprintln(os.Stdout, localShellRaw)
	var buf [32]byte
	for {
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			return 98
		}
		for _, key := range buf[:n] {
			fmt.Fprintf(os.Stdout, "KEY=%02x\n", key)
			if key == 'x' {
				return f.Exit
			}
		}
	}
}
func TestLocalShellPTYDirectStreamsAndResize(t *testing.T) {
	opts, root := localPTYFixture(t, localShellFixture{Exit: 7})
	s := ptytest.StartFunc(t, "local-shell", opts)
	s.Expect("whr: local shell in sample (no grant; host Docker access; detach keys ctrl-] ctrl-^)")
	s.Expect("EXEC_READY")
	s.Send("\x10\x11\x00\x1c\x1d")
	for _, hex := range []string{"10", "11", "00", "1c", "1d"} {
		s.Expect("KEY=" + hex)
	}
	s.Resize(37, 101)
	s.Expect("SIZE=37x101")
	s.Send("x")
	s.ExpectExit(7)
	if _, err := os.Stat(filepath.Join(root, "multiplexer-called")); !os.IsNotExist(err) {
		t.Fatal("whr started a host multiplexer")
	}
	raw := s.Transcript()
	if !bytes.Contains(raw, []byte(localShellRaw)) {
		t.Fatalf("raw child stream transformed: %q", raw)
	}
	if bytes.Count(raw, []byte("whr:")) != 1 || bytes.Contains(raw, []byte("container-canary")) {
		t.Fatalf("unexpected whr output/container env: %q", raw)
	}
}
func TestLocalShellPTYRejectsNonTerminalBeforeDocker(t *testing.T) {
	for _, axis := range []string{"input", "output"} {
		t.Run(axis, func(t *testing.T) {
			opts, root := localPTYFixture(t, localShellFixture{Mode: "quick"})
			opts.Env = append(opts.Env, "WHR_TEST_AXIS="+axis)
			s := ptytest.StartFunc(t, "local-shell-nonterminal", opts)
			s.Expect("requires terminal standard input and output")
			s.ExpectExit(64)
			if _, err := os.Stat(filepath.Join(root, "called")); !os.IsNotExist(err) {
				t.Fatal("nonterminal invocation reached Docker")
			}
		})
	}
}
func TestLocalShellPTYEnvironmentCanaries(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/bash"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				if shell == "/bin/bash" {
					t.Skip("optional Bash wrapper unavailable")
				}
				t.Fatal(err)
			}
			opts, root := localPTYFixture(t, localShellFixture{Mode: "quick"})
			// Darwin's /bin/sh is Bash. Exercise its wrapper environment on Linux too.
			wrapper := filepath.Join(root, "docker")
			source, err := os.ReadFile(wrapper)
			if err != nil {
				t.Fatal(err)
			}
			source = bytes.Replace(source, []byte("#!/bin/sh\n"), []byte("#!"+shell+"\n"), 1)
			if err := os.WriteFile(wrapper, source, 0700); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "DOCKER_CONTEXT", "DOCKER_AUTH_CONFIG", "DOCKER_TLS_VERIFY", "HERDR_SESSION", "WHR_LOCAL_WORKSPACE", "WHR_LOCAL_MODE"} {
				opts.Env = append(opts.Env, key+"=host-canary-secret")
			}
			s := ptytest.StartFunc(t, "local-shell", opts)
			s.Expect("ENV_OK")
			s.ExpectExit(0)
			if bytes.Contains(s.Transcript(), []byte("host-canary-secret")) || bytes.Contains(s.Transcript(), []byte("container-canary")) {
				t.Fatal("environment canary leaked")
			}
		})
	}
}
func TestLocalShellPTYRedirectedStderrFallback(t *testing.T) {
	opts, root := localPTYFixture(t, localShellFixture{Mode: "quick", Fallback: true})
	file := filepath.Join(root, "stderr")
	opts.Env = append(opts.Env, "WHR_TEST_STDERR="+file)
	s := ptytest.StartFunc(t, "local-shell-redirect", opts)
	s.Expect("ENV_OK")
	s.ExpectExit(0)
	raw, err := os.ReadFile(file)
	want := "whr: warning: no workspace folder mount in sample; starting in /fallback\n"
	if err != nil || string(raw) != want {
		t.Fatalf("redirectedstderr=%q %v", raw, err)
	}
}

func TestLocalShellPTYExitAndLiveness(t *testing.T) {
	for _, tc := range []struct {
		name, live  string
		child, want int
		signal      syscall.Signal
	}{{"normal255", "", 255, 255, 0}, {"missing127", "missing", 127, 127, 0}, {"lost", "lost", 255, 72, 0}, {"killed", "", 0, 137, syscall.SIGKILL}, {"terminated", "", 0, 143, syscall.SIGTERM}} {
		t.Run(tc.name, func(t *testing.T) {
			opts, root := localPTYFixture(t, localShellFixture{Mode: "quick", Exit: tc.child, Liveness: tc.live, Signal: tc.signal})
			s := ptytest.StartFunc(t, "local-shell", opts)
			s.Expect("ENV_OK")
			s.ExpectExit(tc.want)
			raw, err := os.ReadFile(filepath.Join(root, "called"))
			if err != nil || string(raw) != "xxxxx" {
				t.Fatalf("want discovery2 + inspect1 + exec1 + liveness1, got %q %v", raw, err)
			}
			count := bytes.Count(s.Transcript(), []byte("whr:"))
			want := 1
			if tc.live == "lost" {
				want = 2
			}
			if count != want {
				t.Fatalf("unexpected whr output after handover: %q", s.Transcript())
			}
		})
	}
}
func TestLocalShellPTYPreflightSignalsExit72(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			opts, root := localPTYFixture(t, localShellFixture{Mode: "block-discovery"})
			s := ptytest.StartFunc(t, "local-shell", opts)
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(root, "called")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fake discovery did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := syscall.Kill(s.Pid(), sig); err != nil {
				t.Fatal(err)
			}
			s.Expect("whr: local shell interrupted before handover")
			s.ExpectExit(72)
			if bytes.Contains(s.Transcript(), []byte("whr: local shell in sample")) {
				t.Fatalf("notice emitted before invalid handover: %q", s.Transcript())
			}
		})
	}
}
