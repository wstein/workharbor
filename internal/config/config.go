// Package config reads the minimal configuration file `whr serve` needs
// (design D34, §13). Paths to secrets are files, never values: a token written
// in the config would end up in a backup, a log or a screenshot. The file is
// JSON, decoded strictly, so an unknown key or a misspelt one is an error and
// not silently ignored.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/wstein/workharbor/internal/hostgit"
)

// Repository is one repository workharbor works on.
type Repository struct {
	Name       string `json:"name"`        // "owner/name"
	CloneDepth int    `json:"clone_depth"` // 0 keeps the full history (design §4.5)
}

// Roots are the directories workharbor owns on the host.
type Roots struct {
	// Workspaces are the folders workspaces may live under (D42): on the
	// internal disk or an external SSD. An agent writes them, so nothing
	// workharbor trusts lives in or contains one, and none is inside a git
	// repository of the human's.
	Workspaces []string `json:"workspaces"`
	ToolStore  string   `json:"tool_store"` // the shared read-only tool store (§5.6)
}

// GitHub is the App the forge adapter acts as (D31).
type GitHub struct {
	AppID   int64  `json:"app_id"`
	KeyFile string `json:"key_file"` // the App's private key
}

// Config is the whole file.
type Config struct {
	// Listen is the address `whr serve` binds: a loopback address (D29).
	Listen       string       `json:"listen"`
	Repositories []Repository `json:"repositories"`
	Roots        Roots        `json:"roots"`
	GitHub       GitHub       `json:"github"`
	// AgentAPIKeyEnvFile holds an agent API key (for example
	// ANTHROPIC_API_KEY) as KEY=VALUE lines. It is optional: with a
	// subscription login there is none, because whr never handles a
	// subscription credential; the human signs in inside the environment
	// (D40), and a subscription token in this file is refused.
	AgentAPIKeyEnvFile string `json:"agent_api_key_env_file,omitempty"`
	// APITokenFile holds the API token that guards the API (D29).
	APITokenFile string `json:"api_token_file"`
	// StateDir holds the supervisor's database. Optional: an absolute path, or
	// ~/.local/state/whr. It must not lie in a workspace root, where an agent
	// writes.
	StateDir string `json:"state_dir,omitempty"`
	// Environment shapes the environments `whr serve` provisions. Optional.
	Environment Environment `json:"environment,omitzero"`
}

// Environment is the supervisor's choice of what an agent environment looks
// like (design §5.1, D38: a repository may request, never grant). Zero values
// take the defaults of the field.
type Environment struct {
	// Image is the stock base image, Fedora by default (D43).
	Image string `json:"image,omitempty"`
	// EgressAllow are the host names the agent may reach through the egress
	// proxy, `api.anthropic.com` by default. Names only: no IP, no wildcard.
	EgressAllow []string `json:"egress_allow,omitempty"`
	CPUs        int      `json:"cpus,omitempty"`      // default 2
	MemoryMB    int      `json:"memory_mb,omitempty"` // default 4096
	DiskMB      int      `json:"disk_mb,omitempty"`   // default 10240
}

// Defaults of Environment.
const (
	DefaultImage    = "docker.io/library/fedora:latest" // provisional: pin by digest (D43)
	DefaultCPUs     = 2
	DefaultMemoryMB = 4096
	DefaultDiskMB   = 10240
)

// Resolved returns the environment with the defaults filled in.
func (e Environment) Resolved() Environment {
	if e.Image == "" {
		e.Image = DefaultImage
	}
	if len(e.EgressAllow) == 0 {
		e.EgressAllow = []string{"api.anthropic.com"}
	}
	if e.CPUs == 0 {
		e.CPUs = DefaultCPUs
	}
	if e.MemoryMB == 0 {
		e.MemoryMB = DefaultMemoryMB
	}
	if e.DiskMB == 0 {
		e.DiskMB = DefaultDiskMB
	}
	return e
}

