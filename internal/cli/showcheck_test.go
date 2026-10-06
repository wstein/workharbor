package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestShowPrintsTheCheckReceiptAndTheFailedPublishSteps(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	body := `{"id":"t-aaa111","repo":"wstein/workharbor","issue":"#7","state":"ready_for_review","runs":[],"open_decisions":[],` +
		`"candidate":{"branch":"agent/docs","sha":"abc123","ci":"pending","pushed":false,"files":1,"added":2,"removed":0},` +
		`"check":{"sha":"abc123","command":"make check","source":"devcontainer","exit_status":2,"duration_ms":4200,"output":"ok 1\nFAIL \u001b[31mx\u001b[0m\n"},` +
		`"publish_attempts":[{"sha":"abc123","attempt":1,"transient":true,"error":"push: Could not resolve host","retry_at":"2026-10-01T09:01:00Z"},` +
		`{"sha":"abc123","attempt":2,"transient":false,"error":"the branch is not a fast-forward"}]}`
	s.reply("GET /v1/tasks/t-aaa111", 200, ok(body))
	code, out, errOut := s.runCLI("", "show", "t-aaa111")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"Check:    make check (from devcontainer) exit status 2 in 4.2s, on abc123",
		"  | FAIL ?[31mx?[0m",
		"Publish:  attempt 1 failed (will retry after 2026-10-01T09:01:00Z): push: Could not resolve host",
		"Publish:  attempt 2 failed (refused): the branch is not a fast-forward",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("a control character reached the terminal: %q", out)
	}
}

func TestShowPrintsAgentMayRunWithRunAndEscapesErrors(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("GET /v1/tasks/t-aaa111", 200, ok(`{"id":"t-aaa111","state":"cancelled","agent_may_run":[{"run_id":"r1","env_id":"e1","path":"kill-all","error":"stop \u001b refused"}]}`))
	code, out, errOut := s.runCLI("", "show", "t-aaa111")
	if code != 0 || errOut != "" || !strings.Contains(out, "agent may still run (run r1, environment e1, path kill-all): stop ? refused") || strings.ContainsRune(out, 0x1b) {
		t.Fatalf("show: %d %q %q", code, out, errOut)
	}
}

func TestKillAllPrintsStopWarningsForEachTask(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/kill-all", 200, ok(`{"cancelled":["t1","t2"],"agent_may_run":["t1"],"agent_stop_pending":["t2"],"tokens_revoked":0,"problems":[]}`))
	code, out, errOut := s.runCLI("", "kill-all", "--yes")
	if code != 0 || out != "t1\nt2\n" || !strings.Contains(errOut, "agent may still run: task t1") || !strings.Contains(errOut, "agent stop pending: task t2") {
		t.Fatalf("kill-all: %d %q %q", code, out, errOut)
	}
}

func TestShowIncludesInitiatingTerminalReason(t *testing.T) {
	var c taskCard
	if err := json.Unmarshal([]byte(`{"id":"t1","state":"failed","runs":[{"id":"r1","state":"stopped","terminal_reason":"budget_breach"}]}`), &c); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := printCard(&b, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "reason budget_breach") {
		t.Fatalf("terminal reason omitted: %s", b.String())
	}
}
