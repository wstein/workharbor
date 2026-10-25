// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"io/fs"
	"os/exec"
	"sync"
)

const stageMaxWorkingEntries = 100000
const stageMaxObjectEntries = 1000000

type stageCaptureLimits struct {
	WorkingEntries, ObjectEntries, ObjectStores, SubmoduleDepth, SubmoduleCount, Objects int
	FileBytes, WorkingBytes, ObjectBytes                                                 int64
}
type stageCaptureControl struct {
	Limits  *stageCaptureLimits
	Phase   func(string, string)
	Command func(*exec.Cmd)
}
type stageCaptureControlKey struct{}

func defaultStageCaptureLimits() stageCaptureLimits {
	return stageCaptureLimits{WorkingEntries: stageMaxWorkingEntries, ObjectEntries: stageMaxObjectEntries, ObjectStores: 32, SubmoduleDepth: 8, SubmoduleCount: 32, Objects: 1000000, FileBytes: stageMaxFileBytes, WorkingBytes: stageMaxWorkingBytes, ObjectBytes: 8 << 30}
}
func stageCaptureLimitsFor(ctx context.Context) stageCaptureLimits {
	if control, ok := ctx.Value(stageCaptureControlKey{}).(stageCaptureControl); ok && control.Limits != nil {
		return *control.Limits
	}
	return defaultStageCaptureLimits()
}
func stageCapturePhase(ctx context.Context, phase, path string) {
	if control, ok := ctx.Value(stageCaptureControlKey{}).(stageCaptureControl); ok && control.Phase != nil {
		control.Phase(phase, path)
	}
}
func stageCaptureCommand(ctx context.Context, cmd *exec.Cmd) {
	if control, ok := ctx.Value(stageCaptureControlKey{}).(stageCaptureControl); ok && control.Command != nil {
		control.Command(cmd)
	}
}

type stageObjectAudit struct {
	mu      sync.Mutex
	checked map[string]bool
	entries int
}
type stageObjectAuditKey struct{}

type stageDirectoryMode struct {
	path string
	mode fs.FileMode
}
type stageCaptureOperation struct {
	scratchParent string
	directories   []stageDirectoryMode
}
type stageCaptureOperationKey struct{}
