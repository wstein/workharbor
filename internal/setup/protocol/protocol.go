// Package protocol defines the setup protocol v1: an append-only log, one JSON
// object per line, that `whr setup` (both phases) and `whr offboard` write for
// each run and each step, and that a later `whr setup history` can read. See
// issue #337.
//
// What it is: an index of what a run decided and did, per account, in a fixed
// file (setup-protocol.jsonl in the account's default state directory; it does
// not follow state_dir). What it is not: proof. Every line after the first of
// the file carries prev, the sha256 of the previous line, so an edit or a cut in
// the middle is detectable by Chain; but a process running as the same user can
// rewrite the whole file and every digest after the edit, so a same-user log is
// a record of intent and outcome, not evidence against that user.
//
// What it never holds: free text, command output, error text, host names,
// environment, passwords, tokens or keys. A line has only enumerated strings,
// hex digests, a build identity, a time and small integers. A fix is recorded
// by the digest of its argv, never by the argv; the resume flags are the only
// user-chosen text and pass through textsafe, with the home directory shown
// as ~.
//
// Concurrency and tearing: a run takes a non-blocking exclusive flock on the
// file for its whole life, so two runs of one account never interleave (the
// second gets ErrConflict). Each line is one write(2) of the encoded entry and a
// newline on an O_APPEND descriptor, followed by fsync. A crash can leave a cut
// last line; the next run starts with a newline so the cut line stays a line of
// its own, and Chain reports it at its line number.
//
// The canonical form of a line is Go's json.Marshal of Entry, which also
// escapes <, > and & as \u003c, \u003e and \u0026; Decode requires exactly
// those bytes. The field confirm is reserved for a later link to a confirmation
// record: it is part of the struct so the key order is fixed, but any value in it
// is refused until it is defined.
//
// Unverified: flock and O_APPEND behaviour on APFS, and O_NOFOLLOW under an
// admin versus a standard macOS account, were not measured; the tests run on
// the development host's file system only.
package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Version is the only line version this package reads or writes.
const Version = 1

// Schema is the fixed schema name of a protocol line.
const Schema = "workharbor.setup-protocol"

// FileName is the protocol file in the state directory.
const FileName = "setup-protocol.jsonl"

// Prefixes hashed before the bytes they separate from other uses of sha256.
const (
	PrevPrefix = "workharbor-setup-protocol-v1\x00"
	RanPrefix  = "workharbor-setup-ran-v1\x00"
)

// Limits.
const (
	MaxLine     = 4096
	MaxFlags    = 1024
	WarnBytes   = 8 << 20
	maxTailRead = 1 << 20
)

// Events.
const (
	EventRunStart   = "run.start"
	EventStepBefore = "step.before"
	EventStepAfter  = "step.after"
	EventRunEnd     = "run.end"
)

// Commands and phases.
const (
	CmdSetup    = "setup"
	CmdOffboard = "offboard"
	PhaseHost   = "host"
	PhaseUser   = "user"
)

// Answers recorded at step.before.
const (
	AnswerRun  = "run"
	AnswerSkip = "skip"
	AnswerQuit = "quit"
	AnswerNone = "none"
)

// Decision sources.
const (
	SourceInteractive = "interactive"
	SourceAnswers     = "answers"
	SourceNone        = "none"
)

// Outcomes of step.after.
const (
	OutAlreadyDone  = "already_done"
	OutWarnAccepted = "warn_accepted"
	OutSkipped      = "skipped"
	OutNoFix        = "no_fix"
	OutNotRun       = "not_run"
	OutDeclined     = "declined"
	OutNeedsHuman   = "needs_human"
	OutQuit         = "quit"
	OutFixed        = "fixed"
	OutNotFixed     = "not_fixed"
	OutFixFailed    = "fix_failed"
	OutInterrupted  = "interrupted"
	RunDone         = "done"
	RunLeft         = "left"
	RunNeedsHuman   = "needs_human"
	RunQuit         = "quit"
	RunError        = "error"
	RunInterrupted  = "interrupted"
)

// Sentinel errors; match with errors.Is.
var (
	ErrSyntax       = errors.New("protocol: not valid JSON of the expected shape")
	ErrTooLarge     = errors.New("protocol: line is too long")
	ErrUnknownField = errors.New("protocol: unknown field")
	ErrDuplicateKey = errors.New("protocol: duplicate key")
	ErrNull         = errors.New("protocol: null is not allowed")
	ErrFloat        = errors.New("protocol: only integers are allowed")
	ErrTrailing     = errors.New("protocol: trailing data after the line")
	ErrVersion      = errors.New("protocol: unknown version")
	ErrBadValue     = errors.New("protocol: a field has a value outside its set")
	ErrBadEvent     = errors.New("protocol: a field is missing or not allowed for the event")
	ErrNotCanonical = errors.New("protocol: line is not in canonical form")
	ErrChain        = errors.New("protocol: the chain is broken")
	ErrCutOff       = errors.New("protocol: the last line is cut off")
	ErrConflict     = errors.New("protocol: another run holds the protocol")
	ErrUnsafeFile   = errors.New("protocol: file is not a regular file of this user")
	ErrStateDir     = errors.New("protocol: the state directory is not private")
	ErrRunOrder     = errors.New("protocol: entry out of order for the run")
	ErrCorrupt      = errors.New("protocol: the end of the file cannot be read as a line")
	ErrBroken       = errors.New("protocol: an earlier write failed; the run stops logging")
)

