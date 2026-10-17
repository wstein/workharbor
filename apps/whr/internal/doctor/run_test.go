// SPDX-License-Identifier: EUPL-1.2

package doctor_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

func run(w *world) doctor.Report { return doctor.Run(context.Background(), w.ports()) }

func TestDoctorSocketCandidates(t *testing.T) {
	cands := []string{
		"/var/run/docker.sock", "/run/user/1000/docker.sock", "/home/alice/.orbstack/run/docker.sock",
		"/home/alice/.docker/run/docker.sock", "/home/alice/.colima/default/docker.sock", "/home/alice/.rd/docker.sock",
	}
	for i, c := range cands {
		w := healthy()
		w.env["XDG_RUNTIME_DIR"] = "/run/user/1000"
		w.sockets = map[string]error{c: nil}
		got := statusOf(run(w), "container.socket")
		if got.Status != doctor.StatusPass {
			t.Errorf("candidate %d %s: %+v", i, c, got)
		}
		if len(w.dialed) != i+1 || w.dialed[i] != c {
			t.Errorf("candidate %d dialed %v: later candidates must not be tried after a hit", i, w.dialed)
		}
	}
	// DOCKER_HOST unix is the only candidate.
	w := healthy()
	w.env["DOCKER_HOST"] = "unix:///custom/d.sock"
	w.sockets = map[string]error{"/var/run/docker.sock": nil}
	got := statusOf(run(w), "container.socket")
	if got.Status != doctor.StatusFail || len(w.dialed) != 1 || w.dialed[0] != "/custom/d.sock" {
		t.Errorf("DOCKER_HOST unix: %+v dialed %v", got, w.dialed)
	}
	w = healthy()
	w.env["DOCKER_HOST"] = "unix:///custom/d.sock"
	w.sockets = map[string]error{"/custom/d.sock": nil}
	if got := statusOf(run(w), "container.socket"); got.Status != doctor.StatusPass {
		t.Errorf("DOCKER_HOST unix hit: %+v", got)
	}
	// tcp and other schemes are unknown and never dialed.
	for _, h := range []string{"tcp://10.0.0.1:2375", "ssh://u@h", "unix://relative.sock", "unix://"} {
		w := healthy()
		w.env["DOCKER_HOST"] = h
		got := statusOf(run(w), "container.socket")
		if got.Status != doctor.StatusUnknown || len(w.dialed) != 0 {
			t.Errorf("%s: %+v dialed %v", h, got, w.dialed)
		}
		if strings.HasPrefix(h, "tcp") && got.Reason != "remote_not_checked" {
			t.Errorf("tcp reason %q", got.Reason)
		}
	}
	// denied and refused fail with the remediation page; none found fails too.
	for name, e := range map[string]error{"denied": errDenied, "refused": errRefused} {
		w := healthy()
		w.sockets = map[string]error{"/var/run/docker.sock": e}
		got := statusOf(run(w), "container.socket")
		if got.Status != doctor.StatusFail || !strings.Contains(got.Remediation, "docs/container-runtime.md") {
			t.Errorf("%s: %+v", name, got)
		}
		raw, _ := json.Marshal(got.Evidence)
		if !strings.Contains(string(raw), name) {
			t.Errorf("%s not in evidence %s", name, raw)
		}
	}
	w = healthy()
	w.sockets = nil
	got = statusOf(run(w), "container.socket")
	if got.Status != doctor.StatusFail || !strings.Contains(got.Remediation, "OrbStack") {
		t.Errorf("none: %+v", got)
	}
	// A timeout or odd error with no hit cannot prove absence.
	w = healthy()
	w.sockets = map[string]error{"/var/run/docker.sock": context.DeadlineExceeded}
	if got := statusOf(run(w), "container.socket"); got.Status != doctor.StatusUnknown {
		t.Errorf("timeout: %+v", got)
	}
	// Without XDG_RUNTIME_DIR and home those candidates are skipped, not guessed.
	w = healthy()
	w.home = ""
	w.sockets = nil
	run(w)
	if len(w.dialed) != 1 {
		t.Errorf("dialed %v with no home and no XDG_RUNTIME_DIR", w.dialed)
	}
}

