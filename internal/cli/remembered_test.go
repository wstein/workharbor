package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/exitcode"
)

// rememberHost confirms every question and records what it is shown, so a test
// sees the diff and the notice that the key was written.
type rememberHost struct {
	*setupHost
	shown *[]string
}

func (h rememberHost) Confirm(string) (bool, error) { h.asked++; return true, nil }
func (h rememberHost) Show(s string)                { *h.shown = append(*h.shown, s) }

func cfgPathOf(home string) string { return filepath.Join(home, ".config", "whr", "config.json") }

// writeKey writes a configuration with the given top-level keys, mode 0600.
func writeKey(t *testing.T, home string, mode os.FileMode, keys map[string]any) {
	t.Helper()
	path := cfgPathOf(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(keys)
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func keysOf(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(cfgPathOf(home))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// runWithEnv is runDevSetup with extra environment variables for the process.
func runWithEnv(t *testing.T, r *setupRig, home string, extra map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Setup: r.env,
		Getenv: func(k string) string {
			if k == "HOME" {
				return home
			}
			return extra[k]
		},
	}
	code := Execute(context.Background(), env, args)
	return code, out.String(), errOut.String()
}

func TestDoctorReadsTheRememberedDevelopmentInstallation(t *testing.T) {
	r, home := devSetupRig(t)
	prefix := filepath.Join(home, ".local")
	writeKey(t, home, 0o600, map[string]any{"development_prefix": prefix})

	code, out, errOut := runDevSetup(t, r, home, "doctor", "--user", "werner")
	if code == exitcode.Usage {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	// development prefix selected from the key alone, no --dev typed
	if !strings.Contains(out, "warn\tprefix\t") || !strings.Contains(out, prefix) {
		t.Errorf("the prefix is not read from the key: stdout %q", out)
	}
	// warn on every run, naming the key, the file and the way out (D49)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "warn\tdevelopment-mode\t") {
			line = l
		}
	}
	for _, want := range []string{"development_prefix", cfgPathOf(home), "whr setup --managed"} {
		if !strings.Contains(line, want) {
			t.Errorf("the warn line lacks %q: %q", want, line)
		}
	}
	if !strings.Contains(errOut, "remembered as development_prefix in "+cfgPathOf(home)) {
		t.Errorf("stderr does not say the mode is remembered: %q", errOut)
	}
	// repair lines do not append --dev: `whr setup` reads the key itself
	if strings.Contains(errOut, "--dev") {
		t.Errorf("a repair line carries --dev while the key is set: %q", errOut)
	}
	if len(r.host.ran) != 0 || r.host.asked != 0 {
		t.Error("doctor changed the host")
	}
	if got := keysOf(t, home); got["development_prefix"] != prefix || len(got) != 1 {
		t.Errorf("doctor changed the configuration: %v", got)
	}
}

func TestAnExplicitFlagWinsOverTheRememberedPrefix(t *testing.T) {
	r, home := devSetupRig(t)
	elsewhere := t.TempDir()
	writeKey(t, home, 0o600, map[string]any{"development_prefix": elsewhere})

	// --dev without --prefix is $HOME/.local, whatever the key says
	_, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner")
	if !strings.Contains(out, "warn\tprefix\t"+filepath.Join(home, ".local")+": development installation") {
		t.Errorf("--dev did not win: stdout %q", out)
	}
	if !strings.Contains(errOut, "whr setup --dev --only config-base") {
		t.Errorf("an explicit --dev keeps being repeated in repair lines: %q", errOut)
	}
	// the key is still warned about
	if !strings.Contains(out, "warn\tdevelopment-mode\t") {
		t.Errorf("no warning for the key: %q", out)
	}

	// an explicit --prefix without --dev is a managed call: the key is ignored
	_, out, _ = runDevSetup(t, r, home, "doctor", "--user", "werner", "--prefix", filepath.Join(home, ".local"))
	if strings.Contains(out, "development prefix") || strings.Contains(out, "warn\tprefix\t") {
		t.Errorf("--prefix alone was read as a development call: %q", out)
	}
	if !strings.Contains(out, "prefix\t") {
		t.Errorf("no prefix check: %q", out)
	}
}

func TestARefusedKeyStopsSetupAndDoctorButNotAManagedCall(t *testing.T) {
	r, home := devSetupRig(t)
	prefix := filepath.Join(home, ".local")
	cases := []struct {
		name  string
		mode  os.FileMode
		value any
		want  string
	}{
		{"relative", 0o600, "local", "absolute"},
		{"managed prefix", 0o600, "/opt/whr", "managed prefix"},
		{"wrong type", 0o600, 7, "wrong type"},
		{"group writable file", 0o660, prefix, "group or other"},
		{"other writable file", 0o606, prefix, "group or other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeKey(t, home, tc.mode, map[string]any{"development_prefix": tc.value})
			for _, args := range [][]string{
				{"doctor", "--user", "werner"},
				{"setup", "--user", "werner", "--only", "config-base", "--dry-run"},
			} {
				code, _, errOut := runDevSetup(t, r, home, args...)
				if code == 0 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "whr setup --managed") {
					t.Errorf("%v: exit %d, stderr %q, want a refusal with %q", args, code, errOut, tc.want)
				}
			}
			// `--prefix` without `--dev` is a managed call and ignores the key
			code, out, errOut := runDevSetup(t, r, home, "doctor", "--user", "werner", "--prefix", filepath.Join(home, "managed"))
			if strings.Contains(errOut, "refuses development_prefix") || code == exitcode.Usage {
				t.Errorf("a managed call was stopped by the key: exit %d, %q", code, errOut)
			}
			// but doctor still reports the refused key
			if !strings.Contains(out, "fail\tdevelopment-mode\t") {
				t.Errorf("the refused key is not reported: %q", out)
			}
		})
	}
}

