// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
)

// observeClaude classifies special targets without opening them. Only a real
// directory is traversed, and regular file reads are bounded independently of
// the owner's file size.
func observeClaude(ctx context.Context, root *os.Root, s *Snapshot, beforeOpen func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := root.Lstat(".claude")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return discoveryConflict()
	}
	s.Entries[".claude"] = Entry{Kind: "special", Mode: parent.Mode(), Info: parent}
	if !parent.IsDir() {
		return nil
	}
	if beforeOpen != nil {
		beforeOpen(".claude")
	}
	dir, err := root.OpenRoot(".claude")
	if err != nil {
		return discoveryConflict()
	}
	defer dir.Close()
	heldParent, err := dir.Stat(".")
	latestParent, e2 := root.Lstat(".claude")
	if err != nil || e2 != nil || !os.SameFile(parent, heldParent) || !os.SameFile(parent, latestParent) || !latestParent.IsDir() {
		return discoveryConflict()
	}
	s.Entries[".claude"] = Entry{Kind: "directory", Mode: heldParent.Mode(), Info: heldParent}
	if err := scanTemps(ctx, dir); err != nil {
		return err
	}
	named, err := dir.Lstat("settings.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return discoveryConflict()
	}
	if !named.Mode().IsRegular() {
		s.Entries[claudeSettingsPath] = Entry{Kind: "special", Mode: named.Mode(), Info: named}
		return nil
	}
	if beforeOpen != nil {
		beforeOpen(claudeSettingsPath)
	}
	file, err := openObservedFile(dir, "settings.json")
	if err != nil {
		return discoveryConflict()
	}
	defer file.Close()
	held, err := file.Stat()
	if err != nil || !held.Mode().IsRegular() || !os.SameFile(named, held) {
		return discoveryConflict()
	}
	// Inspect the descriptor size before reading; retain only a bounded prefix
	// for stale-write checks and the equality test against the authored default.
	limit := int64(claudeReadBound)
	if held.Size() < limit {
		limit = held.Size()
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	after, e1 := file.Stat()
	latest, e2 := dir.Lstat("settings.json")
	if err != nil || e1 != nil || e2 != nil || !os.SameFile(held, after) || !os.SameFile(held, latest) || held.Size() != after.Size() || held.Size() != latest.Size() || held.ModTime() != after.ModTime() || held.ModTime() != latest.ModTime() || held.Mode() != latest.Mode() {
		return discoveryConflict()
	}
	retained := data[:min(len(data), len(claudeSettings)+1)]
	s.Entries[claudeSettingsPath] = Entry{Kind: "file", Data: retained, Digest: digest(data), Mode: held.Mode(), Info: held}
	return nil
}
