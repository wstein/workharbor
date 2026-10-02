package cli

import (
	"strings"
	"testing"
)

func TestPasskeyAddPrintsTheLinkAndTheHumanTextGoesToStderr(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/passkeys/enrolments", 201, ok(`{"url":"https://whr.example.test/enrol?token=abc","expires_at":"2026-10-02T12:00:00Z"}`))
	code, out, errOut := s.runCLI("", "passkey", "add", "phone")
	if code != 0 || out != "https://whr.example.test/enrol?token=abc\n" || !strings.Contains(errOut, "works once") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if body := s.requests("POST /v1/passkeys/enrolments")[0].body; !strings.Contains(body, `"name":"phone"`) {
		t.Errorf("body %q", body)
	}
	if code, out, _ := s.runCLI("", "passkey", "add", "--json"); code != 0 || !strings.Contains(out, `"schema_version":1`) {
		t.Errorf("--json: %d %q", code, out)
	}
}

func TestPasskeyLsAndRm(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/passkeys", 200, ok(`[{"id":"AbCdEfGhIjKlMnOp","name":"phone","created_at":"2026-10-01T10:00:00Z","backed_up":true,"backup_eligible":true}]`))
	s.reply("DELETE /v1/passkeys/AbCdEf", 200, ok(`{}`))
	code, out, _ := s.runCLI("", "passkey", "ls")
	if code != 0 || !strings.Contains(out, "AbCdEfGhIjKl ") || !strings.Contains(out, "phone") || !strings.Contains(out, "never") || !strings.Contains(out, "synced") {
		t.Errorf("ls: %d %q", code, out)
	}
	code, out, _ = s.runCLI("", "passkey", "rm", "AbCdEf")
	if code != 0 || out != "AbCdEf\n" || s.requests("DELETE /v1/passkeys/AbCdEf")[0].header.Get("Idempotency-Key") == "" {
		t.Errorf("rm: %d %q", code, out)
	}
}
