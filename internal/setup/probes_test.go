package setup

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
)

// A probe runs once per run: the doctor and the wizard share its answer, and
// a command that is run for a fix makes the next probe run again.
func TestTerminalRunsAProbeOnce(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	log, err := runlog.Open(filepath.Join(dir, "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	h := Terminal{Probes: &Probes{}, Log: log}
	probe := []string{"sh", "-c", "echo x >> " + count + "; echo answer"}
	calls := func() int {
		b, _ := os.ReadFile(count) //nolint:gosec // a test path
		return strings.Count(string(b), "x")
	}
	for range 3 {
		out, err := h.Output(context.Background(), probe...)
		if err != nil || strings.TrimSpace(string(out)) != "answer" {
			t.Fatalf("out %q err %v", out, err)
		}
	}
	if n := calls(); n != 1 {
		t.Errorf("probe ran %d times, want 1", n)
	}
	if err := h.Run(context.Background(), doctor.Cmd{Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = h.Output(context.Background(), probe...)
	if n := calls(); n != 2 {
		t.Errorf("after a fix the probe ran %d times in all, want 2", n)
	}
	_ = log.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "run.log")) //nolint:gosec // a test path
	if n := strings.Count(string(b), "$ sh -c"); n != 2 {
		t.Errorf("the log holds the probe %d times, want 2:\n%s", n, b)
	}
}

func TestExpectedAnswersAreNamed(t *testing.T) {
	absent := "Error: Could not find key 'com.apple.autologout.AutoLogOutDelay' in domain 'kCFPreferencesAnyApplication'.\n"
	key := []string{"defaults", "read", "/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay"}
	for _, tc := range []struct {
		argv   []string
		exit   int
		stderr string
		want   string
	}{
		{[]string{"dseditgroup", "-o", "checkmember", "-m", "u", "admin"}, 67, "", "not a member"},
		{[]string{"dseditgroup", "-o", "checkmember", "-m", "u", "admin"}, 1, "", ""},
		{[]string{"dseditgroup", "-o", "read", "admin"}, 67, "", ""},
		{[]string{"dseditgroup", "-x", "checkmember"}, 67, "", ""},
		{[]string{"dscl", ".", "-read", "/Users/u", "UniqueID"}, 56, "", "no such record"},
		{[]string{"dscl", ".", "-list", "/Users"}, 56, "", ""},
		{key, 1, absent, "key not set"},
		{key, 1, "The domain/default pair of (/Library/Preferences/.GlobalPreferences, com.apple.autologout.AutoLogOutDelay) does not exist\n", "key not set"},
		{key, 1, "The domain/default pair of (/Library/Preferences/.GlobalPreferences, other.key) does not exist\n", ""},
		{key, 1, "Error: permission denied\n", ""},
		{key, 2, absent, ""},
		{[]string{"defaults", "read", "x", "y"}, 1, absent, ""},
		{[]string{"pmset", "-g"}, 67, "", ""},
	} {
		if got := doctor.ExpectedAnswer(tc.argv, tc.exit, tc.stderr); got != tc.want {
			t.Errorf("%v exit %d: %q, want %q", tc.argv, tc.exit, got, tc.want)
		}
	}
}

// Every question the person answers may follow an action of theirs, and the
// check that comes after must read the system again.
func TestAnswersForgetTheProbes(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	_ = os.WriteFile(state, []byte("old"), 0o600)
	read := func(h Terminal) string {
		out, err := h.Output(context.Background(), "cat", state)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	for name, ask := range map[string]func(h Terminal){
		"Ask":     func(h Terminal) { _, _ = h.Ask("Done with this step?", render.DefaultNo) },
		"AskWord": func(h Terminal) { _, _ = h.AskWord("Delete?", "word") },
		"Pause":   func(h Terminal) { _ = h.Pause() },
	} {
		_ = os.WriteFile(state, []byte("old"), 0o600)
		h := Terminal{Probes: &Probes{}, In: bufio.NewReader(strings.NewReader("y\n")), Err: io.Discard}
		if got := read(h); got != "old" {
			t.Fatalf("%s: %q", name, got)
		}
		_ = os.WriteFile(state, []byte("new"), 0o600) // the person acts, a guide-only fix
		if got := read(h); got != "old" {
			t.Fatalf("%s: the probe was not cached: %q", name, got)
		}
		ask(h)
		if got := read(h); got != "new" {
			t.Errorf("%s: the check after the answer read %q, want the new state", name, got)
		}
	}
}

// An interrupted answer is never kept, and the key separates the arguments.
func TestProbesKeepOnlyCompleteAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := Terminal{Probes: &Probes{}}
	_, _ = h.Output(ctx, "echo", "a")
	if _, ok := h.Probes.get(strings.Join([]string{"echo", "a"}, "\x00")); ok {
		t.Error("an answer cut short by the context was cached")
	}
	_, _ = h.Output(context.Background(), "echo", "a b")
	out, _ := h.Output(context.Background(), "echo", "a", "b")
	if string(out) != "a b\n" {
		t.Fatal(string(out))
	}
	if _, ok := h.Probes.get("echo\x00a b"); !ok {
		t.Error("the one-argument probe is missing")
	}
	if _, ok := h.Probes.get("echo\x00a\x00b"); !ok {
		t.Error("the two-argument probe is not a key of its own")
	}
}

func TestTerminalLogsTheExpectedAnswer(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"no u is NOT a member of admin\"\nexit 67\n"
	if err := os.WriteFile(filepath.Join(bin, "dseditgroup"), []byte(script), 0o700); err != nil { //nolint:gosec // a test stand-in
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	log, _ := runlog.Open(filepath.Join(dir, "run.log"))
	h := Terminal{Log: log}
	_, _ = h.Output(context.Background(), "dseditgroup", "-o", "checkmember", "-m", "u", "admin")
	_, _ = h.Output(context.Background(), "sh", "-c", "exit 67")
	_ = log.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "run.log")) //nolint:gosec // a test path
	got := string(b)
	if !strings.Contains(got, "answer: not a member (exit 67)") || !strings.Contains(got, "$ sh -c exit 67\nexit 67\n") {
		t.Errorf("log %q", got)
	}
}

// Terminal hands the command's stderr to ExpectedAnswer: the "key not set"
// answer is named only when stderr says so.
func TestTerminalNamesKeyNotSetFromStderr(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"Error: Could not find key 'com.apple.autologout.AutoLogOutDelay' in domain 'kCFPreferencesAnyApplication'.\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "defaults"), []byte(script), 0o700); err != nil { //nolint:gosec // a test stand-in
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	log, _ := runlog.Open(filepath.Join(dir, "run.log"))
	h := Terminal{Log: log}
	_, _ = h.Output(context.Background(), "defaults", "read", "/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay")
	_ = log.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "run.log")) //nolint:gosec // a test path
	if !strings.Contains(string(b), "answer: key not set (exit 1)") {
		t.Errorf("log %q", b)
	}
}
