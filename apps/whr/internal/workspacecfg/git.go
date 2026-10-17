// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// gitUnsafe lstat-checks the .git directory before any Git call and returns the
// reason it must not be handed to Git, or "". missing is true when there is no
// .git at all.
func (r *run) gitUnsafe() (reason string, missing bool) {
	info, aerr := r.d.lstat(".git")
	switch {
	case aerr != nil && aerr.kind == accMissing:
		return "", true
	case aerr != nil:
		return ".git " + aerr.why, false
	case info.Mode()&fs.ModeSymlink != 0:
		return ".git is a symbolic link", false
	case !info.IsDir():
		if info.Mode().IsRegular() {
			return ".git is a file (gitfile), not a directory", false
		}
		return ".git is " + kindName(info.Mode()), false
	}
	// Directories Git descends into must be real directories: a link there would
	// make Git read refs or objects from outside the validated directory.
	if reason := r.gitLinks(); reason != "" {
		return reason, false
	}
	for _, rel := range []string{".git/commondir", ".git/objects/info/alternates"} {
		if _, aerr := r.d.lstat(rel); aerr == nil {
			return rel[len(".git/"):] + " is present, which redirects Git to other storage", false
		} else if aerr.kind != accMissing {
			return rel + " " + aerr.why, false
		}
	}
	if info, aerr := r.d.lstat(".git/packed-refs"); aerr == nil && info.Mode()&fs.ModeSymlink != 0 {
		return ".git/packed-refs is a symbolic link", false
	}
	// A reftable repository keeps no loose reference files: .git/refs/heads is a
	// placeholder file and the HEAD it names is only a marker.
	if ref := r.headRef(); ref != "" && !r.reftable {
		if info, aerr := r.d.lstat(".git/" + ref); aerr != nil && aerr.kind != accMissing {
			return ".git/" + ref + " " + aerr.why, false
		} else if aerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return ".git/" + ref + " is a symbolic link", false
		}
	}
	for _, f := range []struct {
		rel      string
		optional bool
	}{{".git/HEAD", false}, {".git/index", true}, {".git/config", false}} {
		info, aerr := r.d.lstat(f.rel)
		switch {
		case aerr != nil && aerr.kind == accMissing && f.optional:
		case aerr != nil && aerr.kind == accMissing:
			return f.rel + " does not exist", false
		case aerr != nil:
			return f.rel + " " + aerr.why, false
		case !info.Mode().IsRegular():
			return f.rel + " is " + kindName(info.Mode()) + ", not a regular file", false
		}
	}
	return "", false
}

// unsafeDirMsg is the reason when the path handed to Git no longer names the opened directory.
const unsafeDirMsg = "the directory path no longer names the opened directory"

// Bounds of the .git link walk: a hostile repository cannot make it unbounded.
const maxGitWalkDepth = 32

// maxGitWalkEntries is a variable only so that a test can lower it.
var maxGitWalkEntries = 100000

// gitLinks refuses any symbolic link in the parts of .git that Git reads
// through directory entries: the first level of .git/objects (pack, info and
// the fan-out directories), the files inside pack and the fan-out directories,
// all of .git/refs and .git/reftable. Files there must be regular files. It also
// records whether the repository uses the reftable format.
func (r *run) gitLinks() string {
	budget := maxGitWalkEntries
	// list reads at most the remaining budget (plus one, to see the overflow) so that a
	// hostile directory is never read whole.
	list := func(rel string) ([]fs.DirEntry, string) {
		entries, aerr := r.d.readDir(rel, budget)
		if aerr != nil {
			if aerr.kind == accMissing {
				return nil, ""
			}
			return nil, rel + " " + aerr.why
		}
		if len(entries) > budget {
			hint := "run git gc to pack loose objects"
			if !strings.HasPrefix(rel, ".git/objects") {
				hint = "run git pack-refs --all"
			}
			return nil, fmt.Sprintf("%s has more entries than the %d that can be inspected; %s", rel, maxGitWalkEntries, hint)
		}
		budget -= len(entries)
		return entries, ""
	}
	var walkObjects func(rel string, nested bool) string
	walkObjects = func(rel string, nested bool) string {
		entries, reason := list(rel)
		if reason != "" {
			return reason
		}
		for _, e := range entries {
			child := rel + "/" + e.Name()
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				return child + " is a symbolic link"
			case !nested && e.IsDir() && e.Name() == "multi-pack-index.d" && strings.HasSuffix(rel, "/pack"):
				// The incremental multi-pack-index chain: files only.
				if reason := walkObjects(child, true); reason != "" {
					return reason
				}
			case !e.Type().IsRegular():
				return child + " is not a regular file"
			}
		}
		return ""
	}
	var walk func(rel string, depth int) string
	walk = func(rel string, depth int) string {
		entries, reason := list(rel)
		if reason != "" {
			return reason
		}
		for _, e := range entries {
			child := rel + "/" + e.Name()
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				return child + " is a symbolic link"
			case rel == ".git/objects" && e.IsDir() && (e.Name() == "pack" || isFanout(e.Name())):
				// Pack files and loose objects are read by Git: they must be regular files.
				if reason := walkObjects(child, false); reason != "" {
					return reason
				}
			case e.IsDir() && depth < maxGitWalkDepth && !strings.HasPrefix(rel, ".git/objects"):
				if reason := walk(child, depth+1); reason != "" {
					return reason
				}
			case e.IsDir() && strings.HasPrefix(rel, ".git/refs") && depth >= maxGitWalkDepth:
				return rel + " is nested deeper than can be inspected"
			}
		}
		return ""
	}
	for _, rel := range []string{".git/objects", ".git/refs", ".git/reftable"} {
		if reason := walk(rel, 1); reason != "" {
			return reason
		}
	}
	if info, aerr := r.d.lstat(".git/reftable"); aerr == nil && info.IsDir() {
		r.reftable = true
	} else if r.configNamesReftable() {
		r.reftable = true
	}
	return ""
}

