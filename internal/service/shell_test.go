package service

import (
	"context"
	"encoding/json"
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
			target, err := shellTargetForTest(bg, r.ws, ws.Name)
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
			operation, err := r.svc.HoldEnvironment(bg, ws)
			must(t, err) // failed preparation must release the exclusive marker too
			operation()
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
		_, err := shellTargetForTest(bg, r.ws, ws.Name)
		finished <- err
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("preparation ended before stopping: %v", err)
	case <-t.Context().Done():
		t.Fatal("preparation did not reach stop")
	}
	_, _, runErr := r.ws.StartTask(userContext(), StartRequest{AgentID: agent.ID, Issue: "#7"})
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
	r.ws.cfg.Shell = &ShellConfig{Bin: "/tools/profiles/p/bin", Dir: "/home/workharbor", Env: []string{"HOME=/home/workharbor", "CLAUDE_CONFIG_DIR=/home/workharbor/.claude"}}
	return r
}

func TestTheShellTargetIsTheEnvironmentAndTheVariablesOfARun(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	w, _ := r.create("docs-ws")
	got, err := shellTargetForTest(bg, r.ws, "docs-ws")
	if err != nil {
		t.Fatal(err)
	}
	if got.EnvID != string(w.EnvID) || got.Dir != "/home/workharbor" || got.Runtime != r.rt.Adapter.Name() {
		t.Errorf("target = %+v", got)
	}
	for _, want := range []string{"HOME=/home/workharbor", "CLAUDE_CONFIG_DIR=/home/workharbor/.claude"} {
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
	_, run, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = shellTargetForTest(bg, r.ws, "docs-ws")
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
	_, err := shellTargetForTest(bg, r.ws, ws.Name)
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
	if _, err := shellTargetForTest(bg, r.ws, "docs-ws"); !errors.As(err, &ce) {
		t.Errorf("without a shell configuration: %v, want a conflict", err)
	}
	r.ws.cfg.Shell = &ShellConfig{Bin: "/tools/bin", Dir: "/home/workharbor"}
	var nf *domain.NotFoundError
	if _, err := shellTargetForTest(bg, r.ws, "nope"); !errors.As(err, &nf) {
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
	path = hostile + ":.:/home/workharbor/bin::" + path
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
	for _, bin := range []string{"", "bin", "/home/workharbor/bin", "/tools/../home/workharbor", "/tools/bin:", "/tools/bin::/bin", "/tools/bin\n"} {
		r.ws.cfg.Shell.Bin = bin
		target, err := shellTargetForTest(bg, r.ws, "unused")
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

// Preparation-only tests release as soon as the target is obtained.
func shellTargetForTest(ctx context.Context, w *Workspaces, workspace string) (ShellTarget, error) {
	s, err := w.OpenShell(ctx, workspace, "api")
	if err != nil {
		return ShellTarget{}, err
	}
	s.Release()
	return s.Target, nil
}

func TestSignInShellHoldsOffRunAndRebuildUntilChildExits(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, agent := r.create("docs-ws")
	shell, err := r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	defer shell.Release()
	// A fake child keeps the API holder alive until its exit. The fake runtime
	// beneath StartTask must see the environment held throughout that lifetime.
	childExit, holderReleased := make(chan struct{}), make(chan struct{})
	go func() { <-childExit; shell.Release(); close(holderReleased) }()
	_, _, err = r.ws.StartTask(userContext(), StartRequest{AgentID: agent.ID, Issue: "#7"})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || conflict.Rule != domain.RuleEnvBusy {
		t.Fatalf("run beside shell: %v", err)
	}
	_, err = r.ws.Rebuild(bg, ws.Name, "api")
	if !errors.As(err, &conflict) {
		t.Fatalf("rebuild beside shell: %v", err)
	}
	close(childExit)
	<-holderReleased
	_, _, err = r.ws.StartTask(userContext(), StartRequest{AgentID: agent.ID, Issue: "#7"})
	must(t, err)
}

func TestSignInShellOpeningRecordsMetadataOnly(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	shell, err := r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	defer shell.Release()
	events, err := r.store.EventsSince(bg, domain.SupervisorStream, 0, 100)
	must(t, err)
	var found []domain.Event
	for _, e := range events {
		if e.Kind == domain.EventSignInShell {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("audit events: %v", found)
	}
	e := found[0]
	var payload map[string]string
	must(t, json.Unmarshal(e.Payload, &payload))
	if e.Tier != domain.TierAudit || e.At.IsZero() || len(payload) != 3 || payload["workspace"] != ws.Name || payload["env_id"] != string(ws.EnvID) || payload["actor"] != "api" {
		t.Fatalf("metadata audit: %+v %v", e, payload)
	}
}

func TestSupervisorShutdownEndsSignInShellHold(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	shell, err := r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	defer shell.Release()
	r.svc.Shutdown()
	select {
	case <-shell.Done:
	default:
		t.Fatal("shutdown left shell hold live")
	}
}

func TestASecondSignInShellDoesNotRestartTheFirst(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	rt := &shellRestartRuntime{Adapter: r.rt.Adapter}
	r.svc.rt = rt
	first, err := r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	defer first.Release()
	before := len(rt.calls)
	second, err := r.ws.OpenShell(bg, ws.Name, "api")
	if err == nil {
		second.Release()
		t.Fatal("second shell was admitted")
	}
	if len(rt.calls) != before {
		t.Fatalf("second shell restarted first: %v", rt.calls)
	}
}

func TestSignInShellRejectsLaterOperationHold(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	shell, err := r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	defer shell.Release()
	release, err := r.svc.HoldEnvironment(bg, ws)
	if release != nil {
		defer release()
	}
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || conflict.Rule != domain.RuleEnvBusy {
		t.Fatalf("operation admitted beside sign-in shell: %v", err)
	}
}

func TestSignInShellExcludesOperationHoldsDuringRestart(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	entered, proceed := make(chan struct{}), make(chan struct{})
	rt := &shellRestartRuntime{Adapter: r.rt.Adapter, gate: func() { close(entered); <-proceed }}
	r.svc.rt = rt
	done := make(chan error, 1)
	go func() {
		shell, err := r.ws.OpenShell(bg, ws.Name, "api")
		if err == nil {
			shell.Release()
		}
		done <- err
	}()
	<-entered
	release, err := r.svc.HoldEnvironment(bg, ws)
	if release != nil {
		release()
	}
	close(proceed)
	must(t, <-done)
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) || conflict.Rule != domain.RuleEnvBusy {
		t.Fatalf("operation admitted during shell preparation: %v", err)
	}
}

func TestOperationHoldPreventsSignInShellAndStillNests(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	first, err := r.svc.HoldEnvironment(bg, ws)
	must(t, err)
	defer first()
	nested, err := r.svc.HoldEnvironment(bg, ws)
	must(t, err)
	defer nested()
	shell, err := r.ws.OpenShell(bg, ws.Name, "api")
	if err == nil {
		shell.Release()
		t.Fatal("shell admitted beside nested operation holds")
	}
	nested()
	first()
	shell, err = r.ws.OpenShell(bg, ws.Name, "api")
	must(t, err)
	shell.Release()
	// A released exclusive hold leaves neither its marker nor a leaked count.
	first, err = r.svc.HoldEnvironment(bg, ws)
	must(t, err)
	first()
}

func TestExclusiveAndOperationHoldsCannotBothWinConcurrentAdmission(t *testing.T) {
	t.Parallel()
	r := shellRig(t)
	ws, _ := r.create("docs-ws")
	type result struct {
		release func()
		err     error
	}
	for attempt := 0; attempt < 30; attempt++ {
		start := make(chan struct{})
		outcomes := make(chan result, 2)
		for _, exclusive := range []bool{false, true} {
			go func() {
				<-start
				release, err := r.svc.holdEnvironment(bg, ws, exclusive)
				outcomes <- result{release, err}
			}()
		}
		close(start)
		first, second := <-outcomes, <-outcomes
		successes := 0
		for _, outcome := range []result{first, second} {
			if outcome.err == nil {
				successes++
				outcome.release()
			} else {
				var conflict *domain.ConflictError
				if !errors.As(outcome.err, &conflict) || conflict.Rule != domain.RuleEnvBusy {
					t.Fatalf("unexpected admission error: %v", outcome.err)
				}
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent shell/operation admitted %d holders", successes)
		}
		must(t, r.svc.checkNotHeld(ws.EnvID))
	}
	// Losing admissions and every exclusive release must also release their lease.
	marked, leased := r.svc.markRebuilding(ws.ID)
	if !marked || leased {
		t.Fatalf("hold admission leaked a lease: marked=%v leased=%v", marked, leased)
	}
	r.svc.unmarkRebuilding(ws.ID)
}
