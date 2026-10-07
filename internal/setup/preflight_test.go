package setup

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
)

func init() {
	// the same on every machine: only sudo is found
	lookPath = func(name string) (string, error) {
		if name == "sudo" {
			return "/usr/bin/sudo", nil
		}
		return "", errors.New("not found")
	}
}

func TestPreflightIsOneLineBeforeTheFirstStep(t *testing.T) {
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb}
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	got := errb.String()
	if n := strings.Count(got, "preflight:"); n != 1 {
		t.Fatalf("%d preflight lines:\n%s", n, got)
	}
	i := strings.Index(got, "preflight:")
	if strings.Index(got, "Step 1") < i && strings.Contains(got, "Step 1") {
		t.Errorf("preflight comes after the first step:\n%s", got)
	}
	for _, w := range []string{"found: sudo", "missing: brew, container", "change nothing (dry run)", "a new run"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

func TestNewSetupLinesFitIn80Columns(t *testing.T) {
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb}
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(errb.String(), "\n") {
		if strings.Contains(l, "preflight:") || strings.HasPrefix(l, "           ") &&
			strings.Contains(l, "Next:") {
			if n := len([]rune(l)); n > 80 {
				t.Errorf("over 80 columns (%d): %q", n, l)
			}
		}
	}
}

func TestYesStillAsksGuidedStepsAndReadyToRun(t *testing.T) {
	guided := &doctor.Fix{Guide: "Turn the toggle off."}
	cmds := &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"true"}}}}
	for name, tc := range map[string]struct {
		fix  *doctor.Fix
		want string // asked even with Yes
	}{
		"guided":  {guided, "Done with this step?"},
		"command": {cmds, ""},
	} {
		h := &fakeHost{}
		var out, errb bytes.Buffer
		o := Options{Phase: doctor.PhaseHost, Yes: true, Out: &out, Err: &errb}
		steps := []doctor.Check{fixStep("s", doctor.Fail, "bad", tc.fix)}
		if _, err := Run(bg, steps, h, o); err != nil {
			t.Fatal(name, err)
		}
		if tc.want != "" {
			if len(h.asked) == 0 || !strings.Contains(h.asked[len(h.asked)-1], tc.want) {
				t.Errorf("%s: --yes answered the person's own work: %v", name, h.asked)
			}
			continue
		}
		if len(h.asked) != 0 || !strings.Contains(errb.String(), "yes: Ready to run") {
			t.Errorf("%s: --yes did not answer Ready to run: %v\n%s", name, h.asked, errb.String())
		}
	}
}

func TestToolFoundLooksForBrewAtTheManagedPrefixFirst(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	var asked []string
	// brew exists only at the managed prefix, not on PATH
	lookPath = func(name string) (string, error) {
		asked = append(asked, name)
		if name == managedBrew {
			return name, nil
		}
		return "", errors.New("not found")
	}
	if !toolFound("brew") {
		t.Error("brew at the managed prefix was not found")
	}
	if len(asked) != 1 || asked[0] != "/opt/homebrew/bin/brew" {
		t.Errorf("looked up %v, want the managed path first and only", asked)
	}
	// a tool other than brew is looked up by name
	asked = nil
	if toolFound("sudo") || len(asked) != 1 || asked[0] != "sudo" {
		t.Errorf("sudo: asked %v", asked)
	}
}

// A Homebrew at the managed prefix counts as found in the preflight line even
// when no brew is on PATH.
func TestPreflightFindsTheManagedBrewOffPath(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(name string) (string, error) {
		if name == managedBrew {
			return name, nil
		}
		return "", errors.New("not found")
	}
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb}
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	if got := errb.String(); !strings.Contains(got, "found: brew") || !strings.Contains(got, "missing: sudo, container") {
		t.Errorf("preflight:\n%s", got)
	}
}
