// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"sort"
	"strings"
)

// TreeEntry is one line of `ls-tree -r` or `ls-files --stage`.
type TreeEntry struct{ Mode, SHA, Path string }

// GitSnapshot is the Git state as the resolver sees it: the HEAD tree (absent
// for an unborn HEAD), the index and the .gitmodules blob of the source tree
// (committed when HEAD exists, staged otherwise).
type GitSnapshot struct {
	ProjectsPath string
	HeadExists   bool
	Head, Index  []TreeEntry
	Gitmodules   []byte
}

// GitInfo is what the analysis learned for callers.
type GitInfo struct{ gitlinks map[string]bool }

// HasGitlink reports whether path is a gitlink in the HEAD tree or the index.
func (g *GitInfo) HasGitlink(path string) bool { return g != nil && g.gitlinks[path] }

const (
	modeGitlink = "160000"
	modeRegular = "100644"
	modeExec    = "100755"
)

func isRegularMode(m string) bool { return m == modeRegular || m == modeExec }

func (s GitSnapshot) source() []TreeEntry {
	if s.HeadExists {
		return s.Head
	}
	return s.Index
}

func gitlinks(es []TreeEntry) map[string]string {
	m := map[string]string{}
	for _, e := range es {
		if e.Mode == modeGitlink {
			m[e.Path] = e.SHA
		}
	}
	return m
}

func find1(es []TreeEntry, path string) (TreeEntry, bool) {
	for _, e := range es {
		if e.Path == path {
			return e, true
		}
	}
	return TreeEntry{}, false
}

// treePathBad reports a path whose components are empty, . or .., or a .git
// in any case.
func treePathBad(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || isDotGit(part) {
			return true
		}
	}
	return false
}

// AnalyzeGit applies the Git-derived checks of FR-054 to a snapshot without
// touching Git or the file system: tree paths, projects files, the committed
// .gitmodules blob and every submodule rule. A gitlink counts as pinned when
// the source tree (HEAD, or the index with an unborn HEAD) holds it.
func AnalyzeGit(s GitSnapshot) ([]Finding, *GitInfo) {
	var c collector
	src := s.source()
	if !s.HeadExists {
		c.add("git.head_missing", "", "the repository has no commit yet; submodules are read from the index", "")
	}
	for _, e := range src {
		if treePathBad(e.Path) {
			c.add("git.tree_path_invalid", e.Path, "a tree path has an empty, . , .. or .git component", "")
		}
	}
	info := &GitInfo{gitlinks: map[string]bool{}}
	for p := range gitlinks(s.Head) {
		info.gitlinks[p] = true
	}
	for p := range gitlinks(s.Index) {
		info.gitlinks[p] = true
	}

	if s.ProjectsPath != "" {
		prefix := s.ProjectsPath + "/"
		for _, e := range src {
			if isRegularMode(e.Mode) && strings.HasPrefix(e.Path, prefix) && e.Path != prefix+".gitkeep" {
				c.add("projects.not_a_submodule", e.Path, e.Path+" is a committed file below the projects directory, not a submodule", "")
			}
		}
	}

	srcLinks := gitlinks(src)
	gm, hasGM := find1(src, ".gitmodules")
	var entries []smEntry
	usable := true
	if hasGM {
		switch {
		case !isRegularMode(gm.Mode):
			c.add("gitmodules.unsafe", ".gitmodules", ".gitmodules is not a regular file in the repository", "")
			usable = false
		case s.Gitmodules == nil:
			usable = false
		default:
			res := parseGitmodules(s.Gitmodules, &c)
			usable = res.usable
			entries = res.entries
			if usable {
				checkSubmoduleKeys(&c, res)
			}
		}
	}
	if s.HeadExists {
		idx, inIdx := find1(s.Index, ".gitmodules")
		if inIdx != hasGM || (inIdx && idx.SHA != gm.SHA) {
			c.add("gitmodules.uncommitted_change", ".gitmodules", "the staged .gitmodules differs from the committed one", "")
		}
	}
	if usable {
		checkSubmodules(&c, s, entries, srcLinks)
	}
	if s.HeadExists {
		headLinks, idxLinks := gitlinks(s.Head), gitlinks(s.Index)
		for p, sha := range idxLinks {
			if h, ok := headLinks[p]; !ok || h != sha {
				c.add("submodule.not_committed", p, "the staged gitlink at "+echo(p)+" differs from HEAD", "")
			}
		}
		for p := range headLinks {
			if _, ok := idxLinks[p]; !ok {
				c.add("submodule.not_committed", p, "the gitlink at "+echo(p)+" is removed in the index but present in HEAD", "")
			}
		}
	}
	return c.list, info
}

func checkSubmoduleKeys(c *collector, res gitmodulesResult) {
	for _, x := range res.extra {
		if x.key == "update" && strings.HasPrefix(x.value, "!") {
			c.add("submodule.update_command", "", "submodule "+quoteKey(x.section)+" has an update command", "")
			continue
		}
		c.add("gitmodules.unsupported_key", "", "submodule "+quoteKey(x.section)+" has the key "+quoteKey(x.key)+", which the resolver rejects", "")
	}
}

func checkSubmodules(c *collector, s GitSnapshot, entries []smEntry, links map[string]string) {
	listed := map[string]bool{}
	paths := map[string]int{}
	for _, e := range entries {
		listed[e.path] = true
		paths[e.path]++
		switch submoduleURLCode(e.url) {
		case "submodule.url_credentials":
			c.add("submodule.url_credentials", e.path, "submodule "+quoteKey(e.name)+" has credentials in its URL", "")
		case "submodule.url_invalid":
			c.add("submodule.url_invalid", e.path, "submodule "+quoteKey(e.name)+" has a URL that is not https://, ssh:// or user@host:path", "")
		}
		if why := canonicalPathProblem(e.path); why != "" {
			c.add("submodule.outside_projects", e.path, "submodule "+quoteKey(e.name)+" has a path that is not canonical", "")
		} else if s.ProjectsPath != "" && !strings.HasPrefix(e.path, s.ProjectsPath+"/") {
			c.add("submodule.outside_projects", e.path, "submodule "+quoteKey(e.name)+" is not below the projects directory", "")
		}
		if sha, ok := links[e.path]; ok {
			c.add("submodule.pinned", e.path, "pinned at "+sha, "")
		} else {
			c.add("submodule.not_pinned", e.path, "submodule "+quoteKey(e.name)+" has no pinned gitlink", "")
		}
	}
	for p := range links {
		if !listed[p] {
			c.add("submodule.unlisted", p, "the gitlink at "+echo(p)+" has no .gitmodules entry", "")
		}
	}
	// Nested or overlapping: a path whose ancestor (or itself, repeated) is another submodule path.
	all := map[string]bool{}
	for p := range paths {
		all[p] = true
	}
	for p := range links {
		all[p] = true
	}
	var sorted []string
	for p := range all {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	for _, p := range sorted {
		nested := paths[p] > 1
		for i := strings.LastIndex(p, "/"); i > 0 && !nested; i = strings.LastIndex(p[:i], "/") {
			nested = all[p[:i]]
		}
		if nested {
			c.add("submodule.nested", p, "the submodule path "+echo(p)+" is nested in or overlaps another submodule", "")
		}
	}
}
