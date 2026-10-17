// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultBudget is the shared wall-clock budget of one validation run (FR-055).
const DefaultBudget = 5 * time.Second

// Options configure a validation run. The zero value is the production setup.
type Options struct {
	// GitFree selects the Git-free profile: manifest, devcontainer and projects
	// checks only, no Git command and no git.* finding (git_checks: not_run).
	GitFree bool
	// Clock replaces time.Now for the budget (tests); Budget replaces DefaultBudget.
	Clock  func() time.Time
	Budget time.Duration
	// Git replaces the Git runner (tests); LookGit replaces the Git lookup.
	Git     GitRunner
	LookGit func() (string, error)
}

type budget struct {
	now   func() time.Time
	start time.Time
	total time.Duration
}

func newBudget(o Options) *budget {
	b := &budget{now: o.Clock, total: o.Budget}
	if b.now == nil {
		b.now = time.Now
	}
	if b.total <= 0 {
		b.total = DefaultBudget
	}
	b.start = b.now()
	return b
}

func (b *budget) remaining() time.Duration { return b.total - b.now().Sub(b.start) }
func (b *budget) expired() bool            { return b.remaining() <= 0 }

// run is the state of one on-disk validation.
type run struct {
	ctx context.Context
	opt Options
	b   *budget
	d   *dir
	c   collector
	m   *Manifest
	// gitkeeps records what the Git stage learned for the project declaration.
	gitState *GitInfo
	gitRan   bool
}

// Validate checks the configuration directory dir. It reads the directory
// through a no-follow walk, never writes and never follows or executes anything
// in it. The error is ErrNotFound or ErrNotDirectory for the directory itself,
// the context error when ctx ends, and otherwise nil: every defect of the
// content is a finding of the report.
func Validate(ctx context.Context, dirPath string, opt Options) (*Report, error) {
	r := &run{ctx: ctx, opt: opt, b: newBudget(opt)}
	d, err := openDir(dirPath)
	if err != nil {
		var ae *accessError
		if errors.As(err, &ae) {
			r.c.add("manifest.unsafe_path", "", "the directory "+ae.why, "pass the real directory, not a link")
			return newReport(dirPath, r.c.list), nil
		}
		return nil, err
	}
	defer d.Close()
	r.d = d
	for _, step := range []func(){r.manifest, r.devcontainer, r.projects, r.git, r.declaration} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.b.expired() {
			r.c.add("findings.time_budget_exhausted", "", "the shared time budget ran out; later checks did not run", "")
			break
		}
		step()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newReport(dirPath, r.c.list), nil
}

// fileFinding turns an access error of a file into the finding of area.
func (r *run) fileFinding(area, rel string, e *accessError, missingOK bool) {
	switch e.kind {
	case accMissing:
		r.c.add(area+".missing", rel, rel+" does not exist", "")
	case accTooLarge:
		r.c.add(area+".too_large", rel, rel+" is larger than its limit", "")
	default:
		r.c.add(area+".unsafe_path", rel, fmt.Sprintf("%s %s", rel, e.why), "")
	}
}

func (r *run) manifest() {
	raw, aerr := r.d.readFile(ManifestFile, MaxManifestBytes)
	if aerr != nil {
		r.fileFinding("manifest", ManifestFile, aerr, false)
		return
	}
	res := CheckManifest(raw)
	r.c.addAll(res.Findings)
	r.m = res.Manifest
}

func (r *run) devcontainer() {
	if r.m == nil || r.m.DevcontainerPath == "" {
		return
	}
	rel := r.m.DevcontainerPath
	raw, aerr := r.d.readFile(rel, MaxDevcontainerLen)
	if aerr != nil {
		r.fileFinding("devcontainer", rel, aerr, false)
		return
	}
	for _, f := range CheckDevcontainer(raw) {
		f.Subject = rel
		r.c.list = append(r.c.list, f)
	}
}

func (r *run) projects() {
	if r.m == nil || r.m.ProjectsPath == "" {
		return
	}
	rel := r.m.ProjectsPath
	info, aerr := r.d.lstat(rel)
	switch {
	case aerr != nil && aerr.kind == accMissing:
		r.c.add("projects.missing", rel, rel+" does not exist", "")
	case aerr != nil:
		r.c.add("projects.unsafe_path", rel, rel+" "+aerr.why, "")
	case info.IsDir():
	case info.Mode().IsRegular():
		r.c.add("projects.missing", rel, rel+" is not a directory", "")
	default:
		r.c.add("projects.unsafe_path", rel, fmt.Sprintf("%s is %s", rel, kindName(info.Mode())), "")
	}
}

// declaration reports a project declaration of the manifest that no pinned
// gitlink backs (FR-049). Without Git state (the Git-free profile) nothing can
// be pinned; when Git state exists the gitlink must be in the HEAD tree or index.
func (r *run) declaration() {
	if r.m == nil || r.m.Project == nil {
		return
	}
	if r.gitRan && r.gitState == nil {
		return // Git failed; its own errors say why
	}
	if r.gitState != nil && r.gitState.HasGitlink(r.m.Project.Path) {
		return
	}
	declaredNotPinned(&r.c, r.m.Project.Path)
}

func declaredNotPinned(c *collector, path string) {
	c.add("project.declared_not_pinned", path, "the manifest declares a project at "+echo(path)+" that is not a pinned gitlink; the workspace is not creation-ready", "")
}

// MemoryInput is the content of a configuration for the in-memory checker.
type MemoryInput struct {
	Manifest            []byte
	Devcontainer        []byte
	DevcontainerPresent bool
	ProjectsPresent     bool
}

// ValidateMemory is the Git-free checker over content held in memory: the
// manifest, the devcontainer file and the presence of the projects directory.
// It touches no file and runs no Git, and yields the same report as Validate
// with GitFree on a directory holding that content. `init` uses it before it
// writes and Validate with GitFree after.
func ValidateMemory(directory string, in MemoryInput) *Report {
	var c collector
	res := CheckManifest(in.Manifest)
	c.addAll(res.Findings)
	m := res.Manifest
	if m != nil && m.DevcontainerPath != "" {
		if !in.DevcontainerPresent {
			c.add("devcontainer.missing", m.DevcontainerPath, m.DevcontainerPath+" does not exist", "")
		} else {
			for _, f := range CheckDevcontainer(in.Devcontainer) {
				f.Subject = m.DevcontainerPath
				c.list = append(c.list, f)
			}
		}
	}
	if m != nil && m.ProjectsPath != "" && !in.ProjectsPresent {
		c.add("projects.missing", m.ProjectsPath, m.ProjectsPath+" does not exist", "")
	}
	if m != nil && m.Project != nil {
		declaredNotPinned(&c, m.Project.Path)
	}
	return newReport(directory, c.list)
}
