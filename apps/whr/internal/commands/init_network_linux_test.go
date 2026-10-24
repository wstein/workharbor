//go:build linux

// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

type initNamespaceLog struct {
	sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (b *initNamespaceLog) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	keep := min(len(p), 32*1024-b.data.Len())
	_, _ = b.data.Write(p[:keep])
	b.overflow = b.overflow || keep != len(p)
	return len(p), nil
}

func initNetworkWrite(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

// The child fails closed before any negative-control dial unless the namespace
// identity, interfaces and routes prove isolation from the parent.
func verifyInitNetworkNamespace(t *testing.T) {
	t.Helper()
	parent := os.Getenv("WHR_INIT_PARENT_NETNS")
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil || parent == "" || current == parent {
		t.Fatalf("network namespace is not isolated: parent=%q child=%q error=%v", parent, current, err)
	}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" || interfaces[0].Flags&net.FlagLoopback == 0 || interfaces[0].Flags&net.FlagUp != 0 {
		t.Fatalf("fresh namespace must have only down loopback: %+v, %v", interfaces, err)
	}
	routes, err := os.ReadFile("/proc/net/route")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(routes), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 0 && fields[0] != "Iface" {
			t.Fatalf("fresh namespace has IPv4 route: %s", line)
		}
	}
	routes6, err := os.ReadFile("/proc/net/ipv6_route")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(routes6), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Linux may retain reject-only entries on the down loopback.
		if len(fields) != 10 {
			t.Fatalf("unrecognized IPv6 route: %s", line)
		}
		flags, err := strconv.ParseUint(fields[8], 16, 32)
		if err != nil || fields[9] != "lo" || flags&syscall.RTF_REJECT == 0 {
			t.Fatalf("fresh namespace has routable IPv6 entry: %s", line)
		}
	}
	t.Logf("isolated network namespace: parent=%s child=%s; only down loopback, no routable IPv4/IPv6 routes", parent, current)
}

