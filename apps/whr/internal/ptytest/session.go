// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
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
	Timeout time.Duration // default deadline of Expect and ExpectExit; default 10s
}

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

	mu      sync.Mutex
	buf     []byte
	mark    int // raw offset up to which Expect has consumed output
	changed chan struct{}

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
		opts.Timeout = 10 * time.Second
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
	chunk := make([]byte, 4096)
	for {
		n, err := s.master.Read(chunk)
		if n > 0 {
			s.mu.Lock()
			s.buf = append(s.buf, chunk[:n]...)
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		}
		if err != nil { // EIO once every slave descriptor closed, or closed master
			return
		}
	}
}

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	st := ExitStatus{Code: s.cmd.ProcessState.ExitCode()}
	if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		st.Signaled, st.Signal, st.Code = true, ws.Signal(), -1
	}
	_ = err
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
}

// Transcript returns every byte the child wrote to the terminal so far.
func (s *Session) Transcript() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf...)
}

// Screen renders the transcript so far as text (see the package comment).
func (s *Session) Screen() string { return render(s.Transcript()) }

// Expect waits until the visible output not yet consumed contains substr.
func (s *Session) Expect(substr string) {
	s.t.Helper()
	s.ExpectWithin(s.opts.Timeout, substr)
}

// ExpectWithin is Expect with an explicit deadline.
func (s *Session) ExpectWithin(d time.Duration, substr string) {
	s.t.Helper()
	s.expect(d, fmt.Sprintf("%q", substr), func(p string) (int, bool) {
		i := strings.Index(p, substr)
		if i < 0 {
			return 0, false
		}
		return i + len(substr), true
	})
}

// ExpectRegexp waits for a match of pattern and returns the submatches.
func (s *Session) ExpectRegexp(pattern string) []string {
	s.t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		s.t.Fatalf("ptytest: bad pattern %q: %v", pattern, err)
	}
	var got []string
	s.expect(s.opts.Timeout, "/"+pattern+"/", func(p string) (int, bool) {
		loc := re.FindStringSubmatchIndex(p)
		if loc == nil {
			return 0, false
		}
		got = got[:0]
		for i := 0; i < len(loc); i += 2 {
			if loc[i] < 0 {
				got = append(got, "")
			} else {
				got = append(got, p[loc[i]:loc[i+1]])
			}
		}
		return loc[1], true
	})
	return got
}

// ExpectScreen waits until the rendered screen contains substr. Unlike
// Expect it consumes nothing.
func (s *Session) ExpectScreen(substr string) {
	s.t.Helper()
	deadline := time.NewTimer(s.opts.Timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		raw, changed := append([]byte(nil), s.buf...), s.changed
		s.mu.Unlock()
		if strings.Contains(render(raw), substr) {
			return
		}
		select {
		case <-changed:
		case <-s.drained:
			if strings.Contains(s.Screen(), substr) {
				return
			}
			s.t.Fatalf("ptytest: output ended before screen contained %q\nscreen: %q", substr, s.Screen())
		case <-deadline.C:
			s.t.Fatalf("ptytest: timed out after %v waiting for screen to contain %q\nscreen: %q",
				s.opts.Timeout, substr, s.Screen())
		}
	}
}

func (s *Session) expect(d time.Duration, what string, match func(plain string) (int, bool)) {
	s.t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		s.mu.Lock()
		plain, ends := project(s.buf[s.mark:])
		if end, ok := match(string(plain)); ok {
			if end > 0 {
				s.mark += ends[end-1]
			}
			s.mu.Unlock()
			return
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-s.drained:
			// All output is in the buffer; check once more, then give up.
			s.mu.Lock()
			plain, ends = project(s.buf[s.mark:])
			end, ok := match(string(plain))
			if ok && end > 0 {
				s.mark += ends[end-1]
			}
			s.mu.Unlock()
			if ok {
				return
			}
			s.t.Fatalf("ptytest: output ended before %s appeared\ntranscript: %q", what, s.Transcript())
		case <-timer.C:
			s.t.Fatalf("ptytest: timed out after %v waiting for %s\ntranscript: %q", d, what, s.Transcript())
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
			s.t.Fatalf("ptytest: process did not exit within %v (or kept the terminal open)\ntranscript: %q", d, s.Transcript())
		}
	}
	if s.status.Signaled {
		s.t.Fatalf("ptytest: process killed by signal %v, want exit status %d\ntranscript: %q", s.status.Signal, code, s.Transcript())
	}
	if s.status.Code != code {
		s.t.Fatalf("ptytest: exit status %d, want %d\ntranscript: %q", s.status.Code, code, s.Transcript())
	}
}

// AssertNoRawControls fails the test if the transcript contains a control
// character in any FR-022 class other than newline, carriage return and tab,
// after removing each literal sequence in allowed (for example the program's
// own color codes). It proves untrusted text reached the terminal inert.
func (s *Session) AssertNoRawControls(allowed ...string) {
	s.t.Helper()
	text := string(s.Transcript())
	for _, a := range allowed {
		text = strings.ReplaceAll(text, a, "")
	}
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		bad := r == utf8.RuneError && n == 1
		if r != '\n' && r != '\r' && terminal.IsUnsafe(r) {
			bad = true
		}
		if bad {
			s.t.Fatalf("ptytest: raw control character %U at offset %d reached the terminal\ntranscript: %q", r, i, s.Transcript())
		}
		i += n
	}
}

// Close kills the whole process group and waits for it. It is idempotent and
// registered automatically as test cleanup.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		killGroup(s.pid)
		<-s.exited
		select {
		case <-s.drained:
		case <-time.After(5 * time.Second): // a descendant escaped the group
		}
		s.master.Close()
	})
}
