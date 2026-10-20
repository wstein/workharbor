// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// Discover reads only current init targets, through held directory handles.
// It never reads startup files, Git metadata, credentials or unrelated content.
func Discover(ctx context.Context, directory string) (Snapshot, error) {
	return discover(ctx, directory, nil)
}
func discoveryConflict() error {
	return exitcode.Errorf(exitcode.PathConflict, "init target is unsafe, unreadable or changed while it was inspected")
}

func discover(ctx context.Context, directory string, beforeOpen func(string)) (Snapshot, error) {
	return discoverSelected(ctx, directory, beforeOpen, nil, false)
}

func discoverSelected(ctx context.Context, directory string, beforeOpen func(string), selected *string, noAgents bool) (Snapshot, error) {
	s := Snapshot{Directory: directory, Entries: map[string]Entry{}}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if !utf8.ValidString(directory) {
		return s, exitcode.Errorf(exitcode.PathBadArguments, "init directory must be UTF-8")
	}
	if directory == "" {
		directory = "."
		s.Directory = directory
	}
	// Remove only trailing separators and dot components so Lstat inspects the
	// selected leaf itself. Never clean dot-dot or resolve ancestor links.
	for len(directory) > 1 {
		if os.IsPathSeparator(directory[len(directory)-1]) {
			directory = directory[:len(directory)-1]
		} else if len(directory) >= 2 && directory[len(directory)-1] == '.' && os.IsPathSeparator(directory[len(directory)-2]) {
			directory = directory[:len(directory)-1]
		} else {
			break
		}
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return s, exitcode.Errorf(exitcode.PathTargetAbsent, "init directory does not exist")
	}
	if err != nil {
		return s, discoveryConflict()
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return s, discoveryConflict()
	}
	if !info.IsDir() {
		return s, exitcode.Errorf(exitcode.PathBadArguments, "init target is not a directory")
	}
	if beforeOpen != nil {
		beforeOpen(".")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return s, discoveryConflict()
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil {
		return s, discoveryConflict()
	}
	now, err := os.Lstat(directory)
	if err != nil || !now.IsDir() || now.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, opened) || !os.SameFile(info, now) {
		return s, discoveryConflict()
	}
	s.RootInfo = opened
	if err := scanTemps(ctx, root); err != nil {
		return s, err
	}
	if err := observe(ctx, root, "workharbor.json", "file", workspacecfg.MaxManifestBytes, &s, beforeOpen); err != nil {
		return s, err
	}
	dc, projects := defaultDevcontainer, "projects"
	if entry, ok := s.Entries["workharbor.json"]; ok {
		m := workspacecfg.CheckManifest(entry.Data)
		if m.Manifest == nil || len(m.Findings) != 0 {
			return s, exitcode.Errorf(exitcode.PathConflict, "existing workharbor.json fails current validation")
		}
		dc = m.Manifest.DevcontainerPath
		projects = m.Manifest.ProjectsPath
	}
	if selected != nil {
		dc = *selected
	}
	for _, target := range []struct {
		name, kind string
		limit      int
	}{{dc, "file", workspacecfg.MaxDevcontainerLen}, {projects, "directory", 0}, {projects + "/.gitkeep", "file", workspacecfg.MaxManifestBytes}} {
		if err := observe(ctx, root, target.name, target.kind, target.limit, &s, beforeOpen); err != nil {
			return s, err
		}
	}
	if !noAgents {
		if err := observeAgents(ctx, root, &s, beforeOpen); err != nil {
			return s, err
		}
	}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	// Reject observations whose names stopped referring to the captured objects.
	// Held handles prevent following a replaced ancestor while reading; this
	// final check also refuses a stale directory name before returning a preview.
	final, err := os.Lstat(directory)
	if err != nil || !final.IsDir() || !os.SameFile(s.RootInfo, final) {
		return s, discoveryConflict()
	}
	for name, entry := range s.Entries {
		current, err := root.Lstat(name)
		if err != nil || current.Mode() != entry.Mode || !os.SameFile(entry.Info, current) || current.Size() != entry.Info.Size() || !current.ModTime().Equal(entry.Info.ModTime()) {
			return s, discoveryConflict()
		}
	}
	return s, nil
}

func scanTemps(ctx context.Context, root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return discoveryConflict()
	}
	defer f.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := f.ReadDir(128)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".whr-init-tmp-") {
				return exitcode.Errorf(exitcode.PathConflict, "leftover .whr-init-tmp- file must be recovered before init")
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return discoveryConflict()
		}
	}
}

// observe walks one component at a time. Each directory is held, checked against
// lstat before/after opening, and closed after the leaf is captured. Root's
// containment and O_NOFOLLOW/nonblocking leaf access complement those checks.
func observe(ctx context.Context, root *os.Root, relative, want string, limit int, s *Snapshot, beforeOpen func(string)) error {
	if !relativePath(relative) {
		return discoveryConflict()
	}
	current := root
	var opened []*os.Root
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			opened[i].Close()
		}
	}()
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := current.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return discoveryConflict()
		}
		name := strings.Join(parts[:i+1], "/")
		directory := i < len(parts)-1 || want == "directory"
		if directory {
			if !info.IsDir() {
				return discoveryConflict()
			}
			if beforeOpen != nil {
				beforeOpen(name)
			}
			next, err := current.OpenRoot(part)
			if err != nil {
				return discoveryConflict()
			}
			opened = append(opened, next)
			actual, err := next.Stat(".")
			if err != nil {
				return discoveryConflict()
			}
			after, err := current.Lstat(part)
			if err != nil || !after.IsDir() || after.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, actual) || !os.SameFile(info, after) {
				return discoveryConflict()
			}
			s.Entries[name] = Entry{Kind: "directory", Mode: actual.Mode(), Info: actual}
			current = next
			if err := scanTemps(ctx, current); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > int64(limit) {
			return discoveryConflict()
		}
		if beforeOpen != nil {
			beforeOpen(name)
		}
		f, err := openObservedFile(current, part)
		if err != nil {
			return discoveryConflict()
		}
		actual, err := f.Stat()
		if err != nil || !actual.Mode().IsRegular() || actual.Size() > int64(limit) || !os.SameFile(info, actual) {
			f.Close()
			return discoveryConflict()
		}
		data, readErr := io.ReadAll(io.LimitReader(f, int64(limit)+1))
		afterRead, statErr := f.Stat()
		f.Close()
		after, err := current.Lstat(part)
		if readErr != nil || statErr != nil || err != nil || len(data) > limit || !after.Mode().IsRegular() || !os.SameFile(actual, after) || actual.Size() != afterRead.Size() || !actual.ModTime().Equal(afterRead.ModTime()) {
			return discoveryConflict()
		}
		s.Entries[name] = Entry{Kind: "file", Data: data, Mode: actual.Mode(), Info: actual}
	}
	return nil
}