func initNetworkChild(t *testing.T) {
	t.Helper()
	t.Log("owned init namespace child started")
	verifyInitNetworkNamespace(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// A literal documentation address avoids DNS. This dial is deliberately
	// made only after verifying a separate namespace with no external interface.
	conn, err := (&net.Dialer{Timeout: 250 * time.Millisecond}).DialContext(ctx, "tcp4", "203.0.113.1:443")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || (!errors.Is(err, syscall.ENETUNREACH) && !errors.Is(err, syscall.EHOSTUNREACH)) {
		t.Fatalf("isolated external-connection negative control: %v", err)
	}
	t.Logf("namespace-only external-connection negative control refused: %v", err)
	root := os.Getenv("WHR_INIT_NETWORK_ROOT")
	if root == "" {
		t.Fatal("missing owned fixture root")
	}
	calls := filepath.Join(root, "bin", "calls")
	control := exec.CommandContext(ctx, filepath.Join(root, "bin", "git"), "--version")
	var exit *exec.ExitError
	if err := control.Run(); !errors.As(err, &exit) || exit.ExitCode() != 97 {
		t.Fatalf("Git trace positive control did not exit 97: %v", err)
	}
	startup := exec.CommandContext(ctx, "/bin/sh", "-c", `. "$HOME/.profile"`)
	if err := startup.Run(); err != nil {
		t.Fatalf("owned startup canary positive control: %v", err)
	}
	if raw, err := os.ReadFile(calls); err != nil || string(raw) != "git\nstartup\n" {
		t.Fatalf("positive controls did not record exact calls: %q, %v", raw, err)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	t.Log("Git and owned startup-file positive controls recorded; trace cleared before init")
	private := filepath.Join(root, "private")
	beforePrivate := doctorTreeSnapshot(t, private)
	workspace := filepath.Join(root, "workspace")
	beforeWorkspace := doctorTreeSnapshot(t, workspace)
	schema := compileSchema(t)
	invoke := func(args ...string) map[string]any {
		t.Helper()
		var out, diagnostics initNamespaceLog
		code := commands.Execute(ctx, args, commands.Env{Stdout: &out, Stderr: &diagnostics, Getenv: os.Getenv, IsTerminal: func(io.Writer) bool { return false }}, commands.DefaultHandlers())
		if out.overflow || diagnostics.overflow {
			t.Fatal("isolated command exceeded the 32 KiB stdout/stderr bound")
		}
		if code != 0 {
			t.Fatalf("isolated %v failed: exit=%d stderr=%s", args, code, diagnostics.data.String())
		}
		value, err := jsonUnmarshal(out.data.String())
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("isolated command result schema: %v", err)
		}
		if raw, err := os.ReadFile(calls); !os.IsNotExist(err) {
			t.Fatalf("init/validate invoked an external-program or startup canary: %q, %v", raw, err)
		}
		if after := doctorTreeSnapshot(t, private); !reflect.DeepEqual(beforePrivate, after) {
			t.Fatal("init/validate changed private HOME/XDG/TMP or startup canaries")
		}
		return decodeEnvelope(t, out.data.String())["result"].(map[string]any)
	}
	base := []string{"init", workspace, "--non-interactive", "--yes", "--name=isolated-demo", "--json"}
	preview := invoke(append(append([]string{}, base...), "--dry-run")...)
	if preview["dry_run"] != true || !reflect.DeepEqual(beforeWorkspace, doctorTreeSnapshot(t, workspace)) {
		t.Fatal("isolated dry-run changed workspace")
	}
	written := invoke(base...)
	if written["git_checks"] != "not_run" || written["runtime_enforcement"] != "not_in_this_release" {
		t.Fatal("isolated init claimed Git/runtime enforcement")
	}
	state := doctorTreeSnapshot(t, workspace)
	validated := invoke("validate", workspace, "--json")
	if validated["valid"] != true || !reflect.DeepEqual(state, doctorTreeSnapshot(t, workspace)) {
		t.Fatal("generated workspace failed read-only validation")
	}
	noop := invoke(base...)
	for _, raw := range noop["actions"].([]any) {
		if raw.(map[string]any)["applied"] != false {
			t.Fatal("isolated init rerun applied an action")
		}
	}
	if !reflect.DeepEqual(state, doctorTreeSnapshot(t, workspace)) {
		t.Fatal("isolated no-op changed workspace")
	}
	verifyInitNetworkNamespace(t)
	t.Log("isolated dry-run, init, validate and no-op passed; no canary execution; private HOME/XDG/TMP unchanged")
}

// Mirror workspacecfg's reviewed userns/sudo strategy. The fallback drops UID,
// GID and supplementary groups before env -i restores the exact child environment.
func initNetworkCandidates(look func(string) string, executable string, uid, gid int, childEnv []string) [][]string {
	unshare, sudo, setpriv, env := look("unshare"), look("sudo"), look("setpriv"), look("env")
	var candidates [][]string
	childArgs := []string{executable, "-test.run=^TestInitNoGitNoNetwork$", "-test.v", "-test.timeout=20s"}
	if unshare != "" {
		candidates = append(candidates, append([]string{unshare, "-rn", "--"}, childArgs...))
	}
	if unshare != "" && sudo != "" && setpriv != "" && env != "" {
		argv := []string{sudo, "-n", unshare, "--net", "--", setpriv,
			fmt.Sprintf("--reuid=%d", uid), fmt.Sprintf("--regid=%d", gid), "--clear-groups", env, "-i"}
		argv = append(argv, childEnv...)
		candidates = append(candidates, append(argv, childArgs...))
	}
	return candidates
}

func initNetworkLookPath(name string) string {
	path, err := exec.LookPath(name)
	if err != nil || !filepath.IsAbs(path) {
		return ""
	}
	return path
}

func initNetworkNoProof(t *testing.T, reasons []string) {
	t.Helper()
	message := "network-namespace run did not happen: " + strings.Join(reasons, "; ")
	if os.Getenv("CI") != "" || os.Getenv("WHR_REQUIRE_NETNS") == "1" {
		t.Fatalf("FAILED, NOT SKIPPED: %s", message)
	}
	t.Skip("SKIPPED, NOT PASSED: " + message)
}

func initNetworkNativeUnavailable(text string) bool {
	if strings.Contains(text, "owned init namespace child started") {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "sudo:") || strings.HasPrefix(lower, "setpriv:") || strings.HasPrefix(lower, "env:") {
		return true
	}
	return strings.HasPrefix(lower, "unshare:") && (strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "function not implemented") || strings.Contains(lower, "invalid argument") || strings.Contains(lower, "no space left on device"))
}

