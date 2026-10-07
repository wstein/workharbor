package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/runlog"
)

// A probe runs once per run: the doctor and the wizard share its answer, and
// a command that is run for a fix makes the next probe run again.
func TestTerminalRunsAProbeOnce(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	log, err := runlog.Open(filepath.Join(dir, "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	h := Terminal{Probes: &Probes{}, Log: log}
	probe := []string{"sh", "-c", "echo x >> " + count + "; echo answer"}
	calls := func() int {
		b, _ := os.ReadFile(count) //nolint:gosec // a test path
		return strings.Count(string(b), "x")
	}
	for range 3 {
		out, err := h.Output(context.Background(), probe...)
		if err != nil || strings.TrimSpace(string(out)) != "answer" {
			t.Fatalf("out %q err %v", out, err)
		}
	}
	if n := calls(); n != 1 {
		t.Errorf("probe ran %d times, want 1", n)
	}
	if err := h.Run(context.Background(), doctor.Cmd{Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = h.Output(context.Background(), probe...)
	if n := calls(); n != 2 {
		t.Errorf("after a fix the probe ran %d times in all, want 2", n)
	}
	_ = log.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "run.log")) //nolint:gosec // a test path
	if n := strings.Count(string(b), "$ sh -c"); n != 2 {
		t.Errorf("the log holds the probe %d times, want 2:\n%s", n, b)
	}
}

func TestExpectedAnswersAreNamed(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		exit int
		want string
	}{
		{[]string{"dseditgroup", "-o", "checkmember", "-m", "u", "admin"}, 67, "not a member"},
		{[]string{"dseditgroup", "-o", "checkmember", "-m", "u", "admin"}, 1, ""},
		{[]string{"dscl", ".", "-read", "/Users/u", "UniqueID"}, 56, "no such record"},
		{[]string{"defaults", "read", "/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay"}, 1, "key not set"},
		{[]string{"defaults", "read", "x", "y"}, 1, ""},
		{[]string{"pmset", "-g"}, 67, ""},
	} {
		if got := doctor.ExpectedAnswer(tc.argv, tc.exit); got != tc.want {
			t.Errorf("%v exit %d: %q, want %q", tc.argv, tc.exit, got, tc.want)
		}
	}
}
