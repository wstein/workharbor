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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

const physicalENOSPCBytes = 1 << 20

var errPhysicalENOSPCOutputBound = errors.New("physical ENOSPC child output exceeded bound")
var errPhysicalENOSPCDeadline = errors.New("owned physical ENOSPC child exceeded deadline")

type physicalENOSPCOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *physicalENOSPCOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8192 {
		b.overflow = true
		return 0, errPhysicalENOSPCOutputBound
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
	fmt.Println("physical-enospc-child-started")
	if len(args) != 8 {
		t.Fatal("invalid private ENOSPC helper arguments")
	}
	caseName, mountpoint := args[0], args[1]
	if args[5] == "0" {
		t.Fatal("Go fixture requires a non-root original host owner")
	}
	wantEnv := []string{"HOME=" + filepath.Dir(mountpoint), "TMPDIR=" + filepath.Dir(mountpoint), "PATH=" + filepath.Dir(mountpoint), "TERM=dumb"}
	actualEnv := os.Environ()
	sort.Strings(wantEnv)
	sort.Strings(actualEnv)
	if !reflect.DeepEqual(actualEnv, wantEnv) {
		t.Fatal("physical fixture child environment is not closed")
	}
	for i, name := range []string{"user", "net", "mnt"} {
		current, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil || current == args[i+2] {
			t.Fatalf("refuse mount/fill without a private %s namespace: %v", name, err)
		}
	}
	for i, name := range []string{"uid", "gid"} {
		mapping, err := os.ReadFile("/proc/self/" + name + "_map")
		if err != nil || !reflect.DeepEqual(strings.Fields(string(mapping)), []string{"0", args[i+5], "1"}) {
			t.Fatalf("refuse fixture without exact original-owner %s mapping: %v", name, err)
		}
	}
	if os.Getuid() != 0 || os.Geteuid() != 0 || os.Getgid() != 0 || os.Getegid() != 0 {
		t.Fatal("fixture must be inner root mapped exclusively to the original owner")
	}
	if args[7] == "mapped" {
		groups, err := os.Getgroups()
		if err != nil || len(groups) != 0 {
			t.Fatalf("mapped fallback retained supplementary host groups: %v", err)
		}
	} else if args[7] != "direct" {
		t.Fatal("unknown private fixture launcher")
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

func physicalENOSPCProofRequired(ci, netns, physical string) bool {
	return ci != "" || netns == "1" || physical == "1"
}

func physicalENOSPCCapabilityUnavailable(t *testing.T, reason string) {
	t.Helper()
	if physicalENOSPCProofRequired(os.Getenv("CI"), os.Getenv("WHR_REQUIRE_NETNS"), os.Getenv("WHR_REQUIRE_PHYSICAL_ENOSPC")) {
		t.Fatalf("physical ENOSPC FAILED, NOT SKIPPED: %s", reason)
	}
	t.Skipf("physical ENOSPC not executed: %s", reason)
}

func physicalENOSPCNativeRefusal(runErr error, timedOut bool, stdout, stderr *physicalENOSPCOutput) (bool, error) {
	if timedOut || errors.Is(runErr, exec.ErrWaitDelay) {
		return false, errPhysicalENOSPCDeadline
	}
	// Wait can prefer a process ExitError over a pipe-copy error, so retain
	// the writers' explicit overflow flags as well as the sentinel.
	if stdout.overflow || stderr.overflow || errors.Is(runErr, errPhysicalENOSPCOutputBound) {
		return false, errPhysicalENOSPCOutputBound
	}
	if strings.Contains(stdout.String(), "physical-enospc-child-started\n") {
		var exit *exec.ExitError
		return errors.As(runErr, &exit) && exit.ExitCode() == 77 && strings.HasPrefix(stderr.String(), "UNSUPPORTED:"), nil
	}
	for _, prefix := range []string{"unshare:", "sudo:", "setpriv:"} {
		if runErr != nil && strings.HasPrefix(stderr.String(), prefix) {
			return true, nil
		}
	}
	var missing *os.PathError
	return errors.As(runErr, &missing), nil
}

func physicalENOSPCCandidates(unshare, sudo, setpriv, env string, uid, gid int, childEnv, childArgs []string) [][]string {
	if unshare == "" || env == "" || uid <= 0 {
		return nil
	}
	program := append([]string{env, "-i"}, childEnv...)
	program = append(program, childArgs...)
	direct := append([]string{unshare, "-rnm", "--"}, program...)
	direct = append(direct, "direct")
	candidates := [][]string{direct}
	if sudo != "" && setpriv != "" {
		mapped := []string{sudo, "-n", "--", unshare, "--user", "--mount", "--net",
			"--map-users=0:" + strconv.Itoa(uid) + ":1", "--map-groups=0:" + strconv.Itoa(gid) + ":1",
			"--setgroups=allow", "--setuid=0", "--setgid=0", "--", setpriv, "--clear-groups", "--"}
		mapped = append(mapped, program...)
		candidates = append(candidates, append(mapped, "mapped"))
	}
	return candidates
}

func physicalENOSPCTrustedSystemTool(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(resolved, "/usr/bin/") {
		return ""
	}
	for current := resolved; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil || info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return ""
		}
		if current == "/" {
			return resolved
		}
	}
}

