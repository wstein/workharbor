// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// Apply writes exactly the retained proposal, or returns its dry-run preview.
// The caller supplies a context cancelled by its SIGINT/SIGTERM boundary.
// The final identity/hash check and rename cannot exclude a concurrent rename
// in the remaining window; this is the documented FR-057 race limitation.
func (p *Proposal) Apply(ctx context.Context) (Preview, error) { return p.apply(ctx, nil) }

type undo struct {
	name    string
	info    fs.FileInfo
	old     []byte
	mode    fs.FileMode
	restore bool
}
type transaction struct {
	root      *os.Root
	directory string
	expected  map[string]Fingerprint
	undo      []undo
	step      func(string) error
	counter   int
}

func conflict() error {
	return exitcode.Errorf(exitcode.PathConflict, "init target changed since preview or is unsafe")
}
func mutationError(name string, err error) error {
	code := exitcode.PathUnclassified
	if errors.Is(err, fs.ErrExist) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		code = exitcode.PathConflict
	}
	return exitcode.Errorf(code, "cannot create init target %s: %v", name, err)
}
func interrupted(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return exitcode.Errorf(exitcode.PathUserInterrupt, "init interrupted")
	}
	var coded *exitcode.Error
	if errors.As(err, &coded) {
		return err
	}
	return exitcode.Errorf(exitcode.PathUnclassified, "init filesystem operation failed: %v", err)
}
func (t *transaction) fault(event string) error {
	if t.step != nil {
		return t.step(event)
	}
	return nil
}
func (t *transaction) checkpoint(ctx context.Context, event string) error {
	if err := ctx.Err(); err != nil {
		return interrupted(err)
	}
	if t.step != nil {
		if err := t.step(event); err != nil {
			return interrupted(err)
		}
	}
	return interruptedContext(ctx)
}
func interruptedContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return interrupted(err)
	}
	return nil
}
func rootSpelling(s string) string {
	for len(s) > 1 {
		if os.IsPathSeparator(s[len(s)-1]) {
			s = s[:len(s)-1]
		} else if len(s) >= 2 && s[len(s)-1] == '.' && os.IsPathSeparator(s[len(s)-2]) {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}
func (t *transaction) rootCurrent() bool {
	info, err := os.Lstat(t.directory)
	return err == nil && info.IsDir() && info.Mode() == t.expected["."].Mode && os.SameFile(t.expected["."].Info, info)
}

// parent opens one component at a time, checking the named and held identities.
// No symlink in a target-relative parent is followed intentionally.
func (t *transaction) parent(name string) (*os.Root, error) {
	if !t.rootCurrent() {
		return nil, conflict()
	}
	r, err := t.root.OpenRoot(".")
	if err != nil {
		return nil, conflict()
	}
	dir := path.Dir(name)
	if dir == "." {
		return r, nil
	}
	prefix := ""
	for _, part := range strings.Split(dir, "/") {
		if prefix != "" {
			prefix += "/"
		}
		prefix += part
		expected, ok := t.expected[prefix]
		info, err := r.Lstat(part)
		if !ok || !expected.Exists || expected.Info == nil || err != nil || !info.IsDir() || !os.SameFile(expected.Info, info) || info.Mode() != expected.Mode {
			r.Close()
			return nil, conflict()
		}
		child, err := r.OpenRoot(part)
		if err != nil {
			r.Close()
			return nil, conflict()
		}
		held, e1 := child.Stat(".")
		named, e2 := r.Lstat(part)
		r.Close()
		if e1 != nil || e2 != nil || named.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, held) || !os.SameFile(info, named) {
			child.Close()
			return nil, conflict()
		}
		r = child
	}
	return r, nil
}
func readBound(root *os.Root, name string, limit int) ([]byte, fs.FileInfo, error) {
	named, err := root.Lstat(name)
	if err != nil || !named.Mode().IsRegular() || named.Size() > int64(limit) {
		return nil, nil, conflict()
	}
	f, err := openObservedFile(root, name)
	if err != nil {
		return nil, nil, conflict()
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil || !held.Mode().IsRegular() || !os.SameFile(named, held) {
		return nil, nil, conflict()
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, nil, err
	}
	after, e1 := f.Stat()
	latest, e2 := root.Lstat(name)
	if len(data) > limit || e1 != nil || e2 != nil || !os.SameFile(held, latest) || !os.SameFile(held, after) || held.Size() != after.Size() || held.ModTime() != after.ModTime() || latest.Mode() != held.Mode() {
		return nil, nil, conflict()
	}
	return data, held, nil
}
func (t *transaction) check(name string) error {
	expected := t.expected[name]
	if !expected.Exists {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if ancestor, known := t.expected[dir]; known && !ancestor.Exists {
				return t.check(dir)
			}
		}
	}
	parent, err := t.parent(name)
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(path.Base(name))
	if !expected.Exists {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return conflict()
	}
	if err != nil || expected.Info == nil || !os.SameFile(expected.Info, info) || expected.Mode != info.Mode() {
		return conflict()
	}
	if expected.Kind == "directory" {
		if !info.IsDir() {
			return conflict()
		}
		return nil
	}
	data, held, err := readBound(parent, path.Base(name), workspacecfg.MaxDevcontainerLen)
	if err != nil || !os.SameFile(expected.Info, held) || digest(data) != expected.SHA256 {
		return conflict()
	}
	return nil
}
func (t *transaction) checkAll() error {
	if !t.rootCurrent() {
		return conflict()
	}
	names := make([]string, 0, len(t.expected))
	for name := range t.expected {
		if name != "." {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := t.check(name); err != nil {
			return err
		}
	}
	return nil
}
func (t *transaction) remember(name, kind string, info fs.FileInfo, data []byte) {
	t.expected[name] = Fingerprint{Exists: true, Kind: kind, Info: info, Mode: info.Mode(), SHA256: digest(data)}
}
func (t *transaction) temp(parent *os.Root, data []byte, mode fs.FileMode) (string, fs.FileInfo, error) {
	t.counter++
	name := fmt.Sprintf(".whr-init-tmp-%08d", t.counter)
	f, err := createExclusive(parent, name, mode.Perm())
	if err != nil {
		return "", nil, mutationError(name, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return name, nil, err
	}
	err = t.fault("write-temp:" + name)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = t.fault("sync-temp:" + name)
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		var current fs.FileInfo
		current, err = f.Stat()
		if err == nil {
			info = current
		}
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return name, info, err
}
func verifyTemporary(parent *os.Root, name string, info fs.FileInfo, data []byte, mode fs.FileMode) error {
	current, held, err := readBound(parent, name, workspacecfg.MaxDevcontainerLen)
	if err != nil || info == nil || !os.SameFile(info, held) || held.Mode() != mode || digest(current) != digest(data) {
		return conflict()
	}
	return nil
}
func removeOwned(parent *os.Root, name string, info fs.FileInfo) error {
	current, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || info == nil || !os.SameFile(info, current) {
		return conflict()
	}
	return parent.Remove(name)
}
func (t *transaction) writeFile(name string, data []byte, overwrite bool) error {
	parent, err := t.parent(name)
	if err != nil {
		return err
	}
	defer parent.Close()
	leaf := path.Base(name)
	if !overwrite {
		f, err := createExclusive(parent, leaf, 0644)
		if err != nil {
			return mutationError(name, err)
		}
		info, statErr := f.Stat()
		t.undo = append(t.undo, undo{name: name, info: info})
		if statErr != nil {
			f.Close()
			return statErr
		}
		err = t.fault("write:" + name)
		if err == nil {
			_, err = f.Write(data)
		}
		if err == nil {
			err = t.fault("sync:" + name)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		t.remember(name, "file", info, data)
		return nil
	}
	old, info, err := readBound(parent, leaf, workspacecfg.MaxDevcontainerLen)
	if err != nil {
		return err
	}
	if !os.SameFile(t.expected[name].Info, info) || digest(old) != t.expected[name].SHA256 || info.Mode() != t.expected[name].Mode {
		return conflict()
	}
	temp, created, err := t.temp(parent, data, info.Mode())
	if temp != "" {
		t.undo = append(t.undo, undo{name: path.Join(path.Dir(name), temp), info: created})
	}
	if err != nil {
		return err
	}
	if t.step != nil {
		if err = t.step("rename:" + name); err != nil {
			return err
		}
	}
	if err = verifyTemporary(parent, temp, created, data, info.Mode()); err != nil {
		return err
	}
	// Re-open and re-check immediately before replacing the target name.
	if err = t.check(name); err != nil {
		return err
	}
	if err = parent.Rename(temp, leaf); err != nil {
		return err
	}
	t.undo = append(t.undo, undo{name: name, info: created, old: old, mode: info.Mode(), restore: true})
	t.remember(name, "file", created, data)
	return nil
}
func (t *transaction) makeDir(name string) error {
	parent, err := t.parent(name)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err = parent.Mkdir(path.Base(name), 0755); err != nil {
		return mutationError(name, err)
	}
	info, err := parent.Lstat(path.Base(name))
	t.undo = append(t.undo, undo{name: name, info: info})
	if err != nil {
		return err
	}
	t.remember(name, "directory", info, nil)
	return nil
}
func (t *transaction) rollback() []string {
	var remaining []string
	for i := len(t.undo) - 1; i >= 0; i-- {
		u := t.undo[i]
		if t.step != nil {
			if err := t.step("rollback:" + u.name); err != nil {
				remaining = append(remaining, u.name)
				continue
			}
		}
		parent, err := t.parent(u.name)
		if err != nil {
			remaining = append(remaining, u.name)
			continue
		}
		if !u.restore {
			err = removeOwned(parent, path.Base(u.name), u.info)
		} else {
			err = t.check(u.name)
			if err == nil {
				var temp string
				var info fs.FileInfo
				temp, info, err = t.temp(parent, u.old, u.mode)
				if err == nil {
					err = verifyTemporary(parent, temp, info, u.old, u.mode)
				}
				if err == nil {
					err = t.check(u.name)
				}
				if err == nil {
					err = parent.Rename(temp, path.Base(u.name))
				}
				if temp != "" {
					if cleanup := removeOwned(parent, temp, info); cleanup != nil {
						remaining = append(remaining, path.Join(path.Dir(u.name), temp))
					}
				}
			}
		}
		parent.Close()
		if err != nil {
			remaining = append(remaining, u.name)
		}
	}
	return remaining
}
func (t *transaction) validateDisk() (*workspacecfg.Report, error) {
	if err := t.checkAll(); err != nil {
		return nil, err
	}
	read := func(name string) ([]byte, error) {
		r, err := t.parent(name)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		b, _, err := readBound(r, path.Base(name), workspacecfg.MaxDevcontainerLen)
		return b, err
	}
	raw, err := read("workharbor.json")
	if err != nil {
		return nil, err
	}
	checked := workspacecfg.CheckManifest(raw)
	if checked.Manifest == nil || len(checked.Findings) != 0 {
		return nil, exitcode.Errorf(exitcode.PathUnclassified, "written manifest failed validation")
	}
	dc, err := read(checked.Manifest.DevcontainerPath)
	if err != nil {
		return nil, err
	}
	report := workspacecfg.ValidateMemory(t.directory, workspacecfg.MemoryInput{Manifest: raw, Devcontainer: dc, DevcontainerPresent: true, ProjectsPresent: true})
	if !report.Valid {
		return nil, exitcode.Errorf(exitcode.PathUnclassified, "written proposal failed Git-free validation")
	}
	return report, nil
}

// partialResult keeps recovery paths outside the bounded human error message.
func (p *Proposal) partialResult(remaining []string, message string) (Preview, error) {
	result := p.Preview()
	result.Remaining = append([]string(nil), remaining...)
	result.NextSteps = append([]string{"Inspect and recover every remaining path before retrying init."}, result.NextSteps...)
	err := exitcode.Errorf(exitcode.PathPartialFailure, "%s", message)
	err.Result = result
	return result, err
}
func (p *Proposal) apply(ctx context.Context, step func(string) error) (Preview, error) {
	result := p.Preview()
	if err := interruptedContext(ctx); err != nil {
		return result, err
	}
	// Apply requires real discovery identities, never an invented memory snapshot.
	if p.inputs["."].Info == nil {
		return result, conflict()
	}
	directory := rootSpelling(result.Directory)
	t := &transaction{directory: directory, expected: p.Fingerprints(), step: step}
	if !t.rootCurrent() {
		return result, conflict()
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return result, conflict()
	}
	t.root = root
	defer root.Close()
	held, err := root.Stat(".")
	if err != nil || !os.SameFile(t.expected["."].Info, held) || !t.rootCurrent() {
		return result, conflict()
	}
	if err = scanTemps(ctx, root); err != nil {
		return result, interrupted(err)
	}
	if err = t.checkAll(); err != nil {
		return result, err
	}
	if result.DryRun {
		return result, nil
	}
	changes := false
	for _, a := range result.Actions {
		changes = changes || a.Action == "create" || a.Action == "overwrite"
	}
	if !changes {
		report, err := t.validateDisk()
		result.Validation = report
		return result, err
	}
	// A persistent marker makes crashes fail closed on the next discovery. The
	// in-memory undo list retains old bytes; crash recovery is an explicit action.
	marker := ".whr-init-tmp-journal"
	journal, err := createExclusive(root, marker, 0600)
	if err != nil {
		return result, mutationError(marker, err)
	}
	markerInfo, err := journal.Stat()
	if err != nil {
		journal.Close()
		return p.partialResult([]string{marker}, fmt.Sprintf("inspect %s and recover init manually", marker))
	}
	fail := func(cause error) (Preview, error) {
		journal.Close()
		remaining := t.rollback()
		if len(remaining) == 0 {
			if err := removeOwned(root, marker, markerInfo); err != nil {
				remaining = append(remaining, marker)
			}
		} else {
			remaining = append(remaining, marker)
		}
		if len(remaining) > 0 {
			return p.partialResult(remaining, fmt.Sprintf("init rollback incomplete; inspect and recover these paths before retrying: %s", strings.Join(remaining, ", ")))
		}
		return p.Preview(), interrupted(cause)
	}
	err = t.fault("journal:write")
	if err == nil {
		_, err = journal.WriteString("whr init in progress; inspect recorded paths before recovery\n")
	}
	if err == nil {
		err = journal.Sync()
	}
	if err != nil {
		return fail(err)
	}
	for i, a := range result.Actions {
		if err = t.checkpoint(ctx, "before:"+a.Path); err != nil {
			return fail(err)
		}
		if err = t.checkAll(); err != nil {
			return fail(err)
		}
		switch a.Action {
		case "create":
			if a.Kind == "directory" {
				err = t.makeDir(a.Path)
			} else {
				err = t.writeFile(a.Path, p.files[a.Path], false)
			}
		case "overwrite":
			err = t.writeFile(a.Path, p.files[a.Path], true)
		}
		if err != nil {
			return fail(err)
		}
		if a.Action == "create" || a.Action == "overwrite" {
			result.Actions[i].Applied = true
			if _, err = fmt.Fprintln(journal, a.Path); err == nil {
				err = journal.Sync()
			}
			if err != nil {
				return fail(err)
			}
		}
		if err = t.checkpoint(ctx, "after:"+a.Path); err != nil {
			return fail(err)
		}
	}
	if err = t.checkpoint(ctx, "before:validate"); err != nil {
		return fail(err)
	}
	result.Validation, err = t.validateDisk()
	if err != nil {
		return fail(err)
	}
	if err = journal.Close(); err != nil {
		return fail(err)
	}
	if err = removeOwned(root, marker, markerInfo); err != nil {
		return fail(err)
	}
	for _, action := range result.Actions {
		if action.Applied && action.Action == "create" {
			result.Created = append(result.Created, action.Path)
		}
	}
	return result, nil
}
