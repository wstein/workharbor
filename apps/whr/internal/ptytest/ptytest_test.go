// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin

package ptytest_test

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
)

func TestMain(m *testing.M) {
	ptytest.Main(m, fixtures)
}

const short = 100 * time.Millisecond

func TestPTYDrivesPromptAndCapturesTranscript(t *testing.T) {
	s := ptytest.StartFunc(t, "echo", ptytest.Options{})
	s.Expect("Name: ")
	// Backspace is interpreted by the line discipline (line editing).
	s.Send("Werx")
	s.SendKey(ptytest.KeyBackspace)
	s.Send("ner")
	s.SendKey(ptytest.KeyEnter)
	s.Expect("Hello, Werner")
	s.ExpectExit(0)
	if got := s.Screen(); !strings.Contains(got, "Name: Werner\nHello, Werner") {
		t.Fatalf("screen = %q", got)
	}
	if !strings.Contains(string(s.Transcript()), "Name: ") {
		t.Fatalf("transcript = %q", s.Transcript())
	}
}

func TestPTYKeysAndEnvironment(t *testing.T) {
	s := ptytest.StartFunc(t, "keys", ptytest.Options{})
	s.Expect("ready")
	for _, k := range []struct {
		key  ptytest.Key
		name string
	}{
		{ptytest.KeyUp, "UP"}, {ptytest.KeyDown, "DOWN"}, {ptytest.KeyLeft, "LEFT"},
		{ptytest.KeyRight, "RIGHT"}, {ptytest.KeyEnter, "ENTER"},
		{ptytest.KeyBackspace, "BACKSPACE"}, {ptytest.KeyEscape, "ESC"},
		{ptytest.KeyCtrlD, "EOT"}, {ptytest.KeyDelete, "DELETE"}, {ptytest.KeyHome, "HOME"},
		{ptytest.KeyEnd, "END"}, {ptytest.KeyTab, "TAB"},
	} {
		s.SendKey(k.key)
		s.Expect("key=" + k.name)
	}
	s.Send("q")
	s.ExpectExit(0)

	e := ptytest.StartFunc(t, "env", ptytest.Options{Rows: 30, Cols: 100, Term: "vt100", NoColor: true})
	e.Expect("size=30x100")
	e.Expect("TERM=vt100")
	e.Expect("NO_COLOR=1")
	e.Expect("LC_ALL=C")
	e.Expect("HOME=" + e.Home())
	e.ExpectExit(0)
}

func TestPTYResizeAndDefaults(t *testing.T) {
	s := ptytest.StartFunc(t, "winch", ptytest.Options{})
	s.Expect("size=24x80")
	s.Resize(40, 120)
	s.Expect("size=40x120")
	s.ExpectExit(0)
}

func TestPTYInterruptDelivered(t *testing.T) {
	s := ptytest.StartFunc(t, "ctrlc72", ptytest.Options{})
	s.Expect("ready")
	s.SendKey(ptytest.KeyCtrlC)
	s.Expect("interrupted")
	s.ExpectExit(72)
}

func TestPTYBackAndCancelPrompt(t *testing.T) {
	s := ptytest.StartFunc(t, "backcancel", ptytest.Options{})
	s.Expect("step 1> ")
	s.SendKey(ptytest.KeyEnter)
	s.Expect("step 2> ")
	s.SendKey(ptytest.KeyLeft)
	s.Expect("step 1> ")
	s.SendKey(ptytest.KeyEnter)
	s.Expect("step 2> ")
	s.SendKey(ptytest.KeyEnter)
	s.Expect("done")
	s.ExpectExit(0)
}

func TestPTYUnexpectedExitStatusFailsTest(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "ctrlc72", ptytest.Options{Timeout: 5 * time.Second})
		s.Expect("ready")
		s.SendKey(ptytest.KeyCtrlC)
		s.ExpectExit(0)
	})
	f.wantFatal("exit status 72")
	f.cleanup()
}

