package claude

import (
	"context"
	"errors"
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
