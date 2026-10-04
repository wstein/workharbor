package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

type shellRestartRuntime struct {
	runtime.Adapter
	calls []string
	fail  string
	gate  func()
}

func (r *shellRestartRuntime) Stop(ctx context.Context, id string) error {
	r.calls = append(r.calls, "stop")
	if r.gate != nil {
		r.gate()
	}
	if r.fail == "stop" {
		return errors.New("stop refused")
	}
	return r.Adapter.Stop(ctx, id)
}

func (r *shellRestartRuntime) Start(ctx context.Context, id string) error {
	r.calls = append(r.calls, "start")
	if r.fail == "start" {
		return errors.New("start refused")
	}
	return r.Adapter.Start(ctx, id)
}

func (r *shellRestartRuntime) Exec(ctx context.Context, id string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	r.calls = append(r.calls, "ready")
	if r.fail == "ready" {
		return nil, errors.New("exec refused")
	}
	return r.Adapter.Exec(ctx, id, req)
}

func TestShellRestartsTheWarmEnvironmentBeforeReturningATarget(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"success", "stop", "start", "ready"} {
		t.Run(failure, func(t *testing.T) {
			r := shellRig(t)
			r.home = true
			ws, _ := r.create("docs-ws")
			before, err := r.rt.Adapter.Inspect(bg, string(ws.EnvID))
			must(t, err)
			if !r.svc.envStarted(ws.EnvID) {
				t.Fatal("environment is not warm")
			}
			rt := &shellRestartRuntime{Adapter: r.rt.Adapter, fail: failure}
			r.svc.rt = rt
			r.svc.cfg.ReadyTimeout = 0
			target, err := r.ws.ShellTarget(bg, ws.Name)
			want := []string{"stop", "start", "ready"}
			switch failure {
			case "stop":
				want = want[:1]
			case "start":
				want = want[:2]
			}
			if !slices.Equal(rt.calls, want) {
				t.Fatalf("calls = %v, want %v", rt.calls, want)
			}
			if failure != "success" {
				if err == nil || target.EnvID != "" {
					t.Fatalf("failure returned target %+v, err %v", target, err)
				}
			} else {
				must(t, err)
				after, err := r.rt.Adapter.Inspect(bg, target.EnvID)
				must(t, err)
				if !slices.Equal(before.Mounts, after.Mounts) || after.State != domain.EnvRunning {
					t.Fatalf("restart changed mounts or did not start: %+v", after)
				}
				if len(r.svc.agentEnv(bg, ws.EnvID)) != 3 {
					t.Fatal("restart lost the proxy")
				}
			}
			must(t, r.svc.checkNotHeld(ws.EnvID))
		})
	}
}

func TestShellPreparationExcludesRunAndRebuild(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, agent := r.create("docs-ws")
	rt := &shellRestartRuntime{Adapter: r.rt.Adapter}
	r.svc.rt = rt
	entered, proceed := make(chan struct{}), make(chan struct{})
	rt.gate = func() {
		close(entered)
		<-proceed
	}
	finished := make(chan error, 1)
	go func() {
		_, err := r.ws.ShellTarget(bg, ws.Name)
		finished <- err
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("preparation ended before stopping: %v", err)
	case <-t.Context().Done():
		t.Fatal("preparation did not reach stop")
	}
	_, _, runErr := r.ws.StartTask(bg, StartRequest{AgentID: agent.ID, Issue: "#7"})
	_, rebuildErr := r.ws.Rebuild(bg, ws.Name, "werner")
	close(proceed)
	must(t, <-finished)
	for _, err := range []error{runErr, rebuildErr} {
		var conflict *domain.ConflictError
		if !errors.As(err, &conflict) {
			t.Errorf("operation during preparation: %v", err)
		}
	}
	must(t, r.svc.checkNotHeld(ws.EnvID))
}

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