// isFanout reports a loose-object directory name: two hexadecimal digits.
func isFanout(name string) bool {
	return len(name) == 2 && hexVal(name[0]) >= 0 && hexVal(name[1]) >= 0
}

// configNamesReftable reports extensions.refStorage = reftable in .git/config.
func (r *run) configNamesReftable() bool {
	raw, aerr := r.d.readFile(".git/config", MaxListingBytes)
	if aerr != nil {
		return false
	}
	inExtensions := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			name, _, _ := strings.Cut(strings.TrimPrefix(line, "["), "]")
			inExtensions = strings.EqualFold(strings.TrimSpace(name), "extensions")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if inExtensions && ok && strings.EqualFold(strings.TrimSpace(key), "refstorage") &&
			strings.EqualFold(strings.Trim(strings.TrimSpace(value), `"`), "reftable") {
			return true
		}
	}
	return false
}

// headRef returns the reference .git/HEAD names ("refs/heads/main"), or "" when
// HEAD is detached, unreadable or names something that is not a plain ref path.
func (r *run) headRef() string {
	raw, aerr := r.d.readFile(".git/HEAD", 4096)
	if aerr != nil {
		return ""
	}
	name, ok := strings.CutPrefix(strings.TrimRight(string(raw), "\r\n"), "ref: ")
	if !ok || !validRefName(name) {
		return ""
	}
	return name
}

// validRefName accepts refs/... names made of plain components, so that the
// name is safe to join onto .git/.
func validRefName(name string) bool {
	if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, "\\\x00\r\n\t ") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// gitFailure aborts the Git stage with a finding.
type gitFailure struct{ code, msg string }

func (r *run) git() {
	if r.opt.GitFree {
		return
	}
	reason, missing := r.gitUnsafe()
	if missing {
		r.c.add("git.not_repository", "", "the directory contains no .git; Git checks did not run", "")
		return
	}
	r.gitRan = true
	if reason == "" && !r.d.stillAtAbs() {
		reason = unsafeDirMsg
	}
	if reason != "" {
		r.c.add("git.unsafe_path", ".git", reason+"; Git did not run", "")
		return
	}
	before := len(r.c.list)
	f := r.gitStage()
	if f == nil && !r.d.stillAtAbs() {
		// The directory moved while Git ran: nothing Git printed can be trusted.
		r.c.list = r.c.list[:before]
		r.gitState = nil
		r.c.add("git.unsafe_path", ".git", unsafeDirMsg+"; Git results were discarded", "")
		return
	}
	if f != nil {
		r.gitState = nil
		if f.code != "" {
			r.c.add(f.code, "", f.msg, "")
		}
	}
}

