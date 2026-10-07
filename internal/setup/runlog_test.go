package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/runlog"
)

// failingHost fails every command after logging it, as Terminal does.
type failingHost struct {
	fakeHost
	lg *runlog.Log
}

func (f *failingHost) Run(_ context.Context, c doctor.Cmd) error {
	f.lg.Command(c.Full(), 5, "line1\nline2\nboom: denied\n", "")
	return errors.New("exit status 5")
}

// Issue #379: one log line per step; a failure shows the cause, the next action,
// the tail of the step's output and the log path.
func TestRunLogHasStepLinesAndAFailureSummary(t *testing.T) {
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, err := runlog.Open(lp)
	if err != nil {
		t.Fatal(err)
	}
	var fixed bool
	good := doctor.Check{Name: "power", Phase: doctor.PhaseHost, Run: func(context.Context) (doctor.Status, string) { return doctor.OK, "fine" }}
	bad := step("account", doctor.PhaseHost, &fixed, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"mktool", "go"}}}})
	unk := doctor.Check{Name: "later", Phase: doctor.PhaseHost, Run: func(context.Context) (doctor.Status, string) { return doctor.NotVerified, "cannot tell" }}
	h := &failingHost{fakeHost: fakeHost{answers: []string{"y"}}, lg: lg}
	var so, se strings.Builder
	_, _ = Run(bg, []doctor.Check{good, bad, unk}, h, Options{Phase: doctor.PhaseHost, Out: &so, Err: &se, RunLog: lg, Resume: []string{"whr", "setup", "host"}})
	_ = lg.Close()
	log := readFile(t, lp)
	for _, want := range []string{"step power: ok fine", "step account: fail broken", "step account: fail mktool go failed", "step later: unknown cannot tell"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	e := se.String()
	for _, want := range []string{"cause", "ACTION", "whr setup host --from account", "boom: denied", lp} {
		if !strings.Contains(e, want) {
			t.Errorf("failure summary lacks %q:\n%s", want, e)
		}
	}
	if fi, _ := os.Stat(lp); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}
