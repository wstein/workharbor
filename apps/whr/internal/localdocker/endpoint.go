// SPDX-License-Identifier: EUPL-1.2

// Package localdocker provides bounded local discovery and guarded removal (006 FR-066/067).
package localdocker

import (
	"context"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// ResolveEndpoint uses the very same Unix socket selection as doctor. It never
// consults Docker contexts or dials a remote endpoint. The caller supplies the
// shared discovery deadline, which also bounds all subsequent client calls.
func ResolveEndpoint(ctx context.Context, p doctor.Ports) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	endpoint, check := doctor.ResolveLocalSocket(ctx, p)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if check.Status != doctor.StatusPass {
		return "", exitcode.Errorf(exitcode.PathConflict, "local Docker endpoint unavailable: %s", check.Message).WithRemediation("%s", check.Remediation)
	}
	return endpoint, nil
}
