// SPDX-License-Identifier: EUPL-1.2

// Package localshell implements host-local workspace shell policy.
package localshell

import (
	"context"
	"errors"
	"io/fs"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// ManifestShell resolves the validated human_shell in the host workspace
// folder. An absent folder label, manifest or preference defaults to fish.
// An unsafe or invalid manifest is a conflict, never a defaulted preference.
func ManifestShell(ctx context.Context, folder string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if folder == "" {
		return "fish", nil
	}
	result, err := workspacecfg.ReadManifest(ctx, folder)
	if errors.Is(err, fs.ErrNotExist) {
		return "fish", nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", exitcode.Errorf(exitcode.PathConflict, "cannot read workspace manifest: %s", terminal.Escape(err.Error()))
	}
	for _, finding := range result.Findings {
		if finding.Severity == workspacecfg.SevError {
			return "", exitcode.Errorf(exitcode.PathConflict, "invalid workspace manifest: %s", terminal.Escape(finding.Message))
		}
	}
	if result.Manifest == nil {
		return "", exitcode.Errorf(exitcode.PathConflict, "invalid workspace manifest")
	}
	if result.Manifest.HumanShell == "" {
		return "fish", nil
	}
	return result.Manifest.HumanShell, nil
}
