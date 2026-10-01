package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/claude"
	"github.com/wstein/workharbor/internal/api"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/redact"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/apple"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// Owner is the owner label of this supervisor's environments.
const Owner = "whr"

// GuestHome is the agent's home in the environment: a volume of its own, where
// the human signs in (D40).
const GuestHome = "/home/agent"

// ToolsMount is where the tool store is mounted, read-only, in every environment.
const ToolsMount = "/tools"

// SpecOptions is what an environment's spec is made from.
type SpecOptions struct {
	Owner     string
	Env       config.Environment // resolved
	ToolStore string             // the host directory of the tool store
	Proxy     string             // the host path of whr-proxy for linux/arm64
}

// For returns the spec of a workspace's environment: hardened, on an internal
// network of its own, with the tool store read-only, the agent home on a named
// volume of its own and the egress proxy as the only way out (design §5.1,
// §7.2). The workspace folder is added by the service, not here.
func (o SpecOptions) For(w domain.Workspace) runtime.Spec {
	e := o.Env
	return runtime.Spec{
		Image: e.Image, Owner: o.Owner, CPUs: e.CPUs, MemoryMB: e.MemoryMB, DiskMB: e.DiskMB,
		Network:      runtime.Network{Name: o.Owner + "-net-" + string(w.ID), Internal: true},
		User:         "1000:1000",
		ReadOnlyRoot: true,
		CapDrop:      []string{"ALL"},
		Init:         true,
		Tmpfs:        []string{"/tmp", "/run"},
		Mounts: []runtime.Mount{
			{Kind: runtime.MountBind, Source: o.ToolStore, Target: ToolsMount, ReadOnly: true},
			{Kind: runtime.MountVolume, Source: o.Owner + "-home-" + string(w.ID), Target: GuestHome},
		},
		Egress: &runtime.Egress{Image: e.Image, Proxy: o.Proxy, Allow: e.EgressAllow},
	}
}

// AgentSpec returns the StartSpec of a run. Until host approvals exist (#75)
// the agent runs in dontAsk mode with the supervisor's own allowlist, which is
// required: with none, nothing would be allowed.
func AgentSpec(allowed []string, auth agent.AuthMode) func(domain.Task, domain.Run) agent.StartSpec {
	return func(domain.Task, domain.Run) agent.StartSpec {
		return agent.StartSpec{
			Auth: auth, PermissionMode: agent.PermissionDontAsk, AllowedTools: append([]string(nil), allowed...),
			Prompt: "continue",
		}
	}
}

// unconfiguredForge is the forge of a supervisor that has no forge adapter yet:
// every call says so, so `whr run` fails with a clear message and not a nil.
type unconfiguredForge struct{}

var errNoForge = errors.New("no forge adapter is built yet (the GitHub App adapter, issue #27): the supervisor cannot load issues")

func (unconfiguredForge) GetIssue(context.Context, string, int) (forge.Issue, error) {
	return forge.Issue{}, errNoForge
}

