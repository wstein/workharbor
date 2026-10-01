package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

const wsList = `[{"id":"w1","name":"docs-ws","repo":"wstein/workharbor","integration":"main","path":"/Users/h/ws/docs","agents":[{"id":"a1","role":"docs","branch":"agent/docs"},{"id":"a2","role":"runtime","branch":"agent/runtime"}]}]`

func TestWsAddSendsTheRequestAndExplainsTheWait(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/workspaces", 201, ok(`{"id":"w9","name":"docs-ws","repo":"a/b","integration":"main","path":"/p","agents":[{"id":"a9","role":"docs","branch":"agent/docs"}]}`))
	code, out, errOut := s.runCLI("", "ws", "add", "docs-ws", "--path", "/Users/h/ws/docs", "--repo", "wstein/workharbor", "--role", "docs", "--from", "/Users/h/src/repo", "--instructions", "docs only")
	if code != 0 || out != "docs-ws\tdocs-ws/docs\n" || !strings.Contains(errOut, "creating workspace docs-ws") || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	req := s.requests("POST /v1/workspaces")[0]
	var b map[string]string
	_ = json.Unmarshal([]byte(req.body), &b)
	if b["name"] != "docs-ws" || b["path"] != "/Users/h/ws/docs" || b["repo"] != "wstein/workharbor" || b["role"] != "docs" || b["source"] != "/Users/h/src/repo" || b["integration"] != "main" || b["instructions"] != "docs only" {
		t.Errorf("body = %v", b)
	}
	if req.header.Get("Idempotency-Key") == "" {
		t.Error("no idempotency key")
	}
	for name, args := range map[string][]string{
		"no path": {"ws", "add", "x", "--repo", "a/b", "--role", "docs"},
		"no repo": {"ws", "add", "x", "--path", "/p", "--role", "docs"},
		"no role": {"ws", "add", "x", "--path", "/p", "--repo", "a/b"},
		"no name": {"ws", "add", "--path", "/p", "--repo", "a/b", "--role", "docs"},
	} {
		if code, _, _ := s.runCLI("", args...); code != exitcode.Usage {
			t.Errorf("%s: exit %d, want usage", name, code)
		}
	}
	if n := len(s.requests("POST /v1/workspaces")); n != 1 {
		t.Errorf("a usage error sent a request: %d requests", n)
	}
}

func TestWsLsAndAgentLs(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/workspaces", 200, ok(wsList))
	code, out, errOut := s.runCLI("", "ws", "ls")
	if code != 0 || errOut != "" || !strings.Contains(out, "docs-ws") || !strings.Contains(out, "docs,runtime") || !strings.Contains(out, "/Users/h/ws/docs") {
		t.Errorf("ws ls: %d %q %q", code, out, errOut)
	}
	code, out, _ = s.runCLI("", "agent", "ls")
	if code != 0 || !strings.Contains(out, "docs-ws/docs") || !strings.Contains(out, "agent/runtime") {
		t.Errorf("agent ls: %d %q", code, out)
	}
	if _, out, _ := s.runCLI("", "agent", "ls", "other"); strings.Contains(out, "docs-ws/") {
		t.Errorf("agent ls other showed %q", out)
	}
	if code, out, _ := s.runCLI("", "ws", "ls", "--json"); code != 0 || out != ok(wsList) {
		t.Errorf("--json = %d %q", code, out)
	}
}

func TestAgentAddAndRemoveAndWsRemove(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/workspaces/docs-ws/agents", 201, ok(`{"id":"a3","role":"review","branch":"agent/review"}`))
	s.reply("DELETE /v1/workspaces/docs-ws/agents/review", 200, ok(`{}`))
	s.reply("DELETE /v1/workspaces/docs-ws", 200, ok(`{}`))
	if code, out, _ := s.runCLI("", "agent", "add", "docs-ws", "review", "--instructions", "review only"); code != 0 || out != "docs-ws/review\n" {
		t.Errorf("agent add: %d %q", code, out)
	}
	var b map[string]string
	_ = json.Unmarshal([]byte(s.requests("POST /v1/workspaces/docs-ws/agents")[0].body), &b)
	if b["role"] != "review" || b["instructions"] != "review only" {
		t.Errorf("body = %v", b)
	}
	if code, out, _ := s.runCLI("", "agent", "rm", "docs-ws/review"); code != 0 || out != "docs-ws/review\n" {
		t.Errorf("agent rm: %d %q", code, out)
	}
	for _, bad := range []string{"review", "/review", "docs-ws/"} {
		if code, _, _ := s.runCLI("", "agent", "rm", bad); code != exitcode.Usage {
			t.Errorf("agent rm %q: exit %d, want usage", bad, code)
		}
	}
	if code, out, _ := s.runCLI("", "ws", "rm", "docs-ws"); code != 0 || out != "docs-ws\n" {
		t.Errorf("ws rm: %d %q", code, out)
	}
}

func TestWsErrorsKeepTheServersExitCode(t *testing.T) {
	s := newStub(t)
	s.reply("DELETE /v1/workspaces/busy", 409, fail("conflict", exitcode.Conflict, "workspace busy still has 1 agent(s): remove them first"))
	code, out, errOut := s.runCLI("", "ws", "rm", "busy")
	if code != exitcode.Conflict || out != "" || !strings.Contains(errOut, "remove them first") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestLsShowsTheAgentAsWorkspaceSlashRole(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(`[{"id":"t1","repo":"a/b","issue":"#1","state":"running","agent_id":"x-af2f","agent":"docs-ws/docs"},{"id":"t0","repo":"a/b","issue":"#0","state":"completed","agent_id":"x-old"}]`))
	_, out, _ := s.runCLI("", "ls")
	if !strings.Contains(out, "docs-ws/docs") || strings.Contains(out, "x-af2f") || !strings.Contains(out, "x-old") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestCompletionOffersWorkspacesAndAgents(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/workspaces", 200, ok(wsList))
	for args, want := range map[string]string{"ws rm ": "docs-ws", "agent add ": "docs-ws", "agent rm ": "docs-ws/runtime"} {
		_, out, _ := s.runCLI("", append([]string{"__complete"}, append(strings.Fields(args), "")...)...)
		if !strings.Contains(out, want) {
			t.Errorf("completion for %q = %q, want %s", args, out, want)
		}
	}
}

// `whr completion <shell>` prints the completion script, as data on stdout, for
// each shell cobra supports; it needs no configuration or server.
func TestCompletionScriptsAreGeneratedForEveryShell(t *testing.T) {
	s := newStub(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		code, out, errOut := s.runCLI("", "completion", shell)
		if code != 0 || errOut != "" || !strings.Contains(out, "whr") || len(out) < 500 {
			t.Errorf("%s: exit %d, %d bytes, stderr %q", shell, code, len(out), errOut)
		}
	}
	if n := len(s.req); n != 0 {
		t.Errorf("generating a script sent %d requests", n)
	}
	if _, out, _ := s.runCLI("", "help"); !strings.Contains(out, "completion") {
		t.Error("whr help does not list the completion command")
	}
	for _, args := range [][]string{{"completion", "nonsense"}, {"ws", "bogus"}, {"agent", "bogus"}} {
		if code, out, _ := s.runCLI("", args...); code != exitcode.Usage || out != "" {
			t.Errorf("%v: exit %d, stdout %q, want usage and no data", args, code, out)
		}
	}
}
