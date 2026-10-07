package cli

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

func TestPauseAndResumePostToTheTask(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(taskList))
	s.reply("POST /v1/tasks/t-aaa111/pause", 200, ok(`{}`))
	s.reply("POST /v1/tasks/t-aaa111/resume", 200, ok(`{"run_id":"r1"}`))
	for _, verb := range []string{"pause", "resume"} {
		code, out, errOut := s.runCLI("", verb, "t-aaa111")
		if code != 0 || out != "t-aaa111\n" || errOut != "" {
			t.Errorf("%s: %d %q %q", verb, code, out, errOut)
		}
		if got := s.requests("POST /v1/tasks/t-aaa111/" + verb); len(got) != 1 || got[0].header.Get("Idempotency-Key") == "" {
			t.Errorf("%s: requests %+v", verb, got)
		}
	}
	s.reply("POST /v1/tasks/t-aaa111/pause", 409, fail("conflict", exitcode.Conflict, "task has no run to pause"))
	if code, _, errOut := s.runCLI("", "pause", "t-aaa111"); code != exitcode.Conflict || !strings.Contains(errOut, "no run to pause") {
		t.Errorf("a refused pause: %d %q", code, errOut)
	}
}

func TestPurgeSaysWhatGoesAndAsks(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(taskList))
	s.reply("GET /v1/tasks/t-aaa111/transcript", 200, ok(`{"events":12,"bytes":3400}`))
	s.reply("POST /v1/tasks/t-aaa111/purge", 200, ok(`{"events":12,"bytes":3400,"digest":"abc"}`))

	code, _, errOut := s.runCLI("no\n", "purge", "t-aaa111")
	flat := strings.Join(strings.Fields(errOut), " ")
	if code != exitcode.Usage || !strings.Contains(flat, "12 transcript event(s), 3400 byte(s)") || !strings.Contains(flat, "audit entries, usage and Decisions stay") {
		t.Fatalf("declined: %d %q", code, errOut)
	}
	if n := len(s.requests("POST /v1/tasks/t-aaa111/purge")); n != 0 {
		t.Fatalf("a declined purge sent %d request(s)", n)
	}
	code, out, errOut := s.runCLI("purge\n", "purge", "t-aaa111")
	if code != 0 || out != "t-aaa111\n" || !strings.Contains(errOut, "deleted 12 event(s)") {
		t.Fatalf("confirmed: %d %q %q", code, out, errOut)
	}
	if body := s.requests("POST /v1/tasks/t-aaa111/purge")[0].body; !strings.Contains(body, `"confirm":true`) {
		t.Errorf("body %q", body)
	}
	if code, _, _ := s.runCLI("", "purge", "--yes", "t-aaa111"); code != 0 {
		t.Errorf("--yes: %d", code)
	}
}