func TestDoctorGitVersion(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*world)
		want   string
	}{
		{"absent", func(w *world) { w.gitErr = errors.New("not found") }, doctor.StatusFail},
		{"old", func(w *world) { w.gitOut = "git version 2.39.4" }, doctor.StatusFail},
		{"older major", func(w *world) { w.gitOut = "git version 1.9.9" }, doctor.StatusFail},
		{"equal", func(w *world) { w.gitOut = "git version 2.39.5" }, doctor.StatusPass},
		{"new", func(w *world) { w.gitOut = "git version 2.51.0" }, doctor.StatusPass},
		{"new major", func(w *world) { w.gitOut = "git version 3.0.0" }, doctor.StatusPass},
		{"two components old", func(w *world) { w.gitOut = "git version 2.39" }, doctor.StatusFail},
		{"vendor suffix", func(w *world) { w.gitOut = "git version 2.39.5 (Apple Git-154)\n" }, doctor.StatusPass},
		{"windows suffix", func(w *world) { w.gitOut = "git version 2.45.1.windows.1" }, doctor.StatusPass},
		{"run error", func(w *world) { w.gitRunErr = errors.New("killed") }, doctor.StatusUnknown},
		{"garbage", func(w *world) { w.gitOut = "" }, doctor.StatusUnknown},
	}
	for _, c := range cases {
		w := healthy()
		c.mutate(w)
		got := statusOf(run(w), "git.version")
		if got.Status != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
		if c.want == doctor.StatusFail && got.Remediation == "" {
			t.Errorf("%s: fail without remediation", c.name)
		}
	}
}

func TestDoctorMacGitShim(t *testing.T) {
	mac := func() *world {
		w := healthy()
		w.os, w.arch, w.gitPath = "darwin", "arm64", "/usr/bin/git"
		return w
	}
	w := mac()
	got := statusOf(run(w), "git.version")
	if got.Status != doctor.StatusFail || w.gitRuns != 0 {
		t.Fatalf("shim: %+v, runs %d (it must be detected without executing it)", got, w.gitRuns)
	}
	if !strings.Contains(got.Remediation, "installer") {
		t.Errorf("remediation %q", got.Remediation)
	}
	for _, dev := range []string{"/Library/Developer/CommandLineTools/usr/bin/git", "/Applications/Xcode.app/Contents/Developer/usr/bin/git", "/dev/xcode/usr/bin/git"} {
		w := mac()
		w.existing[dev] = true
		w.env["DEVELOPER_DIR"] = "/dev/xcode"
		if got := statusOf(run(w), "git.version"); got.Status != doctor.StatusPass || w.gitRuns != 1 {
			t.Errorf("developer dir %s: %+v", dev, got)
		}
	}
	// Another git path on macOS (Homebrew) is not the shim.
	w = mac()
	w.gitPath = "/opt/homebrew/bin/git"
	if got := statusOf(run(w), "git.version"); got.Status != doctor.StatusPass {
		t.Errorf("homebrew: %+v", got)
	}
	// The shim rule is macOS only.
	w = healthy()
	w.gitPath = "/usr/bin/git"
	if got := statusOf(run(w), "git.version"); got.Status != doctor.StatusPass {
		t.Errorf("linux /usr/bin/git: %+v", got)
	}
}

func TestDoctorDiskThresholds(t *testing.T) {
	const th = uint64(2147483648)
	for _, c := range []struct {
		free uint64
		want string
	}{{0, doctor.StatusFail}, {th - 1, doctor.StatusFail}, {th, doctor.StatusPass}, {th + 1, doctor.StatusPass}} {
		w := healthy()
		w.free["/work/project"], w.free["/var/lib/docker"] = c.free, c.free
		r := run(w)
		for _, id := range []string{"disk.working_directory", "disk.container_storage"} {
			got := statusOf(r, id)
			if got.Status != c.want {
				t.Errorf("%s free %d: %+v", id, c.free, got)
			}
			if c.want == doctor.StatusFail && got.Remediation == "" {
				t.Errorf("%s: no remediation", id)
			}
			if got.Evidence["free_bytes"] != c.free || got.Evidence["threshold_bytes"] != th {
				t.Errorf("%s evidence %v", id, got.Evidence)
			}
		}
	}
	w := healthy()
	w.freeErr["/work/project"] = errors.New("x")
	if got := statusOf(run(w), "disk.working_directory"); got.Status != doctor.StatusUnknown || got.Reason != "not_measurable" {
		t.Errorf("unmeasurable: %+v", got)
	}
	// unreadable /var/lib/docker is unknown, not a guess.
	w = healthy()
	w.readable["/var/lib/docker"] = syscall.EACCES
	got := statusOf(run(w), "disk.container_storage")
	if got.Status != doctor.StatusUnknown || len(w.freed) != 1 {
		t.Errorf("unreadable: %+v freed %v", got, w.freed)
	}
	// macOS has no host-visible container storage.
	w = healthy()
	w.os = "darwin"
	if got := statusOf(run(w), "disk.container_storage"); got.Status != doctor.StatusUnknown {
		t.Errorf("darwin: %+v", got)
	}
}

