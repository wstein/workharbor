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

// This literal is executed by a canonical system shell, never an owned script.
// Every variable comes from one quoted positional argument; Go runs only after
// setpriv drops the initial-user-namespace root identity and supplementary groups.
const physicalENOSPCPrelude = `fixture_mount=$1
fixture_setpriv=$2
fixture_env=$3
fixture_readlink=$4
fixture_uid=$5
fixture_gid=$6
fixture_target=$7
fixture_net=$8
fixture_mnt=$9
shift 9
fixture_user=$1
shift
case "$fixture_uid:$fixture_gid" in *[!0-9:]*|:*|*:) exit 64;; esac
[ "$fixture_uid" -gt 0 ]
case "$fixture_target" in /*/bounded-filesystem) ;; *) exit 64;; esac
[ -d "$fixture_target" ] && [ ! -L "$fixture_target" ]
for fixture_entry in "$fixture_target"/* "$fixture_target"/.[!.]* "$fixture_target"/..?*; do
  [ ! -e "$fixture_entry" ] && [ ! -L "$fixture_entry" ] || exit 64
done
[ "$("$fixture_readlink" /proc/self/ns/user)" = "$fixture_user" ]
[ "$("$fixture_readlink" /proc/self/ns/net)" != "$fixture_net" ]
[ "$("$fixture_readlink" /proc/self/ns/mnt)" != "$fixture_mnt" ]
"$fixture_mount" --no-mtab --internal-only -t tmpfs -o "size=1048576,mode=0700,uid=$fixture_uid,gid=$fixture_gid,nosuid,nodev,noexec" tmpfs "$fixture_target"
exec "$fixture_setpriv" "--reuid=$fixture_uid" "--regid=$fixture_gid" --clear-groups -- "$fixture_env" -i "$@"
`

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
	if len(args) != 9 {
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
	premounted := args[8] == "premounted"
	if !premounted && args[8] != "direct" {
		t.Fatal("unknown private fixture launcher")
	}
	// Reject an invalid dropped identity before inspecting namespaces. This
	// also lets owner-only negatives reach the actual guard without mounting.
	if premounted {
		uid, uidErr := strconv.Atoi(args[5])
		gid, gidErr := strconv.Atoi(args[6])
		groups, groupErr := os.Getgroups()
		if uidErr != nil || gidErr != nil || uid <= 0 || os.Getuid() != uid || os.Geteuid() != uid || os.Getgid() != gid || os.Getegid() != gid {
			t.Fatal("premounted Go fixture must run only as the original non-root owner")
		}
		if groupErr != nil || len(groups) != 0 {
			t.Fatal("premounted Go fixture requires cleared supplementary groups")
		}
	}
	for i, name := range []string{"user", "net", "mnt"} {
		current, err := os.Readlink("/proc/self/ns/" + name)
		wantSame := premounted && name == "user"
		if err != nil || (current == args[i+2]) != wantSame {
			t.Fatalf("refuse mount/fill with unexpected %s namespace: %v", name, err)
		}
	}
	if !premounted {
		for i, name := range []string{"uid", "gid"} {
			mapping, err := os.ReadFile("/proc/self/" + name + "_map")
			if err != nil || !physicalENOSPCDirectMapMatches(string(mapping), args[i+5]) {
				t.Fatalf("refuse fixture without exact original-owner %s mapping: %v", name, err)
			}
		}
		if os.Getuid() != 0 || os.Geteuid() != 0 || os.Getgid() != 0 || os.Getegid() != 0 {
			t.Fatal("direct fixture must be inner root mapped exclusively to the original owner")
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
	if !premounted {
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
	}
	var mounted unix.Stat_t
	var capacity unix.Statfs_t
	if err := unix.Stat(mountpoint, &mounted); err != nil {
		t.Fatal(err)
	}
	originalDevice, err := strconv.ParseUint(args[7], 10, 64)
	if err != nil {
		t.Fatal("invalid original mountpoint device")
	}
	if premounted && (mounted.Uid != uint32(os.Getuid()) || mounted.Gid != uint32(os.Getgid()) || info.Mode().Perm() != 0700) {
		t.Fatal("premounted filesystem must belong exclusively to the original owner")
	}
	if err := unix.Statfs(mountpoint, &capacity); err != nil || capacity.Type != unix.TMPFS_MAGIC || capacity.Blocks == 0 || capacity.Blocks*uint64(capacity.Bsize) > physicalENOSPCBytes || originalDevice == mounted.Dev {
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

// Re-execute the actual runtime entry point as the ordinary owner. Deliberate
// identity/environment mismatches must fail at their specific guards. All
// namespace identities remain unchanged, so even a disabled guard cannot get
// past namespace validation to a mount or filesystem fill.
func TestInitPhysicalENOSPCHelperRejectsUnsafeRuntime(t *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		t.Fatal("runtime guard negatives require an ordinary non-root owner")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var namespaces []string
	for _, name := range []string{"user", "net", "mnt"} {
		value, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			t.Fatal(err)
		}
		namespaces = append(namespaces, value)
	}
	for _, name := range []string{"owner-uid", "owner-gid", "extra-environment"} {
		t.Run(name, func(t *testing.T) {
			private := t.TempDir()
			mountpoint := filepath.Join(private, "bounded-filesystem")
			if err := os.Mkdir(mountpoint, 0700); err != nil {
				t.Fatal(err)
			}
			uid, gid := os.Getuid(), os.Getgid()
			if name == "owner-uid" {
				uid++
			}
			if name == "owner-gid" {
				gid++
			}
			args := []string{"-test.run=^TestInitPhysicalENOSPCProcess$", "--", "create-write", mountpoint}
			args = append(args, namespaces...)
			args = append(args, strconv.Itoa(uid), strconv.Itoa(gid), "0", "premounted")
			env := []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "TERM=dumb"}
			want := "premounted Go fixture must run only as the original non-root owner"
			if name == "extra-environment" {
				env = append(env, "WHR_ENOSPC_UNEXPECTED_ENV=owned-negative-control")
				want = "physical fixture child environment is not closed"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, args...)
			cmd.Dir, cmd.Env = private, env
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = time.Second
			var stdout, stderr physicalENOSPCOutput
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if ctx.Err() != nil || stdout.overflow || stderr.overflow || !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(stdout.String(), "physical-enospc-child-started\n") || !strings.Contains(stdout.String(), want) {
				t.Fatalf("runtime guard did not refuse with %q: error=%v stdout=%q stderr=%q", want, err, stdout.String(), stderr.String())
			}
			entries, err := os.ReadDir(mountpoint)
			if err != nil || len(entries) != 0 {
				t.Fatalf("runtime negative touched owned mountpoint: %v, %v", entries, err)
			}
		})
	}
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

func physicalENOSPCDirectMapMatches(mapping, owner string) bool {
	return reflect.DeepEqual(strings.Fields(mapping), []string{"0", owner, "1"})
}

func physicalENOSPCCandidates(unshare, sudo, setpriv, env, shell, mount, readlink string, uid, gid int, childEnv, childArgs []string) [][]string {
	if unshare == "" || env == "" || uid <= 0 {
		return nil
	}
	program := append([]string{env, "-i"}, childEnv...)
	program = append(program, childArgs...)
	direct := append([]string{unshare, "-rnm", "--"}, program...)
	direct = append(direct, "direct")
	candidates := [][]string{direct}
	if sudo != "" && setpriv != "" && shell != "" && mount != "" && readlink != "" && len(childArgs) == 11 {
		// childArgs are constructed locally: executable, test flag, --, case,
		// mountpoint, parent user/net/mount namespaces, owner UID/GID, device.
		staged := []string{sudo, "-n", "--", env, "-i"}
		staged = append(staged, childEnv...)
		staged = append(staged, []string{unshare, "--mount", "--net", "--propagation", "private", "--", shell, "-eu", "-c", physicalENOSPCPrelude, "whr-enospc-root-prelude",
			mount, setpriv, env, readlink, strconv.Itoa(uid), strconv.Itoa(gid), childArgs[4], childArgs[6], childArgs[7], childArgs[5]}...)
		staged = append(staged, childEnv...)
		staged = append(staged, childArgs...)
		candidates = append(candidates, append(staged, "premounted"))
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

func physicalENOSPCResolvedCandidates(unshare, sudo, setpriv, env, shell, mount, readlink string, uid, gid int, childEnv, childArgs []string) [][]string {
	// Only the ordinary owner candidate may retain PATH aliases. Every tool in
	// the root stage is a canonical path with root-owned nonwritable ancestors.
	candidates := physicalENOSPCCandidates(unshare, "", "", env, "", "", "", uid, gid, childEnv, childArgs)
	canonical := []string{unshare, sudo, setpriv, env, shell, mount, readlink}
	for i, path := range canonical {
		canonical[i] = physicalENOSPCTrustedSystemTool(path)
		if canonical[i] == "" {
			return candidates
		}
	}
	verified := physicalENOSPCCandidates(canonical[0], canonical[1], canonical[2], canonical[3], canonical[4], canonical[5], canonical[6], uid, gid, childEnv, childArgs)
	if len(verified) == 2 {
		candidates = append(candidates, verified[1])
	}
	return candidates
}

func physicalENOSPCRunCase(t *testing.T, caseName string, premountedOnly bool) {
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
	args = append(args, strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()), strconv.FormatUint(uint64(before.Sys().(*syscall.Stat_t).Dev), 10))
	unshare, sudo, setpriv, env := lookup("unshare"), lookup("sudo"), lookup("setpriv"), lookup("env")
	candidates := physicalENOSPCResolvedCandidates(unshare, sudo, setpriv, env, lookup("sh"), lookup("mount"), lookup("readlink"), os.Getuid(), os.Getgid(), childEnv, args)
	if premountedOnly {
		if len(candidates) < 2 {
			physicalENOSPCCapabilityUnavailable(t, "premounted fallback utilities unavailable")
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

func TestInitPhysicalENOSPCPremountedFallback(t *testing.T) {
	physicalENOSPCRunCase(t, "overwrite-restore", true)
}

func TestInitPhysicalENOSPCFallbackArgvAndClosedEnvironment(t *testing.T) {
	childEnv := []string{"HOME=/private", "TMPDIR=/private", "PATH=/private", "TERM=dumb"}
	childArgs := []string{"/own/test", "-test.run=^TestInitPhysicalENOSPCProcess$", "--", "create-write", "/private/mount", "user-ns", "net-ns", "mount-ns", "1001", "1002", "123"}
	got := physicalENOSPCCandidates("/native/unshare", "/native/sudo", "/native/setpriv", "/native/env", "/native/sh", "/native/mount", "/native/readlink", 1001, 1002, childEnv, childArgs)
	direct := []string{"/native/unshare", "-rnm", "--", "/native/env", "-i"}
	direct = append(direct, childEnv...)
	direct = append(direct, childArgs...)
	direct = append(direct, "direct")
	staged := []string{"/native/sudo", "-n", "--", "/native/env", "-i"}
	staged = append(staged, childEnv...)
	staged = append(staged, []string{"/native/unshare", "--mount", "--net", "--propagation", "private", "--", "/native/sh", "-eu", "-c", physicalENOSPCPrelude, "whr-enospc-root-prelude", "/native/mount", "/native/setpriv", "/native/env", "/native/readlink", "1001", "1002", "/private/mount", "net-ns", "mount-ns", "user-ns"}...)
	staged = append(staged, childEnv...)
	staged = append(staged, childArgs...)
	staged = append(staged, "premounted")
	if !reflect.DeepEqual(got, [][]string{direct, staged}) {
		t.Fatalf("namespace candidates must isolate mounts, drop root and close both stage environments: %#v", got)
	}
	if got := physicalENOSPCCandidates("/native/unshare", "/native/sudo", "/native/setpriv", "/native/env", "/native/sh", "/native/mount", "/native/readlink", 0, 1002, childEnv, childArgs); len(got) != 0 {
		t.Fatal("fixture must never launch the Go writer with host UID 0")
	}
}

func TestInitPhysicalENOSPCFallbackAvoidsUIDMappingHelpers(t *testing.T) {
	args := []string{"/own/test", "-test.run=helper", "--", "create-write", "/private/owned $(literal); path/bounded-filesystem", "user-ns", "net-ns", "mount-ns", "1001", "1002", "123"}
	candidates := physicalENOSPCCandidates("/native/unshare", "/native/sudo", "/native/setpriv", "/native/env", "/native/sh", "/native/mount", "/native/readlink", 1001, 1002,
		[]string{"HOME=/private", "TMPDIR=/private", "PATH=/private", "TERM=dumb"}, args)
	if len(candidates) != 2 {
		t.Fatal("fixture must offer a portable privileged candidate")
	}
	if candidates[1][26] != args[4] || strings.Contains(physicalENOSPCPrelude, args[4]) {
		t.Fatal("owned paths must remain quoted positional arguments, never root shell source")
	}
	for _, arg := range candidates[1] {
		if arg == "--user" || strings.HasPrefix(arg, "--map-users") || strings.HasPrefix(arg, "--map-groups") {
			t.Fatalf("portable fallback must not invoke restricted UID/GID mapping helpers: %s", arg)
		}
	}
}

func TestInitPhysicalENOSPCDirectMapGuardRejectsOtherOwners(t *testing.T) {
	for _, mapping := range []string{"0 1002 1", "1 1001 1", "0 1001 2", "0 1001 1\n1 1002 1", ""} {
		if physicalENOSPCDirectMapMatches(mapping, "1001") {
			t.Fatalf("unsafe direct namespace map accepted: %q", mapping)
		}
	}
	if !physicalENOSPCDirectMapMatches("         0       1001          1\n", "1001") {
		t.Fatal("exact original-owner mapping must be accepted")
	}
}

func TestInitPhysicalENOSPCMappingHelperFailureIsHard(t *testing.T) {
	for _, prefix := range []string{"newuidmap:", "newgidmap:"} {
		var stdout, stderr physicalENOSPCOutput
		_, _ = stderr.Write([]byte(prefix + " range not allowed\n"))
		refused, err := physicalENOSPCNativeRefusal(errors.New("mapping helper exit 1"), false, &stdout, &stderr)
		if refused || err != nil {
			t.Fatalf("mapping helper failure must remain a hard failure: refused=%t err=%v", refused, err)
		}
	}
}

func TestInitPhysicalENOSPCPreludeDropsIdentityBeforeGoArgv(t *testing.T) {
	if os.Getuid() == 0 {
		physicalENOSPCCapabilityUnavailable(t, "prelude argv probe executes only as an ordinary owner")
	}
	shell := physicalENOSPCTrustedSystemTool("/usr/bin/sh")
	if shell == "" {
		physicalENOSPCCapabilityUnavailable(t, "trusted shell unavailable for owner-only prelude probe")
	}
	private := t.TempDir()
	mountpoint := filepath.Join(private, "owned $(literal); path", "bounded-filesystem")
	if err := os.MkdirAll(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	// Owner-only stubs observe the fixed prelude protocol. There is no sudo,
	// mount syscall, identity change or execution of the supplied Go argument.
	makeProbe := func(name, script string) string {
		path := filepath.Join(private, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mount := makeProbe("mount-probe", "exit 0\n")
	setpriv := makeProbe("setpriv-probe", "printf '%s\\n' \"$@\"\n")
	readlink := makeProbe("readlink-probe", "case $1 in */user) printf user-ns;; */net) printf child-net;; */mnt) printf child-mount;; *) exit 1;; esac\n")
	env := "/canonical/env"
	childEnv := []string{"HOME=" + private, "TMPDIR=" + private, "PATH=" + private, "TERM=dumb"}
	program := append(append([]string{}, childEnv...), "/owned/go-test", "literal argument $(never-execute); value")
	uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	args := []string{"-eu", "-c", physicalENOSPCPrelude, "owner-only-prelude-probe", mount, setpriv, env, readlink, uid, gid, mountpoint, "parent-net", "parent-mount", "user-ns"}
	args = append(args, program...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = childEnv
	output, err := cmd.CombinedOutput()
	want := append([]string{"--reuid=" + uid, "--regid=" + gid, "--clear-groups", "--", env, "-i"}, program...)
	if err != nil || ctx.Err() != nil || string(output) != strings.Join(want, "\n")+"\n" {
		t.Fatalf("fixed prelude must drop owner identity/groups before env-i and the Go executable: %v output=%q", err, output)
	}
}

func TestInitPhysicalENOSPCPreludeRejectsUnsafeFixturesBeforeMount(t *testing.T) {
	if os.Getuid() == 0 {
		physicalENOSPCCapabilityUnavailable(t, "prelude guard probe executes only as an ordinary owner")
	}
	shell := physicalENOSPCTrustedSystemTool("/usr/bin/sh")
	readlink := physicalENOSPCTrustedSystemTool("/usr/bin/readlink")
	if shell == "" || readlink == "" {
		physicalENOSPCCapabilityUnavailable(t, "trusted shell/readlink unavailable for owner-only prelude probe")
	}
	var namespaces []string
	for _, name := range []string{"user", "net", "mnt"} {
		value, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			t.Fatal(err)
		}
		namespaces = append(namespaces, value)
	}
	for _, failure := range []string{"zero-uid", "invalid-gid", "nonempty", "symlink", "user-namespace", "same-net", "same-mount"} {
		t.Run(failure, func(t *testing.T) {
			private := t.TempDir()
			mountpoint := filepath.Join(private, "bounded-filesystem")
			if err := os.Mkdir(mountpoint, 0700); err != nil {
				t.Fatal(err)
			}
			// This probe runs WITHOUT sudo as the owner. The recording utility
			// is never supplied to a privileged candidate or root shell.
			marker := filepath.Join(private, "mount-reached")
			probe := filepath.Join(private, "owner-only-mount-probe")
			if err := os.WriteFile(probe, []byte("#!/bin/sh\n: > \""+marker+"\"\nexit 99\n"), 0700); err != nil {
				t.Fatal(err)
			}
			uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
			userNS, netNS, mountNS := namespaces[0], "different-net", "different-mount"
			switch failure {
			case "zero-uid":
				uid = "0"
			case "invalid-gid":
				gid = "not-numeric"
			case "nonempty":
				if err := os.WriteFile(filepath.Join(mountpoint, ".owner"), []byte("retained"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(mountpoint); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(private, mountpoint); err != nil {
					t.Fatal(err)
				}
			case "user-namespace":
				userNS = "different-user"
			case "same-net":
				netNS = namespaces[1]
			case "same-mount":
				mountNS = namespaces[2]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell, "-eu", "-c", physicalENOSPCPrelude, "owner-only-prelude-probe", probe, probe, probe, readlink, uid, gid, mountpoint, netNS, mountNS, userNS)
			cmd.Env = []string{"PATH=" + private, "TERM=dumb"}
			if output, err := cmd.CombinedOutput(); err == nil || ctx.Err() != nil || len(output) != 0 {
				t.Fatalf("unsafe prelude input must fail quietly before mount: %v output=%q", err, output)
			}
			if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe fixture reached mount: %v", err)
			}
		})
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

func TestInitPhysicalENOSPCFallbackRejectsTrustedToolOutsideUsrBin(t *testing.T) {
	outside := "/usr/sbin/nologin"
	info, err := os.Stat(outside)
	if err != nil || info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm()&0022 != 0 {
		physicalENOSPCCapabilityUnavailable(t, "root-owned nonwritable /usr/sbin/nologin control unavailable")
	}
	alias := filepath.Join(t.TempDir(), "outside-tool")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, alias} {
		if got := physicalENOSPCTrustedSystemTool(path); got != "" {
			t.Fatalf("privileged tools must resolve strictly inside /usr/bin: %q resolved %q", path, got)
		}
	}
}

func TestInitPhysicalENOSPCPrivilegedArgvSurvivesMutableAliases(t *testing.T) {
	private := t.TempDir()
	var aliases, canonical []string
	for _, name := range []string{"unshare", "sudo", "setpriv", "env", "sh", "mount", "readlink"} {
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
	candidates := physicalENOSPCResolvedCandidates(aliases[0], aliases[1], aliases[2], aliases[3], aliases[4], aliases[5], aliases[6], 1001, 1002,
		[]string{"HOME=/private", "TMPDIR=/private", "PATH=/private", "TERM=dumb"}, []string{"/own/test", "-test.run=helper", "--", "create-write", "/private/bounded-filesystem", "user-ns", "net-ns", "mount-ns", "1001", "1002", "123"})
	if len(candidates) != 2 {
		t.Fatalf("canonical aliases must resolve to a premounted candidate: %#v", candidates)
	}
	for _, alias := range aliases {
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(alias, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if candidates[1][3] != canonical[3] {
		t.Fatal("root stage must begin with canonical env-i, never sudo metadata or a mutable alias")
	}
	// No candidate is executed. Replacing writable aliases after construction
	// must not change any utility selected for the privileged command chain.
	for i, position := range []int{9, 0, 21, 22, 15, 20, 23} {
		if candidates[1][position] != canonical[i] {
			t.Errorf("privileged argv retained mutable %s alias: %q, want canonical %q", filepath.Base(aliases[i]), candidates[1][position], canonical[i])
		}
	}
}