func TestAManagedBinaryRefusesTheRememberedKey(t *testing.T) {
	if _, err := os.Stat("/usr/local"); err != nil {
		t.Skip("no /usr/local here")
	}
	r, home := devSetupRig(t)
	writeKey(t, home, 0o600, map[string]any{"development_prefix": filepath.Join(home, ".local")})
	r.env.Executable = func() (string, error) { return "/usr/local/bin/whr", nil }
	for _, args := range [][]string{
		{"doctor", "--user", "werner"},
		{"setup", "--user", "werner", "--only", "config-base", "--dry-run"},
	} {
		code, _, errOut := runDevSetup(t, r, home, args...)
		if code != exitcode.Usage || !strings.Contains(errOut, "managed prefix") || !strings.Contains(errOut, "whr setup --managed") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}
}

func TestOnlyAnExplicitSetupDevWritesTheKey(t *testing.T) {
	r, home := devSetupRig(t)
	prefix := filepath.Join(home, ".local")
	r.env.Host = devConfigHost{r.host}
	if code, _, errOut := runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "config-base"); code != 0 {
		t.Fatalf("config-base: exit %d: %s", code, errOut)
	}
	if _, ok := keysOf(t, home)["development_prefix"]; ok {
		t.Fatal("config-base wrote the key")
	}
	step := []string{"setup", "--user", "werner", "--only", "development-key"}

	// a managed call (no --dev) never writes it, whatever else is asked
	runDevSetup(t, r, home, step...)
	runDevSetup(t, r, home, "doctor", "--user", "werner")
	if _, ok := keysOf(t, home)["development_prefix"]; ok {
		t.Fatal("a call without --dev wrote the key")
	}

	// --dry-run shows the fix and writes nothing
	code, _, errOut := runDevSetup(t, r, home, append(append([]string{}, step...), "--dev", "--dry-run")...)
	if _, ok := keysOf(t, home)["development_prefix"]; ok || code == exitcode.Usage {
		t.Fatalf("dry run: exit %d, wrote the key", code)
	}
	if !strings.Contains(errOut, "development_prefix "+prefix) {
		t.Errorf("a dry run names what it would write: %q", errOut)
	}

	// an explicit --dev writes it after a confirmation, shows a diff and says so
	var shown []string
	r.env.Host = rememberHost{r.host, &shown}
	code, _, errOut = runDevSetup(t, r, home, append(append([]string{}, step...), "--dev")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	m := keysOf(t, home)
	if m["development_prefix"] != prefix || m["repositories"] == nil {
		t.Errorf("the key is not written next to the rest: %v", m)
	}
	if got := strings.Join(shown, "\n"); !strings.Contains(got, "whr wrote development_prefix "+prefix) {
		t.Errorf("the human is not told: %q", got)
	}
	if fi, err := os.Stat(cfgPathOf(home)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the configuration is not private: %v %v", fi, err)
	}
	// the next call finds it remembered: nothing to write
	before, _ := os.ReadFile(cfgPathOf(home))
	shown = nil
	if code, _, errOut := runDevSetup(t, r, home, step...); code != 0 || len(shown) != 0 {
		t.Errorf("a remembered setup: exit %d, shown %v, stderr %q", code, shown, errOut)
	}
	if after, _ := os.ReadFile(cfgPathOf(home)); !bytes.Equal(before, after) {
		t.Error("a remembered setup rewrote the file")
	}
}

