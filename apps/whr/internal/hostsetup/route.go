// SPDX-License-Identifier: EUPL-1.2
// Package hostsetup routes internal setup capabilities without registering commands.
package hostsetup

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// RouteRequest contains platform facts and flag presence, never enrollment secrets.
// Platform is GOOS, not GOOS-GOARCH. Mode is setup or fix.
type RouteRequest struct {
	Platform, Mode  string
	EnrollmentFlags bool
}
type RoutePort func(context.Context) error
type Routes struct{ Enrollment, Wizard, Fix RoutePort }

// Route validates before invoking exactly one injected effect boundary. Linux
// receives its unchanged enrollment callback; the caller retains its arguments.
func Route(ctx context.Context, r RouteRequest, ports Routes) error {
	var port RoutePort
	switch r.Mode {
	case "setup":
		switch r.Platform {
		case "linux":
			port = ports.Enrollment
		case "darwin":
			if r.EnrollmentFlags {
				return exitcode.Errorf(exitcode.PathBadArguments, "host_setup.flag_not_valid_here")
			}
			port = ports.Wizard
		default:
			return exitcode.Errorf(exitcode.PathConflict, "host_setup.platform_unsupported")
		}
	case "fix":
		if r.Platform != "darwin" {
			return exitcode.Errorf(exitcode.PathConflict, "host_setup.platform_unsupported")
		}
		if r.EnrollmentFlags {
			return exitcode.Errorf(exitcode.PathBadArguments, "host_setup.flag_not_valid_here")
		}
		port = ports.Fix
	default:
		return exitcode.Errorf(exitcode.PathBadArguments, "host_setup.flag_not_valid_here")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if port == nil {
		return exitcode.Errorf(exitcode.PathUnclassified, "host setup route unavailable")
	}
	return port(ctx)
}
