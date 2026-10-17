// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// TB is the subset of testing.TB the harness uses. Tests may pass a recording
// implementation to observe harness failures without failing themselves.
type TB interface {
	Helper()
	Cleanup(func())
	TempDir() string
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
}

// Supported reports whether the harness supports goos.
func Supported(goos string) bool { return goos == "linux" || goos == "darwin" }

// SkipUnlessSupported skips t, loudly, when the platform is unsupported and
// then returns false. A skipped test is never a pass.
func SkipUnlessSupported(t TB, supported bool, goos string) bool {
	t.Helper()
	if !supported {
		t.Skipf("SKIPPED, NOT PASSED: ptytest supports linux and darwin only, not %s", goos)
	}
	return supported
}

// Options configure a session. The zero value is usable.
type Options struct {
	Args    []string      // arguments after the program name
	Env     []string      // extra KEY=VALUE entries, applied last
	Dir     string        // working directory; default a private temp dir
	Rows    int           // default 24
	Cols    int           // default 80
	Term    string        // TERM; default xterm-256color
	NoColor bool          // sets NO_COLOR=1
	Timeout time.Duration // default deadline of Expect and ExpectExit; default DefaultTimeout
	// MaxOutput bounds the transcript in bytes; a child that writes more fails
	// the test instead of exhausting memory. Default DefaultMaxOutput.
	MaxOutput int
}

// maxWindow bounds Options.Rows and Options.Cols (and so the screen model).
const maxWindow = 1000

// Defaults of Options.
const (
	DefaultTimeout   = 10 * time.Second
	DefaultMaxOutput = 64 << 20
)

// ExitStatus is how the child ended.
type ExitStatus struct {
	Code     int            // exit code, -1 when killed by a signal
	Signal   syscall.Signal // set when killed by a signal
	Signaled bool
}

// Session is a running program attached to a pseudo-terminal.
type Session struct {
	t       TB
	opts    Options
	cmd     *exec.Cmd
	master  *os.File
	home    string
	pid     int
	exited  chan struct{} // closed when the process was reaped
	status  ExitStatus
	drained chan struct{} // closed when the reader reached end of output

	mu       sync.Mutex
	buf      []byte
	overflow bool
	changed  chan struct{}
	scanned  int    // raw offset projected into plain
	plain    []byte // visible text not yet consumed by Expect
	scr      *screen
	fed      int    // raw offset drawn on scr
	work     uint64 // bytes projected, drawn and searched; see export_test.go

	groupMu   sync.Mutex
	groupDead bool // the group was swept when the leader exited

	closeOnce sync.Once
}

// Start runs the program at path under a new PTY. It registers cleanup that
// kills the process group. On unsupported platforms it skips the test.
func Start(t TB, path string, opts Options) *Session {
	t.Helper()
	if !SkipUnlessSupported(t, platformSupported(), runtime.GOOS) {
		runtime.Goexit()
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Term == "" {
		opts.Term = "xterm-256color"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxOutput <= 0 {
		opts.MaxOutput = DefaultMaxOutput
	}
	if opts.Rows > maxWindow || opts.Cols > maxWindow {
		t.Fatalf("ptytest: window %dx%d exceeds %d rows or columns", opts.Rows, opts.Cols, maxWindow)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, d := range []string{home, filepath.Join(root, "work"), filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("ptytest: %v", err)
		}
	}
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("ptytest: allocate pseudo-terminal: %v", err)
	}
	if err := setWinsize(master, opts.Rows, opts.Cols); err != nil {
		master.Close()
		slave.Close()
		t.Fatalf("ptytest: set window size: %v", err)
	}
	cmd := exec.Command(path, opts.Args...)
	cmd.Dir = opts.Dir
	if cmd.Dir == "" {
		cmd.Dir = filepath.Join(root, "work")
	}
	cmd.Env = hermeticEnv(root, home, opts)
	if err := startProcess(cmd, slave); err != nil {
		master.Close()
		slave.Close()
		t.Fatalf("ptytest: start %s: %v", path, err)
	}
	slave.Close()
	s := &Session{
		t: t, opts: opts, cmd: cmd, master: master, home: home, pid: cmd.Process.Pid,
		exited: make(chan struct{}), drained: make(chan struct{}), changed: make(chan struct{}),
		scr: newScreen(opts.Cols, opts.Rows),
	}
	t.Cleanup(s.Close)
	go s.readLoop()
	go s.waitLoop()
	return s
}

// StartFunc runs the function registered under name in Main, by re-executing
// the current test binary under the PTY.
func StartFunc(t TB, name string, opts Options) *Session {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("ptytest: locate test binary: %v", err)
	}
	opts.Env = append([]string{funcEnv + "=" + name}, opts.Env...)
	return Start(t, exe, opts)
}

