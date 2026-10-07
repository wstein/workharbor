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

func TestAutoYesAnswersOnlyUndoableSteps(t *testing.T) {
	_, out, w := asker("")
	a, ok := AutoYes(w, "Go on?", DefaultYes, true)
	if !ok || a != Yes || !strings.Contains(out.String(), "yes: Go on?") {
		t.Errorf("undoable with --yes: %v %v %q", a, ok, out.String())
	}
	_, out, w = asker("")
	if a, ok = AutoYes(w, "Delete?", DefaultNo, true); ok || a != No || out.String() != "" {
		t.Errorf("destructive must not be answered: %v %v %q", a, ok, out.String())
	}
	if _, ok = AutoYes(w, "Go on?", DefaultYes, false); ok {
		t.Error("without --yes nothing is answered")
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
