// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

func stageSnapshotDigest(s stageGitSnapshot) string {
	identities := map[string]any{}
	for _, path := range []string{s.Source, s.GitDir, s.CommonDir, s.ObjectDir} {
		if st, err := os.Stat(path); err == nil {
			if raw, ok := st.Sys().(*syscall.Stat_t); ok {
				identities[path] = []uint64{uint64(raw.Dev), uint64(raw.Ino), uint64(raw.Uid), uint64(raw.Mode)}
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"source": s.Source, "git_dir": s.GitDir, "common_dir": s.CommonDir, "objects": s.ObjectDir, "format": s.ObjectFormat, "head": s.Head, "head_ref": s.HeadRef, "refs": s.Refs, "index": s.IndexDigest, "config": s.ConfigDigest, "exclude": s.Exclude, "alternates": s.AlternatesDigest, "identities": identities})
	sum := sha256.Sum256(append(b, '\n'))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func checkStageObjectStores(ctx context.Context, root string, strict bool) error {
	_, err := stageObjectStoreBinding(ctx, root, strict)
	return err
}
func stageObjectStoreBinding(ctx context.Context, root string, strict bool) (string, error) {
	visited := map[string]bool{}
	records := []string{}
	var visit func(string) error
	visit = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		physical, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return stageConflict("Git alternate object store unavailable")
		}
		if visited[physical] {
			return nil
		}
		visited[physical] = true
		identity := ""
		if st, e := os.Stat(physical); e == nil {
			if raw, ok := st.Sys().(*syscall.Stat_t); ok {
				b, _ := json.Marshal([]uint64{uint64(raw.Dev), uint64(raw.Ino), uint64(raw.Uid), uint64(raw.Mode)})
				identity = string(b)
			}
		}
		records = append(records, physical+"\x00"+identity)
		err = filepath.WalkDir(physical, func(name string, d fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				return stageConflict("Git object administration unavailable")
			}
			if d.Type()&os.ModeSymlink != 0 {
				return stageConflict("Git object administration is symlinked")
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				return stageConflict("Git object administration file type unsupported")
			}
			return nil
		})
		if err != nil {
			return err
		}
		data, err := stageAdminFile(physical, filepath.Join("info", "alternates"), 4<<20)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if strict {
			return stageConflict("candidate Git alternates unsupported")
		}
		sum := sha256.Sum256(data)
		records = append(records, physical+"\x00alternates:"+hex.EncodeToString(sum[:]))
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			if strings.HasPrefix(line, "\"") {
				decoded, e := strconv.Unquote(line)
				if e != nil {
					return stageConflict("Git alternate object path quoting malformed")
				}
				line = decoded
			}
			if line == "" || len(line) > 4096 || strings.ContainsAny(line, "\x00\r") {
				return stageConflict("Git alternate object path malformed")
			}
			if !filepath.IsAbs(line) {
				line = filepath.Join(physical, line)
			}
			if err = visit(line); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return "", err
	}
	sort.Strings(records)
	raw, _ := json.Marshal(records)
	sum := sha256.Sum256(append(raw, '\n'))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
