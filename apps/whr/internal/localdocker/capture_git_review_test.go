// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func captureReviewRepository(t *testing.T) (string, string) {
	t.Helper()
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", "--initial-branch=main")
	fixtureGit(t, source, "commit", "--allow-empty", "-m", "base")
	return source, fixtureGit(t, source, "rev-parse", "HEAD")
}

func requireCaptureConflict(t *testing.T, err error, reason string) {
	t.Helper()
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || err.Error() != "local stage "+reason {
		t.Fatalf("error = %v (exit %d), want exit %d and reason %q", err, exitcode.FromError(err), exitcode.Conflict, reason)
	}
}

func TestLocalStageGitCombinedRefQuota(t *testing.T) {
	for _, count := range []int{stageMaxEntries, stageMaxEntries + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source, head := captureReviewRepository(t)
			var packed strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&packed, "%s refs/tags/tag-%d\n", head, i)
			}
			if err := os.WriteFile(filepath.Join(source, ".git", "packed-refs"), []byte(packed.String()), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := readStageGitSnapshotContext(context.Background(), source, false)
			requireCaptureConflict(t, err, "Git ref entry quota")
		})
	}
}

func TestLocalStageGitIndexHeaderQuotaBeforeBody(t *testing.T) {
	source, _ := captureReviewRepository(t)
	header := make([]byte, 12)
	copy(header, "DIRC")
	binary.BigEndian.PutUint32(header[4:], 2)
	binary.BigEndian.PutUint32(header[8:], stageMaxEntries+1)
	file, err := os.OpenFile(filepath.Join(source, ".git", "index"), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(header); err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(16 << 20); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = readStageGitSnapshotContext(context.Background(), source, false)
	requireCaptureConflict(t, err, "Git index version or entry bound unsupported")
}

func TestLocalStageGitSharedIndexHeaderQuota(t *testing.T) {
	source, _ := captureReviewRepository(t)
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("tracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, source, "add", "file")
	fixtureGit(t, source, "update-index", "--split-index")
	shared, err := filepath.Glob(filepath.Join(source, ".git", "sharedindex.*"))
	if err != nil || len(shared) != 1 {
		t.Fatalf("shared index = %v, %v", shared, err)
	}
	file, err := os.OpenFile(shared[0], os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, stageMaxEntries+1)
	if _, err = file.WriteAt(count, 8); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = readStageGitSnapshotContext(context.Background(), source, false)
	requireCaptureConflict(t, err, "Git index version or entry bound unsupported")
}

func TestLocalStageGitRejectsCustomHEADBeforeCapture(t *testing.T) {
	source, head := captureReviewRepository(t)
	fixtureGit(t, source, "update-ref", "refs/custom/x", head)
	fixtureGit(t, source, "symbolic-ref", "HEAD", "refs/custom/x")
	files, err := captureStageFiles(context.Background(), source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	_, err = captureStageGit(context.Background(), source, files.Path, clone)
	requireCaptureConflict(t, err, "Git HEAD symbolic identity unsupported")
	if _, err = os.Lstat(clone); !os.IsNotExist(err) {
		t.Fatalf("candidate created before refusal: %v", err)
	}
}

func TestLocalStageGitCredentialFreeSCPRemotes(t *testing.T) {
	for _, value := range []string{"user:token@host:path", "user:token@[::1]:path", "user:token@host:/project"} {
		if stageCredentialFreeURL(value) {
			t.Errorf("credential-bearing scp remote accepted: %q", value)
		}
	}
	for _, value := range []string{"git@host:path", "git@[::1]:path", "host:path", "/tmp/local:repo", "./local@repo:path", "../local:repo"} {
		if !stageCredentialFreeURL(value) {
			t.Errorf("safe remote refused: %q", value)
		}
	}
}
