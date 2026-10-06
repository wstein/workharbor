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
		msg, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "commitlint:", err)
			return 2
		}
		return report("commit message", commitlint.Lint(string(msg), commitlint.Options{Author: *author, Scissors: true}))
	case *revRange != "":
		return lintRange(*revRange)
	default:
		flag.Usage()
		return 2
	}
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
		problems := commitlint.Lint(msg, commitlint.Options{Author: strings.TrimSpace(author), Final: true})
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

func git(args ...string) (string, error) {
	out, err := exec.CommandContext(context.Background(), "git", args...).Output() //nolint:gosec // fixed git binary; arguments come from the maintainer's own flags
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
