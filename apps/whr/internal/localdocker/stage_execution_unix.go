//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func stageSelectedPath(path string, selected []string) bool {
	for _, p := range selected {
		if p == "." || path == p || strings.HasPrefix(path, p+string(filepath.Separator)) || strings.HasPrefix(p, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// The selection covers the actual hook/smoke/build dependency roots admitted
// during stage. Ordinary candidate files and mutable result refs are separate.
func stageExecutionDigest(ctx context.Context, root string, paths []string) (string, error) {
	info, e := os.Lstat(root)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", stageConflict("execution workspace root identity unavailable")
	}
	selected := append([]string{}, paths...)
	for _, path := range selected {
		if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return "", stageConflict("execution dependency path outside workspace")
		}
	}
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		entries, omitted, e := scanStageFilesSelected(ctx, root, "", selected)
		if e != nil {
			return "", e
		}
		// Direct required omitted roots cannot silently disappear. Generated host
		// facts are handled by their separate, receipt-bound literal-data channel.
		for _, path := range selected {
			for _, missing := range omitted {
				if path == missing || strings.HasPrefix(path, missing+string(filepath.Separator)) {
					return "", stageConflict("required execution dependency was omitted")
				}
			}
		}
		added := false
		for _, entry := range entries {
			if entry["type"] != "symlink" {
				continue
			}
			path := entry["path"].(string)
			resolved, e := filepath.EvalSymlinks(filepath.Join(root, path))
			if e != nil {
				return "", stageConflict("execution dependency symlink changed")
			}
			rel, e := filepath.Rel(root, resolved)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", stageConflict("execution dependency symlink escaped")
			}
			parts := strings.Split(rel, string(filepath.Separator))
			for i := range parts {
				if stageOmit(filepath.Join(parts[:i+1]...)) {
					return "", stageConflict("required execution dependency was omitted")
				}
			}
			if !stageSelectedPath(rel, selected) {
				selected = append(selected, rel)
				added = true
			}
		}
		if added {
			continue
		}
		sort.Strings(selected)
		files := []map[string]any{}
		for _, entry := range entries {
			if entry["type"] != "directory" {
				files = append(files, entry)
			}
		}
		// Directory roots are administrative/private and may have deliberately
		// stronger modes. Working-file modes and raw symlink bytes remain bound.
		return stageDigest(map[string]any{"paths": selected, "files": files}), nil
	}
}

func stageExactGitInputDigest(snapshot stageGitSnapshot) string {
	return stageDigest(map[string]any{"head": snapshot.Head, "refs": snapshot.Refs, "index_digest": snapshot.IndexDigest, "config_digest": snapshot.ConfigDigest})
}
func stageCheckExecutionGit(ctx context.Context, inputs []stageExecutionGitInput, repositories []stageGitSnapshot) error {
	for _, input := range inputs {
		var repository *stageGitSnapshot
		for i := range repositories {
			if repositories[i].Source == input.RepositoryPath {
				repository = &repositories[i]
				break
			}
		}
		if repository == nil {
			return stageConflict("executed Git repository binding missing")
		}
		current, e := readStageGitSnapshotContext(ctx, repository.ClonePath, true)
		if e != nil {
			return e
		}
		if current.GitToolPath != repository.GitToolPath || current.GitToolDigest != repository.GitToolDigest {
			return stageConflict("executed Git tool identity changed")
		}
		switch input.Predicate {
		case "status_success":
			if input.InputDigest != stageDigest("git status succeeds") {
				return stageConflict("executed Git status predicate binding changed")
			}
			if e = stageValidateGitStatus(ctx, current); e != nil {
				return e
			}
		case "exact_version":
			if input.InputDigest != stageExactGitInputDigest(current) {
				return stageConflict("executed Git input version changed")
			}
		default:
			return stageConflict("executed Git predicate unsupported")
		}
	}
	return nil
}

func stageValidateGitStatus(parent context.Context, snapshot stageGitSnapshot) error {
	ctx, tool, e := pinStageGit(parent)
	if e != nil {
		return e
	}
	if tool.Path != snapshot.GitToolPath || tool.Digest != snapshot.GitToolDigest {
		return stageConflict("executed Git tool identity changed")
	}
	private, e := os.MkdirTemp("", "whr-git-predicate-")
	if e != nil {
		return stageConflict("executed Git status predicate storage unavailable")
	}
	defer os.RemoveAll(private)
	if e = os.Chmod(private, 0700); e != nil {
		return stageConflict("executed Git status predicate storage mode")
	}
	reader := filepath.Join(private, "reader.git")
	if e = initStageGit(ctx, private, reader, snapshot.ObjectFormat); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(reader, "HEAD"), []byte(snapshot.Head+"\n"), 0600); e != nil {
		return stageConflict("executed Git status predicate storage failed")
	}
	if _, _, e = prepareStageIndex(ctx, private, reader, snapshot); e != nil {
		return stageConflict("executed Git status predicate failed")
	}
	_, e = stageGitRead(ctx, private, reader, snapshot.ObjectDir, filepath.Join(reader, "index"), nil, "--work-tree="+snapshot.Source, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--no-renames")
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stageConflict("executed Git status predicate failed")
	}
	return nil
}
