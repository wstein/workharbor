// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func stageGitRelativePath(value string) bool {
	return value != "" && value != "." && len(value) <= 4096 && !filepath.IsAbs(value) && filepath.ToSlash(filepath.Clean(value)) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains("/"+value+"/", "/.git/") && !strings.ContainsAny(value, "\x00\r\n")
}
func captureStageGit(ctx context.Context, source, captured, clone string) (result stageGitCapture, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var err error
	for _, path := range []*string{&source, &captured} {
		*path, err = filepath.EvalSymlinks(*path)
		if err != nil {
			return result, stageConflict("Git capture input path unavailable")
		}
		*path, err = filepath.Abs(*path)
		if err != nil {
			return result, stageConflict("Git capture input path unavailable")
		}
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clone))
	if err != nil {
		return result, stageConflict("Git clone parent unavailable")
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return result, stageConflict("Git clone parent unavailable")
	}
	clone = filepath.Join(parent, filepath.Base(clone))
	result.Clone = clone
	if _, err = os.Lstat(clone); !os.IsNotExist(err) {
		return result, stageConflict("Git clone destination already present or unavailable")
	}
	private, err := os.MkdirTemp(parent, "clone-capture-")
	if err != nil {
		return result, stageConflict("Git clone staging storage unavailable")
	}
	identity, err := os.Lstat(private)
	if err != nil {
		return result, stageConflict("Git clone staging identity unavailable")
	}
	cleanup := &capturedStageFiles{Path: private, identity: identity}
	defer func() {
		if err := cleanup.Close(); err != nil {
			failure = err
		}
	}()
	stagedClone := filepath.Join(private, "tree")
	ctx, _, err = pinStageGit(ctx)
	if err != nil {
		return result, err
	}
	var count int
	var objects int
	var objectBytes int64
	var capture func(string, string, string, int) error
	capture = func(src, files, out string, depth int) error {
		if depth > 8 {
			return stageConflict("populated Git submodule depth quota exceeded")
		}
		if count > 32 {
			return stageConflict("populated Git submodule count quota exceeded")
		}
		one, err := captureStageGitOne(ctx, src, files, out)
		if err != nil {
			return err
		}
		s := one.stageGitSnapshot
		rel, e := filepath.Rel(source, src)
		if e != nil {
			return stageConflict("Git submodule source correspondence unavailable")
		}
		s.RelativePath = filepath.ToSlash(rel)
		s.ClonePath = out
		objects += s.ObjectCount
		objectBytes += s.ObjectBytes
		if objects > 1000000 || objectBytes > 8<<30 {
			return stageConflict("Git operation object graph quota exceeded")
		}
		if depth == 0 {
			result.stageGitSnapshot = s
		}
		result.Repositories = append(result.Repositories, s)
		for _, path := range s.populated {
			count++
			if err = capture(filepath.Join(src, path), filepath.Join(files, path), filepath.Join(out, path), depth+1); err != nil {
				return err
			}
		}
		return privateStageGitAdmin(filepath.Join(out, ".git"))
	}
	err = capture(source, captured, stagedClone, 0)
	if err == nil {
		for _, s := range result.Repositories {
			current, e := readStageGitSnapshotContext(ctx, s.Source, false)
			if e != nil || current.BindingDigest != s.BindingDigest {
				return result, stageConflict("source Git state changed during recursive capture")
			}
		}
	}
	if err == nil {
		if err = publishStageClone(stagedClone, clone); err != nil {
			return result, stageConflict("Git clone publication failed or destination changed")
		}
		for i := range result.Repositories {
			rel, _ := filepath.Rel(stagedClone, result.Repositories[i].ClonePath)
			result.Repositories[i].ClonePath = filepath.Join(clone, rel)
		}
		result.stageGitSnapshot.ClonePath = clone
		result.Clone = clone
	}
	return result, err
}
func privateStageGitAdmin(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return stageConflict("private Git administration enumeration unavailable")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return stageConflict("private Git administration is symlinked")
		}
		mode := fs.FileMode(0600)
		if d.IsDir() {
			mode = 0700
		}
		if err := os.Chmod(path, mode); err != nil {
			return stageConflict("private Git administration mode unavailable")
		}
		return nil
	})
}
