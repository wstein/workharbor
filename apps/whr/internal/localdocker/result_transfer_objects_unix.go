// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func stageResultRoots(refs []StageResultRef) []byte {
	var text strings.Builder
	for _, ref := range refs {
		text.WriteString(ref.OID + "\n")
	}
	return []byte(text.String())
}
func inspectStageResultGraph(ctx context.Context, home string, s stageGitSnapshot, refs []StageResultRef) ([]string, int64, string, error) {
	reader, err := os.MkdirTemp(home, "reader-")
	if err != nil {
		return nil, 0, "", err
	}
	defer os.RemoveAll(reader)
	gitdir := filepath.Join(reader, "empty.git")
	if err = initStageGit(ctx, reader, gitdir, s.ObjectFormat); err != nil {
		return nil, 0, "", err
	}
	raw, err := stageGitReadLimit(ctx, reader, gitdir, s.ObjectDir, "", stageResultRoots(refs), 66*1000000, "rev-list", "--objects", "--no-object-names", "--stdin")
	if err != nil {
		return nil, 0, "", stageConflict("Git planned result closure unavailable locally")
	}
	ids := strings.Fields(string(raw))
	if len(ids) > 1000000 {
		return nil, 0, "", stageConflict("Git result object count quota exceeded")
	}
	for _, oid := range ids {
		if !stageOID(oid, s.ObjectFormat) {
			return nil, 0, "", stageConflict("Git result object identity malformed")
		}
	}
	sizes, err := stageGitReadLimit(ctx, reader, gitdir, s.ObjectDir, "", raw, 128*1000000, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return nil, 0, "", err
	}
	lines := strings.Split(strings.TrimSuffix(string(sizes), "\n"), "\n")
	if len(lines) != len(ids) {
		return nil, 0, "", stageConflict("Git result object inventory incomplete")
	}
	var total int64
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != ids[i] || !stageOID(fields[0], s.ObjectFormat) {
			return nil, 0, "", stageConflict("Git result object inventory malformed")
		}
		switch fields[1] {
		case "blob", "tree", "commit", "tag":
		default:
			return nil, 0, "", stageConflict("Git result object type malformed")
		}
		n, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || n < 0 || n > (8<<30)-total {
			return nil, 0, "", stageConflict("Git result expanded object byte quota exceeded")
		}
		total += n
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	sort.Strings(ids)
	return ids, total, "sha256:" + hex.EncodeToString(sum[:]), nil
}
func exportStageResultGraph(ctx context.Context, home string, repo stageResultRepository) error {
	reader, err := os.MkdirTemp(home, "export-reader-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(reader)
	gitdir := filepath.Join(reader, "empty.git")
	if err = initStageGit(ctx, reader, gitdir, repo.Candidate.ObjectFormat); err != nil {
		return err
	}
	pack, err := os.OpenFile(filepath.Join(reader, "objects.pack"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer pack.Close()
	cmd := stageGitCommand(ctx, reader, gitdir, repo.Candidate.ObjectDir, "", false, "pack-objects", "--stdout", "--revs")
	cmd.Stdin = strings.NewReader(string(stageResultRoots(repo.Refs)))
	cmd.Stdout = &stageBoundedWriter{Writer: pack, Remaining: (8 << 30) + 128*1000000}
	cmd.Stderr = io.Discard
	if err = runStageGit(cmd); err != nil {
		return stageConflict("Git result raw object export failed")
	}
	if err = initStageGit(ctx, home, repo.Objects.ExportPath, repo.Candidate.ObjectFormat); err != nil {
		return err
	}
	if _, err = pack.Seek(0, 0); err != nil {
		return err
	}
	verify := stageGitCommand(ctx, reader, repo.Objects.ExportPath, "", "", false, "index-pack", "--strict", "--stdin")
	verify.Stdin = pack
	verify.Stdout, verify.Stderr = io.Discard, io.Discard
	if err = runStageGit(verify); err != nil {
		return stageConflict("Git result raw object verification failed")
	}
	exported := repo.Candidate
	exported.ObjectDir = filepath.Join(repo.Objects.ExportPath, "objects")
	ids, bytes, digest, err := inspectStageResultGraph(ctx, home, exported, repo.Refs)
	if err != nil {
		return err
	}
	if len(ids) != repo.Objects.ObjectCount || bytes != repo.Bytes || digest != repo.Objects.ObjectManifestDigest {
		return stageConflict("Git verified result object manifest changed")
	}
	var refs strings.Builder
	for _, ref := range repo.Refs {
		if ref.ExportRef == "HEAD" {
			if err = os.WriteFile(filepath.Join(repo.Objects.ExportPath, "HEAD"), []byte(ref.OID+"\n"), 0600); err != nil {
				return err
			}
		} else {
			refs.WriteString("create " + ref.ExportRef + " " + ref.OID + "\n")
		}
	}
	if refs.Len() > 0 {
		if _, err = stageGitRead(ctx, home, repo.Objects.ExportPath, "", "", []byte(refs.String()), "update-ref", "--no-deref", "--stdin"); err != nil {
			return err
		}
	}
	return privateStageGitAdmin(repo.Objects.ExportPath)
}
