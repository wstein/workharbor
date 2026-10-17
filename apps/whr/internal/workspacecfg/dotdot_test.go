// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// A path argument that goes through a link and then .. names the parent of the
// link's target, as the kernel and the shell read it, never the directory that
// holds the link.
func TestValidateReadsDotDotPhysically(t *testing.T) {
	real := validDir(t)
	cwd := t.TempDir() // holds no manifest
	if err := os.Symlink(real+"/projects", filepath.Join(cwd, "plink")); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	for _, arg := range []string{"plink/..", "plink/../", "./plink/..", cwd + "/plink/..", "plink/../."} {
		r, err := workspacecfg.Validate(context.Background(), arg, workspacecfg.Options{GitFree: true})
		if err != nil || !r.Valid {
			t.Errorf("%q must read the parent of the link target: err=%v %+v", arg, err, r)
		}
	}
	// Without a link, .. keeps working; a link as the last component is refused.
	if r, err := workspacecfg.Validate(context.Background(), "../"+filepath.Base(real), workspacecfg.Options{GitFree: true}); err != nil || !r.Valid {
		t.Errorf("a plain .. component: %v %+v", err, r)
	}
	if err := os.Symlink(real, filepath.Join(cwd, "rlink")); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"rlink", "rlink/", "rlink/."} {
		if r, err := workspacecfg.Validate(context.Background(), arg, workspacecfg.Options{GitFree: true}); err != nil || errorCodes(r) != "manifest.unsafe_path" {
			t.Errorf("%q: %v %+v", arg, err, r)
		}
	}
}

// The Git stage follows the same physical reading of plink/.. and is pointed at
// the repository that holds the target, not at the directory holding the link.
func TestValidateRealGitDotDotReadsThePhysicalParent(t *testing.T) {
	realGit(t)
	repo := repoDir(t, true)
	cwd := t.TempDir() // not a repository
	if err := os.Symlink(repo+"/projects", filepath.Join(cwd, "plink")); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	r, err := workspacecfg.Validate(context.Background(), "plink/..", workspacecfg.Options{})
	if err != nil || !r.Valid || has(r, "git.not_repository") || has(r, "git.unsafe_path") {
		t.Errorf("%v %+v", err, r)
	}
}

// A link reached through .. is still a link: rlink/projects/.. names rlink.
func TestValidateRefusesLinkReachedThroughDotDot(t *testing.T) {
	real := validDir(t)
	cwd := t.TempDir()
	if err := os.Symlink(real, filepath.Join(cwd, "rlink")); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{cwd + "/rlink/projects/..", cwd + "/rlink/projects/../"} {
		r, err := workspacecfg.Validate(context.Background(), arg, workspacecfg.Options{GitFree: true})
		if err != nil || errorCodes(r) != "manifest.unsafe_path" {
			t.Errorf("%q: %v %+v", arg, err, r)
		}
	}
}
