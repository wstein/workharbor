// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func TestValidateValidDirectory(t *testing.T) {
	dir := validDir(t)
	r := validate(t, dir, workspacecfg.Options{})
	if !r.Valid || r.Counts.Error != 0 || r.ProviderAdmission != "not_evaluated" {
		t.Fatalf("%+v", r)
	}
	if !has(r, "git.not_repository") || find(r, "git.not_repository").Severity != workspacecfg.SevWarning {
		t.Errorf("no git.not_repository warning: %+v", r.Findings)
	}
	if r.Directory != dir {
		t.Errorf("directory %q", r.Directory)
	}
	raw, _ := json.Marshal(r)
	if !strings.Contains(string(raw), `"findings":[`) {
		t.Errorf("%s", raw)
	}
}

func TestValidateGitFreeProfileSaysNothingAboutGit(t *testing.T) {
	r := validate(t, validDir(t), workspacecfg.Options{GitFree: true})
	if len(r.Findings) != 0 || !r.Valid {
		t.Errorf("%+v", r.Findings)
	}
	if raw, _ := json.Marshal(r); !strings.Contains(string(raw), `"findings":[]`) {
		t.Errorf("findings must be an empty array, not null: %s", raw)
	}
}

func TestValidateDirectoryErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := workspacecfg.Validate(context.Background(), dir+"/absent", workspacecfg.Options{})
	if !isErr(err, workspacecfg.ErrNotFound) {
		t.Errorf("absent: %v", err)
	}
	write(t, dir, "file", "x")
	_, err = workspacecfg.Validate(context.Background(), dir+"/file", workspacecfg.Options{})
	if !isErr(err, workspacecfg.ErrNotDirectory) {
		t.Errorf("file: %v", err)
	}
	_, err = workspacecfg.Validate(context.Background(), dir+"/file/below", workspacecfg.Options{})
	if err == nil {
		t.Errorf("below a file: no error")
	}
}

