package launchd

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden file")

var bg = context.Background()

func spec(home string) Spec {
	return Spec{Label: Label, Whr: "/opt/whr/bin/whr", Config: home + "/.config/whr/config.json", Container: "/opt/homebrew/bin/container", Home: home}
}

func TestThePlistIsTheGoldenOne(t *testing.T) {
	got, err := Plist(spec("/Users/whr"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "service.plist.golden")
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Errorf("the plist differs from its golden file (run with -update):\n%s", got)
	}
}

// A path is an argument of the script, never text in it, and XML characters in
// a path are escaped: nothing a path contains changes what runs.
func TestAPathCannotChangeWhatRuns(t *testing.T) {
	s := spec("/Users/whr")
	s.Config = `/Users/whr/a&b/<c>"d'$(touch x)`
	got, err := Plist(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "<c>") || strings.Contains(text, "a&b") || !strings.Contains(text, "a&amp;b/&lt;c&gt;") {
		t.Errorf("a path was not escaped:\n%s", text)
	}
	if strings.Count(text, "touch") != 1 || strings.Contains(strings.SplitN(text, "whr-launch", 2)[0], "touch") {
		t.Error("a path reached the script text")
	}
	for _, want := range []string{"<key>LimitLoadToSessionType</key>\n  <string>Aqua</string>", "<key>KeepAlive</key>", "<key>ThrottleInterval</key>", "Library/Logs/whr/whr.out.log"} {
		if !strings.Contains(text, want) {
			t.Errorf("the plist lacks %q", want)
		}
	}
	for _, bad := range []string{"EnvironmentVariables</key>\n  <dict>\n    <key>ANTHROPIC", "TOKEN", "SECRET", "KEY"} {
		if strings.Contains(text, bad) {
			t.Errorf("the plist mentions %q: it carries no secret", bad)
		}
	}
}