func physicalENOSPCResolvedCandidates(unshare, sudo, setpriv, env string, uid, gid int, childEnv, childArgs []string) [][]string {
	// The ordinary owner candidate may use PATH. The privileged candidate must
	// retain only verified canonical paths, never the mutable lookup aliases.
	candidates := physicalENOSPCCandidates(unshare, "", "", env, uid, gid, childEnv, childArgs)
	canonicalUnshare := physicalENOSPCTrustedSystemTool(unshare)
	canonicalSudo := physicalENOSPCTrustedSystemTool(sudo)
	canonicalSetpriv := physicalENOSPCTrustedSystemTool(setpriv)
	canonicalEnv := physicalENOSPCTrustedSystemTool(env)
	if canonicalUnshare == "" || canonicalSudo == "" || canonicalSetpriv == "" || canonicalEnv == "" {
		return candidates
	}
	verified := physicalENOSPCCandidates(canonicalUnshare, canonicalSudo, canonicalSetpriv, canonicalEnv, uid, gid, childEnv, childArgs)
	if len(verified) == 2 {
		candidates = append(candidates, verified[1])
	}
	return candidates
}

func physicalENOSPCRunCase(t *testing.T, caseName string, mappedOnly bool) {
	t.Helper()
	if os.Getuid() == 0 {
		physicalENOSPCCapabilityUnavailable(t, "Go fixture requires a non-root original host owner")
	}
	lookup := func(name string) string { path, _ := exec.LookPath(name); return path }
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var namespaces []string
	for _, name := range []string{"user", "net", "mnt"} {
		value, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			physicalENOSPCCapabilityUnavailable(t, "cannot inspect "+name+" namespace: "+err.Error())
		}
		namespaces = append(namespaces, value)
	}
	private := t.TempDir()
	mountpoint := filepath.Join(private, "bounded-filesystem")
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	childEnv := []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "TERM=dumb"}
	args := []string{executable, "-test.run=^TestInitPhysicalENOSPCProcess$", "--", caseName, mountpoint}
	args = append(args, namespaces...)
	args = append(args, strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()))
	unshare, sudo, setpriv, env := lookup("unshare"), lookup("sudo"), lookup("setpriv"), lookup("env")
	candidates := physicalENOSPCResolvedCandidates(unshare, sudo, setpriv, env, os.Getuid(), os.Getgid(), childEnv, args)
	if mappedOnly {
		if len(candidates) < 2 {
			physicalENOSPCCapabilityUnavailable(t, "mapped fallback utilities unavailable")
		}
		candidates = candidates[1:]
	}
	var refusals []string
	succeeded := false
	for _, argv := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.Env = private, childEnv
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = 2 * time.Second
		var stdout, stderr physicalENOSPCOutput
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err = cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		refused, boundaryErr := physicalENOSPCNativeRefusal(err, timedOut, &stdout, &stderr)
		if boundaryErr != nil {
			t.Fatal(boundaryErr)
		}
		if refused {
			refusals = append(refusals, fmt.Sprintf("native launcher %s: %v %s", filepath.Base(argv[0]), err, strings.TrimSpace(stderr.String())))
			continue
		}
		if err != nil || !strings.Contains(stdout.String(), "physical-enospc:"+caseName+":") || stderr.Len() != 0 {
			t.Fatalf("physical ENOSPC fixture failed: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}
		succeeded = true
		t.Logf("physical ENOSPC verified via %s launcher after %d native refusals", argv[len(argv)-1], len(refusals))
		break
	}
	if !succeeded {
		physicalENOSPCCapabilityUnavailable(t, "all native candidates unavailable: "+strings.Join(refusals, "; "))
	}
	after, err := os.Stat(mountpoint)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("child changed the owner's mountpoint identity: %v", err)
	}
	entries, err := os.ReadDir(mountpoint)
	if err != nil || len(entries) != 0 {
		t.Fatalf("child touched the underlying host directory: %v entries=%v", err, entries)
	}
}

