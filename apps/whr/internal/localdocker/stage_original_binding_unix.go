// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// checkStageOriginalBinding accounts for this operation's exact additive refs
// without replacing the captured source baseline with a later trust version.
func checkStageOriginalBinding(ctx context.Context, baseline stageGitSnapshot, created []StageResultRef) error {
	current, err := readStageGitSnapshotContext(ctx, baseline.Source, false)
	if err != nil {
		return err
	}
	for _, ref := range created {
		if ref.RepositoryPath != baseline.Source || ref.State != "created" {
			continue
		}
		if !stageRef(ref.TargetRef) || !strings.HasPrefix(ref.TargetRef, "refs/workharbor/stage/") || !stageOID(ref.OID, baseline.ObjectFormat) {
			return stageConflict("Git result allowance is not an exact operation ref")
		}
		if _, existed := baseline.Refs[ref.TargetRef]; existed {
			return stageConflict("Git result allowance would mask a baseline ref")
		}
		// An exact created OID does not authorize replacing its operation ref with
		// a textual symref, even if its current referent happens to match.
		if data, err := stageAdminFile(current.CommonDir, filepath.FromSlash(ref.TargetRef), 4096); err == nil {
			if strings.TrimSuffix(string(data), "\n") != ref.OID {
				return stageConflict("original Git operation result ref changed")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if current.Refs[ref.TargetRef] != ref.OID {
			return stageConflict("original Git operation result ref changed")
		}
		delete(current.Refs, ref.TargetRef)
	}
	current.BindingDigest = stageSnapshotDigest(current)
	if current.BindingDigest != baseline.BindingDigest {
		return stageConflict("original Git binding changed outside exact operation refs")
	}
	return nil
}
