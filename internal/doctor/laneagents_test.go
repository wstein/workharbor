package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func laneRepo(t *testing.T) (repo, home string) {
	t.Helper()
	dir := t.TempDir()
	repo, home = filepath.Join(dir, "repo"), filepath.Join(dir, "home")
	for _, d := range []string{filepath.Join(repo, ".claude", "agents"), filepath.Join(home, ".claude")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []string{"wh-worker", "wh-reviewer", "wh-docs-reviewer", "wh-helper", "wh-platform"} {
		if err := os.WriteFile(filepath.Join(repo, ".claude", "agents", a+".md"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo, home
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLaneAgentsWarnWhenARuleIsMissing(t *testing.T) {
	repo, home := laneRepo(t)
	writeSettings(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"permissions":{"allow":["Agent(wh-worker)"]}}`)
	st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil)
	if st != Warn || !strings.Contains(detail, "wh-reviewer") || strings.Contains(detail, "wh-worker,") || !strings.Contains(detail, `"Agent(wh-helper)"`) {
		t.Errorf("got %s: %s", st, detail)
	}
}

func TestLaneAgentsAreOKWithRulesAcrossFilesOrABareAgentRule(t *testing.T) {
	repo, home := laneRepo(t)
	writeSettings(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"permissions":{"allow":["Agent(wh-worker)","Agent(wh-reviewer)"]}}`)
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), `{"permissions":{"allow":["Agent(wh-docs-reviewer)","Agent(wh-helper)","Agent(wh-platform)"]}}`)
	if st, d := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil); st != OK {
		t.Errorf("rules across files: %s: %s", st, d)
	}
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), `{"permissions":{"allow":["Agent"]}}`)
	_ = os.Remove(filepath.Join(repo, ".claude", "settings.local.json"))
	if st, d := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil); st != OK {
		t.Errorf("bare Agent: %s: %s", st, d)
	}
}

func TestLaneAgentsNameAnUnreadableFileAndNeverFail(t *testing.T) {
	repo, home := laneRepo(t)
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), `{not json`)
	st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil)
	if st != Warn || !strings.Contains(detail, filepath.Join(home, ".claude", "settings.json")) {
		t.Errorf("got %s: %s", st, detail)
	}
}

func TestLaneAgentsNeedACheckoutWithLaneAgents(t *testing.T) {
	if st, _ := laneAgentsCheck(Deps{RepoDir: t.TempDir()})(nil); st != OK {
		t.Errorf("status = %s", st)
	}
}

func TestLaneAgentsAreFoundByGlob(t *testing.T) {
	repo, _ := laneRepo(t)
	if got := laneAgents(repo); len(got) != 5 {
		t.Errorf("got %v", got)
	}
}

func TestLaneAgentsTreatAFIFOAsUnreadable(t *testing.T) {
	repo, home := laneRepo(t)
	if err := syscall.Mkfifo(filepath.Join(home, ".claude", "settings.json"), 0o600); err != nil {
		t.Skip(err)
	}
	st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil)
	if st != Warn || !strings.Contains(detail, "could not read") {
		t.Errorf("got %s: %s", st, detail)
	}
}

func TestLaneAgentsTreatAnOversizedFileAsUnreadable(t *testing.T) {
	repo, home := laneRepo(t)
	big := `{"permissions":{"allow":["Agent"]},"x":"` + strings.Repeat("a", maxSettingsSize) + `"}`
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), big)
	st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil)
	if st != Warn || !strings.Contains(detail, "could not read") {
		t.Errorf("got %s: %s", st, detail)
	}
}

func TestLaneAgentsIgnoreNamesThatDoNotMatch(t *testing.T) {
	repo, _ := laneRepo(t)
	writeSettings(t, filepath.Join(repo, ".claude", "agents", `wh-x"),"Agent.md`), "x")
	writeSettings(t, filepath.Join(repo, ".claude", "agents", "wh-Upper.md"), "x")
	if got := laneAgents(repo); len(got) != 5 {
		t.Errorf("got %v", got)
	}
}

func TestLaneAgentsReadASymlinkToARegularFileButNotToAFIFO(t *testing.T) {
	repo, home := laneRepo(t)
	target := filepath.Join(t.TempDir(), "real.json")
	writeSettings(t, target, `{"permissions":{"allow":["Agent"]}}`)
	link := filepath.Join(home, ".claude", "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil); st != OK || strings.Contains(detail, "could not read") {
		t.Errorf("symlink to a file: got %s: %s", st, detail)
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fifo, link); err != nil {
		t.Skip(err)
	}
	if st, detail := laneAgentsCheck(Deps{RepoDir: repo, Home: home})(nil); st != Warn || !strings.Contains(detail, "could not read") {
		t.Errorf("symlink to a FIFO: got %s: %s", st, detail)
	}
}
