package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
)

func harness(t *testing.T) (agenttest.Harness, *stub) {
	t.Helper()
	st := newStub()
	ad := New(st, Config{Bin: "claude", ConfigDir: "/auth/env-1", ResumeProbe: 2 * time.Second})
	return agenttest.Harness{
		Adapter:   ad,
		Scenarios: st,
		Spec: func() agent.StartSpec {
			return agent.StartSpec{EnvID: "env-1", Workdir: "/work", Prompt: "implement the issue", Auth: agent.AuthSubscription}
		},
	}, st
}

// The adapter meets the contract, in the degraded mode: it reports mid-run
// injection and no host approvals until spike #7.
func TestPassesTheSuite(t *testing.T) {
	agenttest.Run(t, func(t *testing.T) agenttest.Harness {
		h, _ := harness(t)
		return h
	})
}

func TestReportsTheDegradedMode(t *testing.T) {
	h, _ := harness(t)
	c := h.Adapter.Capabilities()
	if c.Mode() != agent.ModeDegraded || c.HostApprovals || !c.MidRunInstruction {
		t.Errorf("capabilities %+v give mode %s, want degraded with injection and no host approvals", c, c.Mode())
	}
}

func TestCommandLine(t *testing.T) {
	h, st := harness(t)
	spec := h.Spec()
	spec.PermissionMode, spec.AllowedTools = agent.PermissionDontAsk, []string{"Read", "Bash(ls:*)"}
	s, err := h.Adapter.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	for range s.Events() {
	}
	cmd := st.lastCall()
	for _, want := range []string{"-p", "--input-format", "stream-json", "--output-format", "--verbose"} {
		if !slices.Contains(cmd, want) {
			t.Errorf("command %v lacks %s", cmd, want)
		}
	}
	if argAfter(cmd, "--permission-mode") != "dontAsk" || argAfter(cmd, "--allowedTools") != "Read,Bash(ls:*)" {
		t.Errorf("command %v does not carry the mode and the allowlist", cmd)
	}
	for _, bad := range []string{"bypassPermissions", "--dangerously-skip-permissions", "--bare"} {
		if strings.Contains(strings.Join(cmd, " "), bad) {
			t.Errorf("command %v contains %s", cmd, bad)
		}
	}
	if st.dirs[0] != "/work" || !slices.Contains(st.envs[0], "CLAUDE_CONFIG_DIR=/auth/env-1") {
		t.Errorf("dir %q env %v, want the workdir and the per-environment auth directory", st.dirs[0], st.envs[0])
	}
}

func TestResumePassesTheSessionID(t *testing.T) {
	h, st := harness(t)
	s, err := h.Adapter.Start(context.Background(), dontAsk(h))
	if err != nil {
		t.Fatal(err)
	}
	for range s.Events() {
	}
	res, _ := s.Wait()
	r, err := h.Adapter.Resume(context.Background(), dontAsk(h), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for range r.Events() {
	}
	if argAfter(st.lastCall(), "--resume") != res.SessionID {
		t.Errorf("command %v does not resume %q", st.lastCall(), res.SessionID)
	}
}

func dontAsk(h agenttest.Harness) agent.StartSpec {
	s := h.Spec()
	s.PermissionMode, s.AllowedTools = agent.PermissionDontAsk, []string{"Read"}
	return s
}

func TestManualModeAndMissingPrompt(t *testing.T) {
	h, _ := harness(t)
	manual := h.Spec()
	manual.Approver = agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
		return agent.Approval{Allow: true}, nil
	})
	if _, err := h.Adapter.Start(context.Background(), manual); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("manual mode = %v, want ErrUnsupported until spike #7", err)
	}
	empty := dontAsk(h)
	empty.Prompt = " "
	if _, err := h.Adapter.Start(context.Background(), empty); !errors.Is(err, agent.ErrBadSpec) {
		t.Errorf("an empty prompt = %v, want ErrBadSpec", err)
	}
}