func TestSetupDevWithAnotherPrefixRewritesTheKeyAfterConfirmation(t *testing.T) {
	r, home := devSetupRig(t)
	old := t.TempDir()
	writeKey(t, home, 0o600, map[string]any{"development_prefix": old, "listen": "127.0.0.1:1"})
	var shown []string
	r.env.Host = rememberHost{r.host, &shown}
	code, _, errOut := runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "development-key")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := keysOf(t, home)["development_prefix"]; got != filepath.Join(home, ".local") {
		t.Errorf("the key = %v", got)
	}
	if got := strings.Join(shown, "\n"); !strings.Contains(got, "- ") || !strings.Contains(got, old) {
		t.Errorf("the diff does not show the old prefix: %q", got)
	}
}

func TestSetupManagedRemovesTheKeyAndChecksTheManagedPrefix(t *testing.T) {
	r, home := devSetupRig(t)
	writeKey(t, home, 0o600, map[string]any{"development_prefix": filepath.Join(home, ".local"), "listen": "127.0.0.1:1"})
	var shown []string
	r.env.Host = rememberHost{r.host, &shown}

	if code, _, errOut := runDevSetup(t, r, home, "setup", "--managed", "--dev", "--user", "werner"); code != exitcode.Usage || !strings.Contains(errOut, "cannot be combined") {
		t.Errorf("--dev with --managed: exit %d, stderr %q", code, errOut)
	}
	if _, ok := keysOf(t, home)["development_prefix"]; !ok {
		t.Fatal("a refused call removed the key")
	}

	// a dry run removes nothing
	runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "development-key", "--dry-run")
	if _, ok := keysOf(t, home)["development_prefix"]; !ok {
		t.Fatal("a dry run removed the key")
	}

	_, _, errOut := runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "development-key")
	m := keysOf(t, home)
	if _, ok := m["development_prefix"]; ok || m["listen"] == nil {
		t.Errorf("the key is not removed alone: %v", m)
	}
	if !strings.Contains(strings.Join(shown, "\n"), "managed installation again") {
		t.Errorf("the human is not told: %v", shown)
	}
	if !strings.Contains(errOut, "\nprefix: ") {
		t.Errorf("the managed prefix is not checked afterwards: %q", errOut)
	}
	// the managed prefix of the rig does not exist: leaving development mode says so
	if code, _, errOut := runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "development-key"); code == 0 || !strings.Contains(errOut, "the managed prefix is not ready") {
		t.Errorf("a missing managed prefix: exit %d, stderr %q", code, errOut)
	}
	// and doctor is quiet about it once it is gone
	if _, out, _ := runDevSetup(t, r, home, "doctor", "--user", "werner"); strings.Contains(out, "warn\tdevelopment-mode") || !strings.Contains(out, "ok\tdevelopment-mode") {
		t.Errorf("doctor after --managed: %q", out)
	}
}

