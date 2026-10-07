package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// The widest a line of human output may be: the code wraps at 80, and a word it
// never breaks (a path, a URL, a log record) may push a line to 90 at most.
const (
	wrapLimit = 80
	hardLimit = 90
)

// checkWidth fails for every line of text over the hard limit, and for every
// line over the wrap limit that has spaces to break at. An audit record
// ("log: ...") is one unbroken line by design and is skipped, and so is a
// copyable command line (render.Cmd, "$ ..."), which is never wrapped.
func checkWidth(t *testing.T, name, text string) {
	t.Helper()
	for _, l := range strings.Split(escape.ReplaceAllString(text, ""), "\n") {
		n := utf8.RuneCountInString(l)
		switch {
		case strings.HasPrefix(strings.TrimSpace(l), "log:"), isCmdLine(l):
		case n > hardLimit:
			t.Errorf("%s: %d columns (hard limit %d): %q", name, n, hardLimit, l)
		case n > wrapLimit && strings.Contains(strings.TrimSpace(l), " "):
			t.Errorf("%s: %d columns (wrap at %d): %q", name, n, wrapLimit, l)
		}
	}
}

// Every human-facing command's output stays narrow: the help of every command,
// the doctor report in its three modes, `whr offboard host` (a dry run, and a
// delete with its prompt and notes) and the kill-all and purge prompts.
func TestHumanOutputStaysNarrow(t *testing.T) {
	root, _ := newRoot(&Env{})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		var b bytes.Buffer
		c.SetOut(&b)
		_ = c.Help()
		checkWidth(t, "help "+c.CommandPath(), b.String())
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)

	for name, o := range map[string]uiOpts{
		"doctor tty":     {stdoutTTY: true, stderrTTY: true},
		"doctor plain":   {},
		"doctor nocolor": {stdoutTTY: true, stderrTTY: true, noColor: "1"},
	} {
		_, _, errOut := newSetupRig(t).runUI(o, "doctor", "--user", "operator")
		checkWidth(t, name, errOut)
	}

	r := newOffboardRig(t)
	_, _, errOut := r.run()
	checkWidth(t, "offboard dry run", errOut)
	r = newOffboardRig(t)
	_, _, errOut = r.run("--delete")
	checkWidth(t, "offboard --delete", errOut)

	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(taskList))
	s.reply("GET /v1/tasks/t-aaa111/transcript", 200, ok(`{"events":12,"bytes":3400}`))
	_, _, errOut = s.runCLI("no\n", "kill-all")
	checkWidth(t, "kill-all prompt", errOut)
	_, _, errOut = s.runCLI("no\n", "purge", "t-aaa111")
	checkWidth(t, "purge prompt", errOut)
}

// isCmdLine is a line made by render.Cmd or a "$ command" line of help text.
func isCmdLine(l string) bool {
	l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "|▌"))
	return strings.HasPrefix(l, "$ ")
}
