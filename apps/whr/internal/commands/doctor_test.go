// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

// fakePorts is a machine with a Docker socket, a good Git and plenty of disk;
// nothing here touches the real host.
func fakePorts() doctor.Ports {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	return doctor.Ports{
		OS: "linux", Arch: "amd64",
		Getenv: func(string) string { return "" },
		Home:   func() (string, error) { return "/home/alice", nil },
		User:   func() string { return "alice" },
		Host:   func() string { return "devbox" },
		Getwd:  func() (string, error) { return "/work", nil },
		Now:    func() time.Time { return now },
		Lstat: func(p string) (fs.FileInfo, error) {
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: syscall.ENOENT}
		},
		DialUnix: func(_ context.Context, p string) error {
			if p == "/var/run/docker.sock" {
				return nil
			}
			return &fs.PathError{Op: "connect", Path: p, Err: syscall.ENOENT}
		},
		ReadFile: func(p string, _ int64) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
		},
		CanRead:    func(string) error { return nil },
		FindGit:    func() (string, error) { return "/usr/bin/git", nil },
		GitVersion: func(context.Context, string) (string, error) { return "git version 2.43.0", nil },
		FreeBytes:  func(string) (uint64, error) { return 10 << 30, nil },
	}
}

// brokenSocket is fakePorts without any socket.
func brokenSocket() doctor.Ports {
	p := fakePorts()
	p.DialUnix = func(_ context.Context, path string) error {
		return &fs.PathError{Op: "connect", Path: path, Err: syscall.ENOENT}
	}
	return p
}

func baseHandlers() map[string]commands.Handler {
	h := commands.DefaultHandlers()
	h["host.doctor"] = commands.DoctorHandler(fakePorts())
	return h
}

func withDoctor(p doctor.Ports) map[string]commands.Handler {
	h := baseHandlers()
	h["host.doctor"] = commands.DoctorHandler(p)
	return h
}

func TestDoctorExitCodes(t *testing.T) {
	// incomplete (skipped checks remain) exits 0 and writes the report to stdout only.
	for _, sp := range [][]string{{"doctor"}, {"host", "doctor"}} {
		got := run(t, withDoctor(fakePorts()), sp...)
		if got.code != 0 || got.err != "" || !strings.Contains(got.stdout, "verdict: incomplete") {
			t.Errorf("%v: %+v", sp, got)
		}
	}
	// a failed check exits 68, keeps the report on stdout and announces on stderr.
	got := run(t, withDoctor(brokenSocket()), "doctor")
	if got.code != 68 || !strings.Contains(got.stdout, "verdict: failed") || !strings.Contains(got.stdout, "container.socket") ||
		!strings.HasPrefix(got.err, "whr: ") || !strings.Contains(got.err, "container.socket") {
		t.Errorf("failed: %+v", got)
	}
	// JSON: one envelope, the report as the result of a conflict failure.
	got = run(t, withDoctor(brokenSocket()), "doctor", "--json")
	env := decodeEnvelope(t, got.stdout)
	res, _ := env["result"].(map[string]any)
	errObj, _ := env["error"].(map[string]any)
	if got.code != 68 || env["ok"] != false || env["command"] != "host.doctor" || res["verdict"] != "failed" || errObj["code"] != "conflict" {
		t.Errorf("failed json: %+v", got)
	}
	got = run(t, withDoctor(fakePorts()), "doctor", "--json")
	env = decodeEnvelope(t, got.stdout)
	if got.code != 0 || env["ok"] != true || env["result"].(map[string]any)["verdict"] != "incomplete" {
		t.Errorf("incomplete json: %+v", got)
	}
	// doctor takes no arguments
	if got := run(t, withDoctor(fakePorts()), "doctor", "extra"); got.code != 64 || got.stdout != "" {
		t.Errorf("arguments: %+v", got)
	}
}