func TestPTYTimeoutFailsTest(t *testing.T) {
	f := newFakeTB(t)
	var pid int
	f.run(func() {
		s := ptytest.StartFunc(f, "hang", ptytest.Options{Timeout: 5 * time.Second})
		pid = s.Pid()
		s.Expect("up")
		s.ExpectWithin(short, "never printed")
	})
	f.wantFatal("timed out")
	f.wantFatal("never printed")
	f.wantFatal("up") // the failure message carries the transcript
	f.cleanup()
	waitGone(t, pid)

	// ExpectExit on a process that never exits also times out.
	g := newFakeTB(t)
	g.run(func() {
		s := ptytest.StartFunc(g, "hang", ptytest.Options{})
		s.Expect("up")
		s.ExpectExitWithin(short, 0)
	})
	g.wantFatal("did not exit")
	g.cleanup()
}

func TestPTYNoLeakedProcess(t *testing.T) {
	f := newFakeTB(t)
	var leader, child int
	f.run(func() {
		s := ptytest.StartFunc(f, "spawn", ptytest.Options{})
		leader = s.Pid()
		line := s.ExpectRegexp(`child=(\d+)`)
		child, _ = strconv.Atoi(line[1])
	})
	if child == 0 {
		t.Fatal("grandchild pid not seen")
	}
	if err := syscall.Kill(child, 0); err != nil {
		t.Fatalf("grandchild should be alive before cleanup: %v", err)
	}
	f.cleanup()
	waitGone(t, leader)
	waitGone(t, child)
}

func TestPTYHostileOutputDetected(t *testing.T) {
	hostile := []string{"\x1b]0;pwned\x07", "\x1b[2J", "\u009b31m", "\x07"}

	raw := newFakeTB(t)
	raw.run(func() {
		s := ptytest.StartFunc(raw, "hostile-raw", ptytest.Options{})
		s.Expect("end")
		s.ExpectExit(0)
		s.AssertNoRawControls()
	})
	raw.wantFatal("raw control")
	raw.cleanup()

	safe := ptytest.StartFunc(t, "hostile-safe", ptytest.Options{})
	safe.Expect("end")
	safe.ExpectExit(0)
	safe.AssertNoRawControls()
	for _, h := range hostile {
		if strings.Contains(string(safe.Transcript()), h) {
			t.Fatalf("hostile sequence %q reached the terminal", h)
		}
	}
	if !strings.Contains(safe.Screen(), "<U+001B>]0;pwned") {
		t.Fatalf("escaped text missing from screen: %q", safe.Screen())
	}

	// Sequences the program legitimately emits can be allowed explicitly.
	ok := ptytest.StartFunc(t, "colored", ptytest.Options{})
	ok.Expect("green")
	ok.ExpectExit(0)
	ok.AssertNoRawControls("\x1b[32m", "\x1b[0m")
	if strings.Contains(ok.Screen(), "\x1b") {
		t.Fatalf("screen must be rendered text: %q", ok.Screen())
	}
}

func TestPTYScreenRendering(t *testing.T) {
	s := ptytest.StartFunc(t, "redraw", ptytest.Options{})
	s.Expect("final")
	s.ExpectExit(0)
	if got, want := s.Screen(), "progress 100%\nfinal"; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
}

func TestKeyBytes(t *testing.T) {
	for k, want := range map[ptytest.Key]string{
		ptytest.KeyEnter: "\r", ptytest.KeyTab: "\t", ptytest.KeyBackspace: "\x7f",
		ptytest.KeyEscape: "\x1b", ptytest.KeyCtrlC: "\x03", ptytest.KeyCtrlD: "\x04",
		ptytest.KeyUp: "\x1b[A", ptytest.KeyDown: "\x1b[B", ptytest.KeyRight: "\x1b[C",
		ptytest.KeyLeft: "\x1b[D", ptytest.KeyHome: "\x1b[H", ptytest.KeyEnd: "\x1b[F",
		ptytest.KeyDelete: "\x1b[3~",
	} {
		if string(k) != want {
			t.Errorf("key %q != %q", string(k), want)
		}
	}
}

func envOf(t *testing.T, s *ptytest.Session) map[string]string {
	t.Helper()
	s.ExpectExit(0)
	got := map[string]string{}
	for _, l := range strings.Split(s.Screen(), "\n") {
		if kv, ok := strings.CutPrefix(l, "ENV:"); ok {
			k, v, _ := strings.Cut(kv, "=")
			got[k] = v
		}
	}
	return got
}