func TestASpecThatIsNotSafeIsRefused(t *testing.T) {
	for name, mut := range map[string]func(*Spec){
		"relative whr":   func(s *Spec) { s.Whr = "bin/whr" },
		"unclean config": func(s *Spec) { s.Config = "/Users/whr/../x/config.json" },
		"newline":        func(s *Spec) { s.Container = "/opt/x\n/container" },
		"bad label":      func(s *Spec) { s.Label = "a/b" },
		"no label":       func(s *Spec) { s.Label = "" },
		"no home":        func(s *Spec) { s.Home = "" },
	} {
		s := spec("/Users/whr")
		mut(&s)
		if _, err := Plist(s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTheBinaryMustBeInstalledNotBuiltInAWorkingTree(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(tree, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(tree, "bin", "whr")
	installed := filepath.Join(dir, "prefix", "bin", "whr")
	for _, p := range []string{built, installed} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
			t.Fatal(err)
		}
	}
	if err := CheckBinary(installed); err != nil {
		t.Errorf("an installed binary was refused: %v", err)
	}
	if err := CheckBinary(built); err == nil || !strings.Contains(err.Error(), "git working tree") {
		t.Errorf("a binary in a working tree = %v", err)
	}
	// a link from outside the tree into it is the tree's binary
	link := filepath.Join(dir, "prefix", "bin", "linked")
	if err := os.Symlink(built, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckBinary(link); err == nil {
		t.Error("a symbolic link into a working tree was accepted")
	}
	if err := CheckBinary(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing binary was accepted")
	}
	notExec := filepath.Join(dir, "prefix", "plain")
	if err := os.WriteFile(notExec, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckBinary(notExec); err == nil {
		t.Error("a file that is not executable was accepted")
	}
}

// fakeLaunchctl records every call and answers like launchctl does.
type fakeLaunchctl struct {
	manager   string
	loaded    bool
	calls     []string
	failBoot  bool
	failFirst int // bootstraps that fail before one succeeds
	print     string
}

func (f *fakeLaunchctl) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name != "launchctl" {
		return nil, errors.New("only launchctl is run")
	}
	switch args[0] {
	case "managername":
		return []byte(f.manager + "\n"), nil
	case "print":
		if !f.loaded {
			return []byte("Could not find service"), errors.New("exit status 113")
		}
		return []byte(f.print), nil
	case "bootout":
		f.loaded = false
		return nil, nil
	case "bootstrap":
		if f.failFirst > 0 {
			f.failFirst--
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		if f.failBoot {
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		f.loaded = true
		return nil, nil
	}
	return nil, errors.New("unexpected " + args[0])
}

func manager(f *fakeLaunchctl) Manager { return Manager{R: f, UID: 501, GOOS: "darwin"} }

func TestInstallWritesTheJobAndLoadsItInTheUsersGUIDomain(t *testing.T) {
	home := t.TempDir()
	f := &fakeLaunchctl{manager: "Aqua"}
	s := spec(home)
	if err := manager(f).Install(bg, s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.PlistPath())
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("plist %v, %v: want a 0644 file", fi, err)
	}
	if di, err := os.Stat(s.LogDir()); err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("log dir %v, %v: want 0700", di, err)
	}
	want := []string{"launchctl managername", "launchctl print gui/501/" + Label, "launchctl bootstrap gui/501 " + s.PlistPath()}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls %v, want %v", f.calls, want)
	}
	if ents, _ := os.ReadDir(filepath.Dir(s.PlistPath())); len(ents) != 1 {
		t.Errorf("the LaunchAgents directory holds %d entries, want only the plist", len(ents))
	}

	// again: the loaded job is replaced, not duplicated
	f.calls = nil
	if err := manager(f).Install(bg, s); err != nil {
		t.Fatal(err)
	}
	want = []string{"launchctl managername", "launchctl print gui/501/" + Label, "launchctl bootout gui/501/" + Label, "launchctl bootstrap gui/501 " + s.PlistPath()}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Errorf("second install: calls %v, want %v", f.calls, want)
	}
}

func TestInstallRefusesOutsideTheGUISessionAndOffAMac(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Background", "System", ""} {
		f := &fakeLaunchctl{manager: name}
		err := manager(f).Install(bg, spec(home))
		if !errors.Is(err, ErrNoGUILogIn) {
			t.Errorf("managername %q = %v, want ErrNoGUILogIn", name, err)
		}
		if _, serr := os.Stat(spec(home).PlistPath()); serr == nil {
			t.Errorf("managername %q left a plist behind", name)
		}
	}
	linux := Manager{R: &fakeLaunchctl{manager: "Aqua"}, UID: 1000, GOOS: "linux"}
	if err := linux.Install(bg, spec(home)); !errors.Is(err, ErrNotMac) {
		t.Errorf("linux = %v, want ErrNotMac", err)
	}
}

func TestAFailedBootstrapIsReportedWithLaunchctlsMessage(t *testing.T) {
	f := &fakeLaunchctl{manager: "Aqua", failBoot: true}
	err := manager(f).Install(bg, spec(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "Input/output error") {
		t.Errorf("err = %v", err)
	}
}

func TestUninstallUnloadsAndRemovesAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	f := &fakeLaunchctl{manager: "Aqua"}
	s := spec(home)
	m := manager(f)
	if err := m.Install(bg, s); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(bg, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.PlistPath()); err == nil {
		t.Error("the plist is still there")
	}
	if f.loaded {
		t.Error("the job is still loaded")
	}
	if _, err := os.Stat(s.LogDir()); err != nil {
		t.Error("the logs were removed: they stay")
	}
	if err := m.Uninstall(bg, s); err != nil {
		t.Errorf("a second uninstall = %v, want nil", err)
	}
}

