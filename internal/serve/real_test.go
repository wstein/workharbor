package serve

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/hostgit"
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
	opts := SpecOptions{Owner: Owner, Env: config.Environment{Image: "whr-base/fedora:abc123abc123"}.Resolved(), ToolStore: store, Proxy: proxy}
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
	// The build volume: one per environment, owned like the home, mounted
	// read-write outside the checkout (D39).
	var build []runtime.Mount
	for _, m := range spec.Mounts {
		if m.Target == GuestBuild {
			build = append(build, m)
		}
	}
	if len(build) != 1 || build[0].Kind != runtime.MountVolume || build[0].ReadOnly || build[0].Source != Owner+"-build-w1" {
		t.Errorf("the build volume = %+v", build)
	}
	if strings.HasPrefix(GuestBuild, "/ws") {
		t.Error("the build volume is inside the checkout")
	}
	for _, m := range other.Mounts {
		if m.Target == GuestBuild && m.Source == build[0].Source {
			t.Error("two workspaces share a build volume")
		}
	}
}

func TestTheAgentRunsInTheDegradedModeWithTheSupervisorsAllowlist(t *testing.T) {
	spec := AgentSpec(agent.PermissionDontAsk, []string{"Read", "Bash(git status:*)"}, agent.AuthSubscription)(domain.Task{}, domain.Run{})
	if spec.PermissionMode != agent.PermissionDontAsk || len(spec.AllowedTools) != 2 || spec.Auth != agent.AuthSubscription {
		t.Errorf("spec = %+v", spec)
	}
	caps := agent.Capabilities{AuthModes: []agent.AuthMode{agent.AuthSubscription}} // no host approvals: the degraded agent
	if err := caps.CheckSpec(spec); err != nil {
		t.Errorf("a degraded agent refuses the spec: %v", err)
	}
	// The allowlist is a copy: changing the result must not change the config's.
	allowed := []string{"Read"}
	s := AgentSpec("", allowed, agent.AuthAPIKey)(domain.Task{}, domain.Run{})
	s.AllowedTools[0] = "Bash"
	if allowed[0] != "Read" {
		t.Error("the allowlist is shared with the configuration")
	}
}