// gitStage runs the fixed Git invocations and the analysis. It returns nil when
// the Git state was read, otherwise the failure to report.
func (r *run) gitStage() *gitFailure {
	look := r.opt.LookGit
	if look == nil {
		look = lookGit
	}
	path, err := look()
	if err != nil {
		return &gitFailure{"git.unavailable", "Git is not available; the Git-derived checks did not run"}
	}
	runner := r.opt.Git
	if runner == nil {
		runner = ExecRunner{}
	}
	g := &gitSession{r: r, runner: runner, path: path, gitDir: r.d.abs + "/.git", workTree: r.d.abs}

	out, f := g.call(MaxListingBytes, "--version")
	if f != nil {
		return f
	}
	if out.Exit != 0 || !gitVersionOK(out.Stdout) {
		return &gitFailure{"git.unavailable", "Git is older than 2.39.5 or its version is unreadable; the Git-derived checks did not run"}
	}

	head, f := g.headExists()
	if f != nil {
		return f
	}
	index, f := g.listing("ls-files", "--stage", "-z")
	if f != nil {
		return f
	}
	snap := GitSnapshot{ProjectsPath: "", HeadExists: head, Index: index}
	if r.m != nil {
		snap.ProjectsPath = r.m.ProjectsPath
	}
	if head {
		if snap.Head, f = g.listing("ls-tree", "-r", "-z", "HEAD"); f != nil {
			return f
		}
	}
	src, spec := snap.Index, ":.gitmodules"
	if head {
		src, spec = snap.Head, "HEAD:.gitmodules"
	}
	if e, ok := find1(src, ".gitmodules"); ok && isRegularMode(e.Mode) {
		out, f := g.call(MaxGitmodulesBytes+1, "cat-file", "blob", spec)
		if f != nil {
			return f
		}
		switch {
		case out.Truncated:
			snap.Gitmodules = make([]byte, MaxGitmodulesBytes+1)
		case out.Exit != 0:
			return &gitFailure{"git.listing_failed", "git cat-file failed on .gitmodules"}
		default:
			snap.Gitmodules = out.Stdout
			if snap.Gitmodules == nil {
				snap.Gitmodules = []byte{}
			}
		}
	}
	findings, info := AnalyzeGit(snap)
	r.c.addAll(findings)
	r.gitState = info
	return nil
}

// gitSession carries one run's Git settings.
type gitSession struct {
	r        *run
	runner   GitRunner
	path     string
	gitDir   string
	workTree string
}

// call runs one Git invocation within the remaining budget.
func (g *gitSession) call(maxOut int, sub ...string) (GitOut, *gitFailure) {
	r := g.r
	if err := r.ctx.Err(); err != nil {
		return GitOut{}, &gitFailure{} // Validate returns the context error
	}
	remaining := r.b.remaining()
	if remaining <= 0 {
		return GitOut{}, &gitFailure{"git.listing_failed", "the shared time budget ran out during Git work"}
	}
	args := sub
	if len(sub) > 0 && sub[0] != "--version" {
		args = gitArgs(g.gitDir, g.workTree, sub...)
	}
	// The absolute path in args must still name the held directory. This narrows the
	// window in which the path could name another directory to one Git call; a swap
	// to another directory and back inside a single call is not detectable (Git is
	// handed a path, not the open descriptor, because os/exec cannot pass a Go root
	// as a /proc/self/fd path portably).
	if !r.d.stillAtAbs() {
		return GitOut{}, &gitFailure{"git.unsafe_path", unsafeDirMsg}
	}
	out, err := g.runner.Run(r.ctx, GitCmd{
		Path: g.path, Args: args, Env: gitEnv(), Dir: "/", MaxOutput: maxOut, Timeout: remaining,
	})
	if cerr := r.ctx.Err(); cerr != nil {
		return GitOut{}, &gitFailure{}
	}
	if !r.d.stillAtAbs() {
		return GitOut{}, &gitFailure{"git.unsafe_path", unsafeDirMsg}
	}
	if err != nil {
		msg := "git " + subName(sub) + " failed"
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "git " + subName(sub) + " ran out of time"
		}
		return GitOut{}, &gitFailure{"git.listing_failed", msg}
	}
	return out, nil
}

func subName(sub []string) string {
	if len(sub) == 0 {
		return ""
	}
	return strings.TrimPrefix(sub[0], "--")
}