// Entry is one protocol line. Its fields are declared in bytewise-sorted tag
// order, so json.Marshal writes the canonical key order; a test holds the
// order. Only strings and integers occur, never null: an absent value is
// omitted.
type Entry struct {
	Account string `json:"account,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Answers string `json:"answers,omitempty"`
	At      string `json:"at"`
	Cmd     string `json:"cmd"`
	Confirm string `json:"confirm,omitempty"`
	Event   string `json:"event"`
	Exit    *int   `json:"exit,omitempty"`
	Fix     string `json:"fix,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Phase   string `json:"phase"`
	Prev    string `json:"prev,omitempty"`
	Ran     string `json:"ran,omitempty"`
	Run     string `json:"run"`
	Schema  string `json:"schema"`
	Seq     int    `json:"seq"`
	Source  string `json:"source,omitempty"`
	Status  string `json:"status,omitempty"`
	Step    string `json:"step,omitempty"`
	V       int    `json:"v"`
	Whr     string `json:"whr"`
}

// Encode returns the canonical bytes of e (no newline). It does not validate e.
func Encode(e Entry) ([]byte, error) { return json.Marshal(e) }

var (
	hex64RE   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	runRE     = regexp.MustCompile(`^[0-9a-f]{16}$`)
	stepRE    = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	accountRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,31}$`)
	whrRE     = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}@[A-Za-z0-9]{1,64}(\+dirty)?$`)
	atRE      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
)

