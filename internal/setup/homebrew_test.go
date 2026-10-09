package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
)

const fakeInstallScript = "#!/bin/bash\necho fake homebrew installer\n"

func homebrewStep(t *testing.T, h *fakeHost, dl func(context.Context, string) ([]byte, error)) []doctor.Check {
	t.Helper()
	d := doctor.Deps{GOOS: "darwin", Runner: h, User: "werner", Account: "werner", Download: dl}
	return doctor.Steps(doctor.Checks(d), doctor.PhaseHost)
}

func runHomebrew(h *fakeHost, steps []doctor.Check, o Options) ([]Outcome, string, error) {
	var so, se bytes.Buffer
	o.Out, o.Err = &so, &se
	o.Phase, o.Only = doctor.PhaseHost, []string{"homebrew"}
	outs, err := Run(context.Background(), steps, h, o)
	return outs, so.String() + se.String(), err
}

func TestHomebrewIsDownloadedShownConfirmedAndRun(t *testing.T) {
	var gotURL string
	h := &fakeHost{answers: []string{"y", "y"}} // run the step, then run the script
	steps := homebrewStep(t, h, func(_ context.Context, url string) ([]byte, error) {
		gotURL = url
		return []byte(fakeInstallScript), nil
	})
	if _, _, err := runHomebrew(h, steps, Options{}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(fakeInstallScript))
	shown := strings.Join(h.shown, "\n")
	if gotURL != doctor.HomebrewInstallURL || !strings.Contains(shown, hex.EncodeToString(sum[:])) || !strings.Contains(shown, doctor.HomebrewInstallURL) || !strings.Contains(shown, "install.sh") {
		t.Errorf("url %q shown %q", gotURL, shown)
	}
	var script string
	for _, r := range h.ran {
		if strings.HasPrefix(r, "/usr/bin/env NONINTERACTIVE=1 /bin/bash ") {
			script = strings.TrimPrefix(r, "/usr/bin/env NONINTERACTIVE=1 /bin/bash ")
		}
	}
	if script == "" {
		t.Fatalf("the installer did not run: %v", h.ran)
	}
	if _, err := os.Stat(script); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the downloaded file is still there: %v", err)
	}
}

func TestHomebrewDoesNotRunWithoutConfirmation(t *testing.T) {
	for name, o := range map[string]Options{"asked": {}, "yes": {Yes: true}} {
		t.Run(name, func(t *testing.T) {
			answers := []string{"y", "n"} // step yes, script no
			if o.Yes {
				answers = []string{"n"} // --yes answers the step, never the script
			}
			h := &fakeHost{answers: answers}
			steps := homebrewStep(t, h, func(context.Context, string) ([]byte, error) { return []byte(fakeInstallScript), nil })
			_, _, _ = runHomebrew(h, steps, o)
			for _, r := range h.ran {
				if strings.Contains(r, "install.sh") || strings.Contains(r, "sudo") {
					t.Errorf("ran %q without confirmation", r)
				}
			}
			if !strings.Contains(strings.Join(h.shown, "\n"), "sha256:") {
				t.Errorf("the hash was not shown: %v", h.shown)
			}
		})
	}
}

func TestHomebrewUnattendedRunsNothing(t *testing.T) {
	h := &fakeHost{}
	called := false
	steps := homebrewStep(t, h, func(context.Context, string) ([]byte, error) { called = true; return []byte(fakeInstallScript), nil })
	_, _, _ = runHomebrew(h, steps, Options{Unattended: true, Yes: true})
	if called || len(h.ran) != 0 {
		t.Errorf("unattended downloaded %v or ran %v", called, h.ran)
	}
}

func TestHomebrewFailedDownloadIsReported(t *testing.T) {
	h := &fakeHost{answers: []string{"y", "y"}}
	steps := homebrewStep(t, h, func(context.Context, string) ([]byte, error) { return nil, errors.New("no route to host") })
	outs, text, err := runHomebrew(h, steps, Options{})
	if err != nil {
		text += err.Error()
	}
	if !strings.Contains(text, "could not download") || !strings.Contains(text, "no route to host") || len(outs) != 1 || outs[0].Fixed {
		t.Errorf("err %v outs %+v text %q", err, outs, text)
	}
	if len(h.ran) != 0 {
		t.Errorf("ran %v after a failed download", h.ran)
	}
}

// Issue #507: without sudo nothing is downloaded or asked for Homebrew; the
// run says to hand it to an administrator.
func TestHomebrewIsNotDownloadedWhenTheAccountCannotSudo(t *testing.T) {
	h := &fakeHost{answers: []string{"y", "y"}}
	steps := homebrewStep(t, h, func(context.Context, string) ([]byte, error) {
		t.Error("the installer was downloaded")
		return nil, errors.New("no")
	})
	outs, out, _ := runHomebrew(h, steps, Options{NoSudo: true, Resume: []string{"whr", "setup"}})
	if len(outs) != 1 || !outs[0].NeedsAdmin || len(h.ran) != 0 || !strings.Contains(out, "whr setup --only homebrew") {
		t.Errorf("outs %+v ran %v out %q", outs, h.ran, out)
	}
}