func TestDoctorJSONMatchesSchema(t *testing.T) {
	schema := compileSchema(t)
	for name, p := range map[string]doctor.Ports{"incomplete": fakePorts(), "failed": brokenSocket()} {
		got := run(t, withDoctor(p), "doctor", "--json")
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(doc); err != nil {
			t.Errorf("%s: %v\n%s", name, err, got.stdout)
		}
	}
}

// TestMutatingHostCommandsRefuseUnsupportedPlatform is table-level: the
// mutating host commands are unavailable until T015, so they refuse before any I/O
// with the usage code 64. The platform refusal of FR-026 (68 on an unsupported
// platform) cannot be exercised before those commands exist; T015 owns that test.
func TestMutatingHostCommandsRefuseUnsupportedPlatform(t *testing.T) {
	n := 0
	for _, e := range commands.Table() {
		if e.Writes != "host" {
			continue
		}
		n++
		got := run(t, withDoctor(fakePorts()), e.Path...)
		if got.code != 64 || got.stdout != "" || !strings.Contains(got.err, e.ID+" is not available") {
			t.Errorf("%s: %+v", e.ID, got)
		}
	}
	if n == 0 {
		t.Fatal("no mutating host command in the table")
	}
}

// N1: an interrupt or deadline during the run is exit 72 or 71, never a verdict.
func TestDoctorExitCodesOnCancelAndDeadline(t *testing.T) {
	var out, errOut strings.Builder
	env := commands.Env{Stdout: &out, Stderr: &errOut}
	for name, c := range map[string]struct {
		mk   func() (context.Context, context.CancelFunc)
		want int
	}{
		"cancelled": {func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, 72},
		"deadline": {func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}, 71},
	} {
		for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}} {
			out.Reset()
			errOut.Reset()
			ctx, cancel := c.mk()
			// a healthy machine would have exit 0; a broken one 68
			for _, p := range []doctor.Ports{fakePorts(), brokenSocket()} {
				out.Reset()
				if code := commands.Execute(ctx, args, env, withDoctor(p)); code != c.want {
					t.Errorf("%s %v: exit %d, want %d (stdout %q)", name, args, code, c.want, out.String())
				}
				if strings.Contains(out.String(), `"ok":true`) || strings.Contains(out.String(), "verdict:") {
					t.Errorf("%s %v: reported a verdict: %q", name, args, out.String())
				}
			}
			cancel()
		}
	}
}

