package commitlint

import (
	"fmt"
	"strings"
)

// Conventional checks only what Conventional Commits asks of a message: a
// `type(scope): summary` subject with a type of the specification's common
// set, and a blank line before any body. It is the host's default linter for a
// repository (D51), and it is always final, as Lint with Options.Final: a
// fixup!, squash! or amend! commit is refused; Lint adds this repository's own rules (the length, the
// trailers) and is the `workharbor` choice.
func Conventional(msg string) []string {
	lines := clean(msg, false)
	if len(lines) == 0 {
		return []string{"empty commit message"}
	}
	for _, p := range exemptPrefixes {
		if strings.HasPrefix(lines[0], p) {
			if p != "Merge " && p != "Revert " {
				// A prepared commit is about to be pushed: it is final.
				return []string{fmt.Sprintf("%q commit is not squashed: run git rebase -i --autosquash before it is pushed", strings.TrimSpace(p))}
			}
			return nil
		}
	}
	var problems []string
	if !subjectRe.MatchString(lines[0]) {
		problems = append(problems, "subject must follow Conventional Commits, e.g. 'feat(domain): add run state'")
	}
	if len(lines) > 1 && lines[1] != "" {
		problems = append(problems, "leave a blank line after the subject")
	}
	return problems
}
