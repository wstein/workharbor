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

	"golang.org/x/term"

	"github.com/wstein/workharbor/internal/doctor"
)

// Terminal is the real Host: a person at a terminal. Prompts go to Err (stdout is
// data), answers come from In, and a secret is read from the terminal itself with
// no echo. Commands run with the terminal attached so sudo can ask for a password.
type Terminal struct {
	In  *bufio.Reader
	Err io.Writer
	// Stdin is the terminal a secret is read from; it must be a terminal.
	Stdin *os.File
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

// Run implements Host.
func (t Terminal) Run(ctx context.Context, c doctor.Cmd) error {
	argv := c.Full()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the fixes the steps list, shown before they run
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, t.Err, t.Err
	return cmd.Run()
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