const funcEnv = "PTYTEST_FUNC"

// Main is called from TestMain. In the re-executed child it runs the function
// selected by StartFunc and exits with its result; in the test process it runs
// the tests.
func Main(m *testing.M, funcs map[string]func(args []string) int) {
	if name := os.Getenv(funcEnv); name != "" {
		f, ok := funcs[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "ptytest: unknown function %q\n", name)
			os.Exit(2)
		}
		os.Exit(f(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func hermeticEnv(root, home string, o Options) []string {
	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + home,
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"LC_ALL=C",
		"LANG=C",
		"TZ=UTC",
		"GORACE=atexit_sleep_ms=0", // no 1 s exit delay in race-enabled test binaries
		"TERM=" + o.Term,
	}
	if o.NoColor {
		env = append(env, "NO_COLOR=1")
	}
	if v := os.Getenv("GOCOVERDIR"); v != "" {
		env = append(env, "GOCOVERDIR="+v)
	}
	return append(env, o.Env...)
}

func (s *Session) readLoop() {
	defer close(s.drained)
	chunk := make([]byte, 32<<10)
	for {
		n, err := s.master.Read(chunk)
		if n > 0 {
			s.mu.Lock()
			if len(s.buf)+n > s.opts.MaxOutput {
				s.overflow = true // keep reading so the child can finish, but drop the data
			} else {
				s.buf = append(s.buf, chunk[:n]...)
			}
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		}
		if err != nil { // EIO once every slave descriptor closed, or closed master
			return
		}
	}
}

// killer signals the process group led by a pid; tests replace it.
var killer = killGroup

func (s *Session) waitLoop() {
	_ = s.cmd.Wait()
	// The leader is reaped, so its pid may be reused once the group is empty:
	// sweep the group now, exactly once, and never signal it again afterwards.
	s.groupMu.Lock()
	killer(s.pid)
	s.groupDead = true
	s.groupMu.Unlock()
	st := ExitStatus{Code: s.cmd.ProcessState.ExitCode()}
	if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		st.Signaled, st.Signal, st.Code = true, ws.Signal(), -1
	}
	s.status = st
	close(s.exited)
}

// Pid is the process id of the child (also its process group id).
func (s *Session) Pid() int { return s.pid }

// Home is the private HOME directory of the child.
func (s *Session) Home() string { return s.home }

// Send writes text to the terminal exactly as typed.
func (s *Session) Send(text string) {
	s.t.Helper()
	if _, err := s.master.WriteString(text); err != nil {
		s.t.Fatalf("ptytest: send %q: %v", text, err)
	}
}

// SendKey sends one keystroke.
func (s *Session) SendKey(k Key) {
	s.t.Helper()
	s.Send(string(k))
}

// Resize changes the window size; the child receives SIGWINCH.
func (s *Session) Resize(rows, cols int) {
	s.t.Helper()
	if err := setWinsize(s.master, rows, cols); err != nil {
		s.t.Fatalf("ptytest: resize: %v", err)
	}
	s.mu.Lock()
	s.scr.resize(cols, rows)
	s.mu.Unlock()
}

// Transcript returns every byte the child wrote to the terminal so far.
func (s *Session) Transcript() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf...)
}

// advanceLocked projects and draws the output received since the last call.
func (s *Session) advanceLocked() {
	p, _, n := project(s.buf[s.scanned:])
	s.plain = append(s.plain, p...)
	s.scanned += n
	d := s.scr.feed(s.buf[s.fed:])
	s.fed += d
	// Count the unterminated tail both consumers look at again next time.
	s.work += uint64(n+d) + uint64(len(s.buf)-s.scanned) + uint64(len(s.buf)-s.fed)
}

