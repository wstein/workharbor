// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
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
	for _, count := range []int{stageMaxEntries - 1, stageMaxEntries, stageMaxEntries + 1} {
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
			if count == stageMaxEntries-1 {
				if err != nil {
					t.Fatalf("exact combined ref bound refused: %v", err)
				}
			} else {
				requireCaptureConflict(t, err, "Git ref entry quota")
			}
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

// This reader makes an accidental whole-body read fail once it grows its
// allocation beyond a fixed streaming buffer.
type captureBoundedZeroReader struct{ read int64 }

func (r *captureBoundedZeroReader) Read(p []byte) (int, error) {
	if len(p) > 32<<10 {
		return 0, fmt.Errorf("unbounded read request: %d", len(p))
	}
	clear(p)
	r.read += int64(len(p))
	return len(p), nil
}

func TestLocalStageGitIndexStreamsLargeExtension(t *testing.T) {
	const extensionSize = 64 << 20
	header := make([]byte, 20)
	copy(header, "DIRC")
	binary.BigEndian.PutUint32(header[4:], 2)
	copy(header[12:], "TEST")
	binary.BigEndian.PutUint32(header[16:], extensionSize)
	body := &captureBoundedZeroReader{}
	input := io.MultiReader(bytes.NewReader(header), io.LimitReader(body, extensionSize), bytes.NewReader(make([]byte, 20)))
	shared, err := stageScanIndex(input, 20+extensionSize+20, "sha1")
	if err != nil || shared != "" || body.read != extensionSize {
		t.Fatalf("stream scan: shared=%q read=%d err=%v", shared, body.read, err)
	}
}

func TestLocalStageGitIndexRejectsQuotaWithoutReadingBody(t *testing.T) {
	header := make([]byte, 12)
	copy(header, "DIRC")
	binary.BigEndian.PutUint32(header[4:], 2)
	binary.BigEndian.PutUint32(header[8:], stageMaxEntries+1)
	body := &captureBoundedZeroReader{}
	_, err := stageScanIndex(io.MultiReader(bytes.NewReader(header), body), 2<<30, "sha1")
	requireCaptureConflict(t, err, "Git index version or entry bound unsupported")
	if body.read != 0 {
		t.Fatalf("read %d body bytes before entry quota refusal", body.read)
	}
}

func TestLocalStageGitCaptureVersionFourIndex(t *testing.T) {
	source, _ := captureReviewRepository(t)
	for _, name := range []string{"prefix/file-one", "prefix/file-two"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, source, "add", name)
	}
	fixtureGit(t, source, "update-index", "--index-version=4")
	before := fixtureGit(t, source, "ls-files", "--stage")
	files, err := captureStageFiles(context.Background(), source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err = captureStageGit(context.Background(), source, files.Path, clone); err != nil {
		t.Fatal(err)
	}
	if after := fixtureGit(t, clone, "ls-files", "--stage"); after != before {
		t.Fatalf("index changed: %q != %q", after, before)
	}
}
