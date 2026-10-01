package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

func TestTheSpecOfAnEnvironmentIsHardenedAndPassesPrepare(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(home, "tools")
	libexec := filepath.Join(home, "libexec")
	ws := filepath.Join(home, "ws", "docs")
	for _, d := range []string{store, libexec, ws} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	proxy := filepath.Join(libexec, "whr-proxy-linux-arm64")
	if err := os.WriteFile(proxy, []byte("x"), 0o700); err != nil { //nolint:gosec // a stand-in binary
		t.Fatal(err)
	}
	opts := SpecOptions{Owner: Owner, Env: config.Environment{}.Resolved(), ToolStore: store, Proxy: proxy}
	spec := opts.For(domain.Workspace{ID: "w1"})
	spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountBind, Source: ws, Target: "/ws"}) // what the service adds
	if err := spec.Validate(); err != nil {
		t.Fatalf("the spec is not valid: %v", err)
	}
	if _, err := runtime.Prepare(runtime.PrepareOptions{
		FS: runtime.OSFS{}, Home: home, Roots: []string{filepath.Join(home, "ws"), store, libexec},
		Owns: func(v string) bool { return strings.HasPrefix(v, Owner+"-") },
	}, spec); err != nil {
		t.Errorf("Prepare refused the spec: %v", err)
	}
	if !spec.Network.Internal || !spec.ReadOnlyRoot || spec.User == "root" || len(spec.CapDrop) != 1 || spec.CapDrop[0] != "ALL" {
		t.Errorf("not hardened: %+v", spec)
	}
	if spec.Egress == nil || spec.Egress.Proxy != proxy || len(spec.Egress.Allow) == 0 {
		t.Errorf("egress = %+v", spec.Egress)
	}
	for _, m := range spec.Mounts {
		if m.Kind == runtime.MountBind && m.Target == ToolsMount && !m.ReadOnly {
			t.Error("the tool store is writable in the environment")
		}
	}
	// Two workspaces never share a network or an agent home.
	other := opts.For(domain.Workspace{ID: "w2"})
	if other.Network.Name == spec.Network.Name || other.Mounts[1].Source == spec.Mounts[1].Source {
		t.Error("two workspaces share a network or a home volume")
	}
}

func TestTheAgentRunsInTheDegradedModeWithTheSupervisorsAllowlist(t *testing.T) {
	spec := AgentSpec([]string{"Read", "Bash(git status:*)"}, agent.AuthSubscription)(domain.Task{}, domain.Run{})
	if spec.PermissionMode != agent.PermissionDontAsk || len(spec.AllowedTools) != 2 || spec.Auth != agent.AuthSubscription {
		t.Errorf("spec = %+v", spec)
	}
	caps := agent.Capabilities{AuthModes: []agent.AuthMode{agent.AuthSubscription}} // no host approvals: the degraded agent
	if err := caps.CheckSpec(spec); err != nil {
		t.Errorf("a degraded agent refuses the spec: %v", err)
	}
	// The allowlist is a copy: changing the result must not change the config's.
	allowed := []string{"Read"}
	s := AgentSpec(allowed, agent.AuthAPIKey)(domain.Task{}, domain.Run{})
	s.AllowedTools[0] = "Bash"
	if allowed[0] != "Read" {
		t.Error("the allowlist is shared with the configuration")
	}
}

func TestToolProfile(t *testing.T) {
	root := t.TempDir()
	c := &config.Config{Roots: config.Roots{ToolStore: root}}
	if _, err := toolProfile(c); err == nil || !strings.Contains(err.Error(), "whr tools build") {
		t.Errorf("no profiles = %v", err)
	}
	for _, p := range []string{"claude-1.0"} {
		if err := os.MkdirAll(filepath.Join(root, "profiles", p), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := toolProfile(c); err != nil || got != "claude-1.0" {
		t.Errorf("one profile = %q, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "profiles", "claude-2.0"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := toolProfile(c); err == nil || !strings.Contains(err.Error(), "set tool_profile") {
		t.Errorf("two profiles = %v", err)
	}
	c.ToolProfile = "claude-2.0"
	if got, err := toolProfile(c); err != nil || got != "claude-2.0" {
		t.Errorf("configured = %q, %v", got, err)
	}
	c.ToolProfile = "nope"
	if _, err := toolProfile(c); err == nil {
		t.Error("an unknown profile was accepted")
	}
}

func TestBuildNeedsAnAllowlistAndAnInstalledProxy(t *testing.T) {
	c := &config.Config{}
	if _, _, err := Build(c, "/x/bin/whr", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "agent_allowed_tools") {
		t.Errorf("no allowlist = %v", err)
	}
	c.AgentAllowedTools = []string{"Read"}
	c.Roots.ToolStore = t.TempDir()
	if err := os.MkdirAll(filepath.Join(c.Roots.ToolStore, "profiles", "p"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build(c, filepath.Join(t.TempDir(), "bin", "whr"), t.TempDir(), nil); err == nil {
		t.Error("a missing installed proxy was accepted")
	}
}

func TestTheUnconfiguredForgeSaysSo(t *testing.T) {
	_, err := unconfiguredForge{}.GetIssue(nil, "a/b", 1) //nolint:staticcheck // the context is unused
	if err == nil || !strings.Contains(err.Error(), "no forge adapter") {
		t.Errorf("err = %v", err)
	}
}

// The store's redactor knows this supervisor's exact secrets, so they are masked
// whatever their format (T9): the API token and the API key's value.
func TestTheRedactorKnowsTheSupervisorsOwnSecrets(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api.token")
	token := "whr-test-token-" + strings.Repeat("x", 20)
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{APITokenFile: tokenFile}
	key := "plainvalue-" + strings.Repeat("k", 24) // not a well-known token format
	rd, err := Redactor(c, []string{"ANTHROPIC_API_KEY=" + key})
	if err != nil {
		t.Fatal(err)
	}
	out := rd.String("token " + token + " key " + key)
	if strings.Contains(out, token) || strings.Contains(out, key) {
		t.Errorf("a supervisor secret was not redacted: %s", out)
	}
	if _, err := Redactor(c, []string{"ANTHROPIC_API_KEY=zq7"}); err == nil || strings.Contains(err.Error(), "zq7") {
		t.Errorf("a value too short to redact = %v, want an error that names no value", err)
	}
}
