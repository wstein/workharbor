package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	for _, m := range widthProblems(name, text, wrapLimit, hardLimit) {
		t.Error(m)
	}
}

// widthProblems is checkWidth for any limits, returning the messages.
func widthProblems(name, text string, wrapAt, hardAt int) []string {
	var out []string
	for _, l := range strings.Split(escape.ReplaceAllString(text, ""), "\n") {
		n := utf8.RuneCountInString(l)
		switch {
		case strings.HasPrefix(strings.TrimSpace(l), "log:"), isCmdLine(l):
		case n > hardAt:
			out = append(out, fmt.Sprintf("%s: %d columns (hard limit %d): %q", name, n, hardAt, l))
		case n > wrapAt && strings.Contains(strings.TrimSpace(l), " "):
			out = append(out, fmt.Sprintf("%s: %d columns (wrap at %d): %q", name, n, wrapAt, l))
		}
	}
	return out
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

// The "$ " exemption: a long copyable command line passes, the same text
// without it fails, and so does a long prose line.
func TestWidthCheckExemptsCommandLines(t *testing.T) {
	cmd := "$ " + strings.Repeat("word ", 30)
	if p := widthProblems("x", cmd, 80, 90); len(p) != 0 {
		t.Errorf("a command line must pass: %v", p)
	}
	if p := widthProblems("x", "  "+cmd, 80, 90); len(p) != 0 {
		t.Errorf("an indented command line must pass: %v", p)
	}
	if p := widthProblems("x", strings.TrimPrefix(cmd, "$ "), 80, 90); len(p) != 1 {
		t.Errorf("prose over the limit must fail: %v", p)
	}
}

// A description wraps under its own column, a plain line under its indent.
func TestHangWrapAlignsTheDescriptionColumn(t *testing.T) {
	in := "      --flag   " + strings.Repeat("alpha beta ", 10) + "\n" +
		"    " + strings.Repeat("plain text ", 10)
	lines := strings.Split(strings.TrimRight(hangWrap(in), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("not wrapped: %q", lines)
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 80 {
			t.Errorf("too wide: %q", l)
		}
	}
	col := len("      --flag   ")
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "    plain") {
			break
		}
		if !strings.HasPrefix(l, strings.Repeat(" ", col)) || strings.HasPrefix(l, strings.Repeat(" ", col+1)) {
			t.Errorf("a description continuation must start at column %d: %q", col, l)
		}
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "    plain") && !strings.HasPrefix(last, "    text") {
		t.Errorf("plain text must keep its indent: %q", last)
	}
}

// narrowEnv is an Env whose streams are terminals 40 columns wide.
func narrowEnv(stdin string, out, errOut io.Writer) Env {
	return Env{
		Stdin: strings.NewReader(stdin), Stdout: out, Stderr: errOut, Getenv: func(string) string { return "" },
		IsTTY: func(io.Writer) bool { return true },
		Cols:  func(io.Writer) int { return 40 },
		Setup: SetupEnv{Host: &setupHost{outputs: map[string]string{}}, User: "whr", UID: 502, GOOS: "linux"},
	}
}

// The detected width, not a fixed 80, shapes the kill-all and purge prompts
// and (through Env.Cols) every report line.
func TestPromptsFollowTheTerminalWidth(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(taskList))
	s.reply("GET /v1/tasks/t-aaa111/transcript", 200, ok(`{"events":12,"bytes":3400}`))
	for _, args := range [][]string{{"kill-all"}, {"purge", "t-aaa111"}} {
		var out, errOut bytes.Buffer
		env := narrowEnv("no\n", &out, &errOut)
		env.NewClient = func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil }
		Execute(context.Background(), env, args)
		if p := widthProblems(args[0], errOut.String(), 40, 60); len(p) != 0 || errOut.Len() == 0 {
			t.Errorf("%v\n%q", p, errOut.String())
		}
		if !strings.Contains(errOut.String(), "\n") || len(strings.Split(errOut.String(), "\n")) < 4 {
			t.Errorf("%s: the prompt was not wrapped: %q", args[0], errOut.String())
		}
	}
}

// Report lines of the offboard dry run wrap at the terminal width.
func TestOffboardReportWrapsAtANarrowWidth(t *testing.T) {
	r := newOffboardRig(t)
	var out, errOut bytes.Buffer
	env := narrowEnv("", &out, &errOut)
	env.Offboard, env.Setup = r.env.Offboard, r.env.Setup
	Execute(context.Background(), env, []string{"offboard", "host"})
	if p := widthProblems("offboard", errOut.String(), 40, 60); len(p) != 0 || errOut.Len() == 0 {
		t.Errorf("%v\n%s", p, errOut.String())
	}
}
