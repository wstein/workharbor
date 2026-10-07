package render

import (
	"strings"
	"testing"
)

func TestFinalBlockHasEverySection(t *testing.T) {
	s := Detect(false, "", false)
	got := FinalBlock(s, Final{
		Changed: []string{"Added the account."},
		Backups: [][2]string{{"the old file", "/tmp/x.bak"}},
		Todo:    []FinalTodo{{"Log in once.", "whr doctor"}},
		LogPath: "/tmp/run.log",
	})
	for _, want := range []string{
		"What changed", "Added the account.", "/tmp/x.bak",
		"ACTION", "Log in once.", "whr doctor", "/tmp/run.log",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if FinalBlock(s, Final{}) != "" {
		t.Error("empty Final must write nothing")
	}
	for _, l := range strings.Split(got, "\n") {
		if len([]rune(l)) > 80 {
			t.Errorf("line over 80 columns: %q", l)
		}
	}
}

func TestAskAutoSkipsOnlyUndoableSteps(t *testing.T) {
	in, out, w := asker("n\n")
	a, err := AskAuto(in, w, "Go on?", DefaultYes, true)
	if err != nil || a != Yes || strings.Contains(out.String(), "[Y/n/q]") {
		t.Errorf("undoable with --yes: %v %v %q", a, err, out.String())
	}
	in, out, w = asker("\n")
	a, _ = AskAuto(in, w, "Delete?", DefaultNo, true)
	if a != No || !strings.Contains(out.String(), "[y/N/q]") {
		t.Errorf("destructive must still ask: %v %q", a, out.String())
	}
	in, _, w = asker("n\n")
	if a, _ = AskAuto(in, w, "Go on?", DefaultYes, false); a != No {
		t.Errorf("without --yes it asks: %v", a)
	}
}

func TestPreflightIsOneLineWithAllFourFacts(t *testing.T) {
	got := Preflight(Detect(false, "", false), []string{"sudo"}, nil,
		"no config", "check 3 steps.")
	for _, w := range []string{"found: sudo", "missing: none", "no config", "check 3"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in %q", w, got)
		}
	}
}
