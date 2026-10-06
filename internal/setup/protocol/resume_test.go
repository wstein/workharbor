package protocol

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAnswersAndSourceAnswersGoTogether(t *testing.T) {
	start := base(EventRunStart)
	start.Schema, start.V, start.Whr, start.Run, start.Seq, start.At = Schema, Version, "v@abc", "0123456789abcdef", 1, "2026-10-06T10:00:00Z"
	for name, mod := range map[string]func(*Entry){
		"source answers without a digest": func(e *Entry) { e.Source = SourceAnswers },
		"a digest without the source":     func(e *Entry) { e.Source = SourceInteractive; e.Answers = hx("f") },
		"a digest and no source":          func(e *Entry) { e.Answers = hx("f") },
	} {
		e := start
		mod(&e)
		if err := e.Validate(); !errors.Is(err, ErrBadEvent) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := start
	ok.Source, ok.Answers = SourceAnswers, hx("f")
	if err := ok.Validate(); err != nil {
		t.Errorf("a user-phase run from a file: %v", err)
	}
	// step.before carries the digest too
	b := Entry{
		Event: EventStepBefore, Account: "workharbor", Cmd: CmdSetup, Phase: PhaseUser, Schema: Schema, V: Version, Whr: "v@abc",
		Run: start.Run, Seq: 2, At: start.At, Step: "x", Fix: hx("x"), Answer: AnswerRun, Source: SourceAnswers, Status: "fail",
	}
	if err := b.Validate(); !errors.Is(err, ErrBadEvent) {
		t.Errorf("step.before from a file without a digest: %v", err)
	}
	b.Answers = hx("f")
	if err := b.Validate(); err != nil {
		t.Errorf("step.before from a file: %v", err)
	}
}

func TestTheHostPhaseNeverRecordsAnAnswersFile(t *testing.T) {
	e := Entry{
		Event: EventRunStart, Account: "admin", Cmd: CmdSetup, Phase: PhaseHost, Schema: Schema, V: Version, Whr: "v@abc",
		Run: "0123456789abcdef", Seq: 1, At: "2026-10-06T10:00:00Z", Source: SourceAnswers, Answers: hx("f"),
	}
	if err := e.Validate(); !errors.Is(err, ErrBadEvent) {
		t.Fatalf("host run.start from a file: %v", err)
	}
	b := Entry{
		Event: EventStepBefore, Account: "admin", Cmd: CmdSetup, Phase: PhaseHost, Schema: Schema, V: Version, Whr: "v@abc",
		Run: "0123456789abcdef", Seq: 2, At: "2026-10-06T10:00:00Z", Step: "power", Fix: hx("p"), Answer: AnswerRun, Source: SourceAnswers, Answers: hx("f"), Status: "fail",
	}
	if err := b.Validate(); !errors.Is(err, ErrBadEvent) {
		t.Fatalf("host step.before from a file: %v", err)
	}
	b.Source, b.Answers = SourceInteractive, ""
	if err := b.Validate(); err != nil {
		t.Fatalf("host step.before interactive: %v", err)
	}
}

func TestFlagsStayWithinTheLimitWhenEscaped(t *testing.T) {
	for _, ch := range []string{"&", "<", ">", `\`, "é", "😀"} {
		argv := []string{strings.Repeat(ch, 3000)}
		got := Flags(argv, "/h")
		if jsonLen(got) > MaxFlags || len(got) > MaxFlags || !validFlags(got) {
			t.Errorf("%q: %d bytes, %d escaped", ch, len(got), jsonLen(got))
		}
		if got == "" {
			t.Errorf("%q: nothing kept", ch)
		}
		// the run.start line holds
		home := newHome(t)
		l, err := Open(home, fixedClock(), fixedRand(0x01))
		if err != nil {
			t.Fatal(err)
		}
		e := base(EventRunStart)
		e.Source, e.Flags = SourceNone, got
		if err := l.Append(e); err != nil {
			t.Errorf("%q: run.start refused: %v", ch, err)
		}
		_ = l.Close()
	}
}

func TestValidFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want bool
	}{
		"plain":              {"--only api-token", true},
		"empty":              {"", true},
		"at the limit":       {strings.Repeat("a", MaxFlags), true},
		"over the limit":     {strings.Repeat("a", MaxFlags+1), false},
		"escaped over limit": {strings.Repeat("<", MaxFlags/6+1), false},
		"escaped at limit":   {strings.Repeat("<", MaxFlags/6), true},
		"a control":          {"a\x1bb", false},
		"a newline":          {"a\nb", false},
	} {
		if got := validFlags(tc.in); got != tc.want {
			t.Errorf("%s: validFlags = %v", name, got)
		}
	}
	e := base(EventRunStart)
	e.Schema, e.V, e.Whr, e.Run, e.Seq, e.At, e.Source = Schema, Version, "v@abc", "0123456789abcdef", 1, "2026-10-06T10:00:00Z", SourceNone
	e.Flags = "a\nb"
	if err := e.Validate(); !errors.Is(err, ErrBadValue) {
		t.Errorf("a control in flags: %v", err)
	}
}

func TestChainRefusesAReusedRunID(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	scenario(t, home, fixedClock(), fixedRand(0x01)) // the same run id again
	data, _ := os.ReadFile(Path(home))
	if _, err := Chain(data); !errors.Is(err, ErrRunOrder) || !strings.Contains(err.Error(), "reused") {
		t.Fatalf("a reused run id: %v", err)
	}
}

func TestChainResumeGoesOnAfterAFault(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	full, _ := os.ReadFile(Path(home))
	// the last line of the first run is cut by a crash, a second run follows
	if err := os.WriteFile(Path(home), full[:len(full)-20], 0o600); err != nil { //nolint:gosec // test path
		t.Fatal(err)
	}
	scenario(t, home, fixedClock(), fixedRand(0x02))
	data, _ := os.ReadFile(Path(home))
	if _, err := Chain(data); err == nil {
		t.Fatal("Chain must stop at the cut line")
	}
	got, faults := ChainResume(data)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "line 11") {
		t.Fatalf("faults: %v", faults)
	}
	if len(got) != 10+11 {
		t.Fatalf("%d entries; want the 10 before the cut and the whole second run", len(got))
	}
	if got[len(got)-1].Run != "0202020202020202" {
		t.Fatalf("last entry: %+v", got[len(got)-1])
	}
}

func TestChainResumeReportsAnEditedLineAndKeepsTheRest(t *testing.T) {
	home := newHome(t)
	scenario(t, home, fixedClock(), fixedRand(0x01))
	data, _ := os.ReadFile(Path(home))
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	lines[3] = strings.Replace(lines[3], `"status":"fail"`, `"status":"ok"`, 1)
	got, faults := ChainResume([]byte(strings.Join(lines, "\n") + "\n"))
	if len(faults) != 1 || !errors.Is(faults[0], ErrChain) || !strings.Contains(faults[0].Error(), "line 5") {
		t.Fatalf("faults: %v", faults)
	}
	if len(got) != 11 { // the edited line decodes; its successor's prev is the fault; all are kept
		t.Fatalf("entries: %d", len(got))
	}
	if _, faults := ChainResume(data); len(faults) != 0 {
		t.Fatalf("a good file: %v", faults)
	}
}

func TestAFailedFsyncBreaksTheLog(t *testing.T) {
	home := newHome(t)
	l, err := Open(home, fixedClock(), fixedRand(0x01))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	l.sync = func() error { return errors.New("disk full") }
	s := base(EventRunStart)
	s.Source = SourceNone
	if err := l.Append(s); err == nil || errors.Is(err, ErrBroken) {
		t.Fatalf("the failing append: %v", err)
	}
	if err := l.Append(Entry{Event: EventRunEnd, Account: "workharbor", Cmd: CmdSetup, Phase: PhaseUser, Outcome: RunDone}); !errors.Is(err, ErrBroken) {
		t.Fatalf("after a failed fsync the log must be broken: %v", err)
	}
	data, _ := os.ReadFile(Path(home))
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("nothing more may be written: %q", data)
	}
}