func TestSetupManagedRemovesAKeyThatIsRefused(t *testing.T) {
	r, home := devSetupRig(t)
	// the way out must work when the key is exactly what is refused
	writeKey(t, home, 0o660, map[string]any{"development_prefix": "relative", "listen": "127.0.0.1:1"})
	r.env.Host = rememberHost{r.host, new([]string)}
	runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "development-key")
	if _, ok := keysOf(t, home)["development_prefix"]; ok {
		t.Error("--managed could not remove a refused key")
	}
	if fi, err := os.Stat(cfgPathOf(home)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the rewritten file is not private: %v %v", fi, err)
	}
}

// No environment variable turns development mode on, and none is read.
func TestNoEnvironmentVariableSelectsDevelopmentMode(t *testing.T) {
	r, home := devSetupRig(t)
	vars := map[string]string{"WORKHARBOR_DEV": "1", "WHR_DEV": "1", "WHR_DEVELOPMENT_PREFIX": filepath.Join(home, ".local"), "DEV": "1"}
	writeKey(t, home, 0o600, map[string]any{"listen": "127.0.0.1:1"})

	args := []string{"setup", "--user", "werner", "--only", "config-base", "--dry-run"}
	codeA, outA, errA := runWithEnv(t, r, home, nil, args...)
	codeB, outB, errB := runWithEnv(t, r, home, vars, args...)
	if codeA != codeB || outA != outB || errA != errB {
		t.Errorf("the variables changed setup:\n%d %q %q\n%d %q %q", codeA, outA, errA, codeB, outB, errB)
	}
	if !strings.Contains(errB, "not an installed binary") || strings.Contains(errB, "development installation") {
		t.Errorf("setup selected development mode from the environment: %q", errB)
	}

	dArgs := []string{"doctor", "--user", "werner"}
	codeA, outA, _ = runWithEnv(t, r, home, nil, dArgs...)
	codeB, outB, _ = runWithEnv(t, r, home, vars, dArgs...)
	if codeA != codeB || outA != outB || strings.Contains(outB, "warn\tdevelopment-mode") || strings.Contains(outB, "development prefix") {
		t.Errorf("the variables changed doctor:\n%q\n%q", outA, outB)
	}
}

