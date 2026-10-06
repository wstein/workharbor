package setup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
)

// Terminal is the real Host: a person at a terminal. Prompts go to Err (stdout is
// data), answers come from In, and a secret is read from the terminal itself with
// no echo. Commands run with the terminal attached so sudo can ask for a password.
type Terminal struct {
	In  *bufio.Reader
	Err io.Writer
	// Stdin is the terminal a secret is read from; it must be a terminal.
	Stdin *os.File
	// Style draws the prompts and the output of commands (zero: plain ASCII).
	Style render.Style
}

// Output implements doctor.Runner.
func (Terminal) Output(ctx context.Context, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // a read-only command named by the steps
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil && errb.Len() > 0 {
		// what the command said is what tells "not set" from "could not read"
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), err
}

// runWaitDelay is how long Run waits for the output pipes after the command
// exited or the context ended.
const runWaitDelay = 2 * time.Second

// Run implements Host.
func (t Terminal) Run(ctx context.Context, c doctor.Cmd) error {
	argv := c.Full()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the fixes the steps list, shown before they run
	// what the tool says is set apart from whr's own text, line by line, with
	// nothing held back, so a prompt of the tool appears at once
	tw := render.NewToolWriter(t.Err, t.Style)
	defer tw.End()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, tw, tw
	// a background child that keeps the pipe open must not stall Run
	cmd.WaitDelay = runWaitDelay
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		// the command itself succeeded; a leftover child only held the pipe
		return nil
	}
	return err
}

// Open implements Host. It runs `open`, so a URL or a System Settings pane opens
// the way a click on it would.
func (Terminal) Open(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "open", target).Run() //nolint:gosec // opens a link the step names
}

// Line implements doctor.Prompter.
func (t Terminal) Line(question string) (string, error) {
	fmt.Fprintf(t.Err, "  %s: ", question)
	s, err := t.In.ReadString('\n')
	if err != nil && s == "" {
		return "", errors.New("no answer: standard input ended")
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// Secret implements doctor.Prompter: no echo, and the value goes nowhere but the
// caller.
func (t Terminal) Secret(question string) (string, error) {
	if t.Stdin == nil || !term.IsTerminal(int(t.Stdin.Fd())) { //nolint:gosec // a file descriptor of this process
		return "", errors.New("a secret is only read from a terminal, without echo")
	}
	fmt.Fprintf(t.Err, "  %s: ", question)
	b, err := term.ReadPassword(int(t.Stdin.Fd())) //nolint:gosec // a file descriptor of this process
	fmt.Fprintln(t.Err)
	return string(b), err
}

// Confirm implements doctor.Prompter: only y or yes is a yes.
func (t Terminal) Confirm(question string) (bool, error) {
	fmt.Fprintf(t.Err, "  %s [y/N] ", question)
	s, err := t.In.ReadString('\n')
	if err != nil && s == "" {
		return false, errors.New("no answer: standard input ended")
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// Show implements doctor.Prompter.
func (t Terminal) Show(text string) { fmt.Fprintln(t.Err, text) }

// Ask implements Asker: [Y/n/q] or [y/N/q] by the default, as an ACTION line.
func (t Terminal) Ask(question string, d render.Default) (render.Answer, error) {
	return render.Ask(t.In, render.Writer{W: t.Err, S: t.Style}, question, d)
}

// AskWord asks for a typed word, for the most destructive steps: only the exact
// word is yes, Enter and anything else is no, q quits.
func (t Terminal) AskWord(question, word string) (render.Answer, error) {
	return render.AskWord(t.In, render.Writer{W: t.Err, S: t.Style}, question, word)
}