// toolProfile finds the tool store profile to use: the configured one, or the
// only one there is.
func toolProfile(c *config.Config) (string, error) {
	if c.ToolProfile != "" {
		if _, err := os.Stat(filepath.Join(c.Roots.ToolStore, "profiles", c.ToolProfile)); err != nil {
			return "", fmt.Errorf("tool_profile %q is not in the tool store: run `whr tools build`: %w", c.ToolProfile, err)
		}
		return c.ToolProfile, nil
	}
	entries, err := os.ReadDir(filepath.Join(c.Roots.ToolStore, "profiles"))
	if err != nil {
		return "", fmt.Errorf("the tool store has no profiles (run `whr tools build`): %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && e.Name()[0] != '.' {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) != 1 {
		return "", fmt.Errorf("the tool store has %d profiles %v: set tool_profile", len(names), names)
	}
	return names[0], nil
}

// Redactor returns the redactor the store applies before it writes (§5.4,
// threat model T9): the well-known token formats, plus the exact secrets this
// supervisor holds, so they are masked whatever their format: the API token and
// the values of the agent's API-key file (D40). A value too short to register
// is refused rather than silently left unredacted.
func Redactor(c *config.Config, agentEnv []string) (*redact.Redactor, error) {
	rd := redact.New()
	tok, err := api.TokenFromConfig(c)
	if err != nil {
		return nil, err
	}
	if !rd.Add(string(tok)) {
		return nil, fmt.Errorf("api_token_file: the token is shorter than %d characters", redact.MinSecretLength)
	}
	for i, e := range agentEnv {
		_, v, _ := strings.Cut(e, "=")
		if v == "" {
			continue
		}
		if !rd.Add(v) {
			return nil, fmt.Errorf("agent_api_key_env_file: the value of entry %d is shorter than %d characters", i+1, redact.MinSecretLength)
		}
	}
	return rd, nil
}

// StateDir returns the directory of the database.
func StateDir(c *config.Config, home string) string {
	if c.StateDir != "" {
		return c.StateDir
	}
	return filepath.Join(home, ".local", "state", "whr")
}

// Build makes the real dependencies of `whr serve` from the configuration:
// the database, hostgit, the Apple Container runtime, the Claude Code adapter
// and the spec factory. exe is the path of the running whr, which says where
// the installed egress proxy is. The returned function releases what was opened.
func Build(c *config.Config, exe, home string, logf func(string, ...any)) (Deps, func(), error) {
	if len(c.AgentAllowedTools) == 0 {
		return Deps{}, nil, errors.New("agent_allowed_tools is needed: while the agent runs in the degraded dontAsk mode (D26, issue #75) only the tools you list may run")
	}
	profile, err := toolProfile(c)
	if err != nil {
		return Deps{}, nil, err
	}
	proxy, err := service.InstalledProxy(exe)
	if err != nil {
		return Deps{}, nil, err
	}
	env, mode, err := service.AgentCredentials(c)
	if err != nil {
		return Deps{}, nil, err
	}

	dir := StateDir(c, home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Deps{}, nil, err
	}
	rd, err := Redactor(c, env)
	if err != nil {
		return Deps{}, nil, err
	}
	st, err := store.Open(context.Background(), filepath.Join(dir, "workharbor.db"), store.WithRedactor(rd))
	if err != nil {
		return Deps{}, nil, err
	}
	git, err := hostgit.New()
	if err != nil {
		_ = st.Close()
		return Deps{}, nil, err
	}
	bin := ToolsMount + "/profiles/" + profile + "/bin"
	rt, err := apple.New(Owner, apple.WithShim(bin+"/whr-shim"))
	if err != nil {
		_ = git.Close()
		_ = st.Close()
		return Deps{}, nil, err
	}
	ag := claude.New(rt, claude.Config{Bin: bin + "/claude", ConfigDir: GuestHome + "/.claude", Env: env})

	opts := SpecOptions{Owner: Owner, Env: c.Environment.Resolved(), ToolStore: c.Roots.ToolStore, Proxy: proxy}
	roots := append(append([]string(nil), c.Roots.Workspaces...), c.Roots.ToolStore, filepath.Dir(proxy))
	prepare := func(s runtime.Spec) (runtime.PreparedSpec, error) {
		return runtime.Prepare(runtime.PrepareOptions{
			FS: runtime.OSFS{}, Home: home, Roots: roots,
			Owns: func(volume string) bool { return len(volume) > len(Owner)+1 && volume[:len(Owner)+1] == Owner+"-" },
		}, s)
	}
	return Deps{
		Config: c, Store: st, Runtime: rt, Agent: ag, Issues: unconfiguredForge{}, Git: git, Owner: Owner,
		Spec: opts.For, Prepare: prepare, AgentSpec: AgentSpec(c.AgentAllowedTools, mode), Logf: logf,
	}, func() {
		_ = git.Close()
		_ = st.Close()
	}, nil
}