func TestValidateMissingManifest(t *testing.T) {
	dir := t.TempDir()
	r := validate(t, dir, workspacecfg.Options{GitFree: true})
	if r.Valid || errorCodes(r) != "manifest.missing" || !strings.Contains(find(r, "manifest.missing").Remediation, "whr init") {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateHostileCorpus(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string // error codes, sorted
	}{
		{"manifest symlink", func(t *testing.T, d string) { symlink(t, d, "workharbor.json", "/etc/hostname") }, "manifest.unsafe_path"},
		{"manifest dangling symlink", func(t *testing.T, d string) { symlink(t, d, "workharbor.json", "nowhere") }, "manifest.unsafe_path"},
		{"manifest fifo", func(t *testing.T, d string) { fifo(t, d, "workharbor.json") }, "manifest.unsafe_path"},
		{"manifest socket", func(t *testing.T, d string) { socket(t, d, "workharbor.json") }, "manifest.unsafe_path"},
		{"manifest directory", func(t *testing.T, d string) { remove(t, d, "workharbor.json"); write(t, d, "workharbor.json/x", "") }, "manifest.unsafe_path"},
		{"manifest sparse 10 GiB", func(t *testing.T, d string) { remove(t, d, "workharbor.json"); sparse(t, d, "workharbor.json", 10<<30) }, "manifest.too_large"},
		{"manifest unreadable", func(t *testing.T, d string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads everything")
			}
			if err := os.Chmod(d+"/workharbor.json", 0); err != nil {
				t.Fatal(err)
			}
		}, "manifest.unsafe_path"},
		{"devcontainer symlink", func(t *testing.T, d string) {
			symlink(t, d, ".devcontainer/devcontainer.json", "/etc/hostname")
		}, "devcontainer.unsafe_path"},
		{"devcontainer directory symlink", func(t *testing.T, d string) {
			remove(t, d, ".devcontainer")
			symlink(t, d, ".devcontainer", "/etc")
		}, "devcontainer.unsafe_path"},
		{"devcontainer ancestor is a file", func(t *testing.T, d string) {
			remove(t, d, ".devcontainer")
			write(t, d, ".devcontainer", "x")
		}, "devcontainer.unsafe_path"},
		{"devcontainer fifo", func(t *testing.T, d string) { fifo(t, d, ".devcontainer/devcontainer.json") }, "devcontainer.unsafe_path"},
		{"devcontainer socket", func(t *testing.T, d string) { socket(t, d, ".devcontainer/devcontainer.json") }, "devcontainer.unsafe_path"},
		{"devcontainer directory", func(t *testing.T, d string) {
			remove(t, d, ".devcontainer/devcontainer.json")
			write(t, d, ".devcontainer/devcontainer.json/x", "")
		}, "devcontainer.unsafe_path"},
		{"devcontainer sparse 10 GiB", func(t *testing.T, d string) {
			remove(t, d, ".devcontainer/devcontainer.json")
			sparse(t, d, ".devcontainer/devcontainer.json", 10<<30)
		}, "devcontainer.too_large"},
		{"devcontainer missing", func(t *testing.T, d string) { remove(t, d, ".devcontainer") }, "devcontainer.missing"},
		{"projects symlink", func(t *testing.T, d string) { remove(t, d, "projects"); symlink(t, d, "projects", "/etc") }, "projects.unsafe_path"},
		{"projects fifo", func(t *testing.T, d string) { fifo(t, d, "projects") }, "projects.unsafe_path"},
		{"projects socket", func(t *testing.T, d string) { socket(t, d, "projects") }, "projects.unsafe_path"},
		{"projects is a file", func(t *testing.T, d string) { remove(t, d, "projects"); write(t, d, "projects", "x") }, "projects.missing"},
		{"projects missing", func(t *testing.T, d string) { remove(t, d, "projects") }, "projects.missing"},
		{"projects ancestor symlink", func(t *testing.T, d string) {
			write(t, d, "workharbor.json", strings.Replace(manifestV1, `"projects"`, `"a/b"`, 1))
			remove(t, d, "projects")
			symlink(t, d, "a", d+"/elsewhere")
			write(t, d, "elsewhere/b/x", "")
		}, "projects.unsafe_path"},
		{"projects deep ancestor symlink", func(t *testing.T, d string) {
			write(t, d, "workharbor.json", strings.Replace(manifestV1, `"projects"`, `"a/b/c"`, 1))
			remove(t, d, "projects")
			write(t, d, "a/x", "")
			symlink(t, d, "a/b", d)
		}, "projects.unsafe_path"},
		{"bom manifest", func(t *testing.T, d string) { write(t, d, "workharbor.json", "\xef\xbb\xbf"+manifestV1) }, "manifest.encoding"},
		{"nul manifest", func(t *testing.T, d string) { write(t, d, "workharbor.json", manifestV1+"\x00") }, "manifest.encoding"},
		{"invalid utf8 devcontainer", func(t *testing.T, d string) {
			write(t, d, ".devcontainer/devcontainer.json", "{\"a\":\"\xff\"}")
		}, "devcontainer.encoding"},
		{"duplicate keys", func(t *testing.T, d string) {
			write(t, d, ".devcontainer/devcontainer.json", `{"a":1,"a":2}`)
		}, "devcontainer.duplicate_key"},
		{"depth bomb", func(t *testing.T, d string) {
			write(t, d, ".devcontainer/devcontainer.json", strings.Repeat("[", 100000))
		}, "devcontainer.syntax"},
	}
	t.Run("git listings", hostileListings)
	t.Run("submodule rules", TestValidateSubmoduleRules)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := validDir(t)
			tc.setup(t, dir)
			r := validate(t, dir, workspacecfg.Options{GitFree: true})
			if got := errorCodes(r); got != tc.want {
				t.Errorf("errors %q, want %q: %+v", got, tc.want, r.Findings)
			}
			if r.Valid {
				t.Error("hostile directory is valid")
			}
		})
	}
}

func TestValidateRootSymlinkAndLinkTargetsStayHidden(t *testing.T) {
	real := validDir(t)
	link := t.TempDir() + "/link"
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	r := validate(t, link, workspacecfg.Options{GitFree: true})
	if errorCodes(r) != "manifest.unsafe_path" {
		t.Errorf("%+v", r.Findings)
	}
	d := validDir(t)
	symlink(t, d, "workharbor.json", "/CANARY/secret-target")
	for _, f := range validate(t, d, workspacecfg.Options{GitFree: true}).Findings {
		if contains(f.Message+f.Subject+f.Remediation, "CANARY") {
			t.Errorf("link target disclosed: %+v", f)
		}
	}
}

