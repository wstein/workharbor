package setup

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
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

// silentFailHost fails a command without logging any output.
type silentFailHost struct{ fakeHost }

func (silentFailHost) Run(context.Context, doctor.Cmd) error { return errors.New("exit status 5") }

func TestFailureTailIsNotTheOutputOfAnEarlierStep(t *testing.T) {
	lg, err := runlog.Open(filepath.Join(t.TempDir(), "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()
	stale := "STALE-" + strconv.Itoa(rand.Int())  //nolint:gosec // a random marker, not a secret
	lg.Command([]string{"earlier"}, 0, stale, "") // the previous step's output
	var fixed bool
	bad := step("account", doctor.PhaseHost, &fixed, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"mktool", "go"}}}})
	h := &silentFailHost{fakeHost{answers: []string{"y"}}}
	var so, se strings.Builder
	_, _ = Run(bg, []doctor.Check{bad}, h, Options{Phase: doctor.PhaseHost, Out: &so, Err: &se, RunLog: lg, Resume: []string{"whr", "setup", "host"}})
	if strings.Contains(se.String(), stale) {
		t.Errorf("the failure shows an earlier step's output:\n%s", se.String())
	}
}

// Issue #397: the cause is the tool's last line, the output is framed once,
// and the command that ends the next action is never wrapped.
func TestFailureSummaryCauseIsTheToolsLastLineAndTheCommandIsNotWrapped(t *testing.T) {
	lg, err := runlog.Open(filepath.Join(t.TempDir(), "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()
	long := "/Users/workharbor/.local/libexec/whr/whr-shim-linux-arm64"
	lg.Command([]string{"whr", "tools", "build"}, 1, "whr tools build: open "+long+": no such file or directory\n", "")
	var b strings.Builder
	FailureSummaryCmd(render.Writer{W: &b}, lg, causeOf(lg, "whr tools build failed: exit status 1"), "fix the cause, then run:", "whr setup --from tool-store --prefix "+long+"/with/a/long/prefix/that/passes/eighty/columns")
	got := b.String()
	if !strings.Contains(strings.Join(strings.Fields(got), " "), "cause whr tools build: open "+long+": no such file or directory") {
		t.Errorf("cause:\n%s", got)
	}
	if strings.Count(got, "tool output:") != 0 || strings.Count(got, "last output:") != 1 || strings.Contains(got, "| |") {
		t.Errorf("output framed wrongly:\n%s", got)
	}
	if !strings.Contains(got, "$ whr setup --from tool-store --prefix "+long+"/with/a/long/prefix/that/passes/eighty/columns\n") {
		t.Errorf("command wrapped:\n%s", got)
	}
}
