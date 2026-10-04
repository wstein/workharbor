package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

func shellRig(t *testing.T) *wsRig {
	t.Helper()
	r := newWsRig(t)
	r.egress = true
	r.ws.cfg.Shell = &ShellConfig{Bin: "/tools/profiles/p/bin", Dir: "/home/agent", Env: []string{"HOME=/home/agent", "CLAUDE_CONFIG_DIR=/home/agent/.claude"}}
	return r
}

func TestTheShellTargetIsTheEnvironmentAndTheVariablesOfARun(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	w, _ := r.create("docs-ws")
	got, err := r.ws.ShellTarget(bg, "docs-ws")
	if err != nil {
		t.Fatal(err)
	}
	if got.EnvID != string(w.EnvID) || got.Dir != "/home/agent" || got.Runtime != r.rt.Adapter.Name() {
		t.Errorf("target = %+v", got)
	}
	for _, want := range []string{"HOME=/home/agent", "CLAUDE_CONFIG_DIR=/home/agent/.claude"} {
		if !slices.Contains(got.Env, want) {
			t.Errorf("env %v lacks %s", got.Env, want)
		}
	}
	// The proxy variables are those a run gets, read from the runtime now.
	want := r.svc.agentEnv(bg, w.EnvID)
	if len(want) != 3 {
		t.Fatalf("the rig has no proxy: %v", want)
	}
	for _, e := range want {
		if !slices.Contains(got.Env, e) {
			t.Errorf("env %v lacks the run's %s", got.Env, e)
		}
	}
	// No secret is added: only the keys the run has.
	for _, e := range got.Env {
		k, _, _ := strings.Cut(e, "=")
		if !slices.Contains([]string{"HOME", "CLAUDE_CONFIG_DIR", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY"}, k) {
			t.Errorf("unexpected variable %s", k)
		}
	}
	if len(got.Cmd) < 5 || got.Cmd[len(got.Cmd)-1] != "/tools/profiles/p/bin" {
		t.Errorf("cmd = %v", got.Cmd)
	}
}

func TestTheShellIsRefusedWhileARunIsUnfinishedAndNamesIt(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	_, a := r.create("docs-ws")
	_, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ws.ShellTarget(bg, "docs-ws")
	var ce *domain.ConflictError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), string(run)) {
		t.Fatalf("err = %v, want a conflict that names run %s", err, run)
	}
}

func TestTheShellNeedsAConfigurationAndAWorkspace(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.create("docs-ws")
	var ce *domain.ConflictError
	if _, err := r.ws.ShellTarget(bg, "docs-ws"); !errors.As(err, &ce) {
		t.Errorf("without a shell configuration: %v, want a conflict", err)
	}
	r.ws.cfg.Shell = &ShellConfig{Bin: "/b", Dir: "/home/agent"}
	var nf *domain.NotFoundError
	if _, err := r.ws.ShellTarget(bg, "nope"); !errors.As(err, &nf) {
		t.Errorf("an unknown workspace: %v, want not found", err)
	}
}

// The agent writes its home, so the shell must not read or write a file there:
// no startup file, no ~/.inputrc (a planted binding could append a command to the
// typed line), no history, no terminfo (review of #281, M1 to M3).
func TestTheShellReadsAndWritesNoFileOfTheAgentsHome(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"exec bash --noprofile --norc --noediting -i",
		"INPUTRC=/dev/null", "HISTFILE=/dev/null", "EDITRC=/dev/null", "export INPUTRC HISTFILE EDITRC",
		"ENV=/dev/null; export ENV; exec sh -i",
	} {
		if !strings.Contains(shellScript, want) {
			t.Errorf("shell script lacks %q: %s", want, shellScript)
		}
	}
	// The variables are set before either shell starts, so the sh fallback has them too.
	if i := strings.Index(shellScript, "exec bash"); strings.Index(shellScript, "export INPUTRC HISTFILE EDITRC") > i {
		t.Errorf("INPUTRC and HISTFILE are exported after bash starts: %s", shellScript)
	}
}

// shellStub is a shell that reports how the script started it.
const shellStub = `#!/bin/sh
echo "ARGS=$*"
for v in INPUTRC HISTFILE EDITRC ENV TERMINFO TERMINFO_DIRS TERMCAP LOCPATH GCONV_PATH NLSPATH BASH_ENV CDPATH PROMPT_COMMAND; do
	eval "echo $v=\${$v-UNSET}"
done
`

// runShellScript runs the real script with a stub named name in the directory
// it is given as $1, with PATH holding nothing else when onlyStub is set, and
// planted variables of the kind an image or a caller could carry.
func runShellScript(t *testing.T, name string, onlyStub bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(shellStub), 0o755); err != nil { //nolint:gosec // a test stub has to be executable
		t.Fatal(err)
	}
	path := "/usr/bin:/bin"
	if onlyStub {
		path = "/nonexistent"
	}
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", shellScript, "whr-shell", dir) //nolint:gosec // the script is the constant under test
	cmd.Env = []string{
		"PATH=" + path, "TERMINFO=/p/ti", "TERMINFO_DIRS=/p/tid", "TERMCAP=/p/tc", "LOCPATH=/p/loc",
		"GCONV_PATH=/p/gc", "NLSPATH=/p/nls", "BASH_ENV=/p/be", "ENV=/p/env", "CDPATH=/p/cd", "PROMPT_COMMAND=touch /p/pc",
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	return string(out)
}

// The script, run for real against a stub shell, starts bash without readline
// (so ~/.terminfo and ~/.inputrc are never read, M3) and clears every variable
// that names another place to read from. Text assertions alone passed while the
// terminfo attack worked, so this checks the arguments and the environment.
func TestTheBashShellStartsWithoutReadlineAndWithACleanEnvironment(t *testing.T) {
	t.Parallel()
	out := runShellScript(t, "bash", false)
	for _, want := range []string{
		"ARGS=--noprofile --norc --noediting -i",
		"INPUTRC=/dev/null", "HISTFILE=/dev/null", "EDITRC=/dev/null",
		"TERMINFO=UNSET", "TERMINFO_DIRS=UNSET", "TERMCAP=UNSET", "LOCPATH=UNSET", "GCONV_PATH=UNSET",
		"NLSPATH=UNSET", "BASH_ENV=UNSET", "CDPATH=UNSET", "PROMPT_COMMAND=UNSET",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("bash started without %q:\n%s", want, out)
		}
	}
}

// Without bash, sh starts with the same clean environment and no ENV file.
func TestTheShFallbackHasTheSameCleanEnvironment(t *testing.T) {
	t.Parallel()
	out := runShellScript(t, "sh", true)
	for _, want := range []string{
		"ARGS=-i", "ENV=/dev/null", "INPUTRC=/dev/null", "HISTFILE=/dev/null", "EDITRC=/dev/null",
		"TERMINFO=UNSET", "TERMINFO_DIRS=UNSET", "TERMCAP=UNSET", "LOCPATH=UNSET", "GCONV_PATH=UNSET",
		"NLSPATH=UNSET", "BASH_ENV=UNSET", "CDPATH=UNSET", "PROMPT_COMMAND=UNSET",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("sh started without %q:\n%s", want, out)
		}
	}
}