func TestPTYChildEnvironmentIsExactAndIsolated(t *testing.T) {
	t.Setenv("PTYTEST_PARENT_SECRET", "leak")
	t.Setenv("HOME", "/home/real-user")
	t.Setenv("NO_COLOR", "")
	t.Setenv("GOCOVERDIR", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	s := ptytest.StartFunc(t, "envdump", ptytest.Options{Cols: 200, Env: []string{"EXTRA=1", "TZ=Europe/Berlin"}})
	got := envOf(t, s)
	root := filepath.Dir(s.Home())
	want := map[string]string{
		"PATH": "/usr/bin:/bin", "HOME": s.Home(), "TMPDIR": filepath.Join(root, "tmp"),
		"XDG_CONFIG_HOME": filepath.Join(s.Home(), ".config"), "XDG_CACHE_HOME": filepath.Join(s.Home(), ".cache"),
		"XDG_DATA_HOME": filepath.Join(s.Home(), ".local", "share"), "XDG_STATE_HOME": filepath.Join(s.Home(), ".local", "state"),
		"LC_ALL": "C", "LANG": "C", "TZ": "Europe/Berlin", "GORACE": "atexit_sleep_ms=0",
		"TERM": "xterm-256color", "PTYTEST_FUNC": "envdump", "EXTRA": "1",
	}
	if len(got) != len(want) {
		t.Errorf("env = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if s.Home() == "/home/real-user" {
		t.Fatal("real HOME used")
	}
}

func TestPTYEnvironmentOptionalVariables(t *testing.T) {
	t.Setenv("GOCOVERDIR", "/cover")
	got := envOf(t, ptytest.StartFunc(t, "envdump", ptytest.Options{NoColor: true, Term: "dumb", Cols: 200}))
	if got["GOCOVERDIR"] != "/cover" || got["NO_COLOR"] != "1" || got["TERM"] != "dumb" {
		t.Fatalf("env = %v", got)
	}
}

func TestPTYDefaultAndCustomWorkingDirectory(t *testing.T) {
	s := ptytest.StartFunc(t, "pwd", ptytest.Options{Cols: 200})
	want, _ := filepath.EvalSymlinks(filepath.Join(filepath.Dir(s.Home()), "work"))
	s.Expect("PWD=" + want)
	s.ExpectExit(0)
	dir := t.TempDir()
	want, _ = filepath.EvalSymlinks(dir)
	c := ptytest.StartFunc(t, "pwd", ptytest.Options{Dir: dir, Cols: 200})
	c.Expect("PWD=" + want)
	c.ExpectExit(0)
}

func emit(t testing.TB, b string, o ptytest.Options) *ptytest.Session {
	o.Args = []string{hex.EncodeToString([]byte(b))}
	return ptytest.StartFunc(t, "emit", o)
}

func TestAssertNoRawControlsClasses(t *testing.T) {
	for _, c := range []struct {
		name    string
		out     string
		allowed []string
		bad     bool
	}{
		{"plain text, newline, tab", "ok\ttext\r\nmore\n", nil, false},
		{"unicode text", "gr\u00fc\u00dfe \u4e2d", nil, false},
		{"lone ESC", "a\x1b", nil, true},
		{"CSI", "a\x1b[31mb", nil, true},
		{"OSC title", "\x1b]0;x\x07", nil, true},
		{"BEL", "a\ab", nil, true},
		{"NUL", "a\x00b", nil, true},
		{"DEL", "a\x7fb", nil, true},
		{"backspace", "a\bb", nil, true},
		{"C1 CSI", "a\u009b31m", nil, true},
		{"bidi override", "a\u202eb", nil, true},
		{"line separator", "a\u2028b", nil, true},
		{"invalid UTF-8 byte", "a\xffb", nil, true},
		{"cut-off multibyte rune at the end", "ok\xe2\x82", nil, false},
		{"cut-off rune in the middle", "a\xe2\x82b", nil, true},
		{"allowed color codes", "\x1b[32mgreen\x1b[0m", []string{"\x1b[32m", "\x1b[0m"}, false},
		{"allowed does not cover other sequences", "\x1b[32mx\x1b[31m", []string{"\x1b[32m"}, true},
		{"allowed literal ending inside a token", "\x1b[32m", []string{"\x1b[3"}, true},
		{"allowed literal starting inside a token", "\x1b[32m", []string{"[32m"}, true},
		{"allowed literals spanning tokens", "\x1b[1mbold\x1b[0m", []string{"\x1b[1mbold\x1b[0m"}, false},
		{"neighbours of an allowed literal stay checked", "\x1b[\x1b[0m0m", []string{"\x1b[0m"}, true},
	} {
		f := newFakeTB(t)
		f.run(func() {
			s := emit(f, c.out, ptytest.Options{})
			s.AssertNoRawControls(c.allowed...)
		})
		f.cleanup()
		if got := len(f.fatals) > 0; got != c.bad {
			t.Errorf("%s: failed=%v (%q), want %v", c.name, got, f.fatals, c.bad)
		}
	}
}

func TestAssertNoRawControlsWaitsForOutputEnd(t *testing.T) {
	// The hostile text arrives after a pause; asserting before ExpectExit must
	// not pass on the incomplete transcript.
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "late", ptytest.Options{})
		s.Expect("first")
		s.Send("\n") // the hostile text is written only now
		s.AssertNoRawControls()
	})
	f.wantFatal("raw control")
	f.cleanup()

	g := newFakeTB(t)
	g.run(func() {
		s := ptytest.StartFunc(g, "hang", ptytest.Options{Timeout: short})
		s.Expect("up")
		s.AssertNoRawControls()
	})
	g.wantFatal("call ExpectExit first")
	g.cleanup()
}

