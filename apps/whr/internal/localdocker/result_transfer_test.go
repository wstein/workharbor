// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func fixtureStageResult(t *testing.T) (stageGitCapture, string) {
	t.Helper()
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, source, "add", "file")
	fixtureGit(t, source, "commit", "-m", "original")
	files, err := captureStageFiles(context.Background(), source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	clone := filepath.Join(t.TempDir(), "clone")
	captured, err := captureStageGit(context.Background(), source, files.Path, clone)
	if err != nil {
		t.Fatal(err)
	}
	return captured, clone
}
func commitStageResult(t *testing.T, clone, value string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(clone, "file"), []byte(value+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, clone, "add", "file")
	fixtureGit(t, clone, "commit", "-m", value)
	return fixtureGit(t, clone, "rev-parse", "HEAD")
}
func requireStageGitError(t *testing.T, err error, code exitcode.Code, reason string) {
	t.Helper()
	var typed *exitcode.Error
	if !errors.As(err, &typed) || typed.Code() != code || !strings.Contains(typed.Message, reason) {
		t.Fatalf("want typed %d %q, got %v", code, reason, err)
	}
}
func TestLocalStageResultTransferCreateOnlyAndFrozenVersion(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	head := commitStageResult(t, clone, "first result")
	plan, err := prepareStageResults(context.Background(), strings.Repeat("a", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if len(plan.Preview()) != 2 {
		t.Fatalf("planned refs=%v", plan.Preview())
	}
	// Later ordinary candidate work is retained, but cannot replace planned OIDs.
	later := commitStageResult(t, clone, "later result")
	beforeIndex, err := os.ReadFile(captured.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	refs, err := plan.Apply(context.Background(), func(action string, target StageTarget, state string) error {
		events = append(events, action+":"+state)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "import_objects:attempted,import_objects:observed,create_result_refs:attempted,create_result_refs:observed" {
		t.Fatalf("effect order=%v", events)
	}
	for _, ref := range refs {
		if ref.State != "created" || ref.OID != head || fixtureGit(t, captured.Source, "rev-parse", ref.TargetRef) != head {
			t.Fatalf("wrong result ref %+v", ref)
		}
	}
	if fixtureGit(t, captured.Source, "rev-parse", "HEAD") != captured.Head {
		t.Fatal("owner HEAD moved")
	}
	if data, err := os.ReadFile(filepath.Join(captured.Source, "file")); err != nil || string(data) != "original\n" {
		t.Fatal("owner working file changed")
	}
	if after, err := os.ReadFile(captured.IndexPath); err != nil || !bytes.Equal(beforeIndex, after) {
		t.Fatal("owner index changed")
	}
	if _, err := os.Stat(filepath.Join(captured.GitDir, "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("original FETCH_HEAD written: %v", err)
	}
	cmd := exec.Command("git", "-C", captured.Source, "cat-file", "-e", later)
	if err := cmd.Run(); err == nil {
		t.Fatal("later unplanned result was imported")
	}
}
func TestLocalStageResultTransferNoChangesIsEmpty(t *testing.T) {
	captured, _ := fixtureStageResult(t)
	plan, err := prepareStageResults(context.Background(), strings.Repeat("b", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	refs, err := plan.Apply(context.Background(), func(string, StageTarget, string) error { t.Fatal("empty transfer attempted effect"); return nil })
	if err != nil || len(refs) != 0 || len(plan.Targets()) != 0 {
		t.Fatalf("empty transfer %v %v", refs, err)
	}
}
func TestLocalStageResultTransferCollisionsAndCancellation(t *testing.T) {
	for _, mode := range []string{"preexisting", "preexisting_packed", "late_transaction", "late_packed", "late_dangling_symref", "cancel_after_import"} {
		t.Run(mode, func(t *testing.T) {
			captured, clone := fixtureStageResult(t)
			commitStageResult(t, clone, "result")
			plan, err := prepareStageResults(context.Background(), strings.Repeat("c", 32), captured.Repositories)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			target := plan.Preview()[0].TargetRef
			if mode == "preexisting" || mode == "preexisting_packed" {
				fixtureGit(t, captured.Source, "update-ref", target, captured.Head)
				if mode == "preexisting_packed" {
					fixtureGit(t, captured.Source, "pack-refs", "--all", "--prune")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			effects := 0
			refs, err := plan.Apply(ctx, func(action string, target StageTarget, state string) error {
				effects++
				if mode == "late_transaction" && action == "create_result_refs" && state == "attempted" {
					fixtureGit(t, captured.Source, "update-ref", plan.Preview()[0].TargetRef, captured.Head)
				}
				if mode == "late_packed" && action == "create_result_refs" && state == "attempted" {
					fixtureGit(t, captured.Source, "update-ref", plan.Preview()[0].TargetRef, captured.Head)
					fixtureGit(t, captured.Source, "pack-refs", "--all", "--prune")
				}
				if mode == "late_dangling_symref" && action == "create_result_refs" && state == "attempted" {
					fixtureGit(t, captured.Source, "symbolic-ref", plan.Preview()[0].TargetRef, "refs/heads/unrelated-absent")
				}
				if mode == "cancel_after_import" && action == "import_objects" && state == "observed" {
					cancel()
				}
				return nil
			})
			if mode == "late_dangling_symref" {
				if _, e := os.Lstat(filepath.Join(captured.CommonDir, "refs", "heads", "unrelated-absent")); !os.IsNotExist(e) {
					t.Fatal("dangling symref redirected transaction into owner branch")
				}
				if fixtureGit(t, captured.Source, "symbolic-ref", target) != "refs/heads/unrelated-absent" {
					t.Fatal("operation symref overwritten")
				}
			} else if mode != "cancel_after_import" && fixtureGit(t, captured.Source, "rev-parse", target) != captured.Head {
				t.Fatal("collision overwrote foreign ref")
			}
			if mode == "preexisting" || mode == "preexisting_packed" {
				requireStageGitError(t, err, exitcode.Conflict, "Git result ref already exists")
				if effects != 0 {
					t.Fatal("collision imported objects")
				}
			} else {
				reason := "create-only ref transaction refused after import"
				if mode == "cancel_after_import" {
					reason = "cancelled after object import"
				}
				requireStageGitError(t, err, exitcode.PartialFailure, reason)
				if len(refs) != 2 {
					t.Fatal("partial ref inventory lost")
				}
			}

			if fixtureGit(t, captured.Source, "rev-parse", "HEAD") != captured.Head {
				t.Fatal("owner HEAD moved")
			}
		})
	}
}
func TestLocalStageResultCandidateUntrustedAdministration(t *testing.T) {
	for _, mode := range []string{"alternates", "symlink_config", "fifo_config", "replace_ref", "symbolic_export"} {
		t.Run(mode, func(t *testing.T) {
			captured, clone := fixtureStageResult(t)
			head := commitStageResult(t, clone, "result")
			reason := ""
			gitdir := filepath.Join(clone, ".git")
			switch mode {
			case "alternates":
				if err := os.WriteFile(filepath.Join(gitdir, "objects", "info", "alternates"), []byte(filepath.Join(t.TempDir(), "foreign-objects")+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "candidate Git alternates unsupported"
			case "symlink_config":
				outside := filepath.Join(t.TempDir(), "owner-config")
				if err := os.WriteFile(outside, []byte("private sentinel\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(gitdir, "config")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(gitdir, "config")); err != nil {
					t.Fatal(err)
				}
				reason = "Git administration file is symlinked or unavailable"
			case "fifo_config":
				if err := os.Remove(filepath.Join(gitdir, "config")); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(filepath.Join(gitdir, "config"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "Git administration file type or quota"
			case "replace_ref":
				fixtureGit(t, clone, "update-ref", "refs/replace/"+head, captured.Head)
				reason = "Git replacement refs unsupported"
			case "symbolic_export":
				fixtureGit(t, clone, "symbolic-ref", "refs/heads/export", "refs/heads/main")
				reason = "Git symbolic export ref unsupported"
			}
			_, err := prepareStageResults(context.Background(), strings.Repeat("d", 32), captured.Repositories)
			requireStageGitError(t, err, exitcode.Conflict, reason)
		})
	}
}

func TestLocalStageResultTransferCompleteRefCapBeforeEffects(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	head := commitStageResult(t, clone, "result")
	var input strings.Builder
	for i := 0; i < 1023; i++ {
		input.WriteString("create refs/tags/extra-" + strconv.Itoa(i) + " " + head + "\n")
	}
	cmd := exec.Command("git", "-C", clone, "update-ref", "--stdin")
	cmd.Stdin = strings.NewReader(input.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture refs: %v %s", err, output)
	}
	_, err := prepareStageResults(context.Background(), strings.Repeat("e", 32), captured.Repositories)
	requireStageGitError(t, err, exitcode.Conflict, "Git result ref count exceeds 1024")
	if fixtureGit(t, captured.Source, "rev-parse", "HEAD") != captured.Head {
		t.Fatal("cap refusal moved source")
	}
	if _, err := os.Stat(filepath.Join(captured.GitDir, "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatal("cap refusal wrote FETCH_HEAD")
	}
}
func TestLocalStageResultTransferMultiRepositoryPartialInventory(t *testing.T) {
	first, c1 := fixtureStageResult(t)
	second, c2 := fixtureStageResult(t)
	commitStageResult(t, c1, "first result")
	commitStageResult(t, c2, "second result")
	repos := append(append([]stageGitSnapshot{}, first.Repositories...), second.Repositories...)
	plan, err := prepareStageResults(context.Background(), strings.Repeat("f", 32), repos)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	imports := 0
	refs, err := plan.Apply(context.Background(), func(action string, target StageTarget, state string) error {
		if action == "import_objects" && state == "attempted" {
			imports++
			if imports == 2 {
				return errors.New("injected durable recorder failure")
			}
		}
		return nil
	})
	requireStageGitError(t, err, exitcode.PartialFailure, "object import intent unavailable")
	if len(refs) != 4 || refs[0].State != "created" || refs[1].State != "created" || refs[2].State != "remaining" || refs[3].State != "remaining" {
		t.Fatalf("partial inventory=%+v", refs)
	}
	if fixtureGit(t, first.Source, "rev-parse", "HEAD") != first.Head || fixtureGit(t, second.Source, "rev-parse", "HEAD") != second.Head {
		t.Fatal("owner branches moved")
	}
}

func TestLocalStageResultOriginalBindingAllowsOnlyExactCreatedRefs(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	commitStageResult(t, clone, "result")
	plan, err := prepareStageResults(context.Background(), strings.Repeat("1", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	refs, err := plan.Apply(context.Background(), func(string, StageTarget, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = checkStageOriginalBinding(context.Background(), captured.stageGitSnapshot, refs); err != nil {
		t.Fatalf("own additive refs invalidated provenance: %v", err)
	}
	fixtureGit(t, captured.Source, "update-ref", "refs/heads/foreign", captured.Head)
	err = checkStageOriginalBinding(context.Background(), captured.stageGitSnapshot, refs)
	requireStageGitError(t, err, exitcode.Conflict, "original Git binding changed outside exact operation refs")
}

func TestLocalStageResultHostileCandidateConfigNeverExecutes(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	commitStageResult(t, clone, "result")
	sentinel := filepath.Join(t.TempDir(), "executed")
	config, err := os.OpenFile(filepath.Join(clone, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.WriteString("\n[uploadpack]\npackObjectsHook = touch " + sentinel + "-pack\n[core]\nfsmonitor = touch " + sentinel + "-monitor\nsshCommand = touch " + sentinel + "-ssh\n[credential]\nhelper = !touch " + sentinel + "-cred\n[include]\npath = /nonexistent/untrusted-include\n[core]\nalternateRefsCommand = touch " + sentinel + "-alt\n")
	config.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(clone, ".git", "hooks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(clone, ".git", "hooks", "reference-transaction"), []byte("#!/bin/sh\ntouch '"+sentinel+"-hook'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareStageResults(context.Background(), strings.Repeat("2", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if _, err = plan.Apply(context.Background(), func(string, StageTarget, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if found, err := filepath.Glob(sentinel + "*"); err != nil || len(found) != 0 {
		t.Fatalf("candidate executable configuration ran: %v %v", found, err)
	}
}

func TestLocalStageResultOriginalIncludesNeverLoad(t *testing.T) {
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, source, "add", "file")
	fixtureGit(t, source, "commit", "-m", "base")
	include := filepath.Join(t.TempDir(), "forbidden-config")
	if err := os.WriteFile(include, []byte("[malformed source include\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(filepath.Join(source, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("\n[include]\npath = " + include + "\n")
	cfg.Close()
	if err != nil {
		t.Fatal(err)
	}
	files, err := captureStageFiles(context.Background(), source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	captured, err := captureStageGit(context.Background(), source, files.Path, clone)
	if err != nil {
		t.Fatal(err)
	}
	commitStageResult(t, clone, "result")
	plan, err := prepareStageResults(context.Background(), strings.Repeat("3", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if _, err = plan.Apply(context.Background(), func(string, StageTarget, string) error { return nil }); err != nil {
		t.Fatalf("source include loaded by publication: %v", err)
	}
}

func TestLocalStageResultCandidateRemoteSymrefRemainsOrdinaryData(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	commitStageResult(t, clone, "result")
	fixtureGit(t, clone, "update-ref", "refs/remotes/owner/main", captured.Head)
	fixtureGit(t, clone, "symbolic-ref", "refs/remotes/owner/HEAD", "refs/remotes/owner/main")
	plan, err := prepareStageResults(context.Background(), strings.Repeat("4", 32), captured.Repositories)
	if err != nil {
		t.Fatalf("ordinary symbolic remote ref blocked result export: %v", err)
	}
	defer plan.Close()
	if len(plan.Preview()) != 2 {
		t.Fatalf("non-export remote refs leaked into result mapping: %v", plan.Preview())
	}
}

func TestLocalStageResultOriginalBindingRejectsMatchingSymbolicResult(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	fixtureGit(t, clone, "tag", "result", captured.Head)
	plan, err := prepareStageResults(context.Background(), strings.Repeat("5", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	refs, err := plan.Apply(context.Background(), func(string, StageTarget, string) error { return nil })
	if err != nil || len(refs) != 1 {
		t.Fatalf("tag result: %v %v", refs, err)
	}
	fixtureGit(t, captured.Source, "symbolic-ref", refs[0].TargetRef, "refs/heads/main")
	err = checkStageOriginalBinding(context.Background(), captured.stageGitSnapshot, refs)
	requireStageGitError(t, err, exitcode.Conflict, "original Git operation result ref changed")
}
