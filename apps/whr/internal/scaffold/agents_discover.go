// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
)

// observeAgents bounds reads on the held descriptor. Oversized regular files
// retain their size for classification without reading any of their contents.
func observeAgents(ctx context.Context, root *os.Root, s *Snapshot, beforeOpen func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	named, err := root.Lstat("AGENTS.md")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !named.Mode().IsRegular() {
		return discoveryConflict()
	}
	if beforeOpen != nil {
		beforeOpen("AGENTS.md")
	}
	file, err := openObservedFile(root, "AGENTS.md")
	if err != nil {
		return discoveryConflict()
	}
	defer file.Close()
	held, err := file.Stat()
	if err != nil || !held.Mode().IsRegular() || !os.SameFile(named, held) {
		return discoveryConflict()
	}
	var data []byte
	if held.Size() <= agentsBlockMaxBytes {
		data, err = io.ReadAll(io.LimitReader(file, agentsBlockMaxBytes+1))
		if err != nil || len(data) > agentsBlockMaxBytes {
			return discoveryConflict()
		}
	}
	after, e1 := file.Stat()
	latest, e2 := root.Lstat("AGENTS.md")
	if e1 != nil || e2 != nil || !os.SameFile(held, after) || !os.SameFile(held, latest) || held.Size() != after.Size() || held.Size() != latest.Size() || held.ModTime() != after.ModTime() || held.ModTime() != latest.ModTime() || held.Mode() != latest.Mode() {
		return discoveryConflict()
	}
	s.Entries["AGENTS.md"] = Entry{Kind: "file", Data: data, Mode: held.Mode(), Info: held}
	return nil
}
