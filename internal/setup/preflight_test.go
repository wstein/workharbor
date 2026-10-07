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
	for _, w := range []string{"found: sudo", "missing: brew, container", "change nothing (dry run)"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}