// In manual mode every prompt goes to the human: no allowlist, and the spec is
// valid only for an agent that routes approvals to the host. The service
// supplies the approver.
func TestManualModeHasNoAllowlistAndNeedsHostApprovals(t *testing.T) {
	spec := AgentSpec(agent.PermissionManual, []string{"Read"}, agent.AuthSubscription)(domain.Task{}, domain.Run{})
	if spec.PermissionMode != agent.PermissionManual || len(spec.AllowedTools) != 0 || spec.ApprovalTimeout <= 0 {
		t.Fatalf("spec = %+v", spec)
	}
	degraded := agent.Capabilities{AuthModes: []agent.AuthMode{agent.AuthSubscription}}
	if err := degraded.CheckSpec(spec); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("a degraded agent accepted manual mode: %v", err)
	}
	full := agent.Capabilities{HostApprovals: true, MidRunInstruction: true, AuthModes: []agent.AuthMode{agent.AuthSubscription}}
	spec.Approver = agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) { return agent.Approval{}, nil })
	if err := full.CheckSpec(spec); err != nil {
		t.Errorf("an agent with host approvals refused the spec: %v", err)
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

// The GitHub App's key is read with ReadSecret, parsed, and registered with the
// redactor (D31): its PEM never reaches the database or a log.
func TestTheGitHubClientIsBuiltFromTheAppKeyAndTheKeyIsRedacted(t *testing.T) {
	dir := t.TempDir()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	keyFile := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(dir, "api.token")
	if err := os.WriteFile(tokenFile, []byte("whr-test-token-"+strings.Repeat("x", 20)), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{
		APITokenFile: tokenFile, GitHub: config.GitHub{AppID: 4242, KeyFile: keyFile},
		Repositories: []config.Repository{{Name: "wstein/workharbor"}},
	}
	rd, err := Redactor(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out := rd.String("leaked: " + string(keyPEM)); strings.Contains(out, "BEGIN RSA PRIVATE KEY") && strings.Contains(out, string(keyPEM)) {
		t.Error("the App key was not registered with the redactor")
	}
	gh, err := newGitHub(c, rd)
	if err != nil || gh.Name() != "github" {
		t.Fatalf("newGitHub = %v, %v", gh, err)
	}
	// A key that is not an RSA private key is an error that does not repeat it.
	if err := os.WriteFile(keyFile, []byte("not a key at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newGitHub(c, rd); err == nil || strings.Contains(err.Error(), "not a key at all") {
		t.Errorf("a bad key = %v", err)
	}
}

// `whr open` needs the mirror and the supervisor's own repository of a forge
// repository, both in the state directory and nowhere an agent writes.
func TestTopicsOpensTheMirrorAndTheSupervisorsOwnRepository(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"mirrors", "topics"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	git, err := hostgit.New(hostgit.WithCacheRoot(filepath.Join(dir, "mirrors")))
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = git.Close() })
	topics := Topics(git, &config.Config{Repositories: []config.Repository{{Name: "wstein/workharbor", CloneDepth: 5}}}, dir)

	repo, cache, err := topics(context.Background(), "wstein/workharbor")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cache.Path()) != filepath.Join(dir, "mirrors") || filepath.Dir(repo.Path()) != filepath.Join(dir, "topics") {
		t.Errorf("mirror %s, repository %s: not under the state directory", cache.Path(), repo.Path())
	}
	// opened again, it is the same place and still works
	repo2, cache2, err := topics(context.Background(), "wstein/workharbor")
	if err != nil || repo2.Path() != repo.Path() || cache2.Path() != cache.Path() {
		t.Errorf("reopen: %v", err)
	}
	if _, _, err := topics(context.Background(), "../etc/passwd"); err == nil {
		t.Error("a repository name that is a path was accepted")
	}
}

// The agent's mode follows the repository's workflow unless the human overrides it.
func TestTheAgentModeFollowsTheWorkflow(t *testing.T) {
	c := &config.Config{
		Repositories:      []config.Repository{{Name: "a/proto", Workflow: "prototype"}, {Name: "a/integ"}, {Name: "a/pub", Workflow: "published"}},
		AgentAllowedTools: []string{"Read"},
	}
	for repo, want := range map[string]agent.PermissionMode{"a/proto": agent.PermissionDontAsk, "a/integ": agent.PermissionDontAsk, "a/pub": agent.PermissionManual, "unknown/x": agent.PermissionDontAsk} {
		spec := AgentSpecFor(c, agent.AuthSubscription)(domain.Task{Repo: repo}, domain.Run{})
		if spec.PermissionMode != want {
			t.Errorf("%s: %s, want %s", repo, spec.PermissionMode, want)
		}
		if want == agent.PermissionManual && len(spec.AllowedTools) != 0 {
			t.Errorf("%s: a manual run has an allowlist", repo)
		}
		if want == agent.PermissionDontAsk && len(spec.AllowedTools) != 1 {
			t.Errorf("%s: no allowlist in dontAsk", repo)
		}
	}
	// the human's override wins, for every repository
	c.AgentPermissionMode = "dontAsk"
	if m := AgentSpecFor(c, agent.AuthSubscription)(domain.Task{Repo: "a/pub"}, domain.Run{}).PermissionMode; m != agent.PermissionDontAsk {
		t.Errorf("an override of a published repository: %s", m)
	}
	// only published repositories: no allowlist needed
	only := &config.Config{Repositories: []config.Repository{{Name: "a/pub", Workflow: "published"}}}
	if needsAllowlist(only) {
		t.Error("a published-only supervisor needs no allowlist")
	}
	if !needsAllowlist(c) {
		t.Error("dontAsk repositories need one")
	}
}

func TestTheConsoleSpecIsHardenedReadOnlyByDefaultAndHasNoSecrets(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootA, rootB := filepath.Join(home, "a", "ws"), filepath.Join(home, "b", "ws") // the same base name
	docs := filepath.Join(rootB, "docs")
	libexec := filepath.Join(home, "libexec")
	for _, d := range []string{rootA, docs, libexec} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	proxy := filepath.Join(libexec, "whr-proxy-linux-arm64")
	if err := os.WriteFile(proxy, []byte("x"), 0o700); err != nil { //nolint:gosec // a stand-in binary
		t.Fatal(err)
	}
	opts := ConsoleOptions{Owner: Owner, Image: "whr-console/fedora:abc123abc123", Console: config.Console{}.Resolved(), Roots: []string{rootA, rootB}, Proxy: proxy}
	prepare := func(s runtime.Spec) error {
		_, err := runtime.Prepare(runtime.PrepareOptions{
			FS: runtime.OSFS{}, Home: home, Roots: []string{rootA, rootB, libexec},
			Owns: func(v string) bool { return strings.HasPrefix(v, Owner+"-") },
		}, s)
		return err
	}

	ro := opts.For(nil)
	if err := ro.Validate(); err != nil {
		t.Fatalf("the spec is not valid: %v", err)
	}
	if err := prepare(ro); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var targets []string
	for _, m := range ro.Mounts {
		targets = append(targets, m.Target)
		if m.Kind == runtime.MountBind && !m.ReadOnly {
			t.Errorf("a workspace root is writable by default: %+v", m)
		}
	}
	want := []string{"/workspaces/ws", "/workspaces/ws-2", GuestConsoleHome}
	if strings.Join(targets, " ") != strings.Join(want, " ") {
		t.Errorf("targets = %v, want %v (two roots with one base name must not share a target)", targets, want)
	}
	if ro.Egress == nil || ro.Egress.Image != opts.Image || len(ro.Egress.Allow) == 0 || ro.Network.Name != "whr-net-console" || !ro.Network.Internal {
		t.Errorf("egress or network: %+v %+v", ro.Egress, ro.Network)
	}
	for _, h := range ro.Egress.Allow {
		if !domain.ValidHost(h) || h == "api.anthropic.com" {
			t.Errorf("console egress host %q: the console reaches registries and the forge, not the model API", h)
		}
	}

	// One workspace writable: a read-write bind over its place in its root.
	rw := opts.For([]domain.Workspace{{ID: "w1", Name: "docs", Path: docs}})
	if err := prepare(rw); err != nil {
		t.Fatalf("Prepare with a writable workspace: %v", err)
	}
	var writable []runtime.Mount
	for _, m := range rw.Mounts {
		if m.Kind == runtime.MountBind && !m.ReadOnly {
			writable = append(writable, m)
		}
	}
	if len(writable) != 1 || writable[0].Source != docs || writable[0].Target != "/workspaces/ws-2/docs" {
		t.Errorf("writable mounts = %+v", writable)
	}
	// Nothing of workharbor's or of the agents' is in the console.
	for _, m := range append(ro.Mounts, rw.Mounts...) {
		for _, secret := range []string{".ssh", "tools", "secrets", ".config", ".claude"} {
			if strings.Contains(m.Source, string(filepath.Separator)+secret) {
				t.Errorf("mount %+v looks like a secret or the tool store", m)
			}
		}
	}
	// A workspace outside every root is never mounted writable.
	outside := opts.For([]domain.Workspace{{ID: "w2", Name: "elsewhere", Path: filepath.Join(home, "elsewhere")}})
	for _, m := range outside.Mounts {
		if m.Kind == runtime.MountBind && !m.ReadOnly {
			t.Errorf("a workspace outside the roots got a writable mount: %+v", m)
		}
	}
}

// A spec may mount only the volumes of its own environment: another workspace's
// volume, the console's from a workspace and a stranger with the owner's prefix are
// refused.
func TestASpecMountsOnlyTheVolumesOfItsOwnEnvironment(t *testing.T) {
	proxy := filepath.Join(t.TempDir(), "whr-proxy")
	if err := os.WriteFile(proxy, []byte("x"), 0o700); err != nil { //nolint:gosec // an executable test fake
		t.Fatal(err)
	}
	ws := SpecOptions{Owner: Owner, Env: config.Environment{Image: "img", CPUs: 1, MemoryMB: 512, DiskMB: 1024}, ToolStore: t.TempDir(), Proxy: proxy}.For(domain.Workspace{ID: "w1"})
	own := ownsVolumes(ws)
	for _, v := range []string{"whr-home-w1", "whr-build-w1"} {
		if !own(v) {
			t.Errorf("a workspace's own volume %q was refused", v)
		}
	}
	for _, v := range []string{"whr-home-w2", "whr-build-w2", "whr-console-home", "whr-anything", "whr-home-w1-extra", "other-home-w1", "whr-", ""} {
		if own(v) {
			t.Errorf("volume %q was accepted for workspace w1", v)
		}
	}
	console := ownsVolumes(ConsoleOptions{Owner: Owner}.For(nil))
	if !console("whr-console-home") || console("whr-home-w1") || console("whr-build-w1") {
		t.Error("the console may mount its own home and nothing else")
	}
	if ownsVolumes(runtime.Spec{})("whr-home-w1") || ownsVolumes(runtime.Spec{Network: runtime.Network{Name: "whr-net-"}})("whr-home-") {
		t.Error("a spec with no workspace identity owns volumes")
	}
	// the whole check: Prepare refuses another workspace's volume in a spec
	spec := ws
	spec.Egress = nil
	spec.Mounts = nil // only the volumes: a bind mount of a temporary directory is refused as a system directory on macOS
	for _, m := range ws.Mounts {
		if m.Kind == runtime.MountVolume {
			spec.Mounts = append(spec.Mounts, m)
		}
	}
	spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "whr-home-w2", Target: "/other"})
	if _, err := runtime.Prepare(runtime.PrepareOptions{FS: runtime.OSFS{}, Home: t.TempDir(), Owns: ownsVolumes(spec)}, spec); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("Prepare accepted another workspace's volume: %v", err)
	}
}