// A process that dies without a result is a failed run with the reason, and
// stderr lines are events.
func TestExitWithoutAResult(t *testing.T) {
	h, _ := harness(t)
	ad := New(deadRunner{}, Config{})
	s, err := ad.Start(context.Background(), dontAsk(h))
	if err != nil {
		t.Fatal(err)
	}
	var sawStderr bool
	for e := range s.Events() {
		sawStderr = sawStderr || (e.Kind == agent.EventError && strings.Contains(e.Text, "boom"))
	}
	res, _ := s.Wait()
	if res.Status != agent.ResultFailed || !strings.Contains(res.Text, "code 2") || !sawStderr {
		t.Errorf("result %+v, stderr seen %v", res, sawStderr)
	}
}

// The CLI reads only what the supervisor passes, on every start and every
// resume (design §5.2, spike/claude-config).
func TestTheCLIReadsOnlyWhatTheSupervisorPasses(t *testing.T) {
	h, st := harness(t)
	start, err := h.Adapter.Start(context.Background(), dontAsk(h))
	if err != nil {
		t.Fatal(err)
	}
	for range start.Events() {
	}
	startCmd := st.lastCall()
	res, _ := start.Wait()
	resume, err := h.Adapter.Resume(context.Background(), dontAsk(h), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for range resume.Events() {
	}
	for name, cmd := range map[string][]string{"start": startCmd, "resume": st.lastCall()} {
		i := slices.Index(cmd, "--setting-sources")
		if i < 0 || i+1 >= len(cmd) || cmd[i+1] != "" {
			t.Errorf("%s: --setting-sources must be present with no source: %v", name, cmd)
		}
		if got := instructionFiles(t, argAfter(cmd, "--settings")); got != "claude-md-or-agents-md" {
			t.Errorf("%s: instructionFiles = %q, want it set explicitly (D38)", name, got)
		}
		for _, flag := range []string{"--strict-mcp-config", "--disable-slash-commands"} {
			if !slices.Contains(cmd, flag) {
				t.Errorf("%s: %s is missing: %v", name, flag, cmd)
			}
		}
		for _, bad := range []string{"--mcp-config", "--plugin-dir", "--plugin-url", "--add-dir", "--agents"} {
			if slices.Contains(cmd, bad) {
				t.Errorf("%s: %s lets something else configure the CLI: %v", name, bad, cmd)
			}
		}
	}

	// The supervisor's own settings replace the default, and are what is passed.
	ad := New(newStub(), Config{Settings: `{"permissions":{"deny":["WebFetch"]}}`})
	got := argAfter(ad.args(dontAsk(h), ""), "--settings")
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil || m["permissions"] == nil {
		t.Errorf("configured settings = %q, want the supervisor's permissions kept", got)
	}
	if instructionFiles(t, got) != "claude-md-or-agents-md" {
		t.Errorf("configured settings = %q, want the instruction-file setting added", got)
	}
	// A value the supervisor sets itself is kept.
	own := New(newStub(), Config{Settings: `{"pluginConfigs":{"agents-md@builtin":{"options":{"instructionFiles":"claude-md-and-agents-md"}}}}`})
	if v := instructionFiles(t, argAfter(own.args(dontAsk(h), ""), "--settings")); v != "claude-md-and-agents-md" {
		t.Errorf("the supervisor's own instructionFiles was replaced by %q", v)
	}
}

// instructionFiles reads the agents-md plugin's instructionFiles option from
// a settings JSON.
func instructionFiles(t *testing.T, settings string) string {
	t.Helper()
	var m struct {
		PluginConfigs map[string]struct {
			Options map[string]string `json:"options"`
		} `json:"pluginConfigs"`
	}
	if err := json.Unmarshal([]byte(settings), &m); err != nil {
		t.Fatalf("settings %q: %v", settings, err)
	}
	return m.PluginConfigs["agents-md@builtin"].Options["instructionFiles"]
}

// D38: a CLAUDE.md-family file above the workspace, or a managed one, would
// replace or add to AGENTS.md, so the adapter refuses to start.
func TestInstructionFilesAboveTheWorkspaceRefuse(t *testing.T) {
	h, _ := harness(t)
	ad := New(foundRunner{found: "/work/CLAUDE.md"}, Config{})
	_, err := ad.Start(context.Background(), dontAsk(h))
	if !errors.Is(err, ErrInstructionFiles) || !strings.Contains(err.Error(), "/work/CLAUDE.md") {
		t.Fatalf("Start with a CLAUDE.md above the workspace = %v, want ErrInstructionFiles naming it", err)
	}
	// Invalid supervisor settings refuse too, instead of passing something the CLI rejects.
	bad := New(newStub(), Config{Settings: "not json"})
	if _, err := bad.Start(context.Background(), dontAsk(h)); err == nil {
		t.Fatal("invalid settings were accepted")
	}
}

func TestStderrIsCapped(t *testing.T) {
	h, _ := harness(t)
	var lines []string
	for i := range 1000 {
		lines = append(lines, fmt.Sprintf("noise %d\n", i))
	}
	s, err := New(scripted{err: lines}, Config{}).Start(context.Background(), dontAsk(h))
	if err != nil {
		t.Fatal(err)
	}
	errors := 0
	for e := range s.Events() {
		if e.Kind == agent.EventError {
			errors++
		}
	}
	if errors > maxStderrLines+2 {
		t.Errorf("%d error events from 1000 stderr lines, want at most %d", errors, maxStderrLines+2)
	}

	// An unfinished line does not grow without bound either.
	sess := &session{events: make(chan agent.Event, 8), p: newParser(clock, true, nil)}
	var rest []byte
	n := 0
	for range 200 {
		rest = sess.flushStderr(append(rest, make([]byte, 64<<10)...), &n)
	}
	if len(rest) > maxStderrLine {
		t.Errorf("an unfinished line kept %d bytes, want at most %d", len(rest), maxStderrLine)
	}
}

// A late Stop must not overwrite a result that has arrived.
func TestALateStopKeepsTheResult(t *testing.T) {
	h, _ := harness(t)
	finish := `{"type":"system","subtype":"init","session_id":"s1","model":"m"}` + "\n" +
		`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s1"}` + "\n"
	s, err := New(scripted{out: []string{finish}, linger: true}, Config{}).Start(context.Background(), dontAsk(h))
	if err != nil {
		t.Fatal(err)
	}
	for e := range s.Events() { // the usage event comes with the result
		if e.Kind == agent.EventUsage {
			break
		}
	}
	_ = s.Stop(context.Background())
	go func() {
		for range s.Events() {
		}
	}()
	res, _ := s.Wait()
	if res.Status != agent.ResultCompleted || res.Text != "done" {
		t.Errorf("result after a late Stop = %+v, want the completed result", res)
	}
}

func TestResumeFailureIsNoSessionOnlyWhenTheSessionIsMissing(t *testing.T) {
	h, _ := harness(t)
	resume := func(text string) error {
		line := fmt.Sprintf(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":%q}`+"\n", text)
		_, err := New(scripted{out: []string{line}}, Config{ResumeProbe: 2 * time.Second}).Resume(context.Background(), dontAsk(h), "s-1")
		return err
	}
	for _, text := range []string{"No conversation found with session ID s-1", "session s-1 does not exist"} {
		if err := resume(text); !errors.Is(err, agent.ErrNoSession) {
			t.Errorf("%q = %v, want ErrNoSession", text, err)
		}
	}
	for _, text := range []string{"Rate limited, try again later", "internal error"} {
		err := resume(text)
		if err == nil || errors.Is(err, agent.ErrNoSession) {
			t.Errorf("%q = %v, want an ordinary error that is not ErrNoSession", text, err)
		}
	}
}

// The check script itself, run by /bin/sh on a real tree: a CLAUDE.md above
// the workspace is found, the workspace's own is the repository's and is not.
func TestInstructionCheckScript(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "a", "b", "ws")
	if err := os.MkdirAll(filepath.Join(root, "a", ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(root, "a", ".claude", "CLAUDE.md"), filepath.Join(ws, "CLAUDE.md")} {
		if err := os.WriteFile(f, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.CommandContext(context.Background(), "/bin/sh", "-c", instructionCheck, "whr-instruction-check", ws).Output() //nolint:gosec // the adapter's own fixed script
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, filepath.Join(root, "a", ".claude", "CLAUDE.md")) {
		t.Errorf("found %q, want the .claude/CLAUDE.md above the workspace", got)
	}
	if strings.Contains(got, filepath.Join(ws, "CLAUDE.md")) {
		t.Errorf("found %q, but the workspace's own CLAUDE.md is the repository's", got)
	}
}
