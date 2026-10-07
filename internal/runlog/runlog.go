// Package runlog is the per-run log of `whr setup`, `whr offboard` and
// `whr doctor` (issue #379): every command a step runs, with its exit code and
// the output it printed, and one line per step. It is a plain text file for a
// person to read (or to follow with `tail -f` in a second terminal).
//
// It never holds a secret. A secret is read by whr and handed over on stdin, so
// it is not in an argv; the caller passes the output through render.ToolWriter,
// which drops a tool's prompt for a secret, and Command masks the secret value
// itself if a tool echoed it anyway.
package runlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor/internal/config"
)

// TailLines is how many of the last lines of a step's output a failure shows.
const TailLines = 8

// Path is the fixed per-run path: <state dir>/logs/<command>-<UTC time>.log, by
// default ~/.local/state/whr/logs/setup-20261007T101500Z.log.
func Path(stateDir, home, command string, now time.Time) string {
	return filepath.Join(config.StateDirOf(stateDir, home), "logs", command+"-"+now.UTC().Format("20060102T150405Z")+".log")
}

// Log is an open run log. A nil *Log does nothing, so callers need no checks.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	path string
	// Stream, when set (--verbose), also gets every record as written.
	Stream io.Writer
	last   string // output of the last command, for the failure tail
}

// Open creates the log at path: the directory 0700, the file 0600, appended to
// when it exists (a --log-file that a second terminal already follows).
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// never through a symlink, and never onto a file with another name too: the
	// log must not change or grow a file the person did not name; O_NONBLOCK so
	// a FIFO at the path fails or is refused below instead of hanging the run
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !fi.Mode().IsRegular() || !ok || st.Nlink != 1 {
		_ = f.Close()
		return nil, errors.New("not a plain file with one name (a link or a device is refused)")
	}
	if st := fi.Sys().(*syscall.Stat_t); !ownerOK(st.Uid, uint32(os.Geteuid())) { //nolint:gosec // a uid fits
		_ = f.Close()
		return nil, errors.New("owned by another user (root writes only its own file)")
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Log{f: f, path: path}, nil
}

// ownerOK is false for root writing a file somebody else owns: with an explicit
// --log-file that would let another user steer what root appends to and chmods.
func ownerOK(fileUID, euid uint32) bool { return euid != 0 || fileUID == euid }

// Path is where the log is; empty for a nil log.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close closes the file.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	return l.f.Close()
}

func (l *Log) put(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.f, s)
	if l.Stream != nil {
		_, _ = io.WriteString(l.Stream, s)
	}
}

// Command records a command (its argv, which holds no secret), its exit code and
// its output. secret, when not empty, is masked in the output.
func (l *Log) Command(argv []string, exit int, output, secret string) {
	l.CommandAnswer(argv, exit, output, secret, "")
}

// CommandAnswer is Command for a command whose non-zero exit is an expected
// answer (answer says which, e.g. "not a member"): the log shows the answer
// and the status, not a bare "exit 67" that reads like an error.
func (l *Log) CommandAnswer(argv []string, exit int, output, secret, answer string) {
	if l == nil {
		return
	}
	if secret != "" {
		output = strings.ReplaceAll(output, secret, "***")
	}
	l.mu.Lock()
	l.last = output
	l.mu.Unlock()
	var b strings.Builder
	if answer != "" {
		fmt.Fprintf(&b, "$ %s\nanswer: %s (exit %d)\n", strings.Join(argv, " "), answer, exit)
	} else {
		fmt.Fprintf(&b, "$ %s\nexit %d\n", strings.Join(argv, " "), exit)
	}
	if output = strings.TrimRight(output, "\n"); output != "" {
		b.WriteString(output + "\n")
	}
	l.put(b.String())
}

// Step records one line for a step: status is ok, fail or unknown.
func (l *Log) Step(name, status, detail string) {
	if l == nil {
		return
	}
	l.put(fmt.Sprintf("step %s: %s %s\n", name, status, oneLine(detail)))
}

// Tail is the last n lines of the output of the last command.
func (l *Log) Tail(n int) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return LastLines(l.last, n)
}

// LastLines returns the last n non-empty lines of s.
func LastLines(s string, n int) string {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, strings.TrimRight(ln, " \r"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
