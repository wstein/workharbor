package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
)

// ownerDeps is a workspace root that exists and belongs to someone else, so the
// workspace-folders fix must ask before it changes the owner.
func ownerDeps(t *testing.T) (*fakeHost, []doctor.Check) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "ws")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(base, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"roots":{"workspaces":["`+root+`"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	df := "Filesystem 512-blocks Used Available Capacity Mounted on\n/dev/disk3s1 100 1 99 1% /System/Volumes/Data\n"
	h := &fakeHost{outputs: map[string]string{
		"df -P " + root:           df,
		"stat -f %Su %Lp " + root: "alice 700\n",
	}}
	d := doctor.Deps{GOOS: "darwin", Runner: h, ConfigPath: cfg, Home: base}
	for _, c := range doctor.Checks(d) {
		if c.Name == "workspace-folders" {
			return h, []doctor.Check{c}
		}
	}
	t.Fatal("no workspace-folders step")
	return nil, nil
}

func TestYesNeverAnswersTheOwnerQuestionAndNoSudoIsPrimedForNothing(t *testing.T) {
	h, steps := ownerDeps(t)
	var so, se bytes.Buffer
	_, _ = Run(bg, steps, h, Options{Phase: doctor.PhaseHost, Yes: true, Out: &so, Err: &se})
	asked := false
	for _, q := range h.asked {
		asked = asked || strings.Contains(q, "Change the owner")
	}
	if !asked {
		t.Errorf("--yes did not leave the owner question to the person: %v", h.asked)
	}
	if len(h.ran) != 0 {
		t.Errorf("a declined chown ran %v (not even sudo -v)", h.ran)
	}
}

func TestUnattendedAsksNoOwnerQuestionAndRunsNothing(t *testing.T) {
	h, steps := ownerDeps(t)
	var so, se bytes.Buffer
	_, _ = Run(bg, steps, h, Options{Phase: doctor.PhaseHost, Yes: true, Unattended: true, Out: &so, Err: &se})
	if len(h.ran) != 0 || len(h.asked) != 0 {
		t.Errorf("unattended ran %v asked %v", h.ran, h.asked)
	}
}
