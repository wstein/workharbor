// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"path/filepath"
)

// Only ref storage is shared with the original. Git configuration, HEAD, logs,
// index and transport state remain private. Both loose refs and live packed-ref
// data are visible, so native create-only transactions use the owner's real loose
// ref lock paths and cannot hide an existing packed operation ref.
func stagePublicationReader(ctx context.Context, home string, s stageGitSnapshot) (string, error) {
	gitdir := filepath.Join(home, "publication.git")
	if err := initStageGit(ctx, home, gitdir, s.ObjectFormat); err != nil {
		return "", err
	}
	refs := filepath.Join(s.CommonDir, "refs")
	st, err := os.Lstat(refs)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", stageConflict("original Git ref directory binding changed")
	}
	if err = os.Remove(filepath.Join(gitdir, "refs", "heads")); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err = os.Remove(filepath.Join(gitdir, "refs", "tags")); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err = os.Remove(filepath.Join(gitdir, "refs")); err != nil {
		return "", err
	}
	if err = os.Symlink(refs, filepath.Join(gitdir, "refs")); err != nil {
		return "", err
	}
	if err = os.Symlink(filepath.Join(s.CommonDir, "packed-refs"), filepath.Join(gitdir, "packed-refs")); err != nil {
		return "", err
	}
	return gitdir, nil
}
