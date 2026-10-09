package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/launchd"
)

type recordingLaunchctl struct {
	manager string
	loaded  bool
	calls   []string
}

func (r *recordingLaunchctl) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	switch args[0] {
	case "managername":
		return []byte(r.manager), nil
	case "print":
		if !r.loaded {
			return nil, errors.New("exit status 113")
		}
		return []byte("\tstate = running\n\tpid = 77\n\tlast exit code = 0\n"), nil
	case "bootstrap":
		r.loaded = true
	case "bootout":
		r.loaded = false
	}
	return nil, nil
}

// serviceRig is a home with a valid configuration and an installed binary.
type serviceRig struct {
	home, whr, cfg string
	launchctl      *recordingLaunchctl
}

func newServiceRig(t *testing.T) *serviceRig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"workspaces", "store", "secrets", "home", "prefix/bin"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	secret := func(name, body string) string {
		p := filepath.Join(dir, "secrets", name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := config.Config{
		Listen: "127.0.0.1:8787", Repositories: []config.Repository{{Name: "wstein/workharbor"}},
		Roots:        config.Roots{Workspaces: []string{filepath.Join(dir, "workspaces")}, ToolStore: filepath.Join(dir, "store")},
		GitHub:       config.GitHub{AppID: 1, KeyFile: secret("app.pem", "-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----\n")},
		APITokenFile: secret("api.token", "x\n"), AgentAllowedTools: []string{"Read"},
	}
	b, _ := json.Marshal(cfg)
	r := &serviceRig{home: filepath.Join(dir, "home"), whr: filepath.Join(dir, "prefix", "bin", "whr"), cfg: filepath.Join(dir, "config.json"), launchctl: &recordingLaunchctl{manager: "Aqua"}}
	if err := os.WriteFile(r.cfg, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.whr, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
		t.Fatal(err)
	}
	return r
}

func (r *serviceRig) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			if k == "HOME" {
				return r.home
			}
			return ""
		},
		Host: Host{
			Manager:    &launchd.Manager{R: r.launchctl, UID: 501, GOOS: "darwin"},
			Executable: func() (string, error) { return r.whr, nil },
			LookPath:   func(string) (string, error) { return "/opt/homebrew/bin/container", nil },
		},
	}
	code := Execute(context.Background(), env, append(args, "--config", r.cfg))
	return code, out.String(), errOut.String()
}

func TestServiceInstallStatusUninstall(t *testing.T) {
	r := newServiceRig(t)
	plist := filepath.Join(r.home, "Library", "LaunchAgents", launchd.Label+".plist")

	code, out, errOut := r.run(t, "service", "install")
	if code != exitcode.OK || strings.TrimSpace(out) != plist {
		t.Fatalf("install: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	data, err := os.ReadFile(plist) //nolint:gosec // a path in the test's temp dir
	if err != nil || !strings.Contains(string(data), r.whr) || !strings.Contains(string(data), r.cfg) || !strings.Contains(string(data), "/opt/homebrew/bin/container") {
		t.Fatalf("plist %v:\n%s", err, data)
	}

	code, out, _ = r.run(t, "service", "status")
	if code != exitcode.OK || out != "installed\ttrue\nloaded\ttrue\nstate\trunning\npid\t77\nlast_exit\t0\n" {
		t.Errorf("status: exit %d, stdout %q", code, out)
	}
	code, out, _ = r.run(t, "service", "status", "--json")
	if code != exitcode.OK || !strings.Contains(out, `"loaded":true`) || !strings.Contains(out, `"schema_version":1`) {
		t.Errorf("status --json: exit %d, stdout %q", code, out)
	}

	if code, _, errOut := r.run(t, "service", "uninstall"); code != exitcode.OK || errOut != "" {
		t.Errorf("uninstall: exit %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(plist); err == nil {
		t.Error("uninstall left the plist")
	}
}

func TestServiceInstallRefusesWhatWouldNotWork(t *testing.T) {
	r := newServiceRig(t)
	plist := filepath.Join(r.home, "Library", "LaunchAgents", launchd.Label+".plist")
	noPlist := func(why string) {
		t.Helper()
		if _, err := os.Stat(plist); err == nil {
			t.Errorf("%s: a plist was written", why)
		}
	}

	r.launchctl.manager = "Background" // an SSH session
	if code, _, errOut := r.run(t, "service", "install"); code != exitcode.Error || !strings.Contains(errOut, "graphical login session") {
		t.Errorf("outside the GUI session: exit %d, stderr %q", code, errOut)
	}
	noPlist("outside the GUI session")
	r.launchctl.manager = "Aqua"

	// a whr that is not an executable file
	plain := filepath.Join(filepath.Dir(filepath.Dir(r.whr)), "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := r.run(t, "service", "install", "--whr", plain); code != exitcode.Usage || !strings.Contains(errOut, "not an executable file") {
		t.Errorf("a file that is not executable: exit %d, stderr %q", code, errOut)
	}
	noPlist("a file that is not executable")

	// a configuration that would not start
	if err := os.WriteFile(r.cfg, []byte(`{"listen":"0.0.0.0:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := r.run(t, "service", "install"); code != exitcode.Error || !strings.Contains(errOut, "not valid") {
		t.Errorf("a bad configuration: exit %d, stderr %q", code, errOut)
	}
	noPlist("a bad configuration")
	for _, c := range r.launchctl.calls {
		if strings.HasPrefix(c, "bootstrap") {
			t.Errorf("launchctl was asked to load a refused job: %v", r.launchctl.calls)
		}
	}
}

// Uninstall and status need neither whr nor container: they must still work
// when either is gone.
func TestUninstallAndStatusNeedNeitherWhrNorContainer(t *testing.T) {
	r := newServiceRig(t)
	if code, _, errOut := r.run(t, "service", "install"); code != exitcode.OK {
		t.Fatalf("install: %d %s", code, errOut)
	}
	gone := Host{
		Manager:    &launchd.Manager{R: r.launchctl, UID: 501, GOOS: "darwin"},
		Executable: func() (string, error) { return "", errors.New("whr is gone") },
		LookPath:   func(string) (string, error) { return "", errors.New("container is gone") },
	}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Host: gone, Getenv: func(k string) string {
			if k == "HOME" {
				return r.home
			}
			return ""
		}}
		code := Execute(context.Background(), env, append(args, "--config", r.cfg))
		return code, out.String() + errOut.String()
	}
	if code, out := run("service", "status"); code != exitcode.OK || !strings.Contains(out, "loaded\ttrue") {
		t.Errorf("status: %d %q", code, out)
	}
	if code, out := run("service", "uninstall"); code != exitcode.OK {
		t.Errorf("uninstall: %d %q", code, out)
	}
	if code, _ := run("service", "install"); code == exitcode.OK {
		t.Error("install worked without the binaries")
	}
}