// Screen renders the transcript so far as text (see the package comment).
func (s *Session) Screen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()
	return s.scr.text()
}

// Expect waits until the visible output not yet consumed contains substr.
func (s *Session) Expect(substr string) {
	s.t.Helper()
	s.ExpectWithin(s.opts.Timeout, substr)
}

// ExpectWithin is Expect with an explicit deadline.
func (s *Session) ExpectWithin(d time.Duration, substr string) {
	s.t.Helper()
	needle := []byte(substr)
	from := 0
	s.expect(d, fmt.Sprintf("%q", substr), func(p []byte) (int, bool) {
		s.addWork(len(p) - from)
		i := bytes.Index(p[from:], needle)
		if i < 0 {
			// Only bytes that could still start a match need rescanning.
			from = max(0, min(len(p), len(p)-len(needle)+1))
			return 0, false
		}
		return from + i + len(needle), true
	})
}

// regexpWindow bounds how much unconsumed text ExpectRegexp searches.
const regexpWindow = 64 << 10

// ExpectRegexp waits for a match of pattern in the last 64 KiB of unconsumed
// visible text and returns the match and its submatches ("" for groups that
// did not take part).
func (s *Session) ExpectRegexp(pattern string) []string {
	s.t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		s.t.Fatalf("ptytest: bad pattern %q: %v", pattern, err)
	}
	var got []string
	s.expect(s.opts.Timeout, "/"+pattern+"/", func(p []byte) (int, bool) {
		base := max(0, len(p)-regexpWindow)
		s.addWork(len(p) - base)
		loc := re.FindSubmatchIndex(p[base:])
		if loc == nil {
			return 0, false
		}
		got = got[:0]
		for i := 0; i < len(loc); i += 2 {
			if loc[i] < 0 {
				got = append(got, "")
			} else {
				got = append(got, string(p[base+loc[i]:base+loc[i+1]]))
			}
		}
		return base + loc[1], true
	})
	return got
}

// ExpectScreen waits until the rendered screen contains substr. Unlike
// Expect it consumes nothing; it looks at the visible rows and the last 256
// scrolled-off lines.
func (s *Session) ExpectScreen(substr string) {
	s.t.Helper()
	s.ExpectScreenWithin(s.opts.Timeout, substr)
}

// ExpectScreenWithin is ExpectScreen with an explicit deadline.
func (s *Session) ExpectScreenWithin(d time.Duration, substr string) {
	s.t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	drained := false
	for {
		s.mu.Lock()
		s.advanceLocked()
		found := strings.Contains(s.scr.tail(), substr)
		changed, over := s.changed, s.overflow
		s.mu.Unlock()
		switch {
		case found:
			return
		case over:
			s.failOverflow()
		case drained:
			s.t.Fatalf("ptytest: output ended before the screen contained %q\nscreen: %s", substr, excerpt([]byte(s.Screen())))
		}
		select {
		case <-changed:
		case <-s.drained:
			drained = true
		case <-timer.C:
			s.t.Fatalf("ptytest: timed out after %v waiting for the screen to contain %q\nscreen: %s", d, substr, excerpt([]byte(s.Screen())))
		}
	}
}

// addWork counts bytes scanned by a matcher; callers hold s.mu.
func (s *Session) addWork(n int) { s.work += uint64(max(n, 0)) }

func (s *Session) failOverflow() {
	s.t.Helper()
	s.t.Fatalf("ptytest: output exceeded MaxOutput (%d bytes)", s.opts.MaxOutput)
}

// expect waits until find reports a match in the unconsumed visible text,
// then consumes the text up to the end of the match. find sees the whole
// unconsumed text on every call and may remember how far it already looked.
func (s *Session) expect(d time.Duration, what string, find func(plain []byte) (int, bool)) {
	s.t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	drained := false
	for {
		s.mu.Lock()
		s.advanceLocked()
		end, ok := find(s.plain)
		if ok {
			if end > 0 {
				s.plain = s.plain[end:]
			}
			s.mu.Unlock()
			return
		}
		changed, over := s.changed, s.overflow
		s.mu.Unlock()
		switch {
		case over:
			s.failOverflow()
		case drained:
			s.t.Fatalf("ptytest: output ended before %s appeared\ntranscript: %s", what, excerpt(s.Transcript()))
		}
		select {
		case <-changed:
		case <-s.drained:
			drained = true
		case <-timer.C:
			s.t.Fatalf("ptytest: timed out after %v waiting for %s\ntranscript: %s", d, what, excerpt(s.Transcript()))
		}
	}
}

