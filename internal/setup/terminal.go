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
	"sync/atomic"
	"syscall"
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
	// Sig records that a command died of an interrupt, see Interrupts. Nil: not
	// recorded, and only the context tells.
	Sig *Interrupts
}

// Interrupts is the sticky record that a command was ended by an interrupt.
// Ctrl-C reaches the command and whr together, and the command often dies
// before whr's signal goroutine cancels the context; a check returns only a
// status, so the mark on its error is lost. The record is what Run, the
// re-check and the guards before a question or a prefix check read besides
// ctx.Err().
type Interrupts struct{ seen atomic.Bool }

// Seen says whether a command died of an interrupt since the last Reset.
func (i *Interrupts) Seen() bool { return i != nil && i.seen.Load() }

// Reset forgets what was seen; Run does it at its start.
func (i *Interrupts) Reset() {
	if i != nil {
		i.seen.Store(false)
	}
}

func (i *Interrupts) note() {
	if i != nil {
		i.seen.Store(true)
	}
}

// Interrupted implements InterruptSeen.
func (t Terminal) Interrupted() bool { return t.Sig.Seen() }

// ResetInterrupts implements InterruptSeen.
func (t Terminal) ResetInterrupts() { t.Sig.Reset() }

// InterruptSeen is what a Host adds to tell Run that a command died of an
// interrupt while the context was still live.
type InterruptSeen interface {
	Interrupted() bool
	ResetInterrupts()
}

// Interrupted says whether the context ended or the host saw a command die of
// an interrupt: the person pressed Ctrl-C, whichever of the two noticed first.
func Interrupted(ctx context.Context, h any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s, ok := h.(InterruptSeen); ok && s.Interrupted() {
		return context.Canceled
	}
	return nil
}

// Output implements doctor.Runner.
func (t Terminal) Output(ctx context.Context, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // a read-only command named by the steps
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := t.interruptOr(ctx, cmd.Run())
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
	return t.interruptOr(ctx, err)
}

// interruptOr marks the error of a command that an interrupt ended, so the
// callers see the context error: os/exec reports the signal that killed the
// command, not the context.
//
// A command that was killed by SIGINT, SIGTERM or SIGHUP, or that caught one
// and exited 130, 129 or 143 (the shell convention, as a trap does), counts as
// interrupted and is recorded in t.Sig.
func (t Terminal) interruptOr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	if isInterruptEnd(err) {
		// Ctrl-C reaches the command and whr together, and the command often
		// dies first: that is an interrupt even before the context says so
		t.Sig.note()
		return fmt.Errorf("%w: %w", context.Canceled, err)
	}
	return err
}

// isInterruptEnd says whether a command ended by an interrupt signal or exited
// with the status a shell gives one.
func isInterruptEnd(err error) bool {
	if sig, ok := killedBy(err); ok {
		return sig == syscall.SIGINT || sig == syscall.SIGTERM || sig == syscall.SIGHUP
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		switch ee.ExitCode() {
		case 128 + int(syscall.SIGHUP), 128 + int(syscall.SIGINT), 128 + int(syscall.SIGTERM):
			return true
		}
	}
	return false
}

// killedBy is the signal that ended a command, if one did.
func killedBy(err error) (syscall.Signal, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return ws.Signal(), true
		}
	}
	return 0, false
}

// Open implements Host. It runs `open`, so a URL or a System Settings pane opens
// the way a click on it would.
func (Terminal) Open(ctx context.Context, target string) error {
	return exec.CommandContext(ctx, "open", target).Run() //nolint:gosec // opens a link the step names
}

// Line implements doctor.Prompter.
func (t Terminal) Line(question string) (string, error) {
	fmt.Fprint(t.Err, render.Question(t.Style, question+":"))
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
	fmt.Fprint(t.Err, render.Question(t.Style, question+":"))
	b, err := term.ReadPassword(int(t.Stdin.Fd())) //nolint:gosec // a file descriptor of this process
	fmt.Fprintln(t.Err)
	return string(b), err
}

// Confirm implements doctor.Prompter: only y or yes is a yes. Nested
// configuration writes still require explicit assent; q stops the setup run.
func (t Terminal) Confirm(question string) (bool, error) {
	a, err := t.Ask(question, render.DefaultNo)
	if a == render.Quit {
		return false, render.ErrQuit
	}
	return a == render.Yes, err
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