func TestValidateDevicesAreNeverOpened(t *testing.T) {
	dir := validDir(t)
	remove(t, dir, ".devcontainer/devcontainer.json")
	// /dev/zero would never end if it were read.
	if err := syscall.Mknod(dir+"/.devcontainer/devcontainer.json", syscall.S_IFCHR|0o600, int(1<<8|5)); err != nil {
		t.Skipf("cannot create a device node here (needs a real host with privileges): %v", err)
	}
	r := validate(t, dir, workspacecfg.Options{GitFree: true})
	if errorCodes(r) != "devcontainer.unsafe_path" {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateIsReadOnly(t *testing.T) {
	dir := validDir(t)
	write(t, dir, "projects/note", "x")
	before := snapshot(t, dir)
	for i := 0; i < 2; i++ {
		validate(t, dir, workspacecfg.Options{GitFree: true})
	}
	if after := snapshot(t, dir); after != before {
		t.Errorf("tree changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestValidateDeterministicAndBounded(t *testing.T) {
	dir := validDir(t)
	var b strings.Builder
	b.WriteString(`{`)
	for i := 0; i < 300; i++ {
		b.WriteString(`"k` + itoa(i) + `":1,`)
	}
	b.WriteString(`"image":"` + digest + `"}`)
	write(t, dir, ".devcontainer/devcontainer.json", b.String())
	r1 := validate(t, dir, workspacecfg.Options{GitFree: true})
	r2 := validate(t, dir, workspacecfg.Options{GitFree: true})
	j1, _ := json.Marshal(r1)
	j2, _ := json.Marshal(r2)
	if string(j1) != string(j2) {
		t.Error("two runs differ")
	}
	if !r1.Truncated || len(r1.Findings) != workspacecfg.MaxFindings || r1.Counts.Warning != 300 || !r1.Valid {
		t.Errorf("truncated=%v len=%d counts=%+v valid=%v", r1.Truncated, len(r1.Findings), r1.Counts, r1.Valid)
	}
	if last := r1.Findings[len(r1.Findings)-1]; last.Code != "findings.truncated" || last.Severity != workspacecfg.SevInfo {
		t.Errorf("last finding %+v", last)
	}
}

func TestValidateSortsErrorsFirst(t *testing.T) {
	dir := validDir(t)
	write(t, dir, ".devcontainer/devcontainer.json", `{"features":{}}`)
	remove(t, dir, "projects")
	r := validate(t, dir, workspacecfg.Options{GitFree: true})
	if len(r.Findings) < 2 || r.Findings[0].Severity != workspacecfg.SevError {
		t.Fatalf("%+v", r.Findings)
	}
	for i := 1; i < len(r.Findings); i++ {
		if r.Findings[i-1].Severity == workspacecfg.SevWarning && r.Findings[i].Severity == workspacecfg.SevError {
			t.Errorf("warning before error: %+v", r.Findings)
		}
	}
}

func TestValidateManifestV2Settings(t *testing.T) {
	dir := validDir(t)
	write(t, dir, "workharbor.json", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"name":"ws","start_dir":"projects/app","human_shell":"fish","console_mode":"herdr","shortcuts":"on","project":{"url":"https://f.example/o/app.git","path":"projects/app"}}`)
	r := validate(t, dir, workspacecfg.Options{GitFree: true})
	if !r.Valid || !has(r, "project.declared_not_pinned") || find(r, "project.declared_not_pinned").Severity != workspacecfg.SevWarning {
		t.Errorf("%+v", r.Findings)
	}
	write(t, dir, "workharbor.json", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"human_shell":"csh","start_dir":"../x","project":{"url":"https://u:p@f.example/o/app.git","path":"projects/app"}}`)
	r = validate(t, dir, workspacecfg.Options{GitFree: true})
	if errorCodes(r) != "manifest.path_invalid,manifest.schema,manifest.url_credentials" {
		t.Errorf("%+v", r.Findings)
	}
	for _, f := range r.Findings {
		if contains(f.Message+f.Subject, "p@f.example") || contains(f.Message, "u:p") {
			t.Errorf("credential echoed: %+v", f)
		}
	}
}

func TestValidateStartDirIsNotCheckedOnDisk(t *testing.T) {
	dir := validDir(t)
	write(t, dir, "workharbor.json", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"start_dir":"projects/not-there"}`)
	if r := validate(t, dir, workspacecfg.Options{GitFree: true}); !r.Valid || len(r.Findings) != 0 {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateDevcontainerWarnings(t *testing.T) {
	dir := validDir(t)
	write(t, dir, ".devcontainer/devcontainer.json", `{"image":"ubuntu:22.04","remoteUser":"vscode","initializeCommand":"x","features":{}}`)
	r := validate(t, dir, workspacecfg.Options{GitFree: true})
	if !r.Valid || r.Counts.Warning != 4 || r.Counts.Error != 0 {
		t.Errorf("%+v", r)
	}
	for _, f := range r.Findings {
		if f.Subject != ".devcontainer/devcontainer.json" {
			t.Errorf("subject %q", f.Subject)
		}
	}
}

func TestValidateExampleDirectories(t *testing.T) {
	// The historical example holds no committed projects directory (Git keeps no
	// empty directory), so it is invalid until one is created.
	for _, tc := range []struct {
		dir      string
		warnings int
		errors   string
	}{{"admitted-image", 0, ""}, {"admitted-dockerfile", 0, ""}, {".", 2, "projects.missing"}} {
		root := "../../../../examples/workspace/" + tc.dir
		r := validate(t, root, workspacecfg.Options{GitFree: true})
		if r.Counts.Warning != tc.warnings || errorCodes(r) != tc.errors {
			t.Errorf("%s: %+v", tc.dir, r.Findings)
		}
	}
}

func TestValidateTimeBudgetOutsideGit(t *testing.T) {
	calls := 0
	opt := workspacecfg.Options{GitFree: true, Clock: func() time.Time {
		calls++
		if calls == 1 {
			return time.Unix(1000, 0)
		}
		return time.Unix(1000+10, 0) // 10 s later, beyond the shared 5 s budget
	}}
	r := validate(t, validDir(t), opt)
	if r.Valid || errorCodes(r) != "findings.time_budget_exhausted" {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateInterruptedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := workspacecfg.Validate(ctx, validDir(t), workspacecfg.Options{}); !isErr(err, context.Canceled) {
		t.Errorf("%v", err)
	}
}

func TestValidatorGitFreeProfileInMemory(t *testing.T) {
	good := workspacecfg.MemoryInput{
		Manifest: []byte(manifestV1), Devcontainer: []byte(devcontainer), DevcontainerPresent: true, ProjectsPresent: true,
	}
	r := workspacecfg.ValidateMemory("", good)
	if !r.Valid || len(r.Findings) != 0 || r.ProviderAdmission != "not_evaluated" {
		t.Errorf("%+v", r)
	}
	bad := good
	bad.Manifest = []byte(`{"schema_version":1.0}`)
	if r := workspacecfg.ValidateMemory("", bad); r.Valid || !has(r, "manifest.schema") {
		t.Errorf("%+v", r.Findings)
	}
	noDev := good
	noDev.DevcontainerPresent = false
	noProjects := good
	noProjects.ProjectsPresent = false
	if r := workspacecfg.ValidateMemory("", noDev); errorCodes(r) != "devcontainer.missing" {
		t.Errorf("%+v", r.Findings)
	}
	if r := workspacecfg.ValidateMemory("", noProjects); errorCodes(r) != "projects.missing" {
		t.Errorf("%+v", r.Findings)
	}
	badDev := good
	badDev.Devcontainer = []byte(`{"a":1,"a":2}`)
	if r := workspacecfg.ValidateMemory("", badDev); errorCodes(r) != "devcontainer.duplicate_key" {
		t.Errorf("%+v", r.Findings)
	}
	declared := good
	declared.Manifest = []byte(`{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"project":{"url":"https://f.example/o/app.git","path":"projects/app"}}`)
	if r := workspacecfg.ValidateMemory("", declared); !r.Valid || !has(r, "project.declared_not_pinned") {
		t.Errorf("%+v", r.Findings)
	}
	// The in-memory checker agrees with the on-disk validator on the same content.
	dir := validDir(t)
	write(t, dir, "workharbor.json", string(declared.Manifest))
	disk := validate(t, dir, workspacecfg.Options{GitFree: true})
	mem := workspacecfg.ValidateMemory(dir, declared)
	dj, _ := json.Marshal(disk)
	mj, _ := json.Marshal(mem)
	if string(dj) != string(mj) {
		t.Errorf("disk %s\nmemory %s", dj, mj)
	}
}