// ExpectExit waits for the child to exit with code.
func (s *Session) ExpectExit(code int) {
	s.t.Helper()
	s.ExpectExitWithin(s.opts.Timeout, code)
}

// ExpectExitWithin is ExpectExit with an explicit deadline. It also waits for
// the remaining output to be read, so Transcript is complete afterwards.
func (s *Session) ExpectExitWithin(d time.Duration, code int) {
	s.t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	for _, ch := range []chan struct{}{s.exited, s.drained} {
		select {
		case <-ch:
		case <-timer.C:
			s.t.Fatalf("ptytest: process did not exit within %v (or kept the terminal open)\ntranscript: %s", d, excerpt(s.Transcript()))
		}
	}
	s.mu.Lock()
	over := s.overflow
	s.mu.Unlock()
	if over {
		s.failOverflow()
	}
	if s.status.Signaled {
		s.t.Fatalf("ptytest: process killed by signal %v, want exit status %d\ntranscript: %s", s.status.Signal, code, excerpt(s.Transcript()))
	}
	if s.status.Code != code {
		s.t.Fatalf("ptytest: exit status %d, want %d\ntranscript: %s", s.status.Code, code, excerpt(s.Transcript()))
	}
}

// AssertNoRawControls waits (up to Options.Timeout) until the child has
// exited and all its output was read, then fails the test if the transcript
// contains a control character in any FR-022 class other than newline,
// carriage return and tab, an escape sequence, or an invalid UTF-8 byte. Each
// literal in allowed (for example the program's own color codes) is exempt
// only where it covers whole tokens. A multibyte character cut off at the very
// end of the output is tolerated.
func (s *Session) AssertNoRawControls(allowed ...string) {
	s.t.Helper()
	timer := time.NewTimer(s.opts.Timeout)
	defer timer.Stop()
	for _, ch := range []chan struct{}{s.exited, s.drained} {
		select {
		case <-ch:
		case <-timer.C:
			s.t.Fatalf("ptytest: AssertNoRawControls needs the process to exit and its output to end within %v; call ExpectExit first", s.opts.Timeout)
		}
	}
	text := s.Transcript()
	// Token boundaries, so an allowed literal never matches part of a token.
	boundary := map[int]bool{0: true}
	for off := 0; off < len(text); {
		t := next(text[off:])
		off += t.n
		boundary[off] = true
	}
	for off := 0; off < len(text); {
		skipped := false
		for _, a := range allowed {
			if a != "" && bytes.HasPrefix(text[off:], []byte(a)) && boundary[off+len(a)] {
				off += len(a)
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		t := next(text[off:])
		bad := false
		switch t.kind {
		case tokText:
			bad = t.r == utf8.RuneError && t.n == 1 || t.r != utf8.RuneError && t.r != '\n' && t.r != '\r' && terminal.IsUnsafe(t.r)
		case tokControl:
			bad = t.r != '\n' && t.r != '\r' && t.r != '\t'
		case tokIncomplete:
			bad = text[off] < 0x80 // a cut-off multibyte rune is tolerated
		default:
			bad = true
		}
		if bad {
			s.t.Fatalf("ptytest: raw control sequence at offset %d reached the terminal\ntranscript: %s", off, excerpt(text))
		}
		off += t.n
	}
}

// Close kills the whole process group and waits for it. It is idempotent and
// registered automatically as test cleanup. The group is signalled only while
// the leader has not been reaped: when it exits the group is swept once (see
// waitLoop), so a recycled process or group id is never signalled.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.groupMu.Lock()
		if !s.groupDead {
			killer(s.pid)
		}
		s.groupMu.Unlock()
		<-s.exited
		select {
		case <-s.drained:
		case <-time.After(5 * time.Second): // a descendant escaped the group
		}
		s.master.Close()
	})
}