func TestStatusReadsWhatLaunchdSays(t *testing.T) {
	home := t.TempDir()
	s := spec(home)
	f := &fakeLaunchctl{manager: "Aqua"}
	m := manager(f)
	if st, err := m.Status(bg, s); err != nil || st.Installed || st.Loaded {
		t.Errorf("nothing installed: %+v, %v", st, err)
	}
	if err := m.Install(bg, s); err != nil {
		t.Fatal(err)
	}
	f.print = "io.github.wstein.workharbor = {\n\tactive count = 1\n\tstate = running\n\tpid = 4242\n\tlast exit code = 0\n\tthrottle interval = 30\n}\n"
	st, err := m.Status(bg, s)
	if err != nil || !st.Installed || !st.Loaded || st.State != "running" || st.PID != 4242 || st.LastExit != "0" {
		t.Errorf("status %+v, %v", st, err)
	}
}

// launchd is strict about the file: plutil, where the machine has it, says
// whether the XML is a property list.
func TestThePlistIsAValidPropertyList(t *testing.T) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil is not available")
	}
	s := spec(t.TempDir())
	s.Config = `/Users/whr/a&b/<c>"d'$(touch x)/config.json`
	data, err := Plist(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "job.plist")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(bg, plutil, "-lint", path).CombinedOutput(); err != nil { //nolint:gosec // plutil from the PATH on a test machine
		t.Errorf("plutil -lint: %v\n%s", err, out)
	}
}

func TestRootIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	home := t.TempDir()
	f := &fakeLaunchctl{manager: "Aqua"} // a sudo shell can still say Aqua
	err := Manager{R: f, UID: 0, GOOS: "darwin"}.Install(bg, spec(home))
	if !errors.Is(err, ErrRoot) {
		t.Fatalf("root = %v, want ErrRoot", err)
	}
	if _, serr := os.Stat(spec(home).PlistPath()); serr == nil {
		t.Error("a plist was written for root")
	}
	if len(f.calls) != 0 {
		t.Errorf("launchctl was run for root: %v", f.calls)
	}
}

func TestAnExistingLogDirectoryIsTightened(t *testing.T) {
	home := t.TempDir()
	s := spec(home)
	if err := os.MkdirAll(s.LogDir(), 0o755); err != nil { //nolint:gosec // the loose directory the install must tighten
		t.Fatal(err)
	}
	if err := manager(&fakeLaunchctl{manager: "Aqua"}).Install(bg, s); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(s.LogDir()); fi.Mode().Perm() != 0o700 {
		t.Errorf("log dir %v, want 0700", fi.Mode().Perm())
	}
}

// Right after a bootout launchd may still be tearing the old job down: a
// reinstall tries again, a first install does not.
func TestAReinstallRetriesTheLoadAfterTheUnload(t *testing.T) {
	home := t.TempDir()
	s := spec(home)
	f := &fakeLaunchctl{manager: "Aqua"}
	m := Manager{R: f, UID: 501, GOOS: "darwin", RetryAfter: time.Millisecond}
	if err := m.Install(bg, s); err != nil {
		t.Fatal(err)
	}
	f.failFirst, f.calls = 2, nil
	if err := m.Install(bg, s); err != nil {
		t.Fatalf("a reinstall that needs a retry: %v", err)
	}
	boots := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "launchctl bootstrap") {
			boots++
		}
	}
	if boots != 3 {
		t.Errorf("%d bootstraps, want 3", boots)
	}

	// a first install fails at once
	f2 := &fakeLaunchctl{manager: "Aqua", failBoot: true}
	m2 := Manager{R: f2, UID: 501, GOOS: "darwin", RetryAfter: time.Millisecond}
	if err := m2.Install(bg, spec(t.TempDir())); err == nil {
		t.Fatal("a failing first install succeeded")
	}
	boots = 0
	for _, c := range f2.calls {
		if strings.HasPrefix(c, "launchctl bootstrap") {
			boots++
		}
	}
	if boots != 1 {
		t.Errorf("a first install tried %d times, want once", boots)
	}
}
