// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (p *stageResultTransfer) Apply(ctx context.Context, record func(string, StageTarget, string) error) ([]StageResultRef, error) {
	out := p.Preview()
	for i := range out {
		out[i].State = "remaining"
	}
	if p.attempted {
		return out, stageConflict("Git result attempt retained; owner reconciliation required")
	}
	if record == nil {
		return out, stageConflict("Git result transfer requires durable effect recorder")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, stageGitToolKey{}, p.tool)
	if err := p.Check(ctx); err != nil {
		return out, err
	}
	if len(out) == 0 {
		return out, nil
	}
	p.attempted = true
	// Freeze and verify every selected graph before importing into any original.
	for _, repo := range p.repositories {
		if len(repo.Refs) > 0 {
			if err := exportStageResultGraph(ctx, p.root, repo); err != nil {
				return out, err
			}
		}
	}
	if err := p.Check(ctx); err != nil {
		return out, err
	}
	offset := 0
	for _, repo := range p.repositories {
		if len(repo.Refs) == 0 {
			continue
		}
		objects := repo.Objects
		if err := record("import_objects", StageTarget{Objects: &objects}, "attempted"); err != nil {
			if offset > 0 {
				return out, stageResultPartial("object import intent unavailable")
			}
			return out, err
		}
		reader, err := os.MkdirTemp(p.root, "import-")
		if err != nil {
			return out, stageResultPartial("private import reader unavailable")
		}
		gitdir := filepath.Join(reader, "empty.git")
		if err = initStageGit(ctx, reader, gitdir, repo.Original.ObjectFormat); err != nil {
			_ = record("import_objects", StageTarget{Objects: &objects}, "uncertain")
			return out, stageResultPartial("import preparation failed")
		}
		args := []string{"fetch", "--atomic", "--no-write-fetch-head", "--no-tags", "--no-auto-maintenance", "--no-recurse-submodules", "--", repo.Objects.ExportPath}
		for i, ref := range repo.Refs {
			args = append(args, ref.ExportRef+":refs/workharbor/import/"+strconv.Itoa(i))
		}
		fetch := stageGitCommand(ctx, reader, gitdir, repo.Original.ObjectDir, "", true, args...)
		fetch.Stdout, fetch.Stderr = io.Discard, io.Discard
		if err = runStageGit(fetch); err != nil {
			_ = record("import_objects", StageTarget{Objects: &objects}, "uncertain")
			return out, stageResultPartial("object import failed or was interrupted")
		}
		if err = record("import_objects", StageTarget{Objects: &objects}, "observed"); err != nil {
			return out, stageResultPartial("object import observation unavailable")
		}
		if ctx.Err() != nil {
			return out, stageResultPartial("cancelled after object import")
		}
		creates := make([]StageRefCreate, 0, len(repo.Refs))
		var input strings.Builder
		input.WriteString("start\n")
		for _, ref := range repo.Refs {
			creates = append(creates, StageRefCreate{Name: ref.TargetRef, OID: ref.OID, ExpectedAbsent: true})
			input.WriteString("create " + ref.TargetRef + " " + ref.OID + "\n")
		}
		input.WriteString("prepare\ncommit\n")
		refs := StageGitRefsTarget{Kind: "git_refs", Repository: repo.Objects.Repository, Creates: creates}
		if err = checkStageOriginalBinding(ctx, repo.Original, out); err != nil {
			return out, stageResultPartial("original Git binding changed after object import")
		}
		publication, e := stagePublicationReader(ctx, reader, repo.Original)
		if e != nil {
			return out, stageResultPartial("private ref publication reader unavailable")
		}
		if err = record("create_result_refs", StageTarget{Refs: &refs}, "attempted"); err != nil {
			return out, stageResultPartial("ref transaction intent unavailable after import")
		}

		_, err = stageGitRead(ctx, reader, publication, repo.Original.ObjectDir, "", []byte(input.String()), "update-ref", "--no-deref", "--stdin")
		if err != nil {
			observeStageResultRefs(ctx, repo, out[offset:offset+len(repo.Refs)])
			_ = record("create_result_refs", StageTarget{Refs: &refs}, "uncertain")
			return out, stageResultPartial("create-only ref transaction refused after import")
		}
		for i := 0; i < len(repo.Refs); i++ {
			out[offset+i].State = "created"
		}
		if err = checkStageOriginalBinding(ctx, repo.Original, out); err != nil {
			observeStageResultRefs(ctx, repo, out[offset:offset+len(repo.Refs)])
			_ = record("create_result_refs", StageTarget{Refs: &refs}, "uncertain")
			return out, stageResultPartial("original Git binding changed after ref transaction")
		}
		if err = record("create_result_refs", StageTarget{Refs: &refs}, "observed"); err != nil {
			return out, stageResultPartial("ref transaction observation unavailable")
		}
		offset += len(repo.Refs)
	}
	return out, nil
}
func observeStageResultRefs(ctx context.Context, repo stageResultRepository, refs []StageResultRef) {
	current, err := readStageGitSnapshotContext(ctx, repo.Original.Source, false)
	for i := range refs {
		if err != nil {
			refs[i].State = "uncertain"
		} else if oid, ok := current.Refs[refs[i].TargetRef]; ok && oid == refs[i].OID {
			refs[i].State = "created"
		}
	}
}
