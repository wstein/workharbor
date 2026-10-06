// Command commitlint checks commit messages against the repository rules.
//
//	commitlint --file .git/COMMIT_EDITMSG --author "Name <email>"   (commit-msg hook)
//	commitlint --range origin/main..HEAD                            (CI, local check)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wstein/workharbor/internal/commitlint"
)

func main() {
	os.Exit(run())
}

func run() int {
	file := flag.String("file", "", "commit message file to check")
	author := flag.String("author", "", "author identity (\"Name <email>\") for --file")
	revRange := flag.String("range", "", "git revision range whose commits are checked, e.g. origin/main..HEAD")
	flag.Parse()

	switch {
	case *file != "":
		return lintFile(*file, *author)
	case *revRange != "":
		return lintRange(*revRange)
	default:
		flag.Usage()
		return 2
	}
}

// hookOptions are the rules for a message git is about to commit: its cleanup
// cuts the message at a scissors line, so the linter does too.
func hookOptions(author string) commitlint.Options {
	return commitlint.Options{Author: author, Scissors: true}
}

// rangeOptions are the rules for stored commits, which keep every line of the
// message: a scissors line is text there and hides nothing.
func rangeOptions(author string) commitlint.Options {
	return commitlint.Options{Author: author, Final: true}
}

func lintFile(file, author string) int {
	msg, err := os.ReadFile(file) //nolint:gosec // the hook's own message file, named by the maintainer's flag
	if err != nil {
		fmt.Fprintln(os.Stderr, "commitlint:", err)
		return 2
	}
	return report("commit message", commitlint.Lint(string(msg), hookOptions(author)))
}

func lintRange(revRange string) int {
	out, err := git("rev-list", "--reverse", "--no-merges", revRange)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commitlint:", err)
		return 2
	}
	status := 0
	for _, sha := range strings.Fields(out) {
		msg, err := git("log", "-1", "--format=%B", sha)
		if err != nil {
			fmt.Fprintln(os.Stderr, "commitlint:", err)
			return 2
		}
		author, err := git("log", "-1", "--format=%an <%ae>", sha)
		if err != nil {
			fmt.Fprintln(os.Stderr, "commitlint:", err)
			return 2
		}
		problems := commitlint.Lint(msg, rangeOptions(strings.TrimSpace(author)))
		if report(sha[:min(len(sha), 10)], problems) != 0 {
			status = 1
		}
	}
	return status
}

func report(name string, problems []string) int {
	if len(problems) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr, "commitlint: %s\n", name)
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "  - %s\n", p)
	}
	return 1
}

// gitCommand builds the git process; a test replaces it with an isolated one.
var gitCommand = func(args ...string) *exec.Cmd {
	return exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // fixed git binary; arguments come from the maintainer's own flags
}

func git(args ...string) (string, error) {
	out, err := gitCommand(args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
