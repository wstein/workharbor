// Package answers defines the setup answer file v1: what a human decided for
// each user-phase step of `whr setup`, bound to the exact command of the step
// and to the build that saved it. See issue #337.
//
// The file holds decisions, never values: there is no field a password, token
// or key could go in, and the strict decoder refuses any field it does not
// know. A decision applies only while the step id, the digest of its fix (the
// argv it would run, sudo included, its description and whether it has an
// in-process action or a builder) and the build identity all match; otherwise
// the wizard asks again. A dirty build has no identity: it neither applies nor
// saves an answer file.
//
// A file with phase "host" is refused at decode. Beyond that, the format alone
// cannot keep a host or sudo step out: an entry only names a step id and a
// digest. The guard is Lookup, which takes the real doctor.Check, runs Eligible
// and FixDigest itself and never returns an answer for a host-phase, sudo,
// guided or irreversible step, and Save, which refuses to write such an entry.
// A same-user file is an index of decisions, not proof; anything that can write
// it can also run whr.
//
// What the digest does not cover: the commands a Build function produces at run
// time and the account UseUser names. The runner must therefore refuse any sudo
// command from Build when the decision came from a file, and ask again.
//
// Unverified: the behaviour of O_NOFOLLOW, fstat ownership and rename on
// macOS (APFS, admin versus standard user) was not measured; the tests run on
// the development host's file system only.
package answers

import (
	"errors"
	"regexp"
	"sort"
)

// Version is the only file version this package reads or writes.
const Version = 1

// Schema is the fixed schema name of an answer file.
const Schema = "workharbor.setup-answers"

// PhaseUser is the only phase an answer file may carry.
const PhaseUser = "user"

// Answers a step can have.
const (
	Run  = "run"
	Skip = "skip"
)

// Limits.
const (
	MaxBytes   = 64 << 10
	MaxAnswers = 64
)

// Sentinel errors; match with errors.Is.
var (
	ErrSyntax           = errors.New("answers: not valid JSON of the expected shape")
	ErrTooLarge         = errors.New("answers: file is larger than 64 KiB")
	ErrUnknownVersion   = errors.New("answers: unknown version")
	ErrUnknownField     = errors.New("answers: unknown field")
	ErrDuplicateKey     = errors.New("answers: duplicate key")
	ErrNull             = errors.New("answers: null is not allowed")
	ErrFloat            = errors.New("answers: only integers are allowed")
	ErrTrailing         = errors.New("answers: trailing data after the document")
	ErrBadSchema        = errors.New("answers: wrong schema")
	ErrBadPhase         = errors.New("answers: phase must be user")
	ErrBadWhr           = errors.New("answers: bad build identity")
	ErrBadAccount       = errors.New("answers: bad account name")
	ErrBadAnswer        = errors.New("answers: bad answer entry")
	ErrTooMany          = errors.New("answers: too many entries")
	ErrDuplicateStep    = errors.New("answers: duplicate step")
	ErrNoBuildIdentity  = errors.New("answers: a dirty build has no identity to bind answers to")
	ErrUnsafeFile       = errors.New("answers: file is not a regular file of this user")
	ErrWritableByOthers = errors.New("answers: file is writable by group or others")
	ErrInGitTree        = errors.New("answers: path is inside a git working tree")
	ErrNoDirectory      = errors.New("answers: parent directory does not exist")
	ErrIneligible       = errors.New("answers: the step cannot be answered from a file")
)

// File is an answer file. Its JSON form is the documented v1 layout.
type File struct {
	Schema  string  `json:"schema"`
	V       int     `json:"v"`
	Whr     string  `json:"whr"`
	Phase   string  `json:"phase"`
	Account string  `json:"account"`
	Answers []Entry `json:"answers"`
}

// Entry is one decision: run or skip the step whose fix has this digest.
type Entry struct {
	Step   string `json:"step"`
	Fix    string `json:"fix"`
	Answer string `json:"answer"`
}

var (
	stepRE    = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	fixRE     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	accountRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	whrRE     = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}@[A-Za-z0-9]{1,64}$`)
)

// Validate checks every rule of the format on f.
func (f File) Validate() error {
	if f.V != Version {
		return ErrUnknownVersion
	}
	if f.Schema != Schema {
		return ErrBadSchema
	}
	if f.Phase != PhaseUser {
		return ErrBadPhase
	}
	if !whrRE.MatchString(f.Whr) {
		return ErrBadWhr
	}
	if !accountRE.MatchString(f.Account) {
		return ErrBadAccount
	}
	if len(f.Answers) > MaxAnswers {
		return ErrTooMany
	}
	seen := map[string]bool{}
	for _, e := range f.Answers {
		switch {
		case !stepRE.MatchString(e.Step), !fixRE.MatchString(e.Fix), e.Answer != Run && e.Answer != Skip:
			return ErrBadAnswer
		case seen[e.Step]:
			return ErrDuplicateStep
		}
		seen[e.Step] = true
	}
	return nil
}

func (f *File) sortEntries() {
	sort.Slice(f.Answers, func(i, j int) bool { return f.Answers[i].Step < f.Answers[j].Step })
}
