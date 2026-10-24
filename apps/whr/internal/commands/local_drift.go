// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"context"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

type localDriftKey struct{}
type localDriftInvocation struct {
	local  bool
	source string
}

// LocalDriftHandler validates the explicit local-only surface before any I/O.
func LocalDriftHandler(report func(context.Context, string, string) (localdocker.BundleDriftResult, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localDriftKey{}).(localDriftInvocation)
		if !v.local || v.source == "" || len(call.Args) != 1 || call.Args[0] == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace drift --local --source <dir> <exact-name|full-id>")
		}
		result, err := report(ctx, v.source, call.Args[0])
		return Result{Value: result, Human: result.Human()}, err
	}
}