var errInitNetworkBound = errors.New("namespace child exceeded time/output bound")

func initNetworkCandidateUnavailable(text string, err error) bool {
	return err != nil && !errors.Is(err, errInitNetworkBound) && initNetworkNativeUnavailable(text)
}

func runInitNetworkCandidate(argv, env []string) (string, error) {
	return runInitNetworkCandidateBudget(argv, env, 25*time.Second)
}

func runInitNetworkCandidateBudget(argv, env []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	cmd.WaitDelay = time.Second
	log := &initNamespaceLog{}
	cmd.Stdout, cmd.Stderr = log, log
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	text := log.data.String()
	if ctx.Err() != nil || log.overflow || errors.Is(err, exec.ErrWaitDelay) {
		return text, fmt.Errorf("%w: context=%v, overflow=%t, process=%v", errInitNetworkBound, ctx.Err(), log.overflow, err)
	}
	return text, err
}

func TestInitNoGitNoNetwork(t *testing.T) {
	if os.Getenv("WHR_INIT_NETWORK_CHILD") == "1" {
		initNetworkChild(t)
		return
	}
	parent, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		initNetworkNoProof(t, []string{"native network namespace identity unavailable: " + err.Error()})
	}
	root := t.TempDir()
	for _, name := range []string{"bin", "workspace/.devcontainer", "private/home", "private/config", "private/state", "private/cache", "private/tmp", "private/config/fish"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	initNetworkWrite(t, filepath.Join(root, "workspace/.devcontainer/devcontainer.json"), `{"image":"debian:bookworm"}`, 0600)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	calls := filepath.Join(root, "bin", "calls")
	for _, name := range []string{"git", "docker", "devcontainer", "ssh", "curl", "wget", "fish", "bash", "sh"} {
		initNetworkWrite(t, filepath.Join(root, "bin", name), "#!/bin/sh\nprintf '%s\\n' "+quote(name)+" >> "+quote(calls)+"\nexit 97\n", 0700)
	}
	for _, name := range []string{".profile", ".bash_profile", ".bashrc", ".zshrc", ".zshenv"} {
		initNetworkWrite(t, filepath.Join(root, "private/home", name), "printf '%s\\n' startup >> "+quote(calls)+"\n", 0600)
	}
	initNetworkWrite(t, filepath.Join(root, "private/config/fish/config.fish"), "printf '%s\\n' startup >> "+quote(calls)+"\n", 0600)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childEnv := []string{"WHR_INIT_NETWORK_CHILD=1", "WHR_INIT_NETWORK_ROOT=" + root, "WHR_INIT_PARENT_NETNS=" + parent, "HOME=" + filepath.Join(root, "private/home"), "XDG_CONFIG_HOME=" + filepath.Join(root, "private/config"), "XDG_STATE_HOME=" + filepath.Join(root, "private/state"), "XDG_CACHE_HOME=" + filepath.Join(root, "private/cache"), "TMPDIR=" + filepath.Join(root, "private/tmp"), "PATH=" + filepath.Join(root, "bin"), "TERM=dumb", "LANG=C"}
	candidates := initNetworkCandidates(initNetworkLookPath, executable, os.Getuid(), os.Getgid(), childEnv)
	var reasons []string
	if len(candidates) == 0 {
		reasons = append(reasons, "unshare unavailable")
	}
	for _, argv := range candidates {
		text, err := runInitNetworkCandidate(argv, childEnv)
		if err != nil {
			if initNetworkCandidateUnavailable(text, err) {
				reasons = append(reasons, filepath.Base(argv[0])+": "+strings.TrimSpace(text))
				continue
			}
			t.Fatalf("isolated test-binary child failed: %v\n%s", err, text)
		}
		if !strings.Contains(text, "isolated dry-run, init, validate and no-op passed") {
			t.Fatalf("namespace child omitted acceptance evidence: %s", text)
		}
		path := "unprivileged user namespace (-rn)"
		if len(argv) > 1 && argv[1] == "-n" {
			path = "sudo network namespace with setpriv and closed env"
		}
		t.Logf("namespace path used: %s", path)
		t.Log(strings.TrimSpace(text))
		return
	}
	initNetworkNoProof(t, reasons)
}

