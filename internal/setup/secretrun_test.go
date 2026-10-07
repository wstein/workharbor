package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
)

// Issue #378: a command with a SecretPrompt gets the secret on stdin only. The
// marker is random at test time; the fake child records argv, environment and
// stdin to files, and prompts for a password on its stdout as sysadminctl did.
func TestRunSecretGoesOnlyToStdin(t *testing.T) {
	var raw [24]byte
	_, _ = rand.Read(raw[:])
	marker := "fake-" + hex.EncodeToString(raw[:])
	dir := t.TempDir()
	rec := filepath.Join(dir, "argv")
	envf := filepath.Join(dir, "env")
	inf := filepath.Join(dir, "stdin")
	script := `printf '%s\n' "$@" > "$1"; env > "$2"; printf 'User password:'; IFS= read -r l; printf '%s\n' "$l" > "$3"; printf '\n'`
	var errBuf strings.Builder
	h := Terminal{Err: &errBuf, Style: render.Style{}, readSecret: func(string) (string, error) { return marker, nil }}
	c := doctor.Cmd{Argv: []string{"/bin/sh", "-c", script, "sh", rec, envf, inf}, SecretPrompt: "pw"}
	if err := h.Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(readFile(t, inf)) != marker {
		t.Fatalf("the child did not get the secret on stdin")
	}
	for name, text := range map[string]string{"relay output": errBuf.String(), "argv": strings.Join(c.Argv, " "), "recorded argv": readFile(t, rec), "env": readFile(t, envf)} {
		if strings.Contains(text, marker) {
			t.Errorf("the secret leaked into %s", name)
		}
	}
	if strings.Contains(strings.ToLower(errBuf.String()), "password:") || !strings.Contains(errBuf.String(), render.NeutralPromptLine) {
		t.Errorf("the child's secret prompt was relayed: %q", errBuf.String())
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p) //nolint:gosec // a path the test made
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Issue #378: a rejected new password (5402) is asked again, three times at
// most, with a stop after the third; any other failure is not retried.
func TestRunNewPasswordRetriesOnlyOnPolicyRejection(t *testing.T) {
	run := func(script string) (int, string, error) {
		dir := t.TempDir()
		count := filepath.Join(dir, "n")
		var b strings.Builder
		h := Terminal{Err: &b, readSecret: func(string) (string, error) { return "same-fake-value", nil }}
		c := doctor.Cmd{Argv: []string{"/bin/sh", "-c", script, "sh", count}, SecretPrompt: "pw", SecretConfirm: true}
		err := h.Run(context.Background(), c)
		n, _ := os.ReadFile(count) //nolint:gosec // a path the test made
		if strings.Contains(b.String(), "same-fake-value") {
			t.Errorf("the secret reached the captured output: %q", b.String())
		}
		return strings.Count(string(n), "x"), b.String(), err
	}
	// first call rejects with 5402, the second succeeds
	n, out, err := run(`echo x >> "$1"; if [ "$(wc -l < "$1")" -lt 2 ]; then echo "New account password error. (5402)" >&2; exit 1; fi`)
	if err != nil || n != 2 || !strings.Contains(out, "password policy") {
		t.Errorf("want a retry then success: n=%d err=%v out=%q", n, err, out)
	}
	// always rejected: three tries, then a fatal stop
	n, _, err = run(`echo x >> "$1"; echo "(5402)" >&2; exit 1`)
	var f fatalError
	if n != 3 || !errors.As(err, &f) || !errors.Is(err, ErrPasswordPolicy) {
		t.Errorf("want three tries and a fatal stop: n=%d err=%v", n, err)
	}
	// another failure: one try, not fatal
	n, _, err = run(`echo x >> "$1"; echo "some other error" >&2; exit 1`)
	if n != 1 || err == nil || errors.As(err, &f) {
		t.Errorf("another error must not retry: n=%d err=%v", n, err)
	}
}

func TestRunNewPasswordMismatchStopsAfterThree(t *testing.T) {
	i := 0
	h := Terminal{Err: &strings.Builder{}, readSecret: func(string) (string, error) { i++; return strings.Repeat("a", i), nil }}
	err := h.Run(context.Background(), doctor.Cmd{Argv: []string{"/bin/false"}, SecretPrompt: "pw", SecretConfirm: true})
	var f fatalError
	if !errors.As(err, &f) {
		t.Errorf("want a fatal stop, got %v", err)
	}
}

// The policy hint comes from `pwpolicy`; a fake one on PATH stands in. The
// hint is shown once before the first prompt and nothing breaks without it.
func TestRunNewPasswordShowsThePolicyHint(t *testing.T) {
	dir := t.TempDir()
	fake := "#!/bin/sh\necho 'Getting global account policies'\ncat <<'EOF'\n<?xml version=\"1.0\"?><plist><dict><key>policyCategoryPasswordContent</key><array><dict><key>policyContentDescription</key><dict><key>en</key><string>Four characters or more.&#x9b;31m</string></dict></dict></array></dict></plist>\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "pwpolicy"), []byte(fake), 0o700); err != nil { //nolint:gosec // a fake tool in a temp dir
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("LC_ALL", "en_US.UTF-8")
	var b strings.Builder
	h := Terminal{Err: &b, readSecret: func(string) (string, error) { return "same-fake-value", nil }}
	if err := h.Run(context.Background(), doctor.Cmd{Argv: []string{"/usr/bin/true"}, SecretPrompt: "pw", SecretConfirm: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(b.String(), "Four characters or more.") != 1 {
		t.Errorf("want the hint once: %q", b.String())
	}
	if strings.ContainsRune(b.String(), 0x9b) || !strings.Contains(b.String(), `\u009b`) {
		t.Errorf("the hint must be escaped: %q", b.String())
	}
}

// Issue #379: the run log gets the command, its exit code and the tool's output,
// but never the secret: not in the file, not in the argv column, and not the
// tool's own prompt for it.
func TestRunLogNeverHoldsTheSecret(t *testing.T) {
	var raw [24]byte
	_, _ = rand.Read(raw[:])
	marker := "fake-" + hex.EncodeToString(raw[:])
	lp := filepath.Join(t.TempDir(), "logs", "run.log")
	lg, err := runlog.Open(lp)
	if err != nil {
		t.Fatal(err)
	}
	// the child echoes what it read, as a careless tool would, and fails
	script := `printf 'User password:'; IFS= read -r l; printf '\n'; printf 'got %s\n' "$l"; echo oops >&2; exit 5`
	h := Terminal{Err: io.Discard, Log: lg, readSecret: func(string) (string, error) { return marker, nil }}
	c := doctor.Cmd{Argv: []string{"/bin/sh", "-c", script, "sh"}, SecretPrompt: "pw"}
	if err := h.Run(context.Background(), c); err == nil {
		t.Fatal("want the exit 5 failure")
	}
	_ = lg.Close()
	got := readFile(t, lp)
	if strings.Contains(got, marker) || strings.Contains(lg.Tail(20), marker) {
		t.Fatalf("the secret reached the log: %q", got)
	}
	for _, want := range []string{"$ /bin/sh -c", "exit 5", "oops", "got ***"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got[strings.Index(got, "tool output:"):], "User password") {
		t.Errorf("the tool's secret prompt reached the log: %q", got)
	}
	if lg.Tail(2) == "" {
		t.Error("no tail for the failure summary")
	}
}

func TestOutputIsLogged(t *testing.T) {
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, _ := runlog.Open(lp)
	h := Terminal{Err: io.Discard, Log: lg}
	_, _ = h.Output(context.Background(), "/bin/sh", "-c", "echo seen; exit 2")
	_ = lg.Close()
	if got := readFile(t, lp); !strings.Contains(got, "exit 2") || !strings.Contains(got, "seen") {
		t.Fatalf("log %q", got)
	}
}

// Ctrl-\ during a secret command must not end whr (and leave echo off): the
// signal is caught while the command runs.
func TestRunOnceSurvivesSIGQUITDuringASecretCommand(t *testing.T) {
	var b strings.Builder
	h := Terminal{Err: &b}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGQUIT)
	}()
	err := h.runOnce(context.Background(), doctor.Cmd{Argv: []string{"/bin/sleep", "2"}, SecretPrompt: "pw"}, "marker-value", nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutputLogEscapesControlCharacters(t *testing.T) {
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, _ := runlog.Open(lp)
	h := Terminal{Err: io.Discard, Log: lg}
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	marker := "M" + hex.EncodeToString(raw[:])
	_, _ = h.Output(context.Background(), "/bin/sh", "-c", `printf '\033[31m`+marker+`\033]0;x\007\r\nsecond\n'`)
	_ = lg.Close()
	got := readFile(t, lp)
	if !strings.Contains(got, marker) || !strings.Contains(got, "second") {
		t.Fatalf("output lost: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\x07\r") {
		t.Errorf("raw control character in the log: %q", got)
	}
}

// The secret is masked before the log text is escaped: a secret with a control
// character the escaper rewrites (DEL here) must not reach the log. The value is
// random at test time, never a real password.
func TestRunOnceMasksASecretWithControlCharactersInTheLog(t *testing.T) {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	head, tail := "S"+hex.EncodeToString(raw[:]), "T"+hex.EncodeToString(raw[:4])
	secret := head + "\x7f" + tail
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, err := runlog.Open(lp)
	if err != nil {
		t.Fatal(err)
	}
	h := Terminal{Err: io.Discard, Log: lg}
	// the tool echoes the secret it read on stdin
	c := doctor.Cmd{Argv: []string{"/bin/sh", "-c", `IFS= read -r l; printf 'got %s\n' "$l"`}, SecretPrompt: "pw"}
	if err := h.runOnce(context.Background(), c, secret, nil); err != nil {
		t.Fatal(err)
	}
	_ = lg.Close()
	got := readFile(t, lp)
	if !strings.Contains(got, "got ***") {
		t.Errorf("the secret was not masked: %q", got)
	}
	for _, part := range []string{head, tail} {
		if strings.Contains(got, part) {
			t.Errorf("part of the secret reached the log: %q", got)
		}
	}
}

// --verbose: the output of a command is printed live by the Terminal and the
// log streams only the argv and the exit code, so the output shows once; the
// log file still holds it.
func TestVerboseShowsCommandOutputOnce(t *testing.T) {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	marker := "v" + hex.EncodeToString(raw[:])
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, err := runlog.Open(lp)
	if err != nil {
		t.Fatal(err)
	}
	var both strings.Builder
	lg.Stream = &both
	h := Terminal{Err: &both, Log: lg}
	// the marker is an argument, so the argv line holds it too: count the
	// lines that are the output itself
	if err := h.Run(context.Background(), doctor.Cmd{Argv: []string{"/bin/sh", "-c", `echo "out-$1"`, "sh", marker}}); err != nil {
		t.Fatal(err)
	}
	_ = lg.Close()
	if n := strings.Count(both.String(), "out-"+marker); n != 1 {
		t.Errorf("output shown %d times, want once:\n%s", n, both.String())
	}
	if !strings.Contains(both.String(), "exit 0") {
		t.Errorf("the stream lacks the exit code:\n%s", both.String())
	}
	if !strings.Contains(readFile(t, lp), "out-"+marker) {
		t.Error("the log file lacks the output")
	}
}