func TestScreenRendersEraseAndCursorMovement(t *testing.T) {
	s := emit(t, "abcdef\r\x1b[3C\x1b[K\nsecond\x1b[1A\rX", ptytest.Options{})
	s.ExpectExit(0)
	if got, want := s.Screen(), "Xbc\nsecond"; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
}

func TestExpectScreenDeadline(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "hang", ptytest.Options{Timeout: short})
		s.ExpectScreen("up")
		s.ExpectScreen("never")
	})
	f.wantFatal("timed out")
	f.wantFatal("never")
	f.cleanup()

	g := newFakeTB(t)
	g.run(func() {
		s := ptytest.StartFunc(g, "quiet", ptytest.Options{})
		s.ExpectScreen("never")
	})
	g.wantFatal("output ended")
	g.cleanup()

	h := ptytest.StartFunc(t, "quiet", ptytest.Options{})
	h.ExpectScreen("bye")
	h.ExpectScreenWithin(short, "bye") // consumes nothing
	h.ExpectExit(0)
}

func TestExpectFailsFastWhenOutputEnds(t *testing.T) {
	f := newFakeTB(t)
	start := time.Now()
	f.run(func() {
		s := ptytest.StartFunc(f, "quiet", ptytest.Options{Timeout: time.Minute})
		s.Expect("never printed")
	})
	f.wantFatal("output ended before")
	f.cleanup()
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("took %v; must not wait for the timeout", d)
	}
}

func TestExpectConsumesThroughTheMatch(t *testing.T) {
	s := emit(t, "alpha\nbeta\ngamma\n", ptytest.Options{})
	s.Expect("alpha")
	s.Expect("beta")
	s.Expect("gamma")
	s.ExpectExit(0)
	// alpha was consumed: it cannot match again.
	g := newFakeTB(t)
	g.run(func() {
		x := emit(g, "alpha\nbeta\n", ptytest.Options{})
		x.Expect("beta")
		x.ExpectWithin(short, "alpha")
	})
	g.wantFatal("alpha")
	g.cleanup()
}

func TestExpectRegexpSubmatches(t *testing.T) {
	s := emit(t, "id=ab-end\n", ptytest.Options{})
	got := s.ExpectRegexp(`id=(a)(x)?(b)`)
	if len(got) != 4 || got[0] != "id=ab" || got[1] != "a" || got[2] != "" || got[3] != "b" {
		t.Fatalf("submatches = %q", got)
	}
	s.Expect("-end") // consumed exactly through the match
	s.ExpectExit(0)
}

func TestExpectHandlesSplitSequencesAndRunes(t *testing.T) {
	s := emit(t, "gr\xc3\xbc\xc3\x9fe \x1b[1mbold\x1b[0m\n", ptytest.Options{})
	s.Expect("gr\u00fc\u00dfe bold")
	s.ExpectExit(0)
}

