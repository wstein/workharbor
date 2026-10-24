//go:build linux

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

const physicalENOSPCBytes = 1 << 20

type physicalENOSPCOutput struct{ bytes.Buffer }

func (b *physicalENOSPCOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8192 {
		return 0, fmt.Errorf("physical ENOSPC child output exceeded bound")
	}
	return b.Buffer.Write(p)
}

func TestInitPhysicalENOSPCProcess(t *testing.T) {
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
	if len(args) != 5 {
		t.Fatal("invalid private ENOSPC helper arguments")
	}
	caseName, mountpoint := args[0], args[1]
	for i, name := range []string{"user", "net", "mnt"} {
		current, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil || current == args[i+2] {
			t.Fatalf("refuse mount/fill without a private %s namespace: %v", name, err)
		}
	}
	info, err := os.Lstat(mountpoint)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		t.Fatalf("unsafe owned mountpoint: %v", err)
	}
	entries, err := os.ReadDir(mountpoint)
	if err != nil || len(entries) != 0 {
		t.Fatalf("mountpoint must be an empty owned directory: %v", err)
	}
	propagation, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil || strings.Contains(string(propagation), " shared:") {
		t.Fatalf("refuse mount with shared propagation: %v", err)
	}
	if err := unix.Mount("tmpfs", mountpoint, "tmpfs", 0, "size=1048576,mode=0700"); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			fmt.Fprintln(os.Stderr, "UNSUPPORTED: private tmpfs mount:", err)
			os.Exit(77)
		}
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Unmount(mountpoint, 0); err != nil {
			t.Errorf("private fixture unmount: %v", err)
		}
	}()
	var mounted unix.Stat_t
	var capacity unix.Statfs_t
	if err := unix.Stat(mountpoint, &mounted); err != nil {
		t.Fatal(err)
	}
	if err := unix.Statfs(mountpoint, &capacity); err != nil || capacity.Type != unix.TMPFS_MAGIC || capacity.Blocks == 0 || capacity.Blocks*uint64(capacity.Bsize) > physicalENOSPCBytes || uint64(info.Sys().(*syscall.Stat_t).Dev) == mounted.Dev {
		t.Fatalf("refuse fill outside a new tmpfs of at most 1 MiB: %v", err)
	}
	dir := filepath.Join(mountpoint, "target")
	for _, name := range []string{dir, filepath.Join(dir, ".devcontainer"), filepath.Join(dir, "projects")} {
		if err := os.Mkdir(name, 0750); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		".devcontainer/devcontainer.json": []byte("{\"image\":\"debian:bookworm\"}\n"),
		"projects/owner.txt":              []byte("preserve project data\n"),
	}
	if caseName != "create-write" {
		files["workharbor.json"] = []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0640); err != nil {
			t.Fatal(err)
		}
	}
	type entry struct {
		info fs.FileInfo
		data []byte
	}
	snapshot := func() map[string]entry {
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
	before := snapshot()
	name := "changed"
	options := Options{Name: &name, NonInteractive: true, Yes: true}
	if caseName != "create-write" {
		options.Overwrite = []string{"workharbor.json"}
	}
	proposal := applyProposal(t, dir, options)
	fillerPath := filepath.Join(mountpoint, "filler")
	var filler *os.File
	release := func() {
		if filler != nil {
			if err := filler.Close(); err != nil {
				t.Fatal(err)
			}
			filler = nil
			if err := os.Remove(fillerPath); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer release()
	filled, positiveControl := false, false
	fill := func() {
		var err error
		filler, err = os.OpenFile(fillerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
		if err != nil {
			t.Fatal(err)
		}
		var actual unix.Statfs_t
		var identity unix.Stat_t
		if err := unix.Fstat(int(filler.Fd()), &identity); err != nil {
			t.Fatal(err)
		}
		if err := unix.Fstatfs(int(filler.Fd()), &actual); err != nil || actual.Type != unix.TMPFS_MAGIC || actual.Blocks*uint64(actual.Bsize) > physicalENOSPCBytes || identity.Dev != mounted.Dev {
			t.Fatalf("refuse to fill an unverified filesystem: %v", err)
		}
		block := make([]byte, 4096)
		for written := 0; written <= physicalENOSPCBytes; {
			n, err := filler.Write(block)
			written += n
			if errors.Is(err, syscall.ENOSPC) {
				filled = true
				break
			}
			if err != nil || n == 0 {
				t.Fatalf("unexpected bounded filler result: %d, %v", n, err)
			}
		}
		if !filled {
			t.Fatal("bounded tmpfs fill did not produce kernel ENOSPC")
		}
		control, err := os.OpenFile(filepath.Join(mountpoint, "control"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := control.Write([]byte("positive control"))
		positiveControl = errors.Is(writeErr, syscall.ENOSPC)
		if err := control.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(control.Name()); err != nil {
			t.Fatal(err)
		}
		if !positiveControl {
			t.Fatalf("full-filesystem positive control did not fail with ENOSPC: %v", writeErr)
		}
	}
	event := "write:AGENTS.md"
	switch caseName {
	case "create-write":
		event = "write:workharbor.json"
	case "overwrite-temp":
		event = "write-temp:.whr-init-tmp-00000001"
	case "overwrite-restore", "overwrite-partial":
	default:
		t.Fatal("unknown physical ENOSPC case")
	}
	releasedForRollback := false
	result, applyErr := proposal.apply(context.Background(), func(at string) error {
		if at == event && !filled {
			fill()
		}
		if filled && strings.HasPrefix(at, "rollback:") && caseName != "overwrite-partial" && !releasedForRollback {
			release()
			releasedForRollback = true
		}
		return nil // The actual product write, not this callback, must fail.
	})
	want := exitcode.Internal
	if caseName == "overwrite-partial" {
		want = exitcode.PartialFailure
	}
	if !filled || !positiveControl || exitcode.FromError(applyErr) != want {
		t.Fatalf("physical ENOSPC must yield %d: filled=%t control=%t err=%v", want, filled, positiveControl, applyErr)
	}
	if want == exitcode.Internal && (!releasedForRollback || !strings.Contains(applyErr.Error(), syscall.ENOSPC.Error()) || len(result.Remaining) != 0) {
		t.Fatalf("missing actual write failure or complete rollback: %v remaining=%v", applyErr, result.Remaining)
	}
	after := snapshot()
	if want == exitcode.PartialFailure {
		remaining := append([]string(nil), result.Remaining...)
		sort.Strings(remaining)
		if !reflect.DeepEqual(remaining, []string{".whr-init-tmp-journal", "workharbor.json"}) || len(after) != len(before)+1 {
			t.Fatalf("partial recovery inventory is inaccurate: remaining=%v before=%v after=%v", remaining, before, after)
		}
		if _, exists := after[".whr-init-tmp-journal"]; !exists || bytes.Equal(after["workharbor.json"].data, before["workharbor.json"].data) {
			t.Fatal("partial failure must retain its marker and report the unrestored manifest")
		}
	} else if len(after) != len(before) {
		t.Fatalf("rollback left generated paths: before=%v after=%v", before, after)
	}
	for path, old := range before {
		current, exists := after[path]
		if !exists || old.info.Mode() != current.info.Mode() {
			t.Fatalf("rollback lost original path/mode: %s", path)
		}
		if want == exitcode.PartialFailure && path == "workharbor.json" {
			continue
		}
		if !bytes.Equal(old.data, current.data) {
			t.Fatalf("rollback did not restore original bytes: %s", path)
		}
		if old.info.IsDir() || path != "workharbor.json" {
			if !os.SameFile(old.info, current.info) {
				t.Fatalf("rollback replaced pre-existing path: %s", path)
			}
		}
	}
	fmt.Printf("physical-enospc:%s:%d\n", caseName, want)
}

func TestInitPhysicalENOSPC(t *testing.T) {
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skipf("physical ENOSPC not executed: unshare unavailable: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var namespaces []string
	for _, name := range []string{"user", "net", "mnt"} {
		value, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			t.Skipf("physical ENOSPC not executed: cannot inspect %s namespace: %v", name, err)
		}
		namespaces = append(namespaces, value)
	}
	for _, caseName := range []string{"create-write", "overwrite-temp", "overwrite-restore", "overwrite-partial"} {
		t.Run(caseName, func(t *testing.T) {
			private := t.TempDir()
			mountpoint := filepath.Join(private, "bounded-filesystem")
			if err := os.Mkdir(mountpoint, 0700); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(mountpoint)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-rnm", "--", executable, "-test.run=^TestInitPhysicalENOSPCProcess$", "--", caseName, mountpoint}
			args = append(args, namespaces...)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, unshare, args...)
			cmd.Dir = private
			cmd.Env = []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "TERM=dumb"}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = 2 * time.Second
			var stdout, stderr physicalENOSPCOutput
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			if ctx.Err() != nil {
				t.Fatalf("owned physical ENOSPC child exceeded deadline: %v", ctx.Err())
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) && ((exit.ExitCode() == 77 && strings.HasPrefix(stderr.String(), "UNSUPPORTED:")) || strings.Contains(stderr.String(), "unshare failed: Operation not permitted")) {
				t.Skipf("physical ENOSPC not executed: namespace/mount permission unavailable: %s", stderr.String())
			}
			if err != nil || !strings.Contains(stdout.String(), "physical-enospc:"+caseName+":") || stderr.Len() != 0 {
				t.Fatalf("physical ENOSPC fixture failed: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			after, err := os.Stat(mountpoint)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("child changed the owner's mountpoint identity: %v", err)
			}
			entries, err := os.ReadDir(mountpoint)
			if err != nil || len(entries) != 0 {
				t.Fatalf("child touched the underlying host directory: %v entries=%v", err, entries)
			}
		})
	}
}