func TestDoctorCustomDataRoot(t *testing.T) {
	const dj = "/etc/docker/daemon.json"
	for name, body := range map[string]string{
		"data-root":  `{"data-root":"/mnt/docker"}`,
		"graph":      `{"graph":"/mnt/docker"}`,
		"invalid":    `{"data-root":`,
		"not object": `[]`,
	} {
		w := healthy()
		w.files[dj] = []byte(body)
		got := statusOf(run(w), "disk.container_storage")
		if got.Status != doctor.StatusUnknown || got.Reason != "not_measurable" {
			t.Errorf("%s: %+v", name, got)
		}
		for _, f := range w.freed {
			if f == "/var/lib/docker" {
				t.Errorf("%s: measured /var/lib/docker despite a non-default data root", name)
			}
		}
	}
	// A daemon.json without the key leaves the default root.
	w := healthy()
	w.files[dj] = []byte(`{"log-driver":"json-file"}`)
	if got := statusOf(run(w), "disk.container_storage"); got.Status != doctor.StatusPass {
		t.Errorf("default root: %+v", got)
	}
	// An unreadable daemon.json cannot show the root is the default.
	w = healthy()
	w.readErr[dj] = syscall.EACCES
	if got := statusOf(run(w), "disk.container_storage"); got.Status != doctor.StatusUnknown {
		t.Errorf("unreadable daemon.json: %+v", got)
	}
	// A remote DOCKER_HOST means the data root is elsewhere.
	w = healthy()
	w.env["DOCKER_HOST"] = "tcp://10.0.0.1:2375"
	if got := statusOf(run(w), "disk.container_storage"); got.Status != doctor.StatusUnknown {
		t.Errorf("tcp: %+v", got)
	}
	// A unix DOCKER_HOST still allows the default-root measurement.
	w = healthy()
	w.env["DOCKER_HOST"] = "unix:///var/run/docker.sock"
	if got := statusOf(run(w), "disk.container_storage"); got.Status != doctor.StatusPass {
		t.Errorf("unix: %+v", got)
	}
}

func TestDoctorPlatformUnknown(t *testing.T) {
	for _, p := range [][2]string{{"darwin", "arm64"}, {"windows", "amd64"}, {"freebsd", "amd64"}, {"linux", "riscv64"}, {"linux", "386"}} {
		w := healthy()
		w.os, w.arch = p[0], p[1]
		w.gitPath = "/opt/tools/git"
		r := run(w)
		got := statusOf(r, "platform.host")
		if got.Status != doctor.StatusUnknown || got.Reason != "unsupported_platform" || got.Remediation == "" {
			t.Errorf("%v: %+v", p, got)
		}
		if r.Platform.OS != p[0] || r.Platform.Arch != p[1] {
			t.Errorf("platform %+v", r.Platform)
		}
		if statusOf(r, "git.version").Status == "" || len(r.Checks) != 10 {
			t.Errorf("%v: remaining checks must still run", p)
		}
		if r.Verdict == doctor.VerdictFailed {
			t.Errorf("%v: an unlisted platform is not a failure", p)
		}
	}
	for _, a := range []string{"amd64", "arm64"} {
		w := healthy()
		w.arch = a
		if got := statusOf(run(w), "platform.host"); got.Status != doctor.StatusPass {
			t.Errorf("linux/%s: %+v", a, got)
		}
	}
}

