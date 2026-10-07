package runlog

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPathIsUnderTheStateDirPerRun(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 15, 0, 0, time.UTC)
	got := Path("", "/home/u", "setup", now)
	if want := "/home/u/.local/state/whr/logs/setup-20261007T101500Z.log"; got != want {
		t.Fatalf("path %q, want %q", got, want)
	}
	if got2 := Path("/s", "/home/u", "doctor", now); got2 != "/s/logs/doctor-20261007T101500Z.log" {
		t.Fatalf("state_dir ignored: %q", got2)
	}
}

func TestOpenModes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "logs", "x.log")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	for path, want := range map[string]os.FileMode{p: 0o600, filepath.Dir(p): 0o700} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode %v, want %v", path, fi.Mode().Perm(), want)
		}
	}
}

func TestCommandStepAndTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	l, _ := Open(p)
	var stream strings.Builder
	l.Stream = &stream
	l.Command([]string{"tool", "-x"}, 3, "a\nb\n\nc\n", "")
	l.Step("account", "fail", "could   not\ncreate")
	_ = l.Close()
	b, _ := os.ReadFile(p) //nolint:gosec // a test path
	if want := "$ tool -x\nexit 3\na\nb\n\nc\nstep account: fail could not create\n"; string(b) != want {
		t.Fatalf("log %q", b)
	}
	if stream.String() != string(b) {
		t.Errorf("stream differs from file")
	}
	if got := l.Tail(2); got != "b\nc" {
		t.Errorf("tail %q", got)
	}
}

func TestNilLogDoesNothing(t *testing.T) {
	var l *Log
	l.Command(nil, 0, "x", "")
	l.Step("a", "ok", "")
	if l.Path() != "" || l.Tail(3) != "" || l.Close() != nil {
		t.Fatal("nil log not inert")
	}
}

func TestCommandMasksTheSecretValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	l, _ := Open(p)
	l.Command([]string{"t"}, 1, "echoed MARKER-1234 here", "MARKER-1234")
	_ = l.Close()
	b, _ := os.ReadFile(p) //nolint:gosec // a test path
	if strings.Contains(string(b), "MARKER-1234") || strings.Contains(l.Tail(5), "MARKER-1234") {
		t.Fatal("secret in log")
	}
}

func TestOpenRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil { //nolint:gosec // a loose mode is the point
		t.Fatal(err)
	}
	sym := filepath.Join(dir, "sym.log")
	if err := os.Symlink(target, sym); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard.log")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sym, hard} {
		if l, err := Open(p); err == nil {
			_ = l.Close()
			t.Errorf("%s: want a refusal", p)
		}
	}
	fi, _ := os.Stat(target)
	if b, _ := os.ReadFile(target); string(b) != "keep" || fi.Mode().Perm() != 0o644 { //nolint:gosec // a test path
		t.Errorf("target changed: %q %v", b, fi.Mode())
	}
}

func TestOpenFixesModeAndAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("old\n"), 0o644); err != nil { //nolint:gosec // a loose mode is the point
		t.Fatal(err)
	}
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l.Step("a", "ok", "")
	_ = l.Close()
	fi, _ := os.Stat(p)
	b, _ := os.ReadFile(p) //nolint:gosec // a test path
	if fi.Mode().Perm() != 0o600 || string(b) != "old\nstep a: ok \n" {
		t.Errorf("mode %v content %q", fi.Mode().Perm(), b)
	}
}

func TestCommandAnswerShowsTheAnswerNotARawExit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	l, _ := Open(p)
	l.CommandAnswer([]string{"dseditgroup", "-o", "checkmember"}, 67, "no u is NOT a member of admin\n", "", "not a member")
	_ = l.Close()
	b, _ := os.ReadFile(p) //nolint:gosec // a test path
	if want := "$ dseditgroup -o checkmember\nanswer: not a member (exit 67)\nno u is NOT a member of admin\n"; string(b) != want {
		t.Fatalf("log %q", b)
	}
}

func TestOpenDoesNotHangOnAFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo.log")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("no mkfifo:", err)
	}
	// a reader makes the non-blocking open succeed, so the regular-file check
	// is what refuses; without a reader the open itself fails (ENXIO)
	for _, withReader := range []bool{false, true} {
		if withReader {
			r, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // a test path
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
		}
		done := make(chan error, 1)
		go func() {
			l, err := Open(p)
			if err == nil {
				_ = l.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("reader=%v: want a refusal for a FIFO", withReader)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("reader=%v: Open hangs on a FIFO", withReader)
		}
	}
}