// TestLargeOutputIsMatchedLinearly checks the work done, not the wall clock:
// every byte is projected once, drawn once and searched a bounded number of
// times, so work stays within a constant factor of the output size.
func TestLargeOutputIsMatchedLinearly(t *testing.T) {
	for _, mib := range []int{1, 4, 10} {
		size := mib << 20
		s := ptytest.StartFunc(t, "flood", ptytest.Options{Args: []string{strconv.Itoa(size)}, Timeout: 120 * time.Second})
		s.Expect("END")
		s.ExpectExit(0)
		out := len(s.Transcript())
		if out < size {
			t.Fatalf("transcript has %d bytes, want >= %d", out, size)
		}
		if w := s.Work(); w > uint64(6*out) {
			t.Fatalf("%d MiB: work %d bytes for %d bytes of output: not linear", mib, w, out)
		}
		r := ptytest.StartFunc(t, "flood", ptytest.Options{Args: []string{strconv.Itoa(size)}, Timeout: 120 * time.Second})
		r.ExpectScreen("END")
		r.ExpectExit(0)
		if w := r.Work(); w > uint64(6*len(r.Transcript())) {
			t.Fatalf("%d MiB ExpectScreen: work %d for %d bytes", mib, w, len(r.Transcript()))
		}
	}
}

// TestUnterminatedSequenceFloodIsMatchedLinearly: a never-terminated OSC, DCS,
// APC or CSI must not be rescanned from its start on every chunk, and
// whatever follows an unterminated string sequence belongs to it (a terminal
// swallows it too), so nothing of the flood or of END may become visible.
func TestUnterminatedSequenceFloodIsMatchedLinearly(t *testing.T) {
	for _, seq := range []string{"\x1b]0;", "\x1bP", "\x1b_", "\x1b[1;", "\x1b[?"} {
		s := ptytest.StartFunc(t, "floodseq", ptytest.Options{
			Args: []string{hex.EncodeToString([]byte(seq)), strconv.Itoa(4 << 20)}, Timeout: 120 * time.Second})
		if strings.HasPrefix(seq, "\x1b[") {
			// A newline is no CSI final byte: it ends the dropped sequence.
			s.ExpectRegexp(`\A\nEND`)
		} else if s.ExpectExit(0); s.Screen() != "" {
			t.Fatalf("%q: text visible after an unterminated string sequence: %.40q", seq, s.Screen())
		}
		s.ExpectExit(0)
		if w, out := s.Work(), len(s.Transcript()); w > uint64(6*out) {
			t.Fatalf("%q: work %d bytes for %d bytes of output: not linear", seq, w, out)
		}
	}
}

// TestTerminatedSequenceFloodAcrossReads: the same floods, ended by CAN (or a
// final byte for CSI), are skipped across many reads and END is then visible
// text with nothing of the flood before it.
func TestTerminatedSequenceFloodAcrossReads(t *testing.T) {
	for _, c := range []struct{ seq, end string }{
		{"\x1b]0;", "\x18"}, {"\x1b]0;", "\x07"}, {"\x1b]0;", "\x1b\\"}, {"\x1b]0;", "\x1a"},
		{"\x1b]0;", "\xc2\x9c"}, {"\x1bP", "\x18"}, {"\x1b_", "\x07"},
		{"\x1b[1;", "m"}, {"\x1b[?", "h"},
	} {
		s := ptytest.StartFunc(t, "floodseq", ptytest.Options{
			Args:    []string{hex.EncodeToString([]byte(c.seq)), strconv.Itoa(4 << 20), hex.EncodeToString([]byte(c.end))},
			Timeout: 120 * time.Second})
		s.ExpectRegexp(`\A\nEND`) // fails if any filler became visible
		s.ExpectExit(0)
		if w, out := s.Work(), len(s.Transcript()); w > uint64(6*out) {
			t.Fatalf("%q+%q: work %d for %d bytes: not linear", c.seq, c.end, w, out)
		}
	}
}

func TestFailureMessagesAreBounded(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "flood", ptytest.Options{Args: []string{strconv.Itoa(1 << 20)}})
		s.Expect("never printed")
	})
	f.cleanup()
	if len(f.fatals) != 1 || len(f.fatals[0]) > 10000 {
		t.Fatalf("message of %d bytes", len(f.fatals[0]))
	}
}

