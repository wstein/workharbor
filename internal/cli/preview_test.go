package cli

import (
	"strings"
	"testing"
)

func TestPreviewOpenPrintsTheLinkAndTheHumanTextGoesToStderr(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/tasks/t1/previews", 201, ok(`{"preview":{"id":"pv-1","task":"t1","port":3000,"listen_port":9401,"expires_at":"2026-10-02T12:00:00Z"},"url":"https://whr.example.test:9401/?whr_preview=g"}`))
	code, out, errOut := s.runCLI("", "preview", "open", "t1", "3000")
	if code != 0 || out != "https://whr.example.test:9401/?whr_preview=g\n" || !strings.Contains(errOut, "works once") || strings.Contains(errOut, "whr_preview") {
		t.Fatalf("exit %d, stdout %q, stderr %q: the link is data and goes to stdout only", code, out, errOut)
	}
	if body := s.requests("POST /v1/tasks/t1/previews")[0].body; !strings.Contains(body, `"port":3000`) {
		t.Errorf("body %q", body)
	}
	if code, _, errOut := s.runCLI("", "preview", "open", "t1", "web"); code == 0 || !strings.Contains(errOut, "not a port") {
		t.Errorf("a bad port: %d %q", code, errOut)
	}
}

func TestPreviewLsLinkAndClose(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/previews", 200, ok(`[{"id":"pv-1","task":"t1","port":3000,"listen_port":9401,"expires_at":"2026-10-02T12:00:00Z"}]`))
	s.reply("POST /v1/previews/pv-1/link", 200, ok(`{"url":"https://whr.example.test:9401/?whr_preview=h"}`))
	s.reply("DELETE /v1/previews/pv-1", 200, ok(`{}`))
	if code, out, _ := s.runCLI("", "preview", "ls"); code != 0 || !strings.Contains(out, "pv-1") || !strings.Contains(out, "9401") {
		t.Errorf("ls: %d %q", code, out)
	}
	if code, out, _ := s.runCLI("", "preview", "link", "pv-1"); code != 0 || out != "https://whr.example.test:9401/?whr_preview=h\n" {
		t.Errorf("link: %d %q", code, out)
	}
	code, out, _ := s.runCLI("", "preview", "close", "pv-1")
	if code != 0 || out != "pv-1\n" || s.requests("DELETE /v1/previews/pv-1")[0].header.Get("Idempotency-Key") == "" {
		t.Errorf("close: %d %q", code, out)
	}
}
