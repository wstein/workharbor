// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"io"
	"io/fs"
	"os"
)

const claudeSettingsPath = ".claude/settings.json"
const claudeSettings = "{\n  \"attribution\": {\n    \"sessionUrl\": false\n  }\n}\n"

func (p *Proposal) claudeSettingsFile(s Snapshot, o Options) error {
	e, exists := s.Entries[claudeSettingsPath]
	parent, parentExists := s.Entries[".claude"]
	unsafeParent := parentExists && parent.Kind != "directory"
	equal := exists && e.Kind == "file" && bytes.Equal(e.Data, []byte(claudeSettings)) && (e.Info == nil || e.Info.Size() == int64(len(claudeSettings)))
	if (unsafeParent || exists && !equal) && !o.permits(claudeSettingsPath) {
		if !unsafeParent {
			if err := p.directories(s, ".claude", false); err != nil {
				return err
			}
		}
		p.preview.Actions = append(p.preview.Actions, Action{Action: "left_unchanged", Kind: "file", Path: claudeSettingsPath})
		p.preview.NextSteps = append(p.preview.NextSteps, "Set attribution.sessionUrl: false in .claude/settings.json, or use --overwrite .claude/settings.json; overwrite replaces the whole file.")
		return nil
	}
	if unsafeParent || exists && e.Kind != "file" {
		return exitcode.Errorf(exitcode.PathConflict, "Claude settings target must be a regular file under a real .claude directory")
	}
	if err := p.directories(s, ".claude", false); err != nil {
		return err
	}
	return p.file(s, o, claudeSettingsPath, []byte(claudeSettings))
}

// claudeReadBound is the content prefix hashed for stale-write checks, the same
// 64 KiB bound as AGENTS.md. A larger existing file is additionally pinned by
// inode, size and mtime, and can only ever be left unchanged or replaced whole.
const claudeReadBound = 64 << 10

// maxClaudeRestore bounds the memory kept to restore an overwritten settings
// file; larger owner files are refused (68) rather than replaced unrestorably.
const maxClaudeRestore = 64 << 20

// readClaudePrefix reads at most the authored length plus one byte, like the
// discovery step, and requires the identity, size and mtime to match the preview.
func readClaudePrefix(root *os.Root, leaf string, expected fs.FileInfo) ([]byte, fs.FileInfo, error) {
	return readClaude(root, leaf, expected, claudeReadBound)
}

// readClaudeFull reads the whole regular file for restore, up to maxClaudeRestore.
func readClaudeFull(root *os.Root, leaf string, expected fs.FileInfo) ([]byte, fs.FileInfo, error) {
	return readClaude(root, leaf, expected, maxClaudeRestore)
}

func readClaude(root *os.Root, leaf string, expected fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
	named, err := root.Lstat(leaf)
	if err != nil || !named.Mode().IsRegular() || expected == nil || expected.Size() > claudeReadBound && (named.Size() != expected.Size() || !named.ModTime().Equal(expected.ModTime())) {
		return nil, nil, conflict()
	}
	f, err := openObservedFile(root, leaf)
	if err != nil {
		return nil, nil, conflict()
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil || !held.Mode().IsRegular() || !os.SameFile(named, held) {
		return nil, nil, conflict()
	}
	if held.Size() > limit && limit == maxClaudeRestore {
		return nil, nil, conflict()
	}
	data, err := io.ReadAll(io.LimitReader(f, min(limit, held.Size())))
	if err != nil {
		return nil, nil, conflict()
	}
	after, e1 := f.Stat()
	latest, e2 := root.Lstat(leaf)
	if e1 != nil || e2 != nil || !os.SameFile(held, after) || !os.SameFile(held, latest) || held.Size() != after.Size() || held.ModTime() != after.ModTime() || latest.Mode() != held.Mode() {
		return nil, nil, conflict()
	}
	return data, held, nil
}
