package answers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/wstein/workharbor/internal/doctor"
)

// DigestPrefix is the domain separator hashed before the digest input.
const DigestPrefix = "workharbor-setup-fix-v1\x00"

// irreversible lists steps that are never answered from a file by name, in
// addition to the step model's own Fix.Irreversible, which Eligible also checks:
// a belt for a step whose fix forgets to say so.
var irreversible = map[string]bool{"drop-admin": true}

// digestInput is what a fix digest covers. Its fields are declared in
// bytewise-sorted tag order, so the marshalled bytes are canonical; a test
// holds the order. Title, Guide and Open are text for the human and are not
// part of what runs, so they are left out.
type digestInput struct {
	Build bool       `json:"build"`
	Cmds  [][]string `json:"cmds"`
	Desc  string     `json:"desc"`
	Do    bool       `json:"do"`
	Phase string     `json:"phase"`
	Step  string     `json:"step"`
	V     int        `json:"v"`
}

// FixDigest returns the hex sha256 of the step's fix: every argv it would run
// (sudo included), its description and whether it has an in-process action or a
// builder. Any change of what would run changes the digest.
func FixDigest(c doctor.Check) string {
	in := digestInput{Cmds: [][]string{}, Phase: string(c.Phase), Step: c.Name, V: 1}
	if c.Fix != nil {
		in.Build = c.Fix.Build != nil
		in.Do = c.Fix.Do != nil
		in.Desc = c.Fix.Desc
		for _, cmd := range c.Fix.Cmds {
			in.Cmds = append(in.Cmds, append([]string{}, cmd.Full()...))
		}
	}
	b, err := json.Marshal(in)
	if err != nil { // strings, bools and ints only
		panic(err)
	}
	sum := sha256.Sum256(append([]byte(DigestPrefix), b...))
	return hex.EncodeToString(sum[:])
}

// Eligible reports whether a step may be answered from a file. It is pure.
// Only a user-phase step with something to run qualifies: never a host step,
// a guided step (text for the human), a step whose preview shows sudo, or an
// irreversible one (Fix.Irreversible, or by name). The preview is all it sees: a Build function may return
// other commands (sudo included) and UseUser may name another account, and
// neither is covered by FixDigest, so the runner must refuse sudo from Build
// when the decision came from a file.
func Eligible(c doctor.Check) (ok bool, reason string) {
	switch {
	case c.Phase != doctor.PhaseUser:
		return false, "only user-phase steps are answered from a file"
	case c.Fix == nil:
		return false, "the step has no fix"
	case c.Fix.Do == nil && c.Fix.Build == nil && len(c.Fix.Cmds) == 0:
		return false, "the step is guided: the human does it"
	case c.Fix.Irreversible, irreversible[c.Name]:
		return false, "the step is irreversible and is always asked"
	}
	for _, cmd := range c.Fix.Cmds {
		if cmd.Sudo {
			return false, "the step runs a command with sudo"
		}
	}
	return true, ""
}

// Lookup returns the answer recorded for the step c, only when c is eligible,
// its name matches an entry and the entry's digest is FixDigest(c). Anything
// else (a changed command, a host or sudo or guided or irreversible step) has no
// answer: ask again.
func (f File) Lookup(c doctor.Check) (answer string, ok bool) {
	if ok, _ := Eligible(c); !ok {
		return "", false
	}
	digest := FixDigest(c)
	for _, e := range f.Answers {
		if e.Step == c.Name && e.Fix == digest {
			return e.Answer, true
		}
	}
	return "", false
}

// checkEntries refuses entries that do not name an eligible step of checks with
// its current digest.
func checkEntries(f File, checks []doctor.Check) error {
	for _, e := range f.Answers {
		found := false
		for _, c := range checks {
			if c.Name != e.Step {
				continue
			}
			found = true
			if ok, why := Eligible(c); !ok {
				return fmt.Errorf("%w: %s: %s", ErrIneligible, e.Step, why)
			}
			if FixDigest(c) != e.Fix {
				return fmt.Errorf("%w: %s: the digest does not match the step", ErrIneligible, e.Step)
			}
		}
		if !found {
			return fmt.Errorf("%w: %s: unknown step", ErrIneligible, e.Step)
		}
	}
	return nil
}
