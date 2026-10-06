package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func init() { identity = func() string { return "v0.1.0@abc1234" } }

func hx(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 10, 6, 10, 0, 0, 123456789, time.UTC)
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func fixedRand(b byte) io.Reader { return bytes.NewReader(bytes.Repeat([]byte{b}, 8)) }

func newHome(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "Users", "zz-secret-home")
}

func base(event string) Entry {
	return Entry{Event: event, Account: "workharbor", Cmd: CmdSetup, Phase: PhaseUser}
}

func exit(n int) *int { return &n }

// scenario writes the run of the golden file.
func scenario(t *testing.T, home string, clock func() time.Time, rnd io.Reader) {
	t.Helper()
	l, err := Open(home, clock, rnd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	appendAll(t, l, home)
}

func appendAll(t *testing.T, l *Log, home string) {
	t.Helper()
	start := base(EventRunStart)
	start.Source = SourceAnswers
	start.Answers = hx("answer file bytes")
	start.Flags = Flags([]string{"--only", "api-token", "--answers=" + filepath.Join(home, ".config/whr/a.json")}, home)
	steps := []Entry{
		start,
		{Event: EventStepBefore, Step: "config-dir", Fix: hx("config-dir"), Answer: AnswerNone, Source: SourceNone, Status: "ok"},
		{Event: EventStepAfter, Step: "config-dir", Outcome: OutAlreadyDone, Status: "ok"},
		{Event: EventStepBefore, Step: "api-token", Fix: hx("api-token"), Answer: AnswerRun, Source: SourceAnswers, Answers: hx("answer file bytes"), Status: "fail"},
		{Event: EventStepAfter, Step: "api-token", Outcome: OutFixed, Status: "ok", Exit: exit(0), Ran: RanDigest([][]string{{"whr", "token"}})},
		{Event: EventStepBefore, Step: "agent-key", Fix: hx("agent-key"), Answer: AnswerSkip, Source: SourceInteractive, Status: "fail"},
		{Event: EventStepAfter, Step: "agent-key", Outcome: OutDeclined, Status: "fail"},
		{Event: EventStepAfter, Step: "github-app", Outcome: OutNeedsHuman, Status: "not_verified"},
		{Event: EventStepBefore, Step: "ssh-ca", Fix: hx("ssh-ca"), Answer: AnswerQuit, Source: SourceInteractive, Status: "fail"},
		{Event: EventStepAfter, Step: "ssh-ca", Outcome: OutQuit, Status: "fail", Exit: exit(3)},
		{Event: EventRunEnd, Outcome: RunQuit},
	}
	for _, e := range steps {
		e.Account, e.Cmd, e.Phase = "workharbor", CmdSetup, PhaseUser
		if err := l.Append(e); err != nil {
			t.Fatalf("%s %s: %v", e.Event, e.Step, err)
		}
	}
}

func TestGoldenRunAndChain(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	got, err := os.ReadFile(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join("testdata", "v1", "run.jsonl")
	if *update {
		if err := os.WriteFile(p, got, 0o600); err != nil { //nolint:gosec // test path
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) //nolint:gosec // golden path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("run.jsonl differs:\n got:\n%s\nwant:\n%s", got, want)
	}
	entries, err := Chain(want)
	if err != nil || len(entries) != 11 {
		t.Fatalf("Chain: %d entries, %v", len(entries), err)
	}
	for i, line := range bytes.Split(bytes.TrimSuffix(want, []byte("\n")), []byte("\n")) {
		e, err := Decode(line)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		again, _ := Encode(e)
		if !bytes.Equal(again, line) {
			t.Fatalf("line %d: round trip differs", i+1)
		}
		if i == 0 && e.Prev != "" {
			t.Fatal("first line has prev")
		}
		if i > 0 && e.Prev == "" {
			t.Fatalf("line %d lacks prev", i+1)
		}
	}
	if strings.Contains(string(want), "zz-secret-home") || !strings.Contains(string(want), "~/.config/whr/a.json") {
		t.Fatalf("home is not redacted:\n%s", want)
	}
}

func TestEntryFieldOrderIsBytewiseSorted(t *testing.T) {
	typ := reflect.TypeOf(Entry{})
	var tags []string
	for i := 0; i < typ.NumField(); i++ {
		tags = append(tags, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		switch k := typ.Field(i).Type.Kind(); k {
		case reflect.String, reflect.Int, reflect.Pointer:
		default:
			t.Fatalf("field %s has kind %v", typ.Field(i).Name, k)
		}
	}
	if !sort.StringsAreSorted(tags) {
		t.Fatalf("not in sorted tag order: %v", tags)
	}
}

func goodLine(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "v1", "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(b), "\n", 2)[0]
}

func TestDecodeRefusals(t *testing.T) {
	good := goodLine(t)
	if _, err := Decode([]byte(good)); err != nil {
		t.Fatal(err)
	}
	at := `"at":"2026-10-06T10:00:01Z"`
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"unknown field", strings.Replace(good, `{"account"`, `{"zzz":"x","account"`, 1), ErrUnknownField},
		{"free text field", strings.Replace(good, `{"account"`, `{"error":"boom","account"`, 1), ErrUnknownField},
		{"unknown event", strings.Replace(good, `"run.start"`, `"run.begin"`, 1), ErrBadValue},
		{"unknown source", strings.Replace(good, `"source":"answers"`, `"source":"telepathy"`, 1), ErrBadValue},
		{"unknown cmd", strings.Replace(good, `"setup"`, `"nuke"`, 1), ErrBadValue},
		{"unknown phase", strings.Replace(good, `"user"`, `"root"`, 1), ErrBadValue},
		{"non-UTC at", strings.Replace(good, at, `"at":"2026-10-06T11:00:01+01:00"`, 1), ErrBadValue},
		{"fractional at", strings.Replace(good, at, `"at":"2026-10-06T10:00:01.5Z"`, 1), ErrBadValue},
		{"impossible at", strings.Replace(good, at, `"at":"2026-13-06T10:00:01Z"`, 1), ErrBadValue},
		{"null", strings.Replace(good, `"flags"`, `"confirm":null,"flags"`, 1), ErrNull},
		{"float", strings.Replace(good, `"seq":1`, `"seq":1.0`, 1), ErrFloat},
		{"duplicate key", strings.Replace(good, `"seq":1`, `"seq":1,"seq":1`, 1), ErrDuplicateKey},
		{"trailing object", good + `{}`, ErrTrailing},
		{"v 2", strings.Replace(good, `"v":1`, `"v":2`, 1), ErrVersion},
		{"bad run id", strings.Replace(good, `"run":"0101010101010101"`, `"run":"xyz"`, 1), ErrBadValue},
		{"not an object", `[]`, ErrSyntax},
		{"not canonical", strings.Replace(good, `{"account"`, `{ "account"`, 1), ErrNotCanonical},
		{"reordered keys", strings.Replace(strings.Replace(good, `"account":"workharbor",`, ``, 1), `"v":1,`, `"account":"workharbor","v":1,`, 1), ErrNotCanonical},
		{"confirm is reserved", strings.Replace(good, `"flags"`, `"confirm":"`+hx("c")+`","flags"`, 1), ErrBadEvent},
		{"field not for event", strings.Replace(good, `"flags"`, `"fix":"`+hx("c")+`","flags"`, 1), ErrBadEvent},
		{"too long", good + strings.Repeat(" ", MaxLine), ErrTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode([]byte(c.in)); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestStepOutcomeSetsAreSeparate(t *testing.T) {
	e := base(EventRunEnd)
	e.Schema, e.V, e.Whr, e.Run, e.Seq, e.At = Schema, 1, "v1@abc", hx("r")[:16], 1, "2026-10-06T10:00:00Z"
	e.Outcome = OutFixed // a step outcome, not a run outcome
	if err := e.Validate(); !errors.Is(err, ErrBadValue) {
		t.Fatalf("got %v", err)
	}
	e.Outcome = RunDone
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestChainRefusals(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "v1", "run.jsonl"))
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	join := func(l []string) []byte { return []byte(strings.Join(l, "\n") + "\n") }

	wrongPrev := append([]string{}, lines...)
	wrongPrev[3] = regexp.MustCompile(`"prev":"[0-9a-f]{64}"`).ReplaceAllString(wrongPrev[3], `"prev":"`+hx("x")+`"`)
	if _, err := Chain(join(wrongPrev)); !errors.Is(err, ErrChain) {
		t.Fatalf("a wrong prev: %v", err)
	}

	edited := append([]string{}, lines...)
	edited[2] = strings.Replace(edited[2], `"already_done"`, `"fixed"`, 1)
	if _, err := Chain(join(edited)); !errors.Is(err, ErrChain) {
		t.Fatalf("edited line: %v", err)
	}
	dropped := append(append([]string{}, lines[:2]...), lines[3:]...)
	if _, err := Chain(join(dropped)); !errors.Is(err, ErrChain) {
		t.Fatalf("dropped line: %v", err)
	}
	if _, err := Chain([]byte(strings.TrimSuffix(string(data), "\n"))); !errors.Is(err, ErrCutOff) {
		t.Fatalf("cut-off last line: %v", err)
	}
	if _, err := Chain(data); err != nil {
		t.Fatal(err)
	}
}

func TestAppendOnlyAcrossRuns(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	first, _ := os.ReadFile(Path(home))
	scenario(t, home, fixedClock(), fixedRand(0x02))
	both, _ := os.ReadFile(Path(home))
	if !bytes.HasPrefix(both, first) || len(both) <= len(first) {
		t.Fatal("earlier bytes changed or nothing was appended")
	}
	entries, err := Chain(both)
	if err != nil || len(entries) != 22 {
		t.Fatalf("Chain: %d, %v", len(entries), err)
	}
	if entries[11].Prev != LineDigest(bytes.Split(first, []byte("\n"))[10]) {
		t.Fatal("second run does not link to the last line of the first")
	}
}

func TestCutOffLastLineIsSetApart(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	full, _ := os.ReadFile(Path(home))
	// The last line loses its end and its newline.
	cut := full[:len(full)-20]
	if err := os.WriteFile(Path(home), cut, 0o600); err != nil { //nolint:gosec // test path
		t.Fatal(err)
	}
	l, err := Open(home, fixedClock(), fixedRand(0x03))
	if err != nil {
		t.Fatal(err)
	}
	e := base(EventRunStart)
	e.Source = SourceNone
	if err := l.Append(e); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	got, _ := os.ReadFile(Path(home))
	if !bytes.HasPrefix(got, cut) || got[len(cut)] != '\n' {
		t.Fatal("the new run must start with a newline after the cut line")
	}
	lines := bytes.Split(bytes.TrimSuffix(got, []byte("\n")), []byte("\n"))
	last, prevLine := lines[len(lines)-1], lines[len(lines)-2]
	d, err := Decode(last)
	if err != nil || d.Prev != LineDigest(prevLine) {
		t.Fatalf("the new line must chain to the cut line: %v", err)
	}
	if _, err := Chain(got); err == nil {
		t.Fatal("Chain must report the cut-off line")
	}
}

func TestSymlinkedFileIsRefused(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(filepath.Dir(Path(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, Path(home)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home, nil, nil); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("got %v", err)
	}
}

func TestOtherOwnerIsRefused(t *testing.T) {
	old := fstat
	fstat = func(f *os.File) (statInfo, error) {
		si, err := old(f)
		si.UID = os.Getuid() + 4242
		return si, err
	}
	defer func() { fstat = old }()
	if _, err := Open(newHome(t), nil, nil); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("got %v", err)
	}
}

func TestNonRegularFileIsRefused(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(Path(home), 0o700); err != nil { // a directory in its place
		t.Fatal(err)
	}
	if _, err := Open(home, nil, nil); err == nil {
		t.Fatal("a directory was opened as the protocol")
	}
}

func TestTooOpenFileWarnsAndIsSet0600(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(filepath.Dir(Path(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(home), nil, 0o644); err != nil { //nolint:gosec // the point of the test
		t.Fatal(err)
	}
	if err := os.Chmod(Path(home), 0o644); err != nil { //nolint:gosec // the point of the test
		t.Fatal(err)
	}
	l, err := Open(home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if len(l.Warnings()) != 1 {
		t.Fatalf("warnings: %v", l.Warnings())
	}
	if fi, _ := os.Stat(Path(home)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
}

// No t.Parallel: the umask is process-wide.
func TestNewFileIs0600UnderUmask0(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	home := newHome(t)
	l, err := Open(home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if fi, _ := os.Stat(Path(home)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(Path(home))); fi.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %o", fi.Mode().Perm())
	}
}

func TestStateDirOpenToOthersIsRefused(t *testing.T) {
	home := newHome(t)
	dir := filepath.Dir(Path(home))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // the point of the test
		t.Fatal(err)
	}
	if _, err := Open(home, nil, nil); !errors.Is(err, ErrStateDir) {
		t.Fatalf("got %v", err)
	}
}

func TestSecondOpenWhileLockedIsAConflict(t *testing.T) {
	home := newHome(t)
	l, err := Open(home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home, nil, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v", err)
	}
	_ = l.Close()
	l2, err := Open(home, nil, nil)
	if err != nil {
		t.Fatalf("after close: %v", err)
	}
	_ = l2.Close()
}

func TestOverWarnSizeWarnsOnce(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(filepath.Dir(Path(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	line := append(bytes.Repeat([]byte("x"), 1023), '\n')
	big := bytes.Repeat(line, (WarnBytes/1024)+2)
	if err := os.WriteFile(Path(home), big, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if len(l.Warnings()) != 1 || !strings.Contains(l.Warnings()[0], "8 MiB") {
		t.Fatalf("warnings: %v", l.Warnings())
	}
}

type countingWriter struct {
	w      io.Writer
	writes [][]byte
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return c.w.Write(p)
}

func TestEachEntryIsOneWrite(t *testing.T) {
	home := newHome(t)
	l, err := Open(home, fixedClock(), fixedRand(0x01))
	if err != nil {
		t.Fatal(err)
	}
	cw := &countingWriter{w: l.w}
	l.w = cw
	appendAll(t, l, home)
	_ = l.Close()
	if len(cw.writes) != 11 {
		t.Fatalf("%d writes for 11 entries", len(cw.writes))
	}
	for _, w := range cw.writes {
		if bytes.Count(w, []byte("\n")) != 1 || w[len(w)-1] != '\n' {
			t.Fatalf("a write is not exactly one line: %q", w)
		}
	}
}

func TestRunOrder(t *testing.T) {
	home := newHome(t)
	l, err := Open(home, fixedClock(), fixedRand(0x01))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Append(base(EventRunEnd)); !errors.Is(err, ErrRunOrder) {
		t.Fatalf("before run.start: %v", err)
	}
	s := base(EventRunStart)
	if err := l.Append(s); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(s); !errors.Is(err, ErrRunOrder) {
		t.Fatalf("second run.start: %v", err)
	}
	end := base(EventRunEnd)
	end.Outcome = RunDone
	if err := l.Append(end); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(end); !errors.Is(err, ErrRunOrder) {
		t.Fatalf("after run.end: %v", err)
	}
	bad := base(EventRunStart)
	bad.Account = "Bad Account"
	l2, _ := Open(newHome(t), nil, nil)
	defer func() { _ = l2.Close() }()
	if err := l2.Append(bad); !errors.Is(err, ErrBadValue) {
		t.Fatalf("invalid entry: %v", err)
	}
}

func TestRedaction(t *testing.T) {
	home := "/Users/zz-secret-home"
	for in, want := range map[string]string{
		home:                  "~",
		home + "/.config/whr": "~/.config/whr",
		home + "x/y":          home + "x/y",
		"/other" + home:       "/other" + home,
		"plain":               "plain",
	} {
		if got := RedactHome(in, home); got != want {
			t.Errorf("RedactHome(%q) = %q, want %q", in, got, want)
		}
	}
	if got := RedactHome("/a", ""); got != "/a" {
		t.Fatal("empty home must change nothing")
	}
	got := Flags([]string{"--answers=" + home + "/a.json", home, "a\nb\x1b[31m"}, home)
	if strings.Contains(got, "zz-secret-home") || strings.ContainsAny(got, "\n\x1b") {
		t.Fatalf("flags %q", got)
	}
	if want := `--answers=~/a.json ~ a\nb\x1b[31m`; got != want {
		t.Fatalf("flags %q, want %q", got, want)
	}
	long := Flags([]string{strings.Repeat("é", 2000)}, home)
	if len(long) > MaxFlags || !validFlags(long) {
		t.Fatalf("long flags: %d bytes", len(long))
	}
}

func TestFakeHomeAppearsInNoLine(t *testing.T) {
	home := newHome(t)
	scenario(t, home, nil, nil)
	b, _ := os.ReadFile(Path(home))
	if strings.Contains(string(b), "zz-secret-home") || strings.Contains(string(b), filepath.Dir(home)) {
		t.Fatalf("home leaked:\n%s", b)
	}
}

func TestDigestsAreDomainSeparated(t *testing.T) {
	a := RanDigest([][]string{{"a"}})
	if a == RanDigest([][]string{{"b"}}) || a == RanDigest([][]string{{"a"}, {"a"}}) || a == RanDigest(nil) {
		t.Fatal("RanDigest collides")
	}
	line := []byte("x")
	plain := sha256.Sum256(line)
	if LineDigest(line) == hex.EncodeToString(plain[:]) {
		t.Fatal("LineDigest has no domain prefix")
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestFailedWritePoisonsTheLog(t *testing.T) {
	home := newHome(t)
	l, err := Open(home, fixedClock(), fixedRand(0x01))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	orig := l.w
	l.w = failingWriter{err: errors.New("disk full")}
	s := base(EventRunStart)
	if err := l.Append(s); err == nil {
		t.Fatal("a failed write was not reported")
	}
	l.w = orig // the disk is fine again, the log still refuses
	if err := l.Append(s); !errors.Is(err, ErrBroken) {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(Path(home)); len(b) != 0 {
		t.Fatalf("something was written: %q", b)
	}
}

func TestRunCannotChangeAccountCmdOrPhase(t *testing.T) {
	l, err := Open(newHome(t), fixedClock(), fixedRand(0x01))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Append(base(EventRunStart)); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*Entry){
		"account": func(e *Entry) { e.Account = "other" },
		"cmd":     func(e *Entry) { e.Cmd = CmdOffboard },
		"phase":   func(e *Entry) { e.Phase = PhaseHost },
	} {
		e := Entry{Event: EventStepAfter, Step: "x", Outcome: OutSkipped, Status: "ok", Account: "workharbor", Cmd: CmdSetup, Phase: PhaseUser}
		mod(&e)
		if err := l.Append(e); !errors.Is(err, ErrRunOrder) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := Entry{Event: EventStepAfter, Step: "x", Outcome: OutSkipped, Status: "ok", Account: "workharbor", Cmd: CmdSetup, Phase: PhaseUser}
	if err := l.Append(ok); err != nil {
		t.Fatalf("a refused entry must not spoil the run: %v", err)
	}
}

func TestCorruptTailMessageIsPlain(t *testing.T) {
	home := newHome(t)
	if err := os.MkdirAll(filepath.Dir(Path(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(home), []byte("x\n\n"), 0o600); err != nil { //nolint:gosec // test path
		t.Fatal(err)
	}
	_, err := Open(home, nil, nil)
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "will not start until the file is fixed or moved away") || !strings.Contains(err.Error(), FileName) {
		t.Fatalf("got %v", err)
	}
}
