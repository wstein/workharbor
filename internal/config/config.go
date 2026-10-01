// Package config reads the minimal configuration file `whr serve` needs
// (design D34, §13). Paths to secrets are files, never values: a token written
// in the config would end up in a backup, a log or a screenshot. The file is
// JSON, decoded strictly, so an unknown key or a misspelt one is an error and
// not silently ignored.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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
	Cache      string `json:"cache"`      // the bare repository caches
	Workspaces string `json:"workspaces"` // the topics' checkouts: agent-writable
	ToolStore  string `json:"tool_store"` // the shared read-only tool store (§5.6)
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
	// AgentLoginEnvFile holds the agent's login (for example the
	// CLAUDE_CODE_OAUTH_TOKEN of a subscription) as KEY=VALUE lines.
	AgentLoginEnvFile string `json:"agent_login_env_file"`
	// APITokenFile holds the API token that guards the API (D29).
	APITokenFile string `json:"api_token_file"`
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

	roots := map[string]string{"roots.cache": c.Roots.Cache, "roots.workspaces": c.Roots.Workspaces, "roots.tool_store": c.Roots.ToolStore}
	resolved := map[string]string{}
	for _, key := range sortedKeys(roots) {
		dir, msg := checkDir(roots[key])
		if msg != "" {
			add("%s: %s", key, msg)
			continue
		}
		resolved[key] = dir
	}
	// An agent writes its workspace, so nothing workharbor trusts may live in or
	// contain it, and the three roots are three places.
	keys := sortedKeys(resolved)
	for i, a := range keys {
		for _, b := range keys[i+1:] {
			if within(resolved[a], resolved[b]) || within(resolved[b], resolved[a]) {
				add("%s and %s overlap (%s, %s): they must be separate directories", a, b, resolved[a], resolved[b])
			}
		}
	}

	if c.GitHub.AppID <= 0 {
		add("github.app_id: a positive App ID is needed")
	}
	for key, path := range map[string]string{
		"github.key_file": c.GitHub.KeyFile, "agent_login_env_file": c.AgentLoginEnvFile, "api_token_file": c.APITokenFile,
	} {
		if msg := checkSecretFile(path); msg != "" {
			add("%s: %s", key, msg)
		}
	}
	// A secret file inside the workspace root would be readable by the agent.
	if ws, ok := resolved["roots.workspaces"]; ok {
		for key, path := range map[string]string{"github.key_file": c.GitHub.KeyFile, "agent_login_env_file": c.AgentLoginEnvFile, "api_token_file": c.APITokenFile} {
			if target, err := filepath.EvalSymlinks(path); err == nil && within(target, ws) {
				add("%s: %s is inside the workspace root, where an agent can read it", key, path)
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

// within reports whether path is dir or lies below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
// user, with mode 0600 and some content: the value is in the file, never in
// the configuration.
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
	if !info.Mode().IsRegular() {
		return fmt.Sprintf("%q is not a regular file (a link or a directory is refused)", path)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return fmt.Sprintf("%q has mode %04o, it must be 0600 so that nobody else can read it", path, perm)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() { //nolint:gosec // a uid fits an int
		return fmt.Sprintf("%q is owned by another user", path)
	}
	if info.Size() == 0 {
		return fmt.Sprintf("%q is empty", path)
	}
	return ""
}