func TestInitPhysicalENOSPC(t *testing.T) {
	for _, caseName := range []string{"create-write", "overwrite-temp", "overwrite-restore", "overwrite-partial"} {
		t.Run(caseName, func(t *testing.T) { physicalENOSPCRunCase(t, caseName, false) })
	}
}

func TestInitPhysicalENOSPCMappedFallback(t *testing.T) {
	physicalENOSPCRunCase(t, "overwrite-restore", true)
}

func TestInitPhysicalENOSPCFallbackArgvAndClosedEnvironment(t *testing.T) {
	childEnv := []string{"HOME=/private", "TMPDIR=/private", "PATH=/private", "TERM=dumb"}
	childArgs := []string{"/own/test", "-test.run=^TestInitPhysicalENOSPCProcess$", "--", "create-write", "/private/mount", "user-ns", "net-ns", "mount-ns", "1001", "1002"}
	got := physicalENOSPCCandidates("/native/unshare", "/native/sudo", "/native/setpriv", "/native/env", 1001, 1002, childEnv, childArgs)
	direct := []string{"/native/unshare", "-rnm", "--", "/native/env", "-i"}
	direct = append(direct, childEnv...)
	direct = append(direct, childArgs...)
	direct = append(direct, "direct")
	mapped := []string{"/native/sudo", "-n", "--", "/native/unshare", "--user", "--mount", "--net", "--map-users=0:1001:1", "--map-groups=0:1002:1", "--setgroups=allow", "--setuid=0", "--setgid=0", "--", "/native/setpriv", "--clear-groups", "--", "/native/env", "-i"}
	mapped = append(mapped, childEnv...)
	mapped = append(mapped, childArgs...)
	mapped = append(mapped, "mapped")
	if !reflect.DeepEqual(got, [][]string{direct, mapped}) {
		t.Fatalf("namespace candidates must preserve owner mappings, clear groups and close the child environment: %#v", got)
	}
	if got := physicalENOSPCCandidates("/native/unshare", "/native/sudo", "/native/setpriv", "/native/env", 0, 1002, childEnv, childArgs); len(got) != 0 {
		t.Fatal("fixture must never launch the Go writer with host UID 0")
	}
}

func TestInitPhysicalENOSPCRequiredRestrictedNamespace(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, refusal := range []string{"unshare: unshare failed: Operation not permitted", "unshare: write failed /proc/self/uid_map: Operation not permitted"} {
		t.Run(refusal, func(t *testing.T) {
			private := t.TempDir()
			if err := os.WriteFile(filepath.Join(private, "unshare"), []byte("#!/bin/sh\nprintf '%s\\n' '"+refusal+"' >&2\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			env, err := exec.LookPath("env")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(env, filepath.Join(private, "env")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestInitPhysicalENOSPC$", "-test.v")
			cmd.Env = []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "WHR_REQUIRE_NETNS=1", "TERM=dumb"}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = 2 * time.Second
			var output physicalENOSPCOutput
			cmd.Stdout, cmd.Stderr = &output, &output
			err = cmd.Run()
			var exit *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(output.String(), "FAILED, NOT SKIPPED") || !strings.Contains(output.String(), refusal) || strings.Contains(output.String(), "--- SKIP:") {
				t.Fatalf("required namespace refusal must fail explicitly, never skip: %v output=%q", err, output.String())
			}
		})
	}
}