func TestMaxOutputFailsTheTest(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "flood", ptytest.Options{Args: []string{strconv.Itoa(2 << 20)}, MaxOutput: 64 << 10})
		s.Expect("never printed")
	})
	f.wantFatal("exceeded MaxOutput")
	f.cleanup()
	g := newFakeTB(t)
	g.run(func() {
		s := ptytest.StartFunc(g, "flood", ptytest.Options{Args: []string{strconv.Itoa(2 << 20)}, MaxOutput: 64 << 10})
		s.ExpectExit(0)
	})
	g.wantFatal("exceeded MaxOutput")
	g.cleanup()
	h := newFakeTB(t)
	h.run(func() {
		s := ptytest.StartFunc(h, "flood", ptytest.Options{Args: []string{strconv.Itoa(2 << 20)}, MaxOutput: 64 << 10})
		s.ExpectScreen("never printed")
	})
	h.wantFatal("exceeded MaxOutput")
	h.cleanup()
}

func TestExpectExitReportsSignalDeath(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "selfkill", ptytest.Options{})
		s.ExpectExit(0)
	})
	f.wantFatal("killed by signal")
	f.wantFatal("killed")
	f.cleanup()
}

func TestCloseSemantics(t *testing.T) {
	f := newFakeTB(t)
	var s *ptytest.Session
	f.run(func() {
		s = ptytest.StartFunc(f, "hang", ptytest.Options{})
		s.Expect("up")
	})
	if len(f.cleanups) != 1 {
		t.Fatalf("%d cleanups registered, want 1", len(f.cleanups))
	}
	start := time.Now()
	guard(t, "Close", s.Close)
	waitGone(t, s.Pid())
	guard(t, "second Close", s.Close) // idempotent
	f.cleanup()
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestExitedLeaderIsNotSignalledAgain(t *testing.T) {
	var kills []int
	restore := ptytest.SetKiller(func(pid int) { kills = append(kills, pid) })
	defer restore()
	f := newFakeTB(t)
	var s *ptytest.Session
	f.run(func() {
		s = ptytest.StartFunc(f, "exitfast", ptytest.Options{})
		s.ExpectExit(3)
	})
	if len(kills) != 1 || kills[0] != s.Pid() {
		t.Fatalf("expected exactly one sweep at exit, got %v", kills)
	}
	guard(t, "Close", s.Close)
	f.cleanup()
	if len(kills) != 1 {
		t.Fatalf("group signalled after the leader was reaped: %v", kills)
	}
}

func TestProcessGone(t *testing.T) {
	if ptytest.ProcessGone(os.Getpid()) {
		t.Fatal("the running test process is not gone")
	}
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("SKIPPED, NOT PASSED: cannot start /bin/sleep: %v", err)
	}
	if ptytest.ProcessGone(cmd.Process.Pid) {
		t.Fatal("a sleeping child is not gone")
	}
	cmd.Process.Kill()
	// Killed but not yet reaped: a zombie counts as gone.
	deadline := time.Now().Add(5 * time.Second)
	for !ptytest.ProcessGone(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("zombie not reported as gone")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cmd.Wait()
	if !ptytest.ProcessGone(cmd.Process.Pid) {
		t.Fatal("reaped child must be gone")
	}
}

func TestDefaults(t *testing.T) {
	if ptytest.DefaultTimeout != 10*time.Second || ptytest.DefaultMaxOutput != 64<<20 {
		t.Fatal("documented defaults changed")
	}
}

func TestResizeChangesScreenWidth(t *testing.T) {
	s := ptytest.StartFunc(t, "winch-wide", ptytest.Options{})
	s.Expect("ready")
	s.Resize(24, 120)
	s.ExpectExit(0)
	if got := s.Screen(); got != "ready\n"+strings.Repeat("w", 100) {
		t.Fatalf("100 columns must fit on a 120 column screen: %q", got)
	}
}

// The next three tests fail fast (seconds, enforced by the fakeTB watchdog) when
// the matching deadline, the exit deadline or the cleanup kill is missing.

func TestPTYExpectDeadlineHonored(t *testing.T) {
	f := newFakeTB(t)
	start := time.Now()
	f.run(func() {
		s := ptytest.StartFunc(f, "hang", ptytest.Options{Timeout: 300 * time.Millisecond})
		s.Expect("up")
		s.Expect("never printed")
	})
	f.wantFatal("timed out after 300ms")
	f.cleanup()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("deadline ignored: %v", d)
	}
}

