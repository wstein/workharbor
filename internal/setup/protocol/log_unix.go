//go:build unix

package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/version"
)

// statInfo is what the ownership check needs from fstat; tests inject their own.
type statInfo struct {
	Mode fs.FileMode
	UID  int
	Size int64
}

var fstat = func(f *os.File) (statInfo, error) {
	fi, err := f.Stat()
	if err != nil {
		return statInfo{}, err
	}
	si := statInfo{Mode: fi.Mode(), UID: -1, Size: fi.Size()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		si.UID = int(st.Uid)
	}
	return si, nil
}

// identity is the build identity written to every line; tests replace it.
var identity = func() string {
	info := version.Get()
	id := info.Version + "@" + info.Commit
	if info.Dirty {
		id += "+dirty"
	}
	return id
}

// Path is the protocol file of the account whose home directory is home: fixed,
// in the default state directory, and not following the configured state_dir.
func Path(home string) string {
	return filepath.Join(config.StateDirOf("", home), FileName)
}

// Log is an open protocol, locked for one run. It is not safe for concurrent
// use by several goroutines except through its own mutex, which Append takes.
type Log struct {
	mu       sync.Mutex
	f        *os.File
	w        io.Writer // the file; a test wraps it to count writes
	clock    func() time.Time
	whr      string
	runID    string
	seq      int
	prev     string // digest of the last line, "" while the file is empty
	needsNL  bool   // the file ended in a cut-off line: set the next one apart
	started  bool
	ended    bool
	warnings []string
	broken   error
	account  string
	cmd      string
	phase    string
}

// Open prepares the protocol of the account at home for one run: it creates the
// state directory 0700 and checks it, opens the file append-only without
// following a link, checks the descriptor (regular, own uid; a mode open to
// others warns and is set back to 0600), takes the run's exclusive lock (a held
// lock is ErrConflict), reads the last line and draws the run id. A nil clock
// is time.Now and a nil rnd is crypto/rand. Close the Log when the run ends.
func Open(home string, clock func() time.Time, rnd io.Reader) (*Log, error) {
	if clock == nil {
		clock = time.Now
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	path := Path(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := config.CheckStateDir(dir); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStateDir, err)
	}
	// O_RDWR so the last line is read through the same descriptor that is
	// checked and locked; O_APPEND puts every write at the end whatever the
	// read offset is. The file is never truncated, renamed or rotated.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600) //nolint:gosec // the fixed protocol path, judged by fstat below
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%w: %s is a symbolic link", ErrUnsafeFile, path)
		}
		return nil, err
	}
	l := &Log{f: f, w: f, clock: clock, whr: identity()}
	fail := func(err error) (*Log, error) {
		_ = f.Close()
		return nil, err
	}
	si, err := fstat(f)
	if err != nil {
		return fail(err)
	}
	switch {
	case !si.Mode.IsRegular():
		return fail(fmt.Errorf("%w: %s is not a regular file", ErrUnsafeFile, path))
	case si.UID != os.Getuid():
		return fail(fmt.Errorf("%w: %s belongs to another user", ErrUnsafeFile, path))
	}
	if si.Mode.Perm()&0o077 != 0 {
		l.warnings = append(l.warnings, fmt.Sprintf("%s was open to others (mode %04o); set to 0600", path, si.Mode.Perm()))
		if err := f.Chmod(0o600); err != nil {
			return fail(err)
		}
	}
	if err := tryLock(f); err != nil {
		return fail(err)
	}
	if si.Size > WarnBytes {
		l.warnings = append(l.warnings, fmt.Sprintf("%s is over 8 MiB; the protocol is never rotated, archive it by hand", path))
	}
	if err := l.readTail(si.Size); err != nil {
		unlock(f)
		if errors.Is(err, ErrCorrupt) {
			return fail(fmt.Errorf("%w: the end of %s cannot be read as a log line; whr setup will not start until the file is fixed or moved away", ErrCorrupt, path))
		}
		return fail(err)
	}
	var id [8]byte
	if _, err := io.ReadFull(rnd, id[:]); err != nil {
		unlock(f)
		return fail(err)
	}
	l.runID = hex.EncodeToString(id[:])
	return l, nil
}

// readTail finds the last line of the file: its digest becomes the next prev,
// and a missing final newline sets needsNL.
func (l *Log) readTail(size int64) error {
	if size == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := l.f.ReadAt(last, size-1); err != nil {
		return err
	}
	l.needsNL = last[0] != '\n'
	end := size
	if !l.needsNL {
		end = size - 1 // the line ends before its newline
	}
	// Walk back in chunks to the newline before the last line.
	const chunk = 4096
	start := end
	for start > 0 {
		if end-start >= maxTailRead {
			return ErrCorrupt
		}
		from := start - chunk
		if from < 0 {
			from = 0
		}
		buf := make([]byte, start-from)
		if _, err := l.f.ReadAt(buf, from); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if i := lastIndex(buf, '\n'); i >= 0 {
			start = from + int64(i) + 1
			break
		}
		start = from
	}
	if end == start {
		return ErrCorrupt // an empty last line
	}
	line := make([]byte, end-start)
	if _, err := l.f.ReadAt(line, start); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	l.prev = LineDigest(line)
	return nil
}

func lastIndex(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// Warnings are what Open noticed and repaired or wants shown: a mode open to
// others, a file over 8 MiB.
func (l *Log) Warnings() []string { return append([]string(nil), l.warnings...) }

// RunID is the id of this run.
func (l *Log) RunID() string { return l.runID }

// Append writes one line. The caller sets the event and its fields, account,
// cmd and phase; Append sets schema, v, whr, run, seq, at and prev, validates
// the entry and writes it in a single write(2) followed by fsync. The first
// entry of a run is run.start; nothing follows run.end.
func (l *Log) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.broken != nil:
		return fmt.Errorf("%w: %w", ErrBroken, l.broken)
	case l.ended:
		return fmt.Errorf("%w: the run has ended", ErrRunOrder)
	case !l.started && e.Event != EventRunStart:
		return fmt.Errorf("%w: a run starts with run.start", ErrRunOrder)
	case l.started && e.Event == EventRunStart:
		return fmt.Errorf("%w: the run has started", ErrRunOrder)
	}
	if l.started && (e.Account != l.account || e.Cmd != l.cmd || e.Phase != l.phase) {
		return fmt.Errorf("%w: account, cmd and phase are fixed by run.start", ErrRunOrder)
	}
	e.Schema, e.V, e.Whr, e.Run, e.Seq = Schema, Version, l.whr, l.runID, l.seq+1
	e.At = l.clock().UTC().Truncate(time.Second).Format(time.RFC3339)
	e.Prev = l.prev
	if err := e.Validate(); err != nil {
		return err
	}
	line, err := Encode(e)
	if err != nil {
		return err
	}
	if len(line) > MaxLine {
		return ErrTooLarge
	}
	out := append(line[:len(line):len(line)], '\n')
	if l.needsNL {
		out = append([]byte{'\n'}, out...)
	}
	n, err := l.w.Write(out)
	if err == nil && n != len(out) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = l.f.Sync()
	}
	if err != nil {
		l.broken = err // the file may hold a torn line: write nothing more
		return err
	}
	if !l.started {
		l.account, l.cmd, l.phase = e.Account, e.Cmd, e.Phase
	}
	l.needsNL = false
	l.started = true
	l.seq++
	l.prev = LineDigest(line)
	l.ended = e.Event == EventRunEnd
	return nil
}

// Close releases the run's lock and the file.
func (l *Log) Close() error {
	unlock(l.f)
	return l.f.Close()
}