func TestDoctorConfigValidUsesValidatorPort(t *testing.T) {
	file := "/work/project/workharbor.json"
	w := healthy()
	// absent file: skipped, validator untouched
	called := 0
	w.validator = func(context.Context, string) (doctor.Outcome, error) { called++; return doctor.Outcome{}, nil }
	got := statusOf(run(w), "config.valid")
	if got.Status != doctor.StatusSkipped || got.Reason != "precondition_absent" || called != 0 {
		t.Errorf("absent: %+v called %d", got, called)
	}
	// present, validator absent: not in this increment
	w = healthy()
	w.existing[file] = true
	got = statusOf(run(w), "config.valid")
	if got.Status != doctor.StatusSkipped || got.Reason != "not_in_increment" || got.Remediation == "" {
		t.Errorf("no validator: %+v", got)
	}
	// present, validator ports
	for name, c := range map[string]struct {
		out  doctor.Outcome
		err  error
		want string
	}{
		"valid":    {doctor.Outcome{Warnings: 2}, nil, doctor.StatusPass},
		"errors":   {doctor.Outcome{Errors: 1}, nil, doctor.StatusFail},
		"failed":   {doctor.Outcome{}, errors.New("x"), doctor.StatusUnknown},
		"deadline": {doctor.Outcome{}, context.DeadlineExceeded, doctor.StatusUnknown},
	} {
		w := healthy()
		w.existing[file] = true
		var dir string
		w.validator = func(_ context.Context, d string) (doctor.Outcome, error) { dir = d; return c.out, c.err }
		got := statusOf(run(w), "config.valid")
		if got.Status != c.want || dir != "/work/project" {
			t.Errorf("%s: %+v dir %q", name, got, dir)
		}
		if c.want == doctor.StatusFail && !strings.Contains(got.Remediation, "whr validate") {
			t.Errorf("%s: remediation %q", name, got.Remediation)
		}
	}
}

// TestDoctorTimeBudget: one 5 s budget per run on an injected clock; the
// validator gets the remainder and exhaustion is unknown.
func TestDoctorTimeBudget(t *testing.T) {
	file := "/work/project/workharbor.json"
	var deadline time.Duration
	var called int
	mk := func(advance time.Duration) *world {
		w := healthy()
		w.existing[file] = true
		w.gitAdvance = advance
		w.validator = func(ctx context.Context, _ string) (doctor.Outcome, error) {
			called++
			if d, ok := ctx.Deadline(); ok {
				deadline = time.Until(d)
			}
			return doctor.Outcome{}, nil
		}
		return w
	}
	w := mk(3 * time.Second)
	got := statusOf(run(w), "config.valid")
	if got.Status != doctor.StatusPass || called != 1 {
		t.Fatalf("remainder: %+v called %d", got, called)
	}
	if deadline <= 0 || deadline > 2*time.Second+100*time.Millisecond || deadline < time.Second {
		t.Errorf("validator deadline %v, want about 2s (the remainder)", deadline)
	}
	called = 0
	w = mk(5 * time.Second)
	r := run(w)
	got = statusOf(r, "config.valid")
	if got.Status != doctor.StatusUnknown || got.Reason != "not_measurable" || called != 0 {
		t.Errorf("exhausted: %+v called %d", got, called)
	}
	// later measurements also draw on the budget
	for _, id := range []string{"disk.working_directory", "disk.container_storage"} {
		if g := statusOf(r, id); g.Status != doctor.StatusUnknown || g.Reason != "not_measurable" {
			t.Errorf("%s after exhaustion: %+v", id, g)
		}
	}
	if len(w.freed) != 0 {
		t.Errorf("disk measured after the budget was spent: %v", w.freed)
	}
	// without the real validator, exhaustion still reports skipped
	w = healthy()
	w.existing[file] = true
	w.gitAdvance = 6 * time.Second
	if g := statusOf(run(w), "config.valid"); g.Status != doctor.StatusSkipped || g.Reason != "not_in_increment" {
		t.Errorf("no validator: %+v", g)
	}
	if doctor.Budget != 5*time.Second {
		t.Errorf("budget %v", doctor.Budget)
	}
}

func TestDoctorRedaction(t *testing.T) {
	w := healthy()
	w.home, w.user, w.host = "/Users/canaryuser", "canaryuser", "canaryhost"
	w.cwd = "/Users/canaryuser/canaryhost-work"
	w.free = map[string]uint64{w.cwd: 1 * gib, "/var/lib/docker": gib}
	w.env["DOCKER_HOST"] = "unix:///Users/canaryuser/.hostcanaryhost/d.sock"
	w.env["SECRET_TOKEN"] = "s3cr3t-canary-value"
	w.env["XDG_RUNTIME_DIR"] = "/run/user/canaryuser"
	w.sockets = nil
	w.existing[w.cwd+"/workharbor.json"] = true
	r := run(w)
	raw, _ := json.Marshal(r)
	for _, bad := range []string{"canaryuser", "canaryhost", "s3cr3t", "/Users/"} {
		if strings.Contains(string(raw), bad) || strings.Contains(r.Human(), bad) {
			t.Errorf("%q leaked:\n%s", bad, raw)
		}
	}
	if !strings.Contains(string(raw), "~") {
		t.Errorf("home not shown as ~: %s", raw)
	}
	w = healthy()
	w.env["DOCKER_HOST"] = "tcp://admin:pa55w0rd@10.1.2.3:2375"
	raw, _ = json.Marshal(run(w))
	if strings.Contains(string(raw), "pa55w0rd") || strings.Contains(string(raw), "10.1.2.3") {
		t.Errorf("tcp DOCKER_HOST leaked: %s", raw)
	}
	// Short names are replaced as whole path segments only.
	rd := doctor.NewRedactor("/home/ab", "ab", "x")
	if got := rd.String("/home/ab/lab/x/abx"); got != "~/lab/<host>/abx" {
		t.Errorf("short names: %q", got)
	}
	if got := doctor.NewRedactor("/home/alice", "alice", "devbox").String("/srv/alice-data/devbox.sock"); got != "/srv/<user>-data/<host>.sock" {
		t.Errorf("long names: %q", got)
	}
}

