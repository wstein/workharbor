// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// A reftable repository has no loose reference files: .git/refs/heads is a
// placeholder file and HEAD names refs/heads/.invalid. That is not an unsafe path.
func reftableLayout(t *testing.T, d string, marker string) {
	t.Helper()
	write(t, d, ".git/HEAD", "ref: refs/heads/.invalid\n")
	write(t, d, ".git/refs/heads", "this repository uses the reftable format\n")
	switch marker {
	case "directory":
		write(t, d, ".git/reftable/tables.list", "")
	case "config":
		write(t, d, ".git/config", "[core]\n\trepositoryformatversion = 1\n[extensions]\n\trefStorage = reftable\n")
	}
}

func TestValidateReftableLayoutIsNotUnsafe(t *testing.T) {
	unborn := func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "rev-parse":
			return workspacecfg.GitOut{Exit: 1}, nil
		case "symbolic-ref":
			return ok("refs/heads/.invalid\n")
		}
		return healthyHandler(sub, c)
	}
	for _, marker := range []string{"directory", "config"} {
		for name, h := range map[string]func(string, workspacecfg.GitCmd) (workspacecfg.GitOut, error){"born": healthyHandler, "unborn": unborn} {
			t.Run(marker+"/"+name, func(t *testing.T) {
				dir := validDir(t)
				minimalGitDir(t, dir)
				reftableLayout(t, dir, marker)
				r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: h}, LookGit: fixedGit("/git")})
				if has(r, "git.unsafe_path") || has(r, "git.listing_failed") || !r.Valid && name == "born" {
					t.Errorf("%+v", r.Findings)
				}
			})
		}
	}
	// A file layout without any reftable marker is still refused: the placeholder
	// below refs/ is then not a loose-ref directory Git would read.
	dir := validDir(t)
	minimalGitDir(t, dir)
	reftableLayout(t, dir, "none")
	if r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: healthyHandler}, LookGit: fixedGit("/git")}); !has(r, "git.unsafe_path") {
		t.Errorf("without a reftable marker: %+v", r.Findings)
	}
}

func TestValidateReftableLinksAreStillUnsafe(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	reftableLayout(t, dir, "directory")
	symlink(t, dir, ".git/reftable", dir+"/elsewhere")
	if r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: healthyHandler}, LookGit: fixedGit("/git")}); !has(r, "git.unsafe_path") {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateRealReftableRepository(t *testing.T) {
	realGit(t)
	dir := validDir(t)
	probe := exec.Command("git", "init", "-q", "-b", "main", "--template=", "--ref-format=reftable")
	probe.Dir = dir
	probe.Env = hermeticEnv(t.TempDir())
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("this Git cannot create a reftable repository: %v %s", err, out)
	}
	runGit(t, dir, "", "add", "-A")
	runGit(t, dir, "", "commit", "-q", "-m", "init")
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable")); err != nil {
		t.Skipf("no reftable directory: %v", err)
	}
	r := validate(t, dir, workspacecfg.Options{})
	if has(r, "git.unsafe_path") || has(r, "git.listing_failed") || !r.Valid {
		t.Errorf("%+v", r.Findings)
	}
	// An unborn reftable repository too.
	empty := validDir(t)
	runGit(t, empty, "", "init", "-q", "-b", "main", "--template=", "--ref-format=reftable")
	if r := validate(t, empty, workspacecfg.Options{}); has(r, "git.unsafe_path") || has(r, "git.listing_failed") {
		t.Errorf("unborn: %+v", r.Findings)
	}
}

// Every entry of .git/refs and the first level of .git/objects must be a real
// file or directory, whatever HEAD says.
func TestValidateGitDirectoryWalkRefusesLinks(t *testing.T) {
	cases := map[string]func(t *testing.T, d string){
		"refs with a detached HEAD": func(t *testing.T, d string) {
			write(t, d, ".git/HEAD", sha+"\n")
			symlink(t, d, ".git/refs", d+"/elsewhere")
		},
		"objects/pack": func(t *testing.T, d string) { symlink(t, d, ".git/objects/pack", "/etc") },
		"objects fan-out directory": func(t *testing.T, d string) {
			write(t, d, ".git/objects/ab/"+strings.Repeat("c", 38), "")
			remove(t, d, ".git/objects/ab")
			symlink(t, d, ".git/objects/ab", "/etc")
		},
		"refs/tags": func(t *testing.T, d string) { symlink(t, d, ".git/refs/tags", "/etc") },
		"refs/remotes/origin/HEAD": func(t *testing.T, d string) {
			write(t, d, ".git/refs/remotes/origin/main", sha+"\n")
			symlink(t, d, ".git/refs/remotes/origin/HEAD", "/etc/hostname")
		},
		"deep ref file": func(t *testing.T, d string) {
			write(t, d, ".git/refs/heads/feature/x/keep", sha+"\n")
			symlink(t, d, ".git/refs/heads/feature/x/y", "/etc/hostname")
		},
		"refs/heads/other ref": func(t *testing.T, d string) {
			write(t, d, ".git/HEAD", sha+"\n")
			symlink(t, d, ".git/refs/heads/other", "/etc/hostname")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := validDir(t)
			minimalGitDir(t, dir)
			setup(t, dir)
			f := &fakeGit{handler: healthyHandler}
			r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
			if r.Valid || errorCodes(r) != "git.unsafe_path" || len(f.calls) != 0 {
				t.Fatalf("%+v calls=%v", r.Findings, f.subs())
			}
			if m := find(r, "git.unsafe_path").Message; !strings.Contains(m, "symbolic link") || !strings.Contains(m, ".git/") {
				t.Errorf("the message must name the link: %q", m)
			}
		})
	}
}