func in(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

var (
	stepOutcomes = []string{OutAlreadyDone, OutWarnAccepted, OutSkipped, OutNoFix, OutNotRun, OutDeclined, OutNeedsHuman, OutQuit, OutFixed, OutNotFixed, OutFixFailed, OutInterrupted}
	runOutcomes  = []string{RunDone, RunLeft, RunNeedsHuman, RunQuit, RunError, RunInterrupted}
	statuses     = []string{string(doctor.OK), string(doctor.Fail), string(doctor.NotVerified), string(doctor.Skipped), string(doctor.Warn)}
)

// fields names the optional-or-required fields each event may carry beyond the
// common ones; required ones are marked in required.
var fields = map[string]struct{ required, optional []string }{
	EventRunStart:   {nil, []string{"answers", "flags", "source"}},
	EventStepBefore: {[]string{"step", "fix", "answer", "source", "status"}, []string{"answers"}},
	EventStepAfter:  {[]string{"step", "outcome", "status"}, []string{"exit", "ran"}},
	EventRunEnd:     {[]string{"outcome"}, nil},
}

func present(e Entry) map[string]bool {
	return map[string]bool{
		"account": e.Account != "", "answer": e.Answer != "", "answers": e.Answers != "",
		"confirm": e.Confirm != "", "exit": e.Exit != nil, "fix": e.Fix != "", "flags": e.Flags != "",
		"outcome": e.Outcome != "", "prev": e.Prev != "", "ran": e.Ran != "", "source": e.Source != "",
		"status": e.Status != "", "step": e.Step != "",
	}
}

// Validate checks every rule of the format on one entry, apart from the
// relation to other lines (see Chain).
func (e Entry) Validate() error {
	if e.V != Version {
		return ErrVersion
	}
	bad := func(what string) error { return errors.Join(ErrBadValue, errors.New(what)) }
	switch {
	case e.Schema != Schema:
		return bad("schema")
	case !whrRE.MatchString(e.Whr):
		return bad("whr")
	case !runRE.MatchString(e.Run):
		return bad("run")
	case e.Seq < 1:
		return bad("seq")
	case !validAt(e.At):
		return bad("at")
	case !accountRE.MatchString(e.Account):
		return bad("account")
	case !in(e.Cmd, CmdSetup, CmdOffboard):
		return bad("cmd")
	case !in(e.Phase, PhaseHost, PhaseUser):
		return bad("phase")
	}
	spec, ok := fields[e.Event]
	if !ok {
		return bad("event")
	}
	have := present(e)
	allowed := map[string]bool{"account": true, "prev": true}
	for _, f := range spec.required {
		if !have[f] {
			return errors.Join(ErrBadEvent, errors.New("missing "+f))
		}
		allowed[f] = true
	}
	for _, f := range spec.optional {
		allowed[f] = true
	}
	for f, p := range have {
		if p && !allowed[f] {
			return errors.Join(ErrBadEvent, errors.New("not allowed: "+f))
		}
	}
	switch {
	case e.Prev != "" && !hex64RE.MatchString(e.Prev):
		return bad("prev")
	case e.Step != "" && !stepRE.MatchString(e.Step):
		return bad("step")
	case e.Fix != "" && !hex64RE.MatchString(e.Fix):
		return bad("fix")
	case e.Ran != "" && !hex64RE.MatchString(e.Ran):
		return bad("ran")
	case e.Answers != "" && !hex64RE.MatchString(e.Answers):
		return bad("answers")
	case e.Answer != "" && !in(e.Answer, AnswerRun, AnswerSkip, AnswerQuit, AnswerNone):
		return bad("answer")
	case e.Source != "" && !in(e.Source, SourceInteractive, SourceAnswers, SourceNone):
		return bad("source")
	case e.Status != "" && !in(e.Status, statuses...):
		return bad("status")
	case e.Flags != "" && !validFlags(e.Flags):
		return bad("flags")
	case e.Exit != nil && (*e.Exit < 0 || *e.Exit > 255):
		return bad("exit")
	}
	// A decision taken from an answers file names the file's digest, and only
	// such a decision does; the host phase is never answered from a file.
	if e.Event == EventRunStart || e.Event == EventStepBefore {
		if (e.Source == SourceAnswers) != (e.Answers != "") {
			return errors.Join(ErrBadEvent, errors.New("answers and source answers go together"))
		}
	}
	if e.Phase == PhaseHost && (e.Source == SourceAnswers || e.Answers != "") {
		return errors.Join(ErrBadEvent, errors.New("the host phase is never answered from a file"))
	}
	switch e.Event {
	case EventStepAfter:
		if !in(e.Outcome, stepOutcomes...) {
			return bad("outcome")
		}
	case EventRunEnd:
		if !in(e.Outcome, runOutcomes...) {
			return bad("outcome")
		}
	}
	return nil
}

func validAt(s string) bool {
	if !atRE.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// validFlags holds flags to MaxFlags both as text and as the JSON string a line
// carries: &, <, > and \ grow when escaped, and a line must stay under MaxLine.
func validFlags(s string) bool {
	return len(s) <= MaxFlags && textsafe.Escape(s) == s && jsonLen(s) <= MaxFlags
}

// jsonLen is the length of s as the content of a JSON string, as Encode writes it.
func jsonLen(s string) int {
	b, _ := json.Marshal(s) // a string always marshals
	return len(b) - 2
}

// LineDigest is the value of prev for the line after line (line without its
// newline): sha256 over the domain prefix and the bytes of the line.
func LineDigest(line []byte) string {
	h := sha256.New()
	h.Write([]byte(PrevPrefix))
	h.Write(line)
	return hex.EncodeToString(h.Sum(nil))
}

// RanDigest is the digest recorded as ran: over the argument vectors that
// actually ran, in order (sudo included), so the log shows what ran without
// holding it.
func RanDigest(argvs [][]string) string {
	if argvs == nil {
		argvs = [][]string{}
	}
	b, _ := json.Marshal(argvs) // strings only
	sum := sha256.Sum256(append([]byte(RanPrefix), b...))
	return hex.EncodeToString(sum[:])
}

// AnswersDigest is the digest recorded for an answer file: a plain sha256 of its
// bytes, with no domain prefix, so it equals `shasum -a 256` of the file.
func AnswersDigest(file []byte) string {
	sum := sha256.Sum256(file)
	return hex.EncodeToString(sum[:])
}

// RedactHome shows a leading home directory as ~. An empty home changes
// nothing; a path that only shares a prefix with home is left alone. The path
// must already be clean (no "..", no double slash, no trailing slash): it is
// compared as text.
func RedactHome(s, home string) string {
	if home == "" || home == "/" {
		return s
	}
	if s == home {
		return "~"
	}
	if strings.HasPrefix(s, home+"/") {
		return "~" + s[len(home):]
	}
	return s
}

// Flags renders the resume argument vector for run.start: the home directory
// as ~ in each argument (also after an equals sign), control and bidi
// characters escaped, joined by single spaces, cut so that it fits MaxFlags as text and as escaped JSON.
func Flags(argv []string, home string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if k, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") {
			a = k + "=" + RedactHome(v, home)
		} else {
			a = RedactHome(a, home)
		}
		parts[i] = textsafe.Escape(a)
	}
	s := strings.Join(parts, " ")
	// cut by what the line will hold, which is the escaped length, and never in
	// the middle of a character
	if len(s) > MaxFlags {
		s = s[:MaxFlags]
	}
	for len(s) > 0 && (!utf8.ValidString(s) || jsonLen(s) > MaxFlags) {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}
