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

func TestOpenRefusesASymlinkToAOneNameFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(dir, "sym.log")
	if err := os.Symlink(target, sym); err != nil {
		t.Fatal(err)
	}
	// the target has one name, so only O_NOFOLLOW can refuse this
	if l, err := Open(sym); err == nil {
		_ = l.Close()
		t.Fatal("a symlink was followed")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" { //nolint:gosec // a test path
		t.Errorf("target changed: %q", b)
	}
}

func TestOpenRefusesADevice(t *testing.T) {
	l, err := Open("/dev/null")
	if err == nil {
		_ = l.Close()
		t.Fatal("/dev/null was opened as a log")
	}
	if !strings.Contains(err.Error(), "plain file") {
		t.Errorf("refused for another reason than the file type: %v", err)
	}
}

func TestOwnerOKRefusesRootOnAForeignFile(t *testing.T) {
	for _, c := range []struct {
		file, euid uint32
		want       bool
	}{{0, 0, true}, {1000, 0, false}, {1000, 1000, true}, {0, 1000, true}} {
		if got := ownerOK(c.file, c.euid); got != c.want {
			t.Errorf("ownerOK(%d, %d) = %v, want %v", c.file, c.euid, got, c.want)
		}
	}
}

func TestOpenAcceptsAnOwnFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "own.log")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}

func TestOpenDefaultTightensAWiderLogsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // a wide mode is the point
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // a wide mode is the point
		t.Fatal(err)
	}
	l, err := OpenDefault(filepath.Join(dir, "setup-x.log"))
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("logs dir mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestOpenLeavesAnExplicitDirAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // a wide mode is the point
		t.Fatal(err)
	}
	l, err := Open(filepath.Join(dir, "x.log"))
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o755 {
		t.Errorf("an explicit --log-file's directory changed to %v", fi.Mode().Perm())
	}
}

func TestOpenDefaultKeepsTwoRunsOfTheSameSecondApart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "logs", "setup-20261007T101500Z.log")
	var paths []string
	for i := 0; i < 3; i++ {
		l, err := OpenDefault(p)
		if err != nil {
			t.Fatal(err)
		}
		l.Step("n", "ok", "run "+string(rune('a'+i)))
		paths = append(paths, l.Path())
		_ = l.Close()
	}
	want := []string{p, strings.TrimSuffix(p, ".log") + "-1.log", strings.TrimSuffix(p, ".log") + "-2.log"}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths %v, want %v", paths, want)
		}
		b, _ := os.ReadFile(want[i]) //nolint:gosec // a test path
		if exp := "step n: ok run " + string(rune('a'+i)) + "\n"; string(b) != exp {
			t.Errorf("%s holds %q, want %q", want[i], b, exp)
		}
	}
}

func TestResetOutputClearsTheTail(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "x.log"))
	defer func() { _ = l.Close() }()
	l.Command([]string{"t"}, 1, "old", "")
	l.ResetOutput()
	if got := l.Tail(5); got != "" {
		t.Errorf("tail after reset %q", got)
	}
	var nilLog *Log
	nilLog.ResetOutput()
}
