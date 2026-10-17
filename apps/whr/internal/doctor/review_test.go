// SPDX-License-Identifier: EUPL-1.2

package doctor_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

// The test binary doubles as the git shim: with a lone --version argument and
// the doctor's marker variable it prints the environment it received.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" && os.Getenv("GIT_TERMINAL_PROMPT") == "0" {
		os.Stdout.WriteString(strings.Join(os.Environ(), "\x01") + "\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// F1: static text is never redacted; user names like "doc" must not mangle it.
func TestDoctorRedactionLeavesStaticTextAlone(t *testing.T) {
	for _, name := range []string{"doc", "run", "the", "docker", "git", "var", "sock", "no", "OrbStack"} {
		w := healthy()
		w.user, w.host, w.sockets = name, name, nil
		r := run(w)
		fix := statusOf(r, "container.socket").Remediation
		if !strings.Contains(fix, "docs/container-runtime.md") || !strings.Contains(fix, "Docker-compatible") {
			t.Errorf("user %q mangled the remediation: %q", name, fix)
		}
	}
	rd := doctor.NewRedactor("/home/u", "doc", "h")
	if got := rd.String("/usr/doctor/doc/x"); got != "/usr/doctor/<user>/x" {
		t.Errorf("component boundaries: %q", got)
	}
}

// F13: a home directory that is a prefix of another name is not replaced.
func TestDoctorRedactionHomeBoundary(t *testing.T) {
	rd := doctor.NewRedactor("/home/al", "zz", "yy")
	for in, want := range map[string]string{
		"/home/alice/x": "/home/alice/x", "/home/al/x": "~/x", "/home/al": "~", "at /home/al, ok": "at ~, ok",
	} {
		if got := rd.String(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// F2: every dial and git draw on the shared budget.
func TestDoctorSharedBudgetDialsAndGit(t *testing.T) {
	w := healthy()
	w.sockets = nil
	w.dialAdvance = 4 * time.Second
	r := run(w)
	if len(w.dialed) != 2 {
		t.Fatalf("dialed %v; the third dial must wait for no budget", w.dialed)
	}
	if w.dialDeadlines[0] > 500*time.Millisecond || w.dialDeadlines[1] > 500*time.Millisecond {
		t.Errorf("dial deadlines %v exceed the cap", w.dialDeadlines)
	}
	if g := statusOf(r, "container.socket"); g.Status != doctor.StatusUnknown || g.Reason != "not_measurable" {
		t.Errorf("socket after exhaustion: %+v", g)
	}
	if g := statusOf(r, "git.version"); g.Status != doctor.StatusUnknown || w.gitRuns != 0 {
		t.Errorf("git after exhaustion: %+v runs %d", g, w.gitRuns)
	}
	// remainder smaller than the cap bounds the dial and git
	w = healthy()
	w.dialAdvance = 4600 * time.Millisecond
	w.sockets = map[string]error{"/var/run/docker.sock": nil}
	r = run(w)
	if g := statusOf(r, "container.socket"); g.Status != doctor.StatusPass {
		t.Fatalf("socket: %+v", g)
	}
	if w.gitRuns != 1 || w.gitDeadline <= 0 || w.gitDeadline > 160*time.Millisecond {
		t.Errorf("git deadline %v with 400ms left (runs %d)", w.gitDeadline, w.gitRuns)
	}
	// the second dial sees only the 200 ms that are left
	w = healthy()
	w.sockets = nil
	w.dialAdvance = 4800 * time.Millisecond
	run(w)
	if len(w.dialDeadlines) != 2 || w.dialDeadlines[1] > 210*time.Millisecond {
		t.Errorf("dial deadlines %v, want the second within the 200ms remainder", w.dialDeadlines)
	}
	// git's own cap
	w = healthy()
	run(w)
	if w.gitDeadline > 2*time.Second {
		t.Errorf("git deadline %v exceeds its cap", w.gitDeadline)
	}
	w = healthy()
	w.sockets = nil
	w.dialAdvance = 4900 * time.Millisecond
	run(w)
	if len(w.dialDeadlines) > 0 && w.dialDeadlines[len(w.dialDeadlines)-1] > 500*time.Millisecond {
		t.Errorf("deadline %v", w.dialDeadlines)
	}
}

// F8: a hung filesystem call cannot outlive the budget.
func TestDoctorHungMeasurementIsUnknown(t *testing.T) {
	w := healthy()
	w.budget = 50 * time.Millisecond
	ch := make(chan struct{})
	defer close(ch)
	w.block = map[string]chan struct{}{"/work/project": ch}
	done := make(chan doctor.Report, 1)
	go func() { done <- run(w) }()
	select {
	case r := <-done:
		g := statusOf(r, "disk.working_directory")
		if g.Status != doctor.StatusUnknown || g.Reason != "not_measurable" {
			t.Errorf("hung statfs: %+v", g)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("doctor did not return while a measurement hung")
	}
}

// F3: the environment actually handed to git is exactly GitEnv().
func TestDoctorGitProcessEnvironment(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("CANARY_SECRET", "s3cr3t")
	t.Setenv("HOME", "/home/canary")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	line, err := doctor.SystemPorts(nil).GitVersion(context.Background(), exe)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(line, "\x01")
	want := doctor.GitEnv()
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("git saw environment\n%v\nwant\n%v", got, want)
	}
}

// F5: a developer directory selected with xcode-select -s is honored.
func TestDoctorMacXcodeSelectLink(t *testing.T) {
	w := healthy()
	w.os, w.gitPath = "darwin", "/usr/bin/git"
	w.links = map[string]string{"/var/db/xcode_select_link": "/Applications/Xcode-beta.app/Contents/Developer"}
	w.existing["/Applications/Xcode-beta.app/Contents/Developer/usr/bin/git"] = true
	if g := statusOf(run(w), "git.version"); g.Status != doctor.StatusPass || w.gitRuns != 1 {
		t.Errorf("xcode-select link: %+v", g)
	}
	w = healthy()
	w.os, w.gitPath = "darwin", "/usr/bin/git"
	w.links = map[string]string{"/var/db/xcode_select_link": "/gone/Developer"}
	if g := statusOf(run(w), "git.version"); g.Status != doctor.StatusFail || w.gitRuns != 0 {
		t.Errorf("dangling link: %+v", g)
	}
}

// F6: a candidate path too long for sockaddr_un is absent, not unmeasurable.
func TestDoctorLongSocketPath(t *testing.T) {
	w := healthy()
	w.sockets = map[string]error{}
	long := "/home/alice/.orbstack/run/docker.sock"
	for _, p := range []string{"/var/run/docker.sock", long, "/home/alice/.docker/run/docker.sock", "/home/alice/.colima/default/docker.sock", "/home/alice/.rd/docker.sock"} {
		w.sockets[p] = &os.PathError{Op: "connect", Path: p, Err: syscall.EINVAL}
	}
	g := statusOf(run(w), "container.socket")
	if g.Status != doctor.StatusFail {
		t.Errorf("EINVAL on absent paths: %+v", g)
	}
	// an existing path that cannot be dialed stays unmeasured
	w.existing[long] = true
	if g := statusOf(run(w), "container.socket"); g.Status != doctor.StatusUnknown {
		t.Errorf("EINVAL on an existing path: %+v", g)
	}
}

// F7: evidence names the sources, never environment values.
func TestDoctorSocketEvidenceHasNoEnvironmentValues(t *testing.T) {
	w := healthy()
	w.env["XDG_RUNTIME_DIR"] = "/run/user/777777"
	w.sockets = map[string]error{"/run/user/777777/docker.sock": nil}
	raw, _ := json.Marshal(run(w))
	if strings.Contains(string(raw), "777777") || !strings.Contains(string(raw), "$XDG_RUNTIME_DIR/docker.sock") {
		t.Errorf("XDG value in report: %s", raw)
	}
	w = healthy()
	w.env["DOCKER_HOST"] = "unix:///opt/weird888/d.sock"
	w.sockets = map[string]error{"/opt/weird888/d.sock": nil}
	raw, _ = json.Marshal(run(w))
	if strings.Contains(string(raw), "weird888") || !strings.Contains(string(raw), "$DOCKER_HOST") {
		t.Errorf("DOCKER_HOST value in report: %s", raw)
	}
	w.sockets = nil
	raw, _ = json.Marshal(run(w))
	if strings.Contains(string(raw), "weird888") {
		t.Errorf("DOCKER_HOST value in failure evidence: %s", raw)
	}
}

// F11: verdict table, unexpected statuses, evidence in the human report.
func TestDoctorVerdictTable(t *testing.T) {
	for _, c := range []struct {
		c    doctor.Counts
		want string
	}{
		{doctor.Counts{Pass: 3}, doctor.VerdictOK},
		{doctor.Counts{Pass: 3, Unknown: 1}, doctor.VerdictIncomplete},
		{doctor.Counts{Pass: 3, Skipped: 1}, doctor.VerdictIncomplete},
		{doctor.Counts{Unknown: 1, Skipped: 1, Fail: 1}, doctor.VerdictFailed},
		{doctor.Counts{}, doctor.VerdictOK},
	} {
		if got := doctor.Verdict(c.c); got != c.want {
			t.Errorf("%+v: %s, want %s", c.c, got, c.want)
		}
	}
	got := doctor.Count([]doctor.Check{{Status: "pass"}, {Status: "bogus"}, {Status: ""}, {Status: "fail"}, {Status: "skipped"}})
	if got != (doctor.Counts{Pass: 1, Fail: 1, Skipped: 1, Unknown: 2}) {
		t.Errorf("unexpected statuses must count as unknown: %+v", got)
	}
}

func TestDoctorHumanShowsEvidenceEscaped(t *testing.T) {
	r := doctor.Report{Verdict: "failed", Checks: []doctor.Check{{ID: "disk.working_directory", Status: "fail", Message: "m",
		Evidence: map[string]any{"path": "/w/\x1b[2J‮x", "free_bytes": uint64(5)}}}}
	h := r.Human()
	if !strings.Contains(h, "evidence:") || !strings.Contains(h, "free_bytes") {
		t.Errorf("no evidence: %q", h)
	}
	for _, bad := range []string{"\x1b", "‮"} {
		if strings.Contains(h, bad) {
			t.Errorf("raw %q in %q", bad, h)
		}
	}
	w := healthy()
	if h := run(w).Human(); !strings.Contains(h, "/var/run/docker.sock") || !strings.Contains(h, "2.43.0") {
		t.Errorf("socket path and git version missing from the human report:\n%s", h)
	}
}

// F12: a release candidate of exactly the minimum is below it.
func TestDoctorGitPrerelease(t *testing.T) {
	for out, want := range map[string]string{
		"git version 2.39.5-rc0": doctor.StatusFail, "git version 2.39.5-rc2": doctor.StatusFail,
		"git version 2.39.6-rc0": doctor.StatusPass, "git version 2.40.0-rc1": doctor.StatusPass,
		"git version 2.39.5": doctor.StatusPass,
	} {
		w := healthy()
		w.gitOut = out
		if g := statusOf(run(w), "git.version"); g.Status != want {
			t.Errorf("%s: %+v", out, g)
		}
	}
}
