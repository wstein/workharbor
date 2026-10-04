package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/launchd"
)

func devSetupRig(t *testing.T) (*setupRig, string) {
	t.Helper()
	r := newSetupRig(t)
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.exe = filepath.Join(home, ".local", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(r.exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
		t.Fatal(err)
	}
	r.env.Executable = func() (string, error) { return r.exe, nil }
	return r, home
}

func runDevSetup(t *testing.T, r *setupRig, home string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Setup: r.env,
		Getenv: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		},
	}
	code := Execute(context.Background(), env, args)
	return code, out.String(), errOut.String()
}

func TestDevSetupSelectsTheSourceInstall(t *testing.T) {
	r, home := devSetupRig(t)
	base := []string{"setup", "--user", "werner", "--only", "config-base", "--dry-run"}
	code, _, errOut := runDevSetup(t, r, home, base...)
	if !strings.Contains(errOut, "not an installed binary") || !strings.Contains(errOut, "--dev") {
		t.Fatalf("managed mode must explain development install: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runDevSetup(t, r, home, append(base, "--dev")...)
	if code == exitcode.Usage || strings.Contains(errOut, "not an installed binary") || !strings.Contains(errOut, "development installation") {
		t.Fatalf("development install: exit %d, stderr %q", code, errOut)
	}
	if len(r.host.ran) != 0 || len(r.host.opened) != 0 || r.host.asked != 0 {
		t.Fatal("dry run changed the host or asked a question")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "whr", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote configuration: %v", err)
	}
}

func TestDevSetupUsesExplicitPrefixAndResolvedBinary(t *testing.T) {
	for _, kind := range []string{"custom", "symlink", "outside", "working-tree", "root"} {
		t.Run(kind, func(t *testing.T) {
			r, home := devSetupRig(t)
			args := []string{"setup", "--dev", "--user", "werner", "--only", "config-base"}
			want := ""
			switch kind {
			case "custom":
				custom := filepath.Join(home, "custom prefix")
				if err := os.MkdirAll(filepath.Join(custom, "bin"), 0o700); err != nil {
					t.Fatal(err)
				}
				r.exe = filepath.Join(custom, "bin", "whr")
				if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
				args = append(args, "--prefix", custom, "--dry-run")
			case "symlink":
				link := filepath.Join(home, "linked-whr")
				if err := os.Symlink(r.exe, link); err != nil {
					t.Fatal(err)
				}
				r.exe = link
				args = append(args, "--dry-run")
			case "outside":
				other := filepath.Join(home, "other-whr")
				if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
				r.exe = other
				want = "not an installed binary"
			case "working-tree":
				if err := os.Mkdir(filepath.Join(home, ".local", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				want = "git working tree"
			case "root":
				r.env.User, r.env.UID = "root", 0
				want = "never runs as root"
			}
			code, _, errOut := runDevSetup(t, r, home, args...)
			if want != "" {
				if code != exitcode.Usage || !strings.Contains(errOut, want) {
					t.Fatalf("exit %d, stderr %q, want %q", code, errOut, want)
				}
			} else if code == exitcode.Usage || strings.Contains(errOut, "not an installed binary") {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
		})
	}
}

func TestDevSetupPassesTheSelectedBinaryToServiceInstall(t *testing.T) {
	r, home := devSetupRig(t)
	code, _, errOut := runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "service-install", "--dry-run")
	if code == exitcode.Usage {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := r.exe + " service install --config " + filepath.Join(home, ".config", "whr", "config.json") + " --whr " + r.exe
	if !strings.Contains(errOut, want) {
		t.Fatalf("service command must pin selected binary %q, stderr %q", want, errOut)
	}
}

func TestDoctorDevReportsTheDevelopmentPrefixAsAWarning(t *testing.T) {
	r, home := devSetupRig(t)
	_, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner")
	if !strings.Contains(out, "warn\tprefix\t") || !strings.Contains(out, filepath.Join(home, ".local")) {
		t.Fatalf("development prefix: stdout %q, stderr %q", out, errOut)
	}
	if !strings.Contains(errOut, "whr setup --dev --only config-base") {
		t.Fatalf("doctor repair omitted --dev: %q", errOut)
	}
	if len(r.host.ran) != 0 || r.host.asked != 0 {
		t.Fatal("doctor changed the host")
	}
}

func TestDoctorDevRejectsUnsafeInstallations(t *testing.T) {
	for _, kind := range []string{"outside", "working-tree", "writable", "root"} {
		t.Run(kind, func(t *testing.T) {
			r, home := devSetupRig(t)
			switch kind {
			case "outside":
				r.exe = filepath.Join(home, "other-whr")
				if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
			case "working-tree":
				if err := os.WriteFile(filepath.Join(home, ".local", ".git"), []byte("gitdir: elsewhere\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(filepath.Dir(r.exe), 0o777); err != nil { //nolint:gosec // deliberately unsafe permissions to test rejection
					t.Fatal(err)
				}
			case "root":
				r.env.User, r.env.UID = "root", 0
			}
			code, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", r.env.User)
			if code == 0 || !strings.Contains(out, "fail\tprefix\t") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestDoctorDevKeepsCustomPrefixInRepairs(t *testing.T) {
	r, home := devSetupRig(t)
	prefix := filepath.Join(home, ".local")
	_, _, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
	if !strings.Contains(errOut, "whr setup --dev --only config-base --prefix '"+prefix+"'") {
		t.Fatal(errOut)
	}
}

type devConfigHost struct{ *setupHost }

func (h devConfigHost) Confirm(string) (bool, error) { h.asked++; return true, nil }
func (h devConfigHost) Line(prompt string) (string, error) {
	h.asked++
	if strings.HasPrefix(prompt, "Repository") {
		return "wstein/workharbor", nil
	}
	return "", nil
}

func TestDevSetupConfigBaseIsPrivateAndIdempotent(t *testing.T) {
	r, home := devSetupRig(t)
	r.env.Host = devConfigHost{r.host}
	args := []string{"setup", "--dev", "--user", "werner", "--only", "config-base"}
	code, _, errOut := runDevSetup(t, r, home, args...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	path := filepath.Join(home, ".config", "whr", "config.json")
	before, err := os.ReadFile(path) //nolint:gosec // path belongs to the isolated test home
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("configuration is not private: %v, %v", fi, err)
	}
	asked := r.host.asked
	code, _, errOut = runDevSetup(t, r, home, args...)
	if code != 0 {
		t.Fatalf("second setup: exit %d: %s", code, errOut)
	}
	after, err := os.ReadFile(path) //nolint:gosec // path belongs to the isolated test home
	if err != nil || !bytes.Equal(before, after) || r.host.asked != asked {
		t.Fatalf("second setup overwrote or prompted: %v", err)
	}
	if len(r.host.ran) != 0 || len(r.host.opened) != 0 {
		t.Fatal("config-base installed a service or ran commands")
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents")); !os.IsNotExist(err) {
		t.Fatalf("config-base created a service: %v", err)
	}
}

func TestServiceInstallRetainsDevelopmentBinary(t *testing.T) {
	r := newServiceRig(t)
	r.whr = filepath.Join(r.home, ".local", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(r.whr), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.whr, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
		t.Fatal(err)
	}
	code, _, errOut := r.run(t, "service", "install", "--whr", r.whr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	plist, err := os.ReadFile(filepath.Join(r.home, "Library", "LaunchAgents", launchd.Label+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plist, []byte(r.whr)) {
		t.Fatalf("service did not retain the development binary: %s", plist)
	}
}
