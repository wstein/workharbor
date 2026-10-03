package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxSettingsSize bounds what the check reads from a settings or pointer file.
const maxSettingsSize = 1 << 20

// readRegular reads a regular file of at most maxSettingsSize bytes; a FIFO, a
// symlink, a device or a larger file is an error, never a blocked read.
func readRegular(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(path) //nolint:gosec // a fixed name under the checkout or home, checked regular above
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxSettingsSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSettingsSize {
		return nil, errors.New("file too large")
	}
	return b, nil
}

// laneAgents are the subagents a workharbor lane starts: the wh-*.md files of
// .claude/agents/, so a new agent is covered without a code change.
func laneAgents(repo string) []string {
	files, _ := filepath.Glob(filepath.Join(repo, ".claude", "agents", "wh-*.md"))
	var names []string
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".md"))
	}
	return names
}

// laneAgentsCheck is a dogfood-phase check (D34, issue #160): Claude Code asks
// before it starts a subagent unless a permission rule allows it, and those
// rules live per clone in settings files nothing else looks at. It reads only
// `permissions.allow` of the three settings files Claude Code documents
// (https://code.claude.com/docs/en/permissions, "Agent (subagents)": the rule
// is `Agent(<name>)`, a bare `Agent` allows every subagent), never writes, and
// never fails: a missing rule is a warn.
func laneAgentsCheck(d Deps) func(context.Context) (Status, string) {
	return func(context.Context) (Status, string) {
		if d.RepoDir == "" {
			return OK, "not run inside a checkout; nothing to check"
		}
		agents := laneAgents(d.RepoDir)
		if len(agents) == 0 {
			return OK, "this is not a workharbor checkout with lane agents; nothing to check"
		}
		files := []string{
			filepath.Join(d.RepoDir, ".claude", "settings.json"),
			filepath.Join(mainRoot(d.RepoDir), ".claude", "settings.local.json"),
		}
		if d.Home != "" {
			files = append(files, filepath.Join(d.Home, ".claude", "settings.json"))
		}
		allowed := map[string]bool{}
		var unreadable []string
		for _, f := range files {
			rules, err := allowRules(f)
			if err != nil {
				unreadable = append(unreadable, f)
				continue
			}
			for _, r := range rules {
				allowed[r] = true
			}
		}
		var missing []string
		if !allowed["Agent"] {
			for _, a := range agents {
				if !allowed["Agent("+a+")"] {
					missing = append(missing, a)
				}
			}
		}
		switch {
		case len(missing) > 0:
			msg := "no allow rule for " + strings.Join(missing, ", ") + ", so Claude Code asks every time a lane starts one; add " + ruleList(missing) + " to permissions.allow in .claude/settings.local.json or ~/.claude/settings.json (the /permissions command or /update-config does it)"
			if len(unreadable) > 0 {
				msg += "; could not read " + strings.Join(unreadable, ", ")
			}
			return Warn, msg
		case len(unreadable) > 0:
			return OK, "every lane agent is allowed; could not read " + strings.Join(unreadable, ", ")
		}
		return OK, "every lane agent has an allow rule"
	}
}

func ruleList(agents []string) string {
	r := make([]string, len(agents))
	for i, a := range agents {
		r[i] = `"Agent(` + a + `)"`
	}
	return strings.Join(r, ", ")
}

// allowRules reads permissions.allow of a settings file and nothing else; a
// file that does not exist has none.
func allowRules(path string) ([]string, error) {
	b, err := readRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return s.Permissions.Allow, nil
}

// mainRoot is the main checkout of a worktree: Claude Code saves settings.local.json
// there (its permissions page). A plain checkout is its own root.
func mainRoot(dir string) string {
	b, err := readRegular(filepath.Join(dir, ".git"))
	if err != nil {
		return dir
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return dir
	}
	if i := strings.LastIndex(gitdir, string(filepath.Separator)+".git"+string(filepath.Separator)+"worktrees"+string(filepath.Separator)); i > 0 {
		return gitdir[:i]
	}
	return dir
}