func TestServiceInstallReadsTheRememberedPrefix(t *testing.T) {
	r := newServiceRig(t)
	prefix := filepath.Dir(filepath.Dir(r.whr))
	raw, err := os.ReadFile(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	set := func(v any) {
		t.Helper()
		m["development_prefix"] = v
		raw, _ := json.Marshal(m)
		if err := os.WriteFile(r.cfg, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	set(prefix)
	code, _, errOut := r.run(t, "service", "install")
	if code != 0 || !strings.Contains(errOut, "development_prefix") {
		t.Errorf("the binary is under the key's prefix: exit %d, stderr %q", code, errOut)
	}

	other := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	set(other)
	if code, _, errOut := r.run(t, "service", "install"); code != exitcode.Usage || !strings.Contains(errOut, "not under the development_prefix") {
		t.Errorf("the binary is outside the key's prefix: exit %d, stderr %q", code, errOut)
	}
	set("/opt/whr")
	if code, _, errOut := r.run(t, "service", "install"); code == 0 || !strings.Contains(errOut, "managed prefix") {
		t.Errorf("a managed value: exit %d, stderr %q", code, errOut)
	}
	// without the key, nothing changes
	delete(m, "development_prefix")
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(r.cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := r.run(t, "service", "install"); code != 0 || strings.Contains(errOut, "development_prefix") {
		t.Errorf("no key: exit %d, stderr %q", code, errOut)
	}
}

// The next command of an interrupted setup repeats what this call chose: --dev
// is not repeated while the key remembers it, and --managed is.
func TestSetupNextCommandFollowsTheMode(t *testing.T) {
	r, home := devSetupRig(t)
	writeKey(t, home, 0o600, map[string]any{"development_prefix": filepath.Join(home, ".local")})
	_, _, errOut := runDevSetup(t, r, home, "setup", "--user", "werner", "--only", "config-base", "--dry-run")
	if !strings.Contains(errOut, "next: whr setup --user werner --only config-base") || strings.Contains(errOut, "--dev") {
		t.Errorf("remembered: %q", errOut)
	}
	_, _, errOut = runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "config-base", "--dry-run")
	if !strings.Contains(errOut, "next: whr setup --dev --user werner --only config-base") {
		t.Errorf("explicit --dev: %q", errOut)
	}
	_, _, errOut = runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "config-base", "--dry-run")
	if !strings.Contains(errOut, "next: whr setup --managed --user werner --only config-base") {
		t.Errorf("--managed: %q", errOut)
	}
}

// A whr in a managed prefix refuses the key in `service install` (the value itself
// is fine, so Load passes): the seam stands in for the fixed managed prefixes.
func TestServiceInstallRefusesTheKeyFromAManagedBinary(t *testing.T) {
	r := newServiceRig(t)
	prefix := filepath.Dir(filepath.Dir(r.whr))
	raw, err := os.ReadFile(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["development_prefix"] = prefix
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(r.cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	old := underManagedPrefix
	t.Cleanup(func() { underManagedPrefix = old })
	underManagedPrefix = func(path string) bool { return config.Within(path, prefix) }

	code, _, errOut := r.run(t, "service", "install")
	if code != exitcode.Usage || !strings.Contains(errOut, "managed prefix") || !strings.Contains(errOut, "whr setup --managed") {
		t.Errorf("a managed binary with the key: exit %d, stderr %q", code, errOut)
	}
	if len(r.launchctl.calls) != 0 {
		t.Errorf("the job was installed anyway: %v", r.launchctl.calls)
	}
}

// From a binary that is not installed, --managed runs only the development-key step
// (and the managed-prefix check): every other step is refused as without --managed.
func TestSetupManagedFromANonInstalledBinaryRunsOnlyTheKeyStep(t *testing.T) {
	r, home := devSetupRig(t)
	writeKey(t, home, 0o600, map[string]any{"development_prefix": filepath.Join(home, ".local"), "listen": "127.0.0.1:1"})
	var shown []string
	r.env.Host = rememberHost{r.host, &shown}

	for _, args := range [][]string{
		{"setup", "--managed", "--user", "werner"},
		{"setup", "--managed", "--user", "werner", "--only", "service-install"},
		{"setup", "--managed", "--user", "werner", "--only", "development-key,service-install"},
		{"setup", "--managed", "--user", "werner", "--from", "development-key"},
	} {
		code, _, errOut := runDevSetup(t, r, home, args...)
		if code != exitcode.Usage || !strings.Contains(errOut, "not an installed binary") || !strings.Contains(errOut, "only development-key") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
		if _, ok := keysOf(t, home)["development_prefix"]; !ok {
			t.Fatalf("%v removed the key", args)
		}
		if len(r.host.ran) != 0 {
			t.Fatalf("%v ran commands: %v", args, r.host.ran)
		}
	}

	code, _, errOut := runDevSetup(t, r, home, "setup", "--managed", "--user", "werner", "--only", "development-key")
	if _, ok := keysOf(t, home)["development_prefix"]; ok {
		t.Errorf("--only development-key left the key: exit %d, stderr %q", code, errOut)
	}
	if !strings.Contains(errOut, "only the development-key step runs") || len(r.host.ran) != 0 {
		t.Errorf("stderr %q, ran %v", errOut, r.host.ran)
	}
}
