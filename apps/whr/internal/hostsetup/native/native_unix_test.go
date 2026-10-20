// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package native

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		child := exec.Command(exe, "-test.run=^TestNativeHelper$", "--", "block")
		child.Env = os.Environ()
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(13)
		}
		fmt.Print(child.Process.Pid)
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
			if cancel {
				time.AfterFunc(30*time.Millisecond, stop)
			}
			start := time.Now()
			_, err := h.Run(ctx, p)
			if err == nil || time.Since(start) > 2*time.Second {
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
	start := time.Now()
	out, err := h.Run(context.Background(), probe(path, "descendant"))
	pid, parseErr := strconv.Atoi(out.Stdout)
	if parseErr != nil || pid <= 0 {
		t.Fatalf("missing owned descendant identity: %+v %v", out, err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err == nil || time.Since(start) > 2*time.Second {
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
