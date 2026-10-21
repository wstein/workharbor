// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
)

// This child is only a copied test executable beneath the test's private root.
// It never dispatches another program or accesses network or owner HOME.
func TestNativeHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "failure":
		os.Exit(7)
	case "version":
		fmt.Print("probe 1.2.3")
	case "environment":
		if len(os.Environ()) != 2 || os.Getenv("PATH") != detect.FixedPATH || os.Getenv("LC_ALL") != "C" {
			os.Exit(9)
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil || len(b) != 0 {
			os.Exit(10)
		}
		fmt.Print("clean")
	case "flood":
		fmt.Print(strings.Repeat("x", 64*1024+1))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("private-do-not-return", 10000))
		fmt.Print("safe")
	case "descendant":
		exe, err := os.Executable()
		if err != nil {
			os.Exit(12)
		}
		child := exec.Command(exe, "-test.run=^TestNativeHelper$", "--", "expire")
		child.Env = os.Environ()
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(13)
		}
		fmt.Print(child.Process.Pid)
	case "expire":
		// Inert orphan safety net: even a failed cleanup test cannot leave it running indefinitely.
		time.Sleep(2 * time.Second)
	case "block":
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(11)
	}
	os.Exit(0)
}
func fixture(t *testing.T) (*Host, string, string) {
	t.Helper()
	root := t.TempDir()
	path := "/opt/homebrew/Cellar/probe/1/bin/probe"
	disk := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(disk), 0700); err != nil {
		t.Fatal(err)
	}
	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
	h, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	return h, root, path
}
func probe(path, mode string) detect.Probe {
	return detect.Probe{Executable: path, Args: []string{"-test.run=^TestNativeHelper$", "--", mode}, Env: map[string]string{"PATH": detect.FixedPATH, "LC_ALL": "C"}, Timeout: 3 * time.Second}
}
func TestNativeDescriptorFS(t *testing.T) {
	h, root, path := fixture(t)
	info, err := h.Lstat(path)
	if err != nil || info.Kind != "regular" || !info.Executable {
		t.Fatalf("metadata: %+v %v", info, err)
	}
	if err := os.Symlink("Cellar", filepath.Join(root, "opt/homebrew/alias")); err != nil {
		t.Fatal(err)
	}
	target, err := h.Readlink("/opt/homebrew/alias")
	if err != nil || target != "Cellar" {
		t.Fatalf("readlink: %q %v", target, err)
	}
	info, err = h.Lstat("/opt/homebrew/alias")
	if err != nil || info.Kind != "symlink" {
		t.Fatalf("link metadata: %+v %v", info, err)
	}
	if _, err = h.Lstat("/opt/homebrew/alias/probe/1/bin/probe"); err == nil {
		t.Fatal("followed ancestor symlink")
	}
	for _, bad := range []string{"relative", "/opt/../etc", "//opt/homebrew", "/opt/./homebrew", "/opt/homebrew/"} {
		if _, err = h.Lstat(bad); err == nil {
			t.Fatalf("accepted noncanonical path %q", bad)
		}
	}
}
func TestNativeProbeIsolationAndBounds(t *testing.T) {
	h, _, path := fixture(t)
	t.Setenv("WHR_NATIVE_PRIVATE_CANARY", "must-not-inherit")
	for _, mode := range []string{"version", "environment", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			out, err := h.Run(context.Background(), probe(path, mode))
			if err != nil || out.Exit != 0 || out.Stdout == "" || strings.Contains(out.Stdout, "private") {
				t.Fatalf("probe: %+v %v", out, err)
			}
		})
	}
	out, err := h.Run(context.Background(), probe(path, "flood"))
	if err == nil || len(out.Stdout) > 64*1024 {
		t.Fatalf("unbounded output: %d %v", len(out.Stdout), err)
	}
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancel), func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			p := probe(path, "block")
			p.Timeout = 100 * time.Millisecond
			want := context.DeadlineExceeded
			if cancel {
				// A detached parent cannot hide behind the short deadline control.
				p.Timeout = 3 * time.Second
				want = context.Canceled
				timer := time.AfterFunc(30*time.Millisecond, stop)
				defer timer.Stop()
			}
			start := time.Now()
			_, err := h.Run(ctx, p)
			if !errors.Is(err, want) || time.Since(start) > 2*time.Second {
				t.Fatalf("unbounded cancellation: %v %s", err, time.Since(start))
			}
		})
	}
}
func TestNativeProbeRejectsAliasesAndUnsafeRequests(t *testing.T) {
	h, root, path := fixture(t)
	if err := os.Symlink("Cellar", filepath.Join(root, "opt/homebrew/alias")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []detect.Probe{probe("/opt/homebrew/alias/probe/1/bin/probe", "version"), probe("/usr/bin/git", "version"), probe("/opt/homebrew/bin/brew", "version"), probe(path+"/../probe", "version")} {
		if _, err := h.Run(context.Background(), p); err == nil {
			t.Fatalf("accepted unsafe executable %q", p.Executable)
		}
	}
	p := probe(path, "version")
	p.Env["HOME"] = "/private"
	if _, err := h.Run(context.Background(), p); err == nil {
		t.Fatal("accepted inherited environment")
	}
	p = probe(path, "version")
	p.Timeout = 6 * time.Second
	if _, err := h.Run(context.Background(), p); err == nil {
		t.Fatal("accepted timeout above5s")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Run(ctx, probe(path, "version")); err == nil {
		t.Fatal("executed after cancellation")
	}
}
func TestNativeDetectorWiring(t *testing.T) {
	h, root, _ := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "opt/homebrew/bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/probe/1/bin/probe", filepath.Join(root, "opt/homebrew/bin/probe")); err != nil {
		t.Fatal(err)
	}
	item := catalogue.Item{ID: "probe", Tool: catalogue.Tool{Command: "probe", VersionArgs: []string{"-test.run=^TestNativeHelper$", "--", "version"}, VersionRegex: `probe ([0-9.]+)`, MinVersion: "1.0.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "probe"}}}
	result := h.Detector().Detect(context.Background(), item)
	if result.State != "present_ok" || result.Version != "1.2.3" || !result.Managed {
		t.Fatalf("wired detection: %+v", result)
	}
}

func TestNativeDescendantPipeCleanup(t *testing.T) {
	h, _, path := fixture(t)
	t.Run("inert helper self expires", func(t *testing.T) {
		p := probe(path, "expire")
		p.Timeout = 5 * time.Second
		out, err := h.Run(context.Background(), p)
		if err != nil || out.Exit != 0 {
			t.Fatalf("inert safety-net helper did not self-terminate: %+v %v", out, err)
		}
	})
	start := time.Now()
	out, err := h.Run(context.Background(), probe(path, "descendant"))
	pid, parseErr := strconv.Atoi(out.Stdout)
	if parseErr != nil || pid <= 0 {
		t.Fatalf("missing owned descendant identity: %+v %v", out, err)
	}
	// Never signal the reparented numeric PID; the inert helper self-expires if cleanup fails.
	if err != nil || out.Exit != 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("descendant cleanup not bounded: %+v %v", out, err)
	}
	// Reparented dead children can remain zombies in minimal Linux containers.
	// Inspect only this explicitly owned helper's status; a zombie cannot run.
	dead := func() bool {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return true
		}
		if runtime.GOOS == "linux" {
			b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if e == nil {
				end := strings.LastIndex(string(b), ") ")
				return end >= 0 && strings.HasPrefix(string(b)[end+2:], "Z ")
			}
		}
		return false
	}
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		if dead() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned descendant survived pipe cleanup")
}
func TestNativeClosedAndSpecialFiles(t *testing.T) {
	h, root, path := fixture(t)
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := h.Lstat("/pipe")
	if err != nil || info.Kind != "special" {
		t.Fatalf("FIFO metadata: %+v %v", info, err)
	}
	start := time.Now()
	if _, err = h.Run(context.Background(), probe("/pipe", "version")); err == nil || time.Since(start) > time.Second {
		t.Fatalf("opened special executable: %v", err)
	}
	if err := os.Symlink("probe", filepath.Join(root, filepath.Dir(path), "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Run(context.Background(), probe(filepath.Join(filepath.Dir(path), "alias"), "version")); err == nil {
		t.Fatal("followed executable symlink")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Lstat(path); err == nil {
		t.Fatal("read after close")
	}
	if _, err := h.Run(context.Background(), probe(path, "version")); err == nil {
		t.Fatal("executed after close")
	}
}

type fakeLease struct {
	events       []string
	released     bool
	exited       bool
	observeErr   error
	waitErr      error
	signalErr    error
	unsafeSignal bool
}

func (p *fakeLease) Exited() (bool, error) {
	p.events = append(p.events, "observe")
	return p.exited, p.observeErr
}
func (p *fakeLease) SignalGroup() error {
	p.events = append(p.events, "signal")
	if p.released {
		p.unsafeSignal = true
	}
	return p.signalErr
}
func (p *fakeLease) Release() error {
	p.events = append(p.events, "release")
	p.released = true
	return nil
}
func (p *fakeLease) Wait() error {
	p.events = append(p.events, "wait")
	p.released = true
	return p.waitErr
}
func TestNativeNeverSignalsReleasedProcess(t *testing.T) {
	for _, mode := range []string{"success", "nonzero", "cancel", "unsupported", "observer_failure", "signal_failure", "ownership_lost"} {
		t.Run(mode, func(t *testing.T) {
			child := &fakeLease{exited: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "nonzero":
				child.waitErr = errors.New("nonzero")
			case "cancel":
				cancel()
			case "unsupported":
				child.observeErr = syscall.ENOSYS
			case "observer_failure":
				child.observeErr = syscall.EIO
			case "signal_failure":
				child.signalErr = syscall.EPERM
			case "ownership_lost":
				child.observeErr = errOwnershipLost
			}
			err := finishProbe(ctx, child)
			if mode == "observer_failure" && (!errors.Is(err, syscall.EIO) || !strings.Contains(err.Error(), "observe probe child exit")) {
				t.Fatalf("observation stage/cause lost: %v", err)
			}
			if mode == "signal_failure" && (!errors.Is(err, syscall.EPERM) || !strings.Contains(err.Error(), "signal owned probe group")) {
				t.Fatalf("group-signal stage/cause lost: %v", err)
			}
			if child.unsafeSignal {
				t.Fatalf("signal after ownership release: %v", child.events)
			}
			if (mode == "success") != (err == nil) {
				t.Fatalf("unexpected result: %v; events=%v", err, child.events)
			}
			if mode == "ownership_lost" {
				if !reflect.DeepEqual(child.events, []string{"observe", "release", "wait"}) {
					t.Fatalf("lost-ownership cleanup: %v", child.events)
				}
			} else {
				signal, wait := -1, -1
				for i, e := range child.events {
					if e == "signal" {
						signal = i
					}
					if e == "wait" {
						wait = i
					}
				}
				if signal < 0 || wait < 0 || signal >= wait {
					t.Fatalf("not owned signal before sole Wait: %v", child.events)
				}
			}
		})
	}
}

func TestNativeFailedStartAndNonzeroExit(t *testing.T) {
	host, root, path := fixture(t)
	out, err := host.Run(context.Background(), probe(path, "failure"))
	if err == nil || out.Exit != 7 {
		t.Fatalf("nonzero exit accepted or lost: %+v %v", out, err)
	}
	bad := filepath.Join(root, "invalid-executable")
	if err := os.WriteFile(bad, []byte("not an executable format"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Run(context.Background(), probe("/invalid-executable", "version")); err == nil {
		t.Fatal("failed process spawn accepted")
	}
}

const metadataPlistFixture = `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>2.2.4</string></dict></plist>`

// Intercept the exact fake plist-reader argv before testing parses its flags.
// The helper is a copied test executable, never the real system plutil.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-extract" {
		expected := []string{"-extract", "CFBundleShortVersionString", "raw", "-o", "-", "/dev/fd/4"}
		if os.Args[0] != "/usr/bin/plutil" || !reflect.DeepEqual(os.Args[1:], expected) || len(os.Environ()) != 2 || os.Getenv("PATH") != detect.FixedPATH || os.Getenv("LC_ALL") != "C" {
			os.Exit(20)
		}
		input, err := io.ReadAll(os.Stdin)
		if err != nil || len(input) != 0 {
			os.Exit(21)
		}
		metadata, err := os.ReadFile("/dev/fd/4")
		if err != nil {
			os.Exit(22)
		}
		switch string(metadata) {
		case metadataPlistFixture:
			fmt.Print("2.2.4\n")
		case strings.Replace(metadataPlistFixture, "2.2.4", "fixture-flood", 1):
			fmt.Print(strings.Repeat("x", outputLimit+1))
		case strings.Replace(metadataPlistFixture, "2.2.4", "fixture-block", 1):
			for {
				time.Sleep(time.Second)
			}
		case strings.Replace(metadataPlistFixture, "2.2.4", "fixture-nonzero", 1):
			os.Exit(7)
		case strings.Replace(metadataPlistFixture, "2.2.4", "fixture-stderr", 1):
			fmt.Fprint(os.Stderr, "private-do-not-return")
			fmt.Print("2.2.4\n")
		default:
			os.Exit(22)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func metadataFixture(t *testing.T) (*Host, string) {
	t.Helper()
	host, root, helper := fixture(t)
	for _, dir := range []string{"/usr/bin", "/Applications/OrbStack.app/Contents", "/opt/homebrew/Caskroom/orbstack"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(filepath.Join(root, helper), filepath.Join(root, "usr/bin/plutil")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, detect.OrbStackInfoPlist), []byte(metadataPlistFixture), 0600); err != nil {
		t.Fatal(err)
	}
	return host, root
}
func TestNativeOrbStackMetadataFixedRoute(t *testing.T) {
	host, _ := metadataFixture(t)
	value, err := host.OrbStackAppVersion(context.Background(), 3*time.Second)
	if err != nil || value != "2.2.4\n" {
		t.Fatalf("fixed metadata route: %q %v", value, err)
	}
	initial, err := catalogue.LoadInitial()
	if err != nil {
		t.Fatal(err)
	}
	result := host.Detector().Detect(context.Background(), initial.Items[0])
	if result.State != "present_ok" || result.Version != "2.2.4" || !result.Managed {
		t.Fatalf("production route not wired: %+v", result)
	}
	p := probe("/usr/bin/plutil", "version")
	p.Args = []string{"-extract", "CFBundleShortVersionString", "raw", "-o", "-", "/dev/fd/4"}
	if _, err := host.Run(context.Background(), p); !errors.Is(err, errUnsafe) {
		t.Fatalf("generic four-argument limit widened: %v", err)
	}
}
func TestNativeOrbStackMetadataRejectsAliasesAndBounds(t *testing.T) {
	for _, kind := range []string{"plist", "parent", "helper", "helper_parent"} {
		t.Run(kind, func(t *testing.T) {
			host, root := metadataFixture(t)
			path := filepath.Join(root, detect.OrbStackInfoPlist)
			if kind == "parent" {
				path = filepath.Dir(path)
			}
			if kind == "helper" {
				path = filepath.Join(root, "usr/bin/plutil")
			}
			if kind == "helper_parent" {
				path = filepath.Join(root, "usr/bin")
			}
			if err := os.Rename(path, path+"-saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+"-saved", path); err != nil {
				t.Fatal(err)
			}
			if _, err := host.OrbStackAppVersion(context.Background(), time.Second); err == nil {
				t.Fatal("followed metadata/helper alias")
			}
		})
	}
	host, _ := metadataFixture(t)
	if _, err := host.OrbStackAppVersion(context.Background(), 6*time.Second); err == nil {
		t.Fatal("accepted metadata budget above5s")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := host.OrbStackAppVersion(ctx, time.Second); err == nil {
		t.Fatal("executed cancelled metadata observation")
	}
	host, root := metadataFixture(t)
	if err := os.WriteFile(filepath.Join(root, detect.OrbStackInfoPlist), make([]byte, 1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OrbStackAppVersion(context.Background(), time.Second); !errors.Is(err, errUnsafe) {
		t.Fatalf("accepted oversized metadata file: %v", err)
	}

}

func TestNativeOrbStackMetadataProcessControls(t *testing.T) {
	for _, mode := range []string{"flood", "block", "nonzero", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			host, root := metadataFixture(t)
			content := strings.Replace(metadataPlistFixture, "2.2.4", "fixture-"+mode, 1)
			if err := os.WriteFile(filepath.Join(root, detect.OrbStackInfoPlist), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			budget := 3 * time.Second
			if mode == "block" {
				budget = 100 * time.Millisecond
			}
			value, err := host.OrbStackAppVersion(context.Background(), budget)
			switch mode {
			case "flood":
				if !errors.Is(err, errOutputLimit) || value != "" {
					t.Fatalf("metadata output cap lost: %q %v", value, err)
				}
			case "block":
				if !errors.Is(err, context.DeadlineExceeded) || value != "" || time.Since(start) > time.Second {
					t.Fatalf("metadata deadline lost: %q %v", value, err)
				}
			case "nonzero":
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 7 || value != "" {
					t.Fatalf("metadata failed probe accepted: %q %v", value, err)
				}
			case "stderr":
				if err != nil || value != "2.2.4\n" {
					t.Fatalf("metadata stderr leaked: %q %v", value, err)
				}
			}
		})
	}
}

func TestNativeExitedLeaderSignalPolicy(t *testing.T) {
	for _, tc := range []struct {
		err                   error
		exited, platform, ign bool
	}{
		{syscall.EPERM, true, true, true},
		{syscall.EPERM, true, false, false},
		{syscall.EPERM, false, true, false},
		{syscall.EPERM, false, false, false},
		{syscall.EIO, true, true, false},
		{syscall.EIO, false, true, false},
	} {
		if got := signalOfExitedLeaderIgnored(tc.err, tc.exited, tc.platform); got != tc.ign {
			t.Errorf("err=%v exited=%v platform=%v: ignored=%v, want %v", tc.err, tc.exited, tc.platform, got, tc.ign)
		}
	}
}

func TestNativeVerifiedDiskPathRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	h, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	target := filepath.Join(root, "tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := h.executable("/tool")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, err := h.verifiedDiskPath(f, "/tool"); err != nil || got != target {
		t.Fatalf("verified path = %q, %v", got, err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.verifiedDiskPath(f, "/tool"); !errors.Is(err, errUnsafe) {
		t.Fatalf("replaced file accepted: %v", err)
	}
}
