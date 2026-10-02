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
	"github.com/wstein/workharbor/internal/baseimage"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge/github"
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

// AgentSpec returns the StartSpec of a run. In dontAsk mode the agent runs only
// the supervisor's own allowlist, which is required: with none, nothing would
// be allowed. In manual mode every prompt goes to the human (D26): the service
// supplies the approver and there is no allowlist.
func AgentSpec(mode agent.PermissionMode, allowed []string, auth agent.AuthMode) func(domain.Task, domain.Run) agent.StartSpec {
	if mode == "" {
		mode = agent.PermissionDontAsk
	}
	return func(domain.Task, domain.Run) agent.StartSpec {
		var tools []string
		if mode == agent.PermissionDontAsk {
			tools = append([]string(nil), allowed...)
		}
		return agent.StartSpec{
			Auth: auth, PermissionMode: mode, AllowedTools: tools,
			ApprovalTimeout: domain.DefaultApprovalTimeout,
			Prompt:          "continue",
			// A read-only root has no home: tools that want one (git, the
			// agent's own config lookups) use the agent home volume.
			Env: []string{"HOME=" + GuestHome},
		}
	}
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
	if c.GitHub.KeyFile != "" {
		key, err := config.ReadSecret(c.GitHub.KeyFile)
		if err != nil {
			return nil, err
		}
		if !rd.Add(string(key)) {
			return nil, fmt.Errorf("github.key_file: the key is shorter than %d characters", redact.MinSecretLength)
		}
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
	permission := agent.PermissionMode(c.AgentPermissionMode)
	if permission == "" {
		permission = agent.PermissionDontAsk
	}
	if permission == agent.PermissionDontAsk && len(c.AgentAllowedTools) == 0 {
		return Deps{}, nil, errors.New("agent_allowed_tools is needed in the dontAsk mode: only the tools you list may run (or set agent_permission_mode to manual to approve each one yourself, D26)")
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
	gh, err := newGitHub(c, rd)
	if err != nil {
		return Deps{}, nil, err
	}
	st, err := store.Open(context.Background(), filepath.Join(dir, "workharbor.db"), store.WithRedactor(rd))
	if err != nil {
		return Deps{}, nil, err
	}
	// The mirrors of the forge repositories and the supervisor's own repositories
	// live in the state directory, outside every workspace root; an editor copy
	// is never made in one (D42, §4.5).
	for _, d := range []string{"mirrors", "topics", EditorCopyDir} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			_ = st.Close()
			return Deps{}, nil, err
		}
	}
	gitOpts := []hostgit.Option{hostgit.WithCacheRoot(filepath.Join(dir, "mirrors"))}
	for _, root := range c.Roots.Workspaces {
		gitOpts = append(gitOpts, hostgit.WithWorkspaceRoot(root))
	}
	git, err := hostgit.New(gitOpts...)
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

	spec := c.Environment.Resolved()
	if spec.Image == "" {
		// The workharbor base image (D44): built once, again only when the
		// pinned base changes. The first start takes a while.
		tag, built, err := baseimage.Ensure(context.Background(), rt, baseimage.Distro(spec.Base), filepath.Join(dir, "build"))
		if err != nil {
			_ = git.Close()
			_ = st.Close()
			return Deps{}, nil, err
		}
		if built {
			logf("built the base image %s", tag)
		}
		spec.Image = tag
	}
	opts := SpecOptions{Owner: Owner, Env: spec, ToolStore: c.Roots.ToolStore, Proxy: proxy}
	roots := append(append([]string(nil), c.Roots.Workspaces...), c.Roots.ToolStore, filepath.Dir(proxy))
	prepare := func(s runtime.Spec) (runtime.PreparedSpec, error) {
		return runtime.Prepare(runtime.PrepareOptions{
			FS: runtime.OSFS{}, Home: home, Roots: roots,
			Owns: func(volume string) bool { return len(volume) > len(Owner)+1 && volume[:len(Owner)+1] == Owner+"-" },
		}, s)
	}
	return Deps{
		Config: c, Store: st, Runtime: rt, Agent: ag, Issues: gh, Forge: gh, Git: git, Owner: Owner,
		Topics: Topics(git, c, dir), EditorDir: filepath.Join(dir, EditorCopyDir),
		Spec: opts.For, Prepare: prepare, AgentSpec: AgentSpec(permission, c.AgentAllowedTools, mode), Logf: logf,
	}, func() {
		_ = git.Close()
		_ = st.Close()
	}, nil
}

// newGitHub builds the GitHub App client: the App's key is read with
// config.ReadSecret, and every installation token it mints is registered with
// the redactor (D31). It is scoped to the configured repositories.
func newGitHub(c *config.Config, rd *redact.Redactor) (*github.Client, error) {
	pemBytes, err := config.ReadSecret(c.GitHub.KeyFile)
	if err != nil {
		return nil, err
	}
	key, err := github.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("github.key_file: %w", err)
	}
	repos := make([]string, len(c.Repositories))
	for i, r := range c.Repositories {
		repos[i] = r.Name
	}
	return github.New(github.Config{AppID: c.GitHub.AppID, Key: key, Repos: repos, Redactor: rd, BaseURL: c.GitHub.APIURL})
}

// EditorCopyDir is the directory of the state directory that `whr open` makes
// its copies in.
const EditorCopyDir = "open"

// Topics opens, for a forge repository, the mirror (fed only from the forge)
// and the supervisor's own bare repository that an agent's branch is imported
// into (D42, §4.5). The mirror is fetched from the repository's public https
// address: an authenticated fetch of a private repository comes with the push
// flow (issue #27), so until then `whr open` on one fails at the fetch.
func Topics(git *hostgit.Git, c *config.Config, dir string) service.TopicsFunc {
	return func(ctx context.Context, repo string) (*hostgit.Repo, *hostgit.Cache, error) {
		mirror, err := git.CachePath(repo)
		if err != nil {
			return nil, nil, err
		}
		depth := 0
		for _, r := range c.Repositories {
			if strings.EqualFold(r.Name, repo) {
				depth = r.CloneDepth
			}
		}
		cache, err := git.OpenCache(ctx, mirror, hostgit.CacheConfig{Source: "https://github.com/" + repo + ".git", CloneDepth: depth})
		if err != nil {
			return nil, nil, err
		}
		own := filepath.Join(dir, "topics", filepath.Base(mirror))
		var topics *hostgit.Repo
		if _, serr := os.Stat(own); serr == nil {
			topics, err = git.OpenBare(ctx, own)
		} else {
			topics, err = git.InitBare(ctx, own)
		}
		if err != nil {
			return nil, nil, err
		}
		return topics, cache, nil
	}
}