// The command adapter must use the same checker as validate, including warnings
// and errors beyond manifest syntax; doctor only publishes aggregate counts.
func TestDoctorConfigValidEqualsValidate(t *testing.T) {
	for _, tc := range []struct{ name, manifest, devcontainer string }{
		{"valid", goodManifest, goodDevc},
		{"warning", goodManifest, `{"image":"ubuntu:22.04"}`},
		{"invalid manifest", `{"schema_version":1.0}`, goodDevc},
		{"invalid devcontainer", goodManifest, `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := goodDir(t)
			put(t, dir, "workharbor.json", tc.manifest)
			put(t, dir, ".devcontainer/devcontainer.json", tc.devcontainer)
			validate := run(t, nil, "validate", dir, "--json")
			expected := decodeEnvelope(t, validate.stdout)["result"].(map[string]any)
			counts := expected["counts"].(map[string]any)
			p := doctorWorkspacePorts(dir)
			got := run(t, withDoctor(p), "doctor", "--json")
			check := doctorConfigCheck(t, got.stdout)
			want := doctor.StatusPass
			if expected["valid"] == false {
				want = doctor.StatusFail
			}
			if check["status"] != want {
				t.Fatalf("config.valid = %v, validate valid = %v; %s", check, expected["valid"], got.stdout)
			}
			evidence := check["evidence"].(map[string]any)
			if evidence["error_findings"] != counts["error"] || evidence["warning_findings"] != counts["warning"] {
				t.Fatalf("doctor evidence %v != validate counts %v", evidence, counts)
			}
			if got.code != validate.code {
				t.Fatalf("doctor exit %d != validate exit %d", got.code, validate.code)
			}
		})
	}
}

func doctorWorkspacePorts(dir string) doctor.Ports {
	p := fakePorts()
	p.Getwd = func() (string, error) { return dir, nil }
	p.Lstat = os.Lstat
	return p
}

func doctorConfigCheck(t *testing.T, output string) map[string]any {
	t.Helper()
	report := decodeEnvelope(t, output)["result"].(map[string]any)
	for _, value := range report["checks"].([]any) {
		check := value.(map[string]any)
		if check["id"] == "config.valid" {
			return check
		}
	}
	t.Fatal("config.valid absent")
	return nil
}

func TestDoctorValidatorBudgetAndOverride(t *testing.T) {
	for _, elapsed := range []time.Duration{2 * time.Second, doctor.Budget} {
		t.Run(elapsed.String(), func(t *testing.T) {
			p := doctorWorkspacePorts(goodDir(t))
			start := time.Now()
			now := start
			p.Now = func() time.Time { return now }
			p.GitVersion = func(context.Context, string) (string, error) {
				now = start.Add(elapsed)
				return "git version 2.43.0", nil
			}
			calls := 0
			p.Validator = func(ctx context.Context, dir string) (doctor.Outcome, error) {
				calls++
				deadline, ok := ctx.Deadline()
				// Earlier measurements spent two seconds: a fresh 5 s context fails here.
				remaining := time.Until(deadline)
				if !ok || remaining > 3*time.Second || remaining < 2*time.Second {
					t.Errorf("validator deadline remainder = %v, present %v", remaining, ok)
				}
				return doctor.Outcome{Errors: 2, Warnings: 3}, nil
			}
			got := run(t, withDoctor(p), "doctor", "--json")
			check := doctorConfigCheck(t, got.stdout)
			if elapsed == doctor.Budget {
				if calls != 0 || check["status"] != doctor.StatusUnknown || got.code != 0 {
					t.Fatalf("spent budget: calls %d, check %v, exit %d", calls, check, got.code)
				}
			} else if calls != 1 || check["status"] != doctor.StatusFail || got.code != 68 {
				t.Fatalf("injected validator overridden: calls %d, check %v, exit %d", calls, check, got.code)
			}
		})
	}
}

func TestDoctorRealValidatorContextEnd(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			var ctx context.Context
			var end context.CancelFunc
			if want == context.Canceled {
				ctx, end = context.WithCancel(context.Background())
			} else {
				ctx, end = context.WithDeadline(context.Background(), time.Now().Add(50*time.Millisecond))
			}
			defer end()
			p := doctorWorkspacePorts(goodDir(t))
			p.GitVersion = func(context.Context, string) (string, error) {
				if want == context.Canceled {
					end()
				} else {
					<-ctx.Done()
				}
				return "git version 2.43.0", nil
			}
			result, err := commands.DoctorHandler(p)(ctx, commands.Call{})
			if !errors.Is(err, want) || result.Value != nil {
				t.Fatalf("got result %+v, error %v; want %v", result, err, want)
			}
		})
	}
}

func TestDoctorRealValidatorSkipsAbsentManifest(t *testing.T) {
	parent := goodDir(t)
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	got := run(t, withDoctor(doctorWorkspacePorts(child)), "doctor", "--json")
	check := doctorConfigCheck(t, got.stdout)
	if got.code != 0 || check["status"] != doctor.StatusSkipped || check["reason"] != "precondition_absent" {
		t.Fatalf("%v, exit %d", check, got.code)
	}
}

// A real Git repository exercises the production checker and hardened runner.
// The wrapper denies every unguarded invocation and every network-capable verb;
// marker and before/after snapshots detect hooks, fsmonitor and file mutations.
func TestDoctorRealValidatorReadOnlyNoNetwork(t *testing.T) {
	dir := goodDir(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git := exec.Command(gitPath, "init", "-q", "-b", "main", "--template=", dir)
	git.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	marker := filepath.Join(dir, "UNEXPECTED_EXECUTION")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	put(t, dir, "probe.sh", "#!/bin/sh\ntouch "+quote(marker)+"\n")
	if err := os.Chmod(filepath.Join(dir, "probe.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, dir, ".git/config", "[core]\n repositoryformatversion = 0\n bare = false\n fsmonitor = "+filepath.Join(dir, "probe.sh")+"\n hooksPath = "+dir+"\n[remote \"origin\"]\n url = https://CANARY_CREDENTIAL@example.invalid/repo\n[protocol \"https\"]\n allow = always\n")
	toolDir := t.TempDir()
	log := filepath.Join(toolDir, "calls")
	guard := "#!/bin/sh\n[ \"$GIT_ALLOW_PROTOCOL\" = workharbor-no-protocol ] || exit 91\nfor arg do\n case \"$arg\" in fetch|pull|push|clone|ls-remote|submodule) exit 92;; esac\ndone\nprintf '%s\\n' \"$*\" >> " + quote(log) + "\nexec " + quote(gitPath) + " \"$@\"\n"
	put(t, toolDir, "git", guard)
	if err := os.Chmod(filepath.Join(toolDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	before := doctorTreeSnapshot(t, dir)
	p := doctorWorkspacePorts(dir)
	p.DialUnix = func(_ context.Context, path string) error {
		if path != "/var/run/docker.sock" {
			t.Errorf("unexpected socket dial: %s", path)
		}
		return nil
	}
	got := run(t, withDoctor(p), "doctor", "--json")
	check := doctorConfigCheck(t, got.stdout)
	if got.code != 0 || check["status"] != doctor.StatusPass {
		t.Fatalf("checker failed: %s %s", got.stdout, got.err)
	}
	if !reflect.DeepEqual(before, doctorTreeSnapshot(t, dir)) {
		t.Fatal("doctor mutated workspace bytes, modes or timestamps")
	}
	calls, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(calls), "ls-files --stage -z") {
		t.Fatalf("real checker Git stage not measured: %q, %v", calls, err)
	}
	if strings.Contains(got.stdout, "CANARY_CREDENTIAL") || strings.Contains(got.stdout, "example.invalid") {
		t.Fatalf("configuration detail leaked: %s", got.stdout)
	}
}

func doctorTreeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		if info.Mode().IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		state[path] = fmt.Sprintf("%v %v %d %x", info.Mode(), info.ModTime(), info.Size(), sha256.Sum256(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// Let the real checker block in its Git subprocess after earlier measurements
// consumed nearly all of doctor's budget. A new validator budget would hang.
func TestDoctorRealValidatorUsesRemainingBudget(t *testing.T) {
	dir := goodDir(t)
	put(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	put(t, dir, ".git/config", "")
	toolDir := t.TempDir()
	put(t, toolDir, "git", "#!/bin/sh\nexec /bin/sleep 30\n")
	if err := os.Chmod(filepath.Join(toolDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := doctorWorkspacePorts(dir)
	start := time.Now()
	now := start
	p.Now = func() time.Time { return now }
	p.GitVersion = func(context.Context, string) (string, error) {
		now = start.Add(doctor.Budget - 100*time.Millisecond)
		return "git version 2.43.0", nil
	}
	got := run(t, withDoctor(p), "doctor", "--json")
	elapsed := time.Since(start)
	check := doctorConfigCheck(t, got.stdout)
	if elapsed > 2*time.Second || check["status"] != doctor.StatusUnknown || check["reason"] != "not_measurable" || got.code != 0 {
		t.Fatalf("validator ignored remainder: elapsed %v, exit %d, check %v", elapsed, got.code, check)
	}
}