func TestPTYExpectExitDeadlineHonored(t *testing.T) {
	f := newFakeTB(t)
	start := time.Now()
	f.run(func() {
		s := ptytest.StartFunc(f, "hang", ptytest.Options{Timeout: 300 * time.Millisecond})
		s.Expect("up")
		s.ExpectExit(0)
	})
	f.wantFatal("did not exit within 300ms")
	f.cleanup()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("deadline ignored: %v", d)
	}
}

func TestPTYCleanupKillsRunningChild(t *testing.T) {
	f := newFakeTB(t)
	var pid int
	f.run(func() {
		s := ptytest.StartFunc(f, "hang", ptytest.Options{Timeout: 300 * time.Millisecond})
		pid = s.Pid()
		s.Expect("up")
	})
	if ptytest.ProcessGone(pid) {
		t.Fatal("child must still run before cleanup")
	}
	f.cleanup()
	waitGone(t, pid) // fails after 5 s if cleanup did not kill it
}

func TestOversizedWindowIsRefused(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() { ptytest.StartFunc(f, "quiet", ptytest.Options{Rows: 5000}) })
	f.wantFatal("exceeds")
	f.cleanup()
}

// TestExpectFindsNeedleCompletedByLaterOutput pins the rescan overlap: the
// text before the needle was already searched when the last byte arrives.
func TestExpectFindsNeedleCompletedByLaterOutput(t *testing.T) {
	f := newFakeTB(t)
	f.run(func() {
		s := ptytest.StartFunc(f, "split", ptytest.Options{})
		go func() {
			// Release the rest only after Expect searched "xxxxxxxxABC".
			deadline := time.Now().Add(10 * time.Second)
			for s.Pending() < len("xxxxxxxxABC") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			s.Send("\n")
		}()
		s.Expect("ABCD")
		s.ExpectExit(0)
	})
	if len(f.fatals) != 0 {
		t.Fatalf("needle completed by later output not found: %q", f.fatals)
	}
	f.cleanup()
}

func TestExpectRegexpWorkIsBounded(t *testing.T) {
	s := ptytest.StartFunc(t, "flood", ptytest.Options{Args: []string{strconv.Itoa(4 << 20)}, Timeout: 120 * time.Second})
	s.ExpectRegexp(`END`)
	s.ExpectExit(0)
	// Each wake-up searches at most the 64 KiB window, never the whole backlog.
	if w, out := s.Work(), len(s.Transcript()); w > uint64(40*out) {
		t.Fatalf("regexp work %d for %d bytes of output: window not applied", w, out)
	}
}

func TestPTYWorkCountsTheUnterminatedTail(t *testing.T) {
	s := ptytest.StartFunc(t, "partialosc", ptytest.Options{})
	deadline := time.Now().Add(10 * time.Second)
	for len(s.Transcript()) < 1005 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s.Screen() // advance over the output
	// The 1005 byte tail is looked at again by both the text and the screen.
	if w := s.Work(); w < 2000 {
		t.Fatalf("work %d: the unterminated tail is not counted", w)
	}
}

func TestResizeRefusesImpossibleSizes(t *testing.T) {
	for _, sz := range [][2]int{{0, 80}, {24, 0}, {-1, 80}, {24, -5}, {1001, 80}, {24, 70000}, {65536 + 24, 80}} {
		f := newFakeTB(t)
		f.run(func() {
			s := ptytest.StartFunc(f, "winch", ptytest.Options{})
			s.Expect("size=24x80")
			s.Resize(sz[0], sz[1])
		})
		f.wantFatal("resize")
		f.cleanup()
	}
	s := ptytest.StartFunc(t, "winch", ptytest.Options{})
	s.Expect("size=24x80")
	s.Resize(1000, 1000)
	s.Expect("size=1000x1000")
	s.ExpectExit(0)
}
