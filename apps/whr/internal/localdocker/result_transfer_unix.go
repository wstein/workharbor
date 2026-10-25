// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type stageResultRepository struct {
	Original, Candidate stageGitSnapshot
	Refs                []StageResultRef
	Objects             StageGitObjectsTarget
	IDs                 []string
	Bytes               int64
}
type stageResultTransfer struct {
	operation, root, digest string
	repositories            []stageResultRepository
	tool                    stageGitTool
	attempted               bool
	complete                bool
}

func (p *stageResultTransfer) Preview() []StageResultRef {
	out := []StageResultRef{}
	for _, repo := range p.repositories {
		out = append(out, repo.Refs...)
	}
	return out
}
func (p *stageResultTransfer) Targets() []StageGitObjectsTarget {
	out := []StageGitObjectsTarget{}
	for _, repo := range p.repositories {
		if len(repo.Refs) > 0 {
			out = append(out, repo.Objects)
		}
	}
	return out
}
func (p *stageResultTransfer) Digest() string { return p.digest }
func (p *stageResultTransfer) Close() {
	if p != nil && (!p.attempted || p.complete) && p.root != "" {
		_ = os.RemoveAll(p.root)
		p.root = ""
	}
}
func stageResultPartial(reason string, cause error) error {
	if cause != nil {
		reason += ": " + terminal.Escape(cause.Error())
	}
	return exitcode.Errorf(exitcode.PathPartialFailure, "local stage Git result transfer partial: %s; retain clone, imported objects and operation refs for owner reconciliation", reason)
}
func stageCandidateSnapshot(ctx context.Context, base stageGitSnapshot) (stageGitSnapshot, error) {
	empty := stageGitSnapshot{}
	physical, err := filepath.EvalSymlinks(base.ClonePath)
	if err != nil || physical != base.ClonePath {
		return empty, stageConflict("candidate Git clone path changed or symlinked")
	}
	admin := filepath.Join(base.ClonePath, ".git")
	st, err := os.Lstat(admin)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return empty, stageConflict("candidate Git administration path changed")
	}
	paths, err := stageGitPaths(base.ClonePath)
	if err != nil {
		return empty, err
	}
	if paths.GitDir != admin || paths.CommonDir != admin || paths.ObjectDir != filepath.Join(admin, "objects") || paths.IndexPath != filepath.Join(admin, "index") {
		return empty, stageConflict("candidate Git administration binding changed")
	}
	if _, err := os.Lstat(filepath.Join(paths.ObjectDir, "info", "alternates")); !os.IsNotExist(err) {
		return empty, stageConflict("candidate Git alternates unsupported")
	}
	s, err := readStageCandidateGitSnapshot(ctx, base.ClonePath)
	if err != nil {
		return s, err
	}
	if s.ObjectFormat != base.ObjectFormat {
		return s, stageConflict("candidate Git object format changed")
	}
	return s, nil
}
func prepareStageResults(ctx context.Context, operationID string, repositories []stageGitSnapshot) (*stageResultTransfer, error) {
	if !lowerHex32(operationID) || len(repositories) == 0 || len(repositories) > 33 {
		return nil, stageConflict("Git result operation or repository count invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ctx, tool, err := pinStageGit(ctx)
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "whr-git-results-")
	if err != nil {
		return nil, stageConflict("private Git result reader unavailable")
	}
	p := &stageResultTransfer{operation: operationID, root: root, tool: tool}
	success := false
	defer func() {
		if !success {
			p.Close()
		}
	}()
	count, totalObjects := 0, 0
	var totalBytes int64
	seen := map[string]bool{}
	for i, base := range repositories {
		if base.ClonePath == "" || seen[base.Source] || base.GitToolDigest != "" && base.GitToolDigest != tool.Digest {
			return nil, stageConflict("Git result repository or tool binding invalid")
		}
		seen[base.Source] = true

		candidate, err := stageCandidateSnapshot(ctx, base)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(base.Source))
		prefix := "refs/workharbor/stage/" + operationID + "/" + hex.EncodeToString(sum[:]) + "/"
		repo := stageResultRepository{Original: base, Candidate: candidate, Refs: []StageResultRef{}}
		add := func(export, suffix, oid string) {
			repo.Refs = append(repo.Refs, StageResultRef{RepositoryPath: base.Source, ExportRef: export, TargetRef: prefix + suffix, OID: oid, State: "planned"})
		}
		if candidate.Head != base.Head {
			add("HEAD", "head", candidate.Head)
		}
		names := []string{}
		for name, oid := range candidate.Refs {
			if stageExportRef(name) && base.Refs[name] != oid {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		selected := map[string]bool{}
		for _, name := range names {
			selected[name] = true
		}
		for _, name := range names {
			for parent := name; strings.Contains(parent, "/"); {
				parent = parent[:strings.LastIndexByte(parent, '/')]
				if selected[parent] {
					return nil, stageConflict("Git result ref namespace conflict")
				}
			}
		}
		for _, name := range names {
			add(name, strings.TrimPrefix(name, "refs/"), candidate.Refs[name])
		}
		if err := checkStageResultNamespaces(ctx, base.Refs, repo.Refs); err != nil {
			return nil, err
		}
		count += len(repo.Refs)
		if count > 1024 {
			return nil, stageConflict("Git result ref count exceeds 1024")
		}
		repo.Objects = StageGitObjectsTarget{Kind: "git_objects", Repository: StageRepositoryBinding{SourcePath: base.Source, GitDir: base.GitDir, CommonDir: base.CommonDir, ObjectFormat: base.ObjectFormat, OriginalHeadOID: base.Head, BindingDigest: base.BindingDigest}, ExportPath: filepath.Join(root, strconv.Itoa(i)+".git")}
		if len(repo.Refs) > 0 {
			ids, bytes, digest, err := inspectStageResultGraph(ctx, root, candidate, repo.Refs)
			if err != nil {
				return nil, err
			}
			repo.IDs, repo.Bytes = ids, bytes
			repo.Objects.ObjectCount = len(ids)
			repo.Objects.ObjectManifestDigest = digest
			totalObjects += len(ids)
			totalBytes += bytes
			if totalObjects > 1000000 || totalBytes > 8<<30 {
				return nil, stageConflict("Git result operation object quota exceeded")
			}
		}
		p.repositories = append(p.repositories, repo)
	}
	if err = p.Check(ctx); err != nil {
		return nil, err
	}
	records := []map[string]any{}
	for _, repo := range p.repositories {
		records = append(records, map[string]any{"original": repo.Original.BindingDigest, "refs": repo.Refs, "objects": repo.Objects.ObjectManifestDigest, "count": repo.Objects.ObjectCount})
	}
	raw, _ := json.Marshal(map[string]any{"operation": operationID, "repositories": records})
	sum := sha256.Sum256(append(raw, '\n'))
	p.digest = "sha256:" + hex.EncodeToString(sum[:])
	success = true
	return p, nil
}
func (p *stageResultTransfer) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, stageGitToolKey{}, p.tool)
	for _, repo := range p.repositories {
		for _, ref := range repo.Refs {
			if err := stageResultRefAbsent(ctx, repo.Original, ref.TargetRef); err != nil {
				return err
			}
		}

		original, err := readStageGitSnapshotContext(ctx, repo.Original.Source)
		if err != nil {
			return err
		}
		for _, ref := range repo.Refs {
			if _, exists := original.Refs[ref.TargetRef]; exists {
				return stageConflict("Git result ref already exists")
			}
		}
		if original.BindingDigest != repo.Original.BindingDigest {
			return stageConflict("original Git binding changed before result transfer")
		}
		candidate, err := stageCandidateSnapshot(ctx, repo.Original)
		if err != nil {
			return err
		}
		if candidate.GitDir != repo.Candidate.GitDir || candidate.ObjectDir != repo.Candidate.ObjectDir {
			return stageConflict("candidate Git administration binding changed")
		}
		if len(repo.Refs) > 0 {
			ids, bytes, digest, err := inspectStageResultGraph(ctx, p.root, candidate, repo.Refs)
			if err != nil {
				return err
			}
			if len(ids) != repo.Objects.ObjectCount || bytes != repo.Bytes || digest != repo.Objects.ObjectManifestDigest {
				return stageConflict("planned Git result object graph changed")
			}
		}
	}
	return nil
}
func stageResultRefAbsent(ctx context.Context, s stageGitSnapshot, name string) error {
	if !stageRef(name) || !strings.HasPrefix(name, "refs/workharbor/stage/") {
		return stageConflict("Git result target ref invalid")
	}
	// Lstat catches dangling symbolic links and textual symbolic refs before import.
	if _, err := os.Lstat(filepath.Join(s.CommonDir, filepath.FromSlash(name))); err == nil {
		return stageConflict("Git result ref already exists")
	} else if !os.IsNotExist(err) {
		return stageConflict("Git result ref collision check unavailable")
	}

	return nil
}

// Packed refs have no filesystem ancestors to inspect. Check both directions
// against the source namespace before any export or original-repository import.
func checkStageResultNamespaces(ctx context.Context, baseline map[string]string, refs []StageResultRef) error {
	selected := make(map[string]bool, len(refs))
	for _, ref := range refs {
		selected[ref.TargetRef] = true
	}
	parents := func(name string, candidates map[string]bool) bool {
		for strings.Contains(name, "/") {
			name = name[:strings.LastIndexByte(name, '/')]
			if candidates[name] {
				return true
			}
		}
		return false
	}
	existing := make(map[string]bool, len(baseline))
	for name := range baseline {
		existing[name] = true
	}
	for name := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		if parents(name, existing) || parents(name, selected) {
			return stageConflict("Git result ref namespace conflict")
		}
	}
	for name := range existing {
		if err := ctx.Err(); err != nil {
			return err
		}
		if parents(name, selected) {
			return stageConflict("Git result ref namespace conflict")
		}
	}
	return nil
}