// headExists decides between a HEAD and an unborn HEAD (FR-054): unborn exactly
// when rev-parse exits 1 with empty standard error and symbolic-ref names a
// reference. Everything else that is not success is a listing failure.
func (g *gitSession) headExists() (bool, *gitFailure) {
	out, f := g.call(512, "rev-parse", "--verify", "--quiet", "HEAD")
	if f != nil {
		return false, f
	}
	switch {
	case out.Exit == 0:
		if !isObjectID(strings.TrimSpace(string(out.Stdout))) {
			return false, &gitFailure{"git.listing_failed", "git rev-parse printed an unexpected value"}
		}
		return true, nil
	case out.Exit == 1 && len(bytes.TrimSpace(out.Stderr)) == 0:
	default:
		return false, &gitFailure{"git.listing_failed", "git rev-parse failed"}
	}
	out, f = g.call(512, "symbolic-ref", "-q", "HEAD")
	if f != nil {
		return false, f
	}
	ref := strings.TrimSpace(string(out.Stdout))
	if out.Exit != 0 || !validRefName(ref) {
		return false, &gitFailure{"git.listing_failed", "HEAD is neither a commit nor an unborn branch"}
	}
	if g.r.reftable {
		return false, nil // the reference store is not made of files; Git has decided
	}
	// Unborn means the named reference is absent, loose and packed.
	if _, aerr := g.r.d.lstat(".git/" + ref); aerr == nil || aerr.kind != accMissing {
		return false, &gitFailure{"git.listing_failed", "HEAD names a reference that exists but cannot be resolved"}
	}
	if packed, aerr := g.r.d.readFile(".git/packed-refs", MaxListingBytes); aerr == nil {
		for _, line := range strings.Split(string(packed), "\n") {
			if _, name, ok := strings.Cut(line, " "); ok && name == ref {
				return false, &gitFailure{"git.listing_failed", "HEAD names a packed reference that cannot be resolved"}
			}
		}
	} else if aerr.kind != accMissing {
		return false, &gitFailure{"git.listing_failed", "packed-refs cannot be read"}
	}
	return false, nil
}

func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if hexVal(s[i]) < 0 || s[i] >= 'A' && s[i] <= 'F' {
			return false
		}
	}
	return true
}

// listing runs ls-files --stage or ls-tree and parses NUL-terminated records.
func (g *gitSession) listing(sub ...string) ([]TreeEntry, *gitFailure) {
	out, f := g.call(MaxListingBytes, sub...)
	if f != nil {
		return nil, f
	}
	if out.Truncated {
		return nil, &gitFailure{"git.limit_exceeded", "the git " + sub[0] + " listing is beyond its size bound"}
	}
	if out.Exit != 0 {
		return nil, &gitFailure{"git.listing_failed", "git " + sub[0] + " failed"}
	}
	var entries []TreeEntry
	data := out.Stdout
	for len(data) > 0 {
		i := bytes.IndexByte(data, 0)
		if i < 0 {
			return nil, &gitFailure{"git.listing_failed", "git " + sub[0] + " printed an unterminated record"}
		}
		rec := string(data[:i])
		data = data[i+1:]
		e, ok := parseListingRecord(sub[0], rec)
		if !ok && sub[0] == "ls-files" && isUnmergedRecord(rec) {
			return nil, &gitFailure{"git.listing_failed", "the index has unmerged entries; finish or abort the merge"}
		}
		if !ok {
			return nil, &gitFailure{"git.listing_failed", "git " + sub[0] + " printed an unparsable record"}
		}
		if len(entries) >= MaxListingEntries {
			return nil, &gitFailure{"git.limit_exceeded", "the git " + sub[0] + " listing has more entries than its bound"}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseListingRecord parses "MODE SHA STAGE\tPATH" (ls-files --stage) or
// "MODE TYPE SHA\tPATH" (ls-tree). Unmerged index entries are refused.
func parseListingRecord(kind, rec string) (TreeEntry, bool) {
	meta, path, ok := strings.Cut(rec, "\t")
	if !ok || path == "" {
		return TreeEntry{}, false
	}
	f := strings.Split(meta, " ")
	if len(f) != 3 || len(f[0]) != 6 {
		return TreeEntry{}, false
	}
	var sha string
	if kind == "ls-files" {
		if f[2] != "0" {
			return TreeEntry{}, false
		}
		sha = f[1]
	} else {
		if f[1] != "blob" && f[1] != "commit" && f[1] != "tree" {
			return TreeEntry{}, false
		}
		sha = f[2]
	}
	if !isObjectID(sha) {
		return TreeEntry{}, false
	}
	return TreeEntry{Mode: f[0], SHA: sha, Path: path}, true
}

// isUnmergedRecord recognises an ls-files --stage record with a stage above 0.
func isUnmergedRecord(rec string) bool {
	meta, _, ok := strings.Cut(rec, "\t")
	f := strings.Split(meta, " ")
	return ok && len(f) == 3 && (f[2] == "1" || f[2] == "2" || f[2] == "3")
}
