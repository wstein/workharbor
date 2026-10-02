package cli

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"
)

// TTY is the user's terminal as `whr console` needs it: the bytes in and out, raw
// mode while a shell runs, the size, and a signal when the size changes.
type TTY interface {
	Reader() io.Reader
	Writer() io.Writer
	// MakeRaw puts the terminal in raw mode, so every key goes to the shell, and
	// returns what restores it.
	MakeRaw() (restore func(), err error)
	Size() (cols, rows uint16, err error)
	// Resizes returns a channel that receives when the terminal's size changed,
	// and a function that stops it.
	Resizes() (<-chan struct{}, func())
}

// errNoTerminal is returned when whr console runs without a terminal.
var errNoTerminal = errors.New("whr console needs a terminal: standard input and output are not one")

// osTTY is the terminal behind a pair of files.
type osTTY struct{ in, out *os.File }

// osTerminal returns the terminal of Stdin and Stdout if they are files and a
// terminal, and errNoTerminal otherwise.
func osTerminal(in io.Reader, out io.Writer) (TTY, error) {
	i, iok := in.(*os.File)
	o, ook := out.(*os.File)
	if !iok || !ook || !term.IsTerminal(int(i.Fd())) || !term.IsTerminal(int(o.Fd())) { //nolint:gosec // a file descriptor of this process fits an int
		return nil, errNoTerminal
	}
	return osTTY{in: i, out: o}, nil
}

func (t osTTY) Reader() io.Reader { return t.in }
func (t osTTY) Writer() io.Writer { return t.out }

func (t osTTY) MakeRaw() (func(), error) {
	fd := int(t.in.Fd()) //nolint:gosec // a file descriptor of this process fits an int
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(fd, old) }, nil
}

func (t osTTY) Size() (uint16, uint16, error) {
	w, h, err := term.GetSize(int(t.out.Fd())) //nolint:gosec // a file descriptor of this process fits an int
	if err != nil || w <= 0 || h <= 0 || w > 65535 || h > 65535 {
		return 80, 24, err
	}
	return uint16(w), uint16(h), nil
}