func TestInitPhysicalENOSPCRequiredPolicy(t *testing.T) {
	for _, tc := range []struct {
		ci, netns, physical string
		want                bool
	}{{"", "", "", false}, {"false", "", "", true}, {"true", "", "", true}, {"", "1", "", true}, {"", "", "1", true}, {"", "0", "0", false}} {
		if got := physicalENOSPCProofRequired(tc.ci, tc.netns, tc.physical); got != tc.want {
			t.Fatalf("required policy (%q,%q,%q)=%t, want %t", tc.ci, tc.netns, tc.physical, got, tc.want)
		}
	}
}

func TestInitPhysicalENOSPCNativePrefixCannotMaskBounds(t *testing.T) {
	for _, boundary := range []string{"stdout-overflow", "stderr-overflow", "copy-error", "deadline", "wait-delay"} {
		t.Run(boundary, func(t *testing.T) {
			var stdout, stderr physicalENOSPCOutput
			_, _ = stderr.Write([]byte("unshare: write failed /proc/self/uid_map: Operation not permitted\n"))
			runErr := errors.New("native exit 1")
			timedOut := false
			want := errPhysicalENOSPCOutputBound
			switch boundary {
			case "stdout-overflow":
				_, _ = stdout.Write(make([]byte, 8193))
			case "stderr-overflow":
				_, _ = stderr.Write(make([]byte, 8193))
			case "copy-error":
				runErr = errPhysicalENOSPCOutputBound
			case "deadline":
				timedOut, want = true, errPhysicalENOSPCDeadline
			case "wait-delay":
				runErr, want = exec.ErrWaitDelay, errPhysicalENOSPCDeadline
			}
			refused, err := physicalENOSPCNativeRefusal(runErr, timedOut, &stdout, &stderr)
			if refused || !errors.Is(err, want) {
				t.Fatalf("native-looking output masked a hard boundary: refused=%t err=%v", refused, err)
			}
		})
	}
}

func TestInitPhysicalENOSPCChildAssertionCannotTriggerFallback(t *testing.T) {
	var stdout, stderr physicalENOSPCOutput
	_, _ = stdout.Write([]byte("physical-enospc-child-started\nassertion failed\n"))
	_, _ = stderr.Write([]byte("unshare: write failed /proc/self/uid_map: Operation not permitted\n"))
	refused, err := physicalENOSPCNativeRefusal(errors.New("child assertion exit 1"), false, &stdout, &stderr)
	if refused || err != nil {
		t.Fatalf("started helper assertion must not become a native refusal: %t, %v", refused, err)
	}
}

func TestInitPhysicalENOSPCFallbackRejectsOwnerWritableTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unshare")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if physicalENOSPCTrustedSystemTool(path) != "" || physicalENOSPCTrustedSystemTool("") != "" {
		t.Fatal("an owner-writable PATH wrapper must not enter the privileged chain")
	}
}

func TestInitPhysicalENOSPCMappedArgvSurvivesMutableAliases(t *testing.T) {
	private := t.TempDir()
	var aliases, canonical []string
	for _, name := range []string{"unshare", "sudo", "setpriv", "env"} {
		system, err := filepath.EvalSymlinks(filepath.Join("/usr/bin", name))
		if err != nil || physicalENOSPCTrustedSystemTool(system) != system {
			physicalENOSPCCapabilityUnavailable(t, fmt.Sprintf("canonical native positive control missing or untrusted for %s: %v", name, err))
		}
		alias := filepath.Join(private, name)
		if err := os.Symlink(system, alias); err != nil {
			t.Fatal(err)
		}
		aliases, canonical = append(aliases, alias), append(canonical, system)
	}
	candidates := physicalENOSPCResolvedCandidates(aliases[0], aliases[1], aliases[2], aliases[3], 1001, 1002,
		[]string{"HOME=/private", "TMPDIR=/private", "PATH=/private", "TERM=dumb"}, []string{"/own/test"})
	if len(candidates) != 2 {
		t.Fatalf("canonical aliases must resolve to a mapped candidate: %#v", candidates)
	}
	for _, alias := range aliases {
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(alias, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// No candidate is executed. Replacing writable aliases after construction
	// must not change any utility selected for the privileged command chain.
	for i, position := range []int{3, 0, 13, 16} {
		if candidates[1][position] != canonical[i] {
			t.Errorf("privileged argv retained mutable %s alias: %q, want canonical %q", filepath.Base(aliases[i]), candidates[1][position], canonical[i])
		}
	}
}
