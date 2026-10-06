package render

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Default is what Enter answers.
type Default int

const (
	// DefaultYes is for steps that can be undone: [Y/n/q], Enter is yes.
	DefaultYes Default = iota
	// DefaultNo is for steps that cannot (deleting an account or a volume,
	// overwriting data): [y/N/q], Enter is no.
	DefaultNo
)

func (d Default) suffix() string {
	if d == DefaultYes {
		return "[Y/n/q]"
	}
	return "[y/N/q]"
}

// Answer is what the person decided.
type Answer int

// The answers.
const (
	No Answer = iota
	Yes
	Quit
)

// ErrQuit is returned when the person answers q: the run stops cleanly.
var ErrQuit = errors.New("quit")

var errEnded = errors.New("no answer: standard input ended")

func readLine(in *bufio.Reader) (string, error) {
	s, err := in.ReadString('\n')
	if err != nil && s == "" {
		return "", errEnded
	}
	return strings.TrimSpace(s), nil
}

func isQuit(s string) bool {
	s = strings.ToLower(s)
	return s == "q" || s == "quit"
}

// Ask asks a yes or no question as an ACTION line and reads the answer. Enter
// takes the default; q or quit quits; anything that is not clearly yes counts
// as no, so a typo never runs a command.
func Ask(in *bufio.Reader, w Writer, question string, d Default) (Answer, error) {
	w.Question(question, d)
	s, err := readLine(in)
	if err != nil {
		return No, err
	}
	switch {
	case isQuit(s):
		return Quit, nil
	case s == "":
		if d == DefaultYes {
			return Yes, nil
		}
		return No, nil
	case strings.EqualFold(s, "y"), strings.EqualFold(s, "yes"):
		return Yes, nil
	}
	return No, nil
}

// AskWord asks for a typed word, for the most destructive steps. Only the exact
// word is yes; Enter and anything else is no; q quits.
func AskWord(in *bufio.Reader, w Writer, question, word string) (Answer, error) {
	w.put(Question(w.S, fmt.Sprintf("%s Type %q to go on, or q to quit:", question, word)))
	s, err := readLine(in)
	if err != nil {
		return No, err
	}
	switch {
	case isQuit(s):
		return Quit, nil
	case s == word:
		return Yes, nil
	}
	return No, nil
}

// PickOptions describe a selection menu. The numbered list is the one that works
// everywhere (SSH, logs, no extra program); fzf is optional.
type PickOptions struct {
	Title   string
	Items   []string
	Default int // index taken by Enter
	In      *bufio.Reader
	W       Writer
	// HaveFzf reports whether fzf is installed (nil: no). Fzf runs it and
	// returns the chosen index.
	HaveFzf func() bool
	Fzf     func(items []string) (int, error)
	// StdinTTY and StdoutTTY say whether both are terminals; NoColor is
	// NO_COLOR set; Plain is --plain.
	StdinTTY, StdoutTTY, NoColor, Plain bool
}

// UseFzf is true only when fzf is installed AND stdin and stdout are terminals
// AND neither NO_COLOR nor --plain is set.
func (o PickOptions) UseFzf() bool {
	return o.HaveFzf != nil && o.Fzf != nil && o.HaveFzf() && o.StdinTTY && o.StdoutTTY && !o.NoColor && !o.Plain
}

// Pick returns the index of the chosen item, or ErrQuit.
func Pick(o PickOptions) (int, error) {
	if len(o.Items) == 0 {
		return 0, errors.New("nothing to choose from")
	}
	if o.UseFzf() {
		if i, err := o.Fzf(o.Items); err == nil {
			return i, nil
		}
		// fzf failed or was cancelled: the numbered list still works
	}
	if o.Title != "" {
		o.W.put(o.Title + "\n")
	}
	for i, it := range o.Items {
		o.W.put(fmt.Sprintf("  %d) %s\n", i+1, it))
	}
	for {
		o.W.put(Question(o.W.S, fmt.Sprintf("Choose 1-%d, Enter for %d, q to quit:", len(o.Items), o.Default+1)))
		s, err := readLine(o.In)
		if err != nil {
			return 0, err
		}
		if isQuit(s) {
			return 0, ErrQuit
		}
		if s == "" {
			return o.Default, nil
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(o.Items) {
			return n - 1, nil
		}
		o.W.put(Report(o.W.S, LevelWarn, "not a number from the list"))
	}
}
