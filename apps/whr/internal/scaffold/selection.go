// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"os"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// DiscoverWithOptions observes the explicitly selected source instead of the
// default devcontainer. It never requires an unused source to exist or be valid.
func DiscoverWithOptions(ctx context.Context, directory string, options Options) (Snapshot, error) {
	if err := options.validate(); err != nil {
		return Snapshot{}, err
	}
	return discoverSelected(ctx, directory, nil, options.Devcontainer)
}

// SelectDevcontainer extends a wizard snapshot with one bounded, no-follow
// observation. Existing observations remain frozen for stale-preview checks.
// The root must still be the original directory; no earlier inputs are refreshed.
func (s Snapshot) SelectDevcontainer(ctx context.Context, selected string) (Snapshot, error) {
	if !relativePath(selected) {
		return s, exitcode.Errorf(exitcode.PathBadArguments, "--devcontainer needs a safe relative file path")
	}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if _, ok := s.Entries[selected]; ok {
		return s, nil
	}
	if s.RootInfo == nil {
		return s, nil
	} // Pure in-memory callers supply their entries.
	info, err := os.Lstat(s.Directory)
	if err != nil || !info.IsDir() || !os.SameFile(s.RootInfo, info) {
		return s, discoveryConflict()
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return s, discoveryConflict()
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(s.RootInfo, opened) {
		return s, discoveryConflict()
	}
	out := snapshotCopy(s)
	if err := observe(ctx, root, selected, "file", workspacecfg.MaxDevcontainerLen, &out, nil); err != nil {
		return s, err
	}
	for name, original := range s.Entries {
		if observed, ok := out.Entries[name]; ok {
			if observed.Mode != original.Mode || observed.Kind != original.Kind || !bytes.Equal(observed.Data, original.Data) || (original.Info != nil && (observed.Info == nil || !os.SameFile(original.Info, observed.Info))) {
				return s, discoveryConflict()
			}
		}
		out.Entries[name] = original
	}
	after, err := os.Lstat(s.Directory)
	if err != nil || !after.IsDir() || !os.SameFile(s.RootInfo, after) {
		return s, discoveryConflict()
	}
	return out, nil
}