func TestDoctorReadOnlyNoNetwork(t *testing.T) {
	w := healthy()
	w.existing["/work/project/workharbor.json"] = true
	w.validator = func(context.Context, string) (doctor.Outcome, error) { return doctor.Outcome{}, nil }
	run(w)
	for _, p := range w.dialed {
		if !strings.HasPrefix(p, "/") {
			t.Errorf("non-path dial %q", p)
		}
	}
	// The real dialer only knows unix sockets and never creates one.
	err := doctor.SystemPorts(nil).DialUnix(context.Background(), "/nonexistent-whr-doctor-dir/x.sock")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("real dial of an absent socket: %v", err)
	}
	// Source scan: no writes, no network packages, exec only for git.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbiddenImports := map[string]bool{"net/http": true, "net/rpc": true, "net/smtp": true, "net/url": true, "plugin": true}
	forbiddenCalls := map[string]bool{
		"os.Create": true, "os.WriteFile": true, "os.OpenFile": true, "os.Remove": true, "os.RemoveAll": true,
		"os.Mkdir": true, "os.MkdirAll": true, "os.MkdirTemp": true, "os.CreateTemp": true, "os.Rename": true, "os.Chmod": true,
		"os.Chown": true, "os.Truncate": true, "os.Symlink": true, "os.Link": true, "os.Setenv": true, "os.Chdir": true,
		"net.Listen": true, "net.ListenPacket": true, "net.LookupHost": true, "net.LookupIP": true,
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if forbiddenImports[p] {
				t.Errorf("%s imports %s", n, p)
			}
			if p == "os/exec" && n != "git.go" {
				t.Errorf("%s imports os/exec: only git.go may run a process", n)
			}
		}
		ast.Inspect(f, func(x ast.Node) bool {
			if c, ok := x.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok {
					if id, ok := s.X.(*ast.Ident); ok && forbiddenCalls[id.Name+"."+s.Sel.Name] {
						t.Errorf("%s calls %s.%s", n, id.Name, s.Sel.Name)
					}
				}
			}
			return true
		})
	}
	// The git invocation is fixed and isolated.
	args := doctor.GitArgs()
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("git args %v", args)
	}
	env := strings.Join(doctor.GitEnv(), "\n")
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"} {
		if !strings.Contains(env, want) {
			t.Errorf("git env lacks %s: %s", want, env)
		}
	}
	if strings.Contains(env, "HOME=") && !strings.Contains(env, "HOME="+os.DevNull) {
		t.Errorf("git env inherits HOME: %s", env)
	}
	// Real runner: bounded and isolated; the output starts with the fixed words.
	gp, err := doctor.SystemPorts(nil).FindGit()
	if err != nil {
		t.Fatalf("git is required on the test machine: %v", err)
	}
	out, err := doctor.SystemPorts(nil).GitVersion(context.Background(), gp)
	if err != nil || !strings.HasPrefix(out, "git version ") {
		t.Errorf("real git --version: %q %v", out, err)
	}
	_ = filepath.Separator
}

func TestDoctorVerdictAndCounts(t *testing.T) {
	r := run(healthy())
	if r.Verdict != doctor.VerdictIncomplete || r.Counts.Skipped != 5 || r.Counts.Pass != 5 || r.Counts.Fail != 0 || r.Counts.Unknown != 0 {
		t.Errorf("healthy first increment: %s %+v", r.Verdict, r.Counts)
	}
	w := healthy()
	w.sockets = nil
	r = run(w)
	if r.Verdict != doctor.VerdictFailed || r.Counts.Fail != 1 {
		t.Errorf("fail: %s %+v", r.Verdict, r.Counts)
	}
	if doctor.Verdict(doctor.Counts{Pass: 10}) != doctor.VerdictOK {
		t.Error("all pass must be ok")
	}
}