func TestShellRefusesAnInterruptedRunBeforeStoppingTheEnvironment(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, agent := r.create("docs-ws")
	interruptedRun(t, r, ws, agent, "r-int")
	rt := &shellRestartRuntime{Adapter: r.rt.Adapter}
	r.svc.rt = rt
	_, err := r.ws.ShellTarget(bg, ws.Name)
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "r-int") || len(rt.calls) != 0 {
		t.Fatalf("interrupted run: err %v, calls %v", err, rt.calls)
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
	r.ws.cfg.Shell = &ShellConfig{Bin: "/tools/bin", Dir: "/home/agent"}
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
		"exec /bin/bash --noprofile --norc --noediting -i",
		"INPUTRC=/dev/null", "HISTFILE=/dev/null", "EDITRC=/dev/null", "export INPUTRC HISTFILE EDITRC",
		"ENV=/dev/null; export ENV; exec /bin/sh -i",
	} {
		if !strings.Contains(shellScript, want) {
			t.Errorf("shell script lacks %q: %s", want, shellScript)
		}
	}
	// The variables are set before either shell starts, so the sh fallback has them too.
	if i := strings.Index(shellScript, "exec /bin/bash"); strings.Index(shellScript, "export INPUTRC HISTFILE EDITRC") > i {
		t.Errorf("INPUTRC and HISTFILE are exported after bash starts: %s", shellScript)
	}
}

// shellStub is a shell that reports how the script started it.
const shellStub = `#!/bin/sh
echo "ARGS=$*"
echo "PATH=$PATH"
for v in INPUTRC HISTFILE EDITRC ENV TERMINFO TERMINFO_DIRS TERMCAP LOCPATH GCONV_PATH NLSPATH BASH_ENV CDPATH PROMPT_COMMAND; do
	eval "echo $v=\${$v-UNSET}"
done
`

// runShellScript maps the absolute image shell paths to fixture files, leaving
// shell selection, flags and sanitisation intact. Hostile PATH entries contain
// competing shells; onlyStub omits the bash fixture to exercise the fallback.
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
	hostile := t.TempDir()
	for _, binary := range []string{"bash", "sh"} {
		if err := os.WriteFile(filepath.Join(hostile, binary), []byte("#!/bin/sh\necho HOSTILE\n"), 0o755); err != nil { //nolint:gosec // executable test stub
			t.Fatal(err)
		}
	}
	path = hostile + ":.:/home/agent/bin::" + path
	script := strings.ReplaceAll(shellScript, "/bin/bash", filepath.Join(dir, "bash"))
	script = strings.ReplaceAll(script, "exec /bin/sh -i", "exec "+filepath.Join(dir, "sh")+" -i")
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", script, "whr-shell", "/tools/profiles/p/bin") //nolint:gosec // absolute image paths mapped to fixture paths
	cmd.Env = []string{
		"PATH=" + path, "TERMINFO=/p/ti", "TERMINFO_DIRS=/p/tid", "TERMCAP=/p/tc", "LOCPATH=/p/loc",
		"GCONV_PATH=/p/gc", "NLSPATH=/p/nls", "BASH_ENV=/p/be", "ENV=/p/env", "CDPATH=/p/cd", "PROMPT_COMMAND=touch /p/pc",
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	if strings.Contains(string(out), "HOSTILE") || !strings.Contains(string(out), "PATH=/tools/profiles/p/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n") {
		t.Fatalf("hostile PATH or shell selected: %s", out)
	}
	return string(out)
}

func TestShellRefusesUnsafeToolDirectories(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	for _, bin := range []string{"", "bin", "/home/agent/bin", "/tools/../home/agent", "/tools/bin:", "/tools/bin::/bin", "/tools/bin\n"} {
		r.ws.cfg.Shell.Bin = bin
		target, err := r.ws.ShellTarget(bg, "unused")
		var conflict *domain.ConflictError
		if !errors.As(err, &conflict) || target.EnvID != "" {
			t.Errorf("accepted %q: %+v, %v", bin, target, err)
		}
	}
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
