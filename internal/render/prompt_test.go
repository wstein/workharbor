package render

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func asker(input string) (*bufio.Reader, *bytes.Buffer, Writer) {
	var out bytes.Buffer
	return bufio.NewReader(strings.NewReader(input)), &out, Writer{W: &out, S: Detect(false, "", false)}
}

func TestPromptDefaultsPerStepClass(t *testing.T) {
	for name, tc := range map[string]struct {
		def    Default
		input  string
		want   Answer
		suffix string
	}{
		"reversible: Enter is yes":     {DefaultYes, "\n", Yes, "[Y/n/q]"},
		"reversible: n is no":          {DefaultYes, "n\n", No, "[Y/n/q]"},
		"reversible: q quits":          {DefaultYes, "q\n", Quit, "[Y/n/q]"},
		"reversible: a typo is no":     {DefaultYes, "yy\n", No, "[Y/n/q]"},
		"irreversible: Enter is no":    {DefaultNo, "\n", No, "[y/N/q]"},
		"irreversible: y is yes":       {DefaultNo, "y\n", Yes, "[y/N/q]"},
		"irreversible: YES is yes":     {DefaultNo, " YES \n", Yes, "[y/N/q]"},
		"irreversible: quit quits":     {DefaultNo, "quit\n", Quit, "[y/N/q]"},
		"reversible: no final newline": {DefaultYes, "y", Yes, "[Y/n/q]"},
	} {
		in, out, w := asker(tc.input)
		got, err := Ask(in, w, "Ready?", tc.def)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v, %v, want %v", name, got, err, tc.want)
		}
		if !strings.Contains(out.String(), tc.suffix) {
			t.Errorf("%s: prompt %q lacks %s", name, out.String(), tc.suffix)
		}
	}
}

func TestPromptEndOfInputIsAnErrorNotAYes(t *testing.T) {
	in, _, w := asker("")
	if a, err := Ask(in, w, "Ready?", DefaultYes); err == nil || a == Yes {
		t.Errorf("EOF = %v, %v", a, err)
	}
}

func TestTypedWordIsRequiredForTheIrreversible(t *testing.T) {
	for in, want := range map[string]Answer{"delete\n": Yes, "y\n": No, "\n": No, "q\n": Quit, "DELETE\n": No} {
		r, _, w := asker(in)
		if got, err := AskWord(r, w, "Delete the volume?", "delete"); err != nil || got != want {
			t.Errorf("%q: %v %v, want %v", in, got, err, want)
		}
	}
}

func TestPickerFallsBackToTheNumberedListWhenFzfIsMissing(t *testing.T) {
	for name, o := range map[string]PickOptions{
		"fzf missing": {HaveFzf: func() bool { return false }, StdinTTY: true, StdoutTTY: true},
		"stdin pipe":  {HaveFzf: func() bool { return true }, StdinTTY: false, StdoutTTY: true},
		"stdout pipe": {HaveFzf: func() bool { return true }, StdinTTY: true, StdoutTTY: false},
		"NO_COLOR":    {HaveFzf: func() bool { return true }, StdinTTY: true, StdoutTTY: true, NoColor: true},
		"--plain":     {HaveFzf: func() bool { return true }, StdinTTY: true, StdoutTTY: true, Plain: true},
		"fzf not set": {StdinTTY: true, StdoutTTY: true},
	} {
		in, out, w := asker("2\n")
		o.In, o.W, o.Title, o.Items, o.Default = in, w, "Pick a step", []string{"a", "b", "c"}, 0
		o.Fzf = func([]string) (int, error) { t.Errorf("%s: fzf must not run", name); return 0, nil }
		got, err := Pick(o)
		if err != nil || got != 1 {
			t.Errorf("%s: %d %v", name, got, err)
		}
		if !strings.Contains(out.String(), "1) a") || !strings.Contains(out.String(), "3) c") {
			t.Errorf("%s: no numbered list in %q", name, out.String())
		}
	}
}

func TestPickerUsesFzfOnlyWhenInstalledAndInteractive(t *testing.T) {
	in, out, w := asker("")
	called := false
	got, err := Pick(PickOptions{
		In: in, W: w, Items: []string{"a", "b"}, HaveFzf: func() bool { return true }, StdinTTY: true, StdoutTTY: true,
		Fzf: func([]string) (int, error) { called = true; return 1, nil },
	})
	if err != nil || got != 1 || !called || out.Len() != 0 {
		t.Errorf("got %d %v called=%v out=%q", got, err, called, out.String())
	}
}

func TestPickerEnterTakesTheDefaultAndQQuits(t *testing.T) {
	o := func(input string) (int, error) {
		in, _, w := asker(input)
		return Pick(PickOptions{In: in, W: w, Items: []string{"a", "b", "c"}, Default: 2})
	}
	if got, err := o("\n"); err != nil || got != 2 {
		t.Errorf("Enter = %d %v", got, err)
	}
	if _, err := o("q\n"); !errors.Is(err, ErrQuit) {
		t.Errorf("q = %v", err)
	}
	if got, err := o("9\n1\n"); err != nil || got != 0 {
		t.Errorf("out of range then 1 = %d %v", got, err)
	}
}
