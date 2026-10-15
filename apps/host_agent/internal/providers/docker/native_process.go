// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// nativeProcess is host-client plumbing used only after typed reconstruction.
// It returns the fully waited owned client outcome, never daemon quiescence,
// official-up completion or a lifecycle receipt. Stderr never exports secrets.
func nativeProcess(ctx context.Context, program string, args []string, dir string, env []string, in io.Reader, out io.Writer) error {
	if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil || !filepath.IsAbs(program) || filepath.Clean(program) != program || !ownedDirectory(dir) {
		return fail("provider_refused")
	}
	for _, arg := range args {
		if !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return fail("provider_refused")
		}
	}
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = dir
	command.Env = env
	command.Stdin = in
	command.Stdout = out
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if e := ownedProcessGroup(command); e != nil {
		return e
	}
	if e := command.Run(); e != nil || ctx.Err() != nil {
		return fail("provider_unavailable")
	}
	return nil
}
