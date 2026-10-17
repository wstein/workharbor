// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin

package ptytest_test

import (
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
		{ptytest.KeyCtrlD, "EOT"},
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