func TestInitNetworkNamespaceRestrictionFailsRequired(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	initNetworkWrite(t, filepath.Join(bin, "unshare"), "#!/bin/sh\nprintf '%s\\n' 'unshare: write failed /proc/self/uid_map: Operation not permitted' >&2\nexit 1\n", 0700)
	for _, required := range []string{"CI=true", "WHR_REQUIRE_NETNS=1"} {
		t.Run(required, func(t *testing.T) {
			env := []string{required, "PATH=" + bin, "HOME=" + t.TempDir(), "TMPDIR=" + t.TempDir()}
			text, err := runInitNetworkCandidate([]string{executable, "-test.run=^TestInitNoGitNoNetwork$", "-test.v"}, env)
			if err == nil || !strings.Contains(text, "FAILED, NOT SKIPPED") || strings.Contains(text, "--- SKIP:") {
				t.Fatalf("required namespace proof accepted restricted unshare: error=%v; %s", err, text)
			}
		})
	}
}

func TestInitNetworkNamespaceFallbackContract(t *testing.T) {
	look := func(name string) string { return "/abs/" + name }
	closed := []string{"WHR_INIT_NETWORK_CHILD=1", "HOME=/private/home", "PATH=/private/bin", "TMPDIR=/private/tmp"}
	before := append([]string(nil), closed...)
	got := initNetworkCandidates(look, "/owned/test-binary", 1001, 1002, closed)
	childArgs := []string{"/owned/test-binary", "-test.run=^TestInitNoGitNoNetwork$", "-test.v", "-test.timeout=20s"}
	wantDirect := append([]string{"/abs/unshare", "-rn", "--"}, childArgs...)
	wantFallback := []string{"/abs/sudo", "-n", "/abs/unshare", "--net", "--", "/abs/setpriv", "--reuid=1001", "--regid=1002", "--clear-groups", "/abs/env", "-i"}
	wantFallback = append(wantFallback, closed...)
	wantFallback = append(wantFallback, childArgs...)
	if len(got) != 2 || !reflect.DeepEqual(got[0], wantDirect) || !reflect.DeepEqual(got[1], wantFallback) || !reflect.DeepEqual(closed, before) {
		t.Fatalf("namespace fallback must drop privileges and clear inherited env before exact child execution: got=%v", got)
	}
	for _, absent := range []string{"unshare", "sudo", "setpriv", "env"} {
		t.Run("missing-"+absent, func(t *testing.T) {
			missing := func(name string) string {
				if name == absent {
					return ""
				}
				return look(name)
			}
			got := initNetworkCandidates(missing, "/owned/test-binary", 1001, 1002, closed)
			want := 1
			if absent == "unshare" {
				want = 0
			}
			if len(got) != want {
				t.Fatalf("unavailable tool candidate count=%d, want %d: %v", len(got), want, got)
			}
		})
	}
}

func TestInitNetworkCandidateBoundsDoNotFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		budget time.Duration
	}{
		{"overflow", "printf '%s\\n' 'unshare: Operation not permitted'; printf '%040960d' 0; exit 1", 3 * time.Second},
		{"timeout", "printf '%s\\n' 'unshare: Operation not permitted'; exec /bin/sleep 60", 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := runInitNetworkCandidateBudget([]string{"/bin/sh", "-c", tc.script}, []string{"PATH=/nonexistent", "LANG=C"}, tc.budget)
			if !errors.Is(err, errInitNetworkBound) || !initNetworkNativeUnavailable(text) {
				t.Fatalf("fixture did not produce native-looking bound failure: error=%v, prefix=%q", err, text[:min(len(text), 80)])
			}
			if initNetworkCandidateUnavailable(text, err) {
				t.Fatalf("%s bound failure was treated as native unavailability: %v", tc.name, err)
			}
		})
	}
}