// Error lists everything wrong with a configuration, each with the key it is
// about, so one start reports every problem and not the first.
type Error struct{ Problems []string }

func (e *Error) Error() string {
	return "invalid configuration:\n  " + strings.Join(e.Problems, "\n  ")
}

// Load reads and validates the file at path. The error is an *Error for a
// configuration problem and an ordinary error for an unreadable file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path) //nolint:gosec // the operator names the config file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse decodes and validates a configuration.
func Parse(raw []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, &Error{Problems: []string{"the file is not a valid configuration: " + err.Error()}}
	}
	if dec.More() {
		return nil, &Error{Problems: []string{"the file has more than one JSON value"}}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the configuration against the host.
func (c *Config) Validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if err := checkListen(c.Listen); err != "" {
		add("listen: %s", err)
	}

	seen := map[string]string{}
	if len(c.Repositories) == 0 {
		add("repositories: at least one repository is needed")
	}
	for i, r := range c.Repositories {
		key := fmt.Sprintf("repositories[%d]", i)
		if !hostgit.ValidRepoName(r.Name) {
			add("%s.name: %q is not owner/name of ASCII letters, digits, '.', '_' and '-'", key, r.Name)
		}
		if lower := strings.ToLower(r.Name); seen[lower] != "" {
			add("%s.name: %q is the same repository as %s on a case-insensitive disk", key, r.Name, seen[lower])
		} else {
			seen[lower] = key
		}
		if r.CloneDepth < 0 {
			add("%s.clone_depth: %d is negative", key, r.CloneDepth)
		}
	}

	roots := map[string]string{"roots.tool_store": c.Roots.ToolStore}
	if len(c.Roots.Workspaces) == 0 {
		add("roots.workspaces: at least one workspace root is needed")
	}
	for i, w := range c.Roots.Workspaces {
		roots[fmt.Sprintf("roots.workspaces[%d]", i)] = w
	}
	resolved := map[string]string{}
	for _, key := range sortedKeys(roots) {
		dir, msg := checkDir(roots[key])
		if msg != "" {
			add("%s: %s", key, msg)
			continue
		}
		resolved[key] = dir
		if repo := enclosingRepo(dir); repo != "" {
			add("%s: %s is inside the git repository %s, which is not workharbor's to mount", key, dir, repo)
		}
	}
	// An agent writes its workspace, so nothing workharbor trusts may live in or
	// contain it, and the roots are separate places.
	keys := sortedKeys(resolved)
	for i, a := range keys {
		for _, b := range keys[i+1:] {
			if within(resolved[a], resolved[b]) || within(resolved[b], resolved[a]) {
				add("%s and %s overlap (%s, %s): they must be separate directories", a, b, resolved[a], resolved[b])
			}
		}
	}

	if c.StateDir != "" {
		if dir, msg := checkDir(c.StateDir); msg != "" {
			add("state_dir: %s", msg)
		} else {
			for _, key := range sortedKeys(resolved) {
				if strings.HasPrefix(key, "roots.workspaces") && (within(dir, resolved[key]) || within(resolved[key], dir)) {
					add("state_dir: %s overlaps %s: the database must be out of reach of an agent", dir, key)
				}
			}
		}
	}
	if e := c.Environment; e.CPUs < 0 || e.MemoryMB < 0 || e.DiskMB < 0 {
		add("environment: cpus, memory_mb and disk_mb must not be negative")
	}
	for _, h := range c.Environment.EgressAllow {
		if !validEgressHost(h) {
			add("environment.egress_allow: %q is not a host name (no IP address, wildcard, port or path)", h)
		}
	}

	if c.GitHub.AppID <= 0 {
		add("github.app_id: a positive App ID is needed")
	}
	secrets := map[string]string{"github.key_file": c.GitHub.KeyFile, "api_token_file": c.APITokenFile}
	if c.AgentAPIKeyEnvFile != "" {
		secrets["agent_api_key_env_file"] = c.AgentAPIKeyEnvFile
	}
	for _, key := range sortedKeys(secrets) {
		if msg := checkSecretFile(secrets[key]); msg != "" {
			add("%s: %s", key, msg)
		} else if key == "agent_api_key_env_file" {
			if _, err := c.AgentAPIKey(); err != nil {
				add("%s: %s", key, strings.TrimPrefix(err.Error(), "config: "+key+": "))
			}
		}
	}
	// A secret file inside a root would reach an agent: it writes the
	// workspaces, every environment mounts the tool store, and checkouts share
	// the cache's objects. Directories are compared by identity, so a case
	// variant of a path on APFS is caught too.
	why := func(root string) string {
		if root == "roots.tool_store" {
			return "the tool store, which every environment mounts"
		}
		return "a workspace root, where an agent can read it"
	}
	for key, path := range secrets {
		for _, root := range sortedKeys(resolved) {
			if within(path, resolved[root]) {
				add("%s: %s is inside %s", key, path, why(root))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &Error{Problems: problems}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// within reports whether path is dir or lies below it. It compares directories
// by identity (device and inode), walking up from path, so a link or a case
// variant of a name on a case-insensitive disk does not hide the relation.
func within(path, dir string) bool {
	d, err := os.Stat(dir)
	if err != nil {
		return false
	}
	p := filepath.Clean(path)
	if target, err := filepath.EvalSymlinks(p); err == nil {
		p = target
	}
	for {
		if fi, err := os.Stat(p); err == nil && os.SameFile(fi, d) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// checkListen accepts host:port where the host is a loopback address: the API
// listens on loopback (D29), and a name is not resolved, so what is bound is
// what is written.
func checkListen(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Sprintf("%q is not host:port", addr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Sprintf("port %q is not a port", port)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Sprintf("%q is not a loopback address: a guest reaches every other address of the host (D29), so the API listens on 127.0.0.1 or ::1", host)
	}
	return ""
}

// checkDir returns the resolved path of an existing, absolute, clean directory.
func checkDir(path string) (string, string) {
	switch {
	case path == "":
		return "", "a directory is needed"
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		return "", fmt.Sprintf("%q must be an absolute, clean path", path)
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Sprintf("%q does not exist", path)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		return "", fmt.Sprintf("%q is not a directory", path)
	}
	return target, ""
}

// checkSecretFile requires a regular file, not a link, owned by the current
// user, with mode 0600, a single link and some content: the value is in the
// file, never in the configuration. A second hard link could sit where an
// agent reads it.
func checkSecretFile(path string) string {
	switch {
	case path == "":
		return "a file path is needed"
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		return fmt.Sprintf("%q must be an absolute, clean path", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Sprintf("%q does not exist", path)
	}
	return checkSecretInfo(path, info)
}

func checkSecretInfo(path string, info os.FileInfo) string {
	if !info.Mode().IsRegular() {
		return fmt.Sprintf("%q is not a regular file (a link or a directory is refused)", path)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return fmt.Sprintf("%q has mode %04o, it must be 0600 so that nobody else can read it", path, perm)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) != os.Getuid() { //nolint:gosec // a uid fits an int
			return fmt.Sprintf("%q is owned by another user", path)
		}
		if st.Nlink != 1 {
			return fmt.Sprintf("%q has %d hard links, it must have one", path, st.Nlink)
		}
	}
	if info.Size() == 0 {
		return fmt.Sprintf("%q is empty", path)
	}
	if info.Size() > maxSecret {
		return fmt.Sprintf("%q is larger than %d bytes", path, maxSecret)
	}
	return ""
}

// maxSecret bounds a secret file; a key or a token is far smaller.
const maxSecret = 64 << 10

// ReadSecret reads a secret file without following a link, and checks the file
// it opened (not the path) like the configuration check does, so a file
// swapped after the check is refused. An error names the path, never the content.
func ReadSecret(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("config: %q must be an absolute, clean path", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // the operator names the secret; checked on the open file
	if err != nil {
		return nil, fmt.Errorf("config: cannot open the secret %q (a link is refused): %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if msg := checkSecretInfo(path, info); msg != "" {
		return nil, errors.New("config: " + msg)
	}
	return io.ReadAll(io.LimitReader(f, maxSecret))
}

// AgentAPIKey reads AgentAPIKeyEnvFile: KEY=VALUE lines, with blank lines and
// lines starting with '#' ignored. The entries go to the agent's process
// environment through the runtime's env file, never a command line. With no
// file it returns nothing. A subscription credential is refused: whr never
// reads, stores or relays one (D40); errors name the line, never its value.
func (c *Config) AgentAPIKey() ([]string, error) {
	if c.AgentAPIKeyEnvFile == "" {
		return nil, nil
	}
	raw, err := ReadSecret(c.AgentAPIKeyEnvFile)
	if err != nil {
		return nil, err
	}
	var env []string
	for i, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, _, ok := strings.Cut(line, "=")
		switch {
		case !ok || !envKey.MatchString(k):
			return nil, fmt.Errorf("config: agent_api_key_env_file: line %d is not KEY=VALUE", i+1)
		case subscriptionKey.MatchString(k):
			return nil, fmt.Errorf("config: agent_api_key_env_file: line %d sets %s, a subscription credential; whr never handles one, so sign in inside the environment instead (D40)", i+1, k)
		}
		env = append(env, line)
	}
	if len(env) == 0 {
		return nil, errors.New("config: agent_api_key_env_file: no KEY=VALUE line")
	}
	return env, nil
}

// subscriptionKey matches variable names that carry a consumer-plan sign-in,
// such as CLAUDE_CODE_OAUTH_TOKEN, rather than an API key.
var subscriptionKey = regexp.MustCompile(`(?i)oauth|session`)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// enclosingRepo returns the nearest directory at or above dir that holds a
// .git, or "". A workspace root there would put a human's repository around
// the agents' clones, and a human's own repository is never mounted (D42).
func enclosingRepo(dir string) string {
	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return p
		}
		if filepath.Dir(p) == p {
			return ""
		}
	}
}

// CheckWorkspacePath says whether a folder may become a workspace (design
// §4.4, D42) and returns its resolved path. It must be absolute and clean, an
// existing empty directory, strictly below one of the workspace roots, and not
// inside a git repository: the human's own repositories are never mounted, and
// a clone of one is not a workspace. Everything is compared by identity, so a
// link or a case variant cannot hide where the folder really is.
func (c *Config) CheckWorkspacePath(path string) (string, error) {
	resolved, msg := checkDir(path)
	if msg != "" {
		return "", fmt.Errorf("workspace path: %s", msg)
	}
	under := false
	for _, root := range c.Roots.Workspaces {
		if rd, err := os.Stat(root); err == nil {
			if pd, err := os.Stat(resolved); err == nil && os.SameFile(rd, pd) {
				return "", fmt.Errorf("workspace path: %s is a workspace root itself: use a folder below it", resolved)
			}
		}
		if within(resolved, root) {
			under = true
		}
	}
	if !under {
		return "", fmt.Errorf("workspace path: %s is not below a workspace root (roots.workspaces)", resolved)
	}
	if repo := enclosingRepo(resolved); repo != "" {
		return "", fmt.Errorf("workspace path: %s is inside the git repository %s: a human's own repository is never mounted", resolved, repo)
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("workspace path: %w", err)
	}
	if len(entries) > 0 {
		return "", fmt.Errorf("workspace path: %s is not empty: the agent clone is created in it", resolved)
	}
	return resolved, nil
}

// validEgressHost accepts a plain DNS name: letters, digits, '-' and '.',
// with at least one dot and no IP address, so the allowlist cannot be a way
// around the proxy's rule that a raw IP is refused.
func validEgressHost(h string) bool {
	if h == "" || len(h) > 253 || !strings.Contains(h, ".") || net.ParseIP(h) != nil {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
