package cli

import (
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
