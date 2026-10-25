//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStageExecutionManifestSeparatesOrdinaryWorkFromDependencies(t *testing.T) {
	c, opts, p := fixtureStagePreparation(t)
	s, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	paths := []string{".devcontainer", "Makefile", ".gitignore", ".gitmodules"}
	before, e := stageExecutionDigest(context.Background(), s.git.Clone, paths)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.git.Clone, "file"), []byte("normal candidate work\n"), 0600); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, s.git.Clone, "add", "file")
	fixtureGit(t, s.git.Clone, "commit", "-m", "normal candidate commit")
	input := stageExecutionGitInput{RepositoryPath: s.git.Source, Predicate: "status_success", InputDigest: stageDigest("git status succeeds")}
	if e = stageCheckExecutionGit(context.Background(), []stageExecutionGitInput{input}, s.git.Repositories); e != nil {
		t.Fatalf("normal result-only commit invalidated predicate: %v", e)
	}
	after, e := stageExecutionDigest(context.Background(), s.git.Clone, paths)
	if e != nil || before != after {
		t.Fatalf("ordinary result changed execution version: %s %s %v", before, after, e)
	}
	if e = os.WriteFile(filepath.Join(s.git.Clone, ".devcontainer/setup.sh"), []byte("printf changed hook\n"), 0600); e != nil {
		t.Fatal(e)
	}
	after, e = stageExecutionDigest(context.Background(), s.git.Clone, paths)
	if e != nil || before == after {
		t.Fatalf("hook dependency change not bound: %s %s %v", before, after, e)
	}
}
