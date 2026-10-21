// SPDX-License-Identifier: EUPL-1.2
package hostsetup

import (
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"testing"
)

func TestLinuxSetupRoutesToEnrollment(t *testing.T) {
	for _, tc := range []struct {
		platform, mode string
		enroll         bool
		want           string
		code           exitcode.Code
	}{{"linux", "setup", true, "enrollment", 0}, {"darwin", "setup", false, "wizard", 0}, {"windows", "setup", false, "", 68}, {"linux", "fix", false, "", 68}, {"windows", "fix", false, "", 68}, {"darwin", "fix", false, "fix", 0}, {"darwin", "setup", true, "", 64}} {
		t.Run(tc.platform+tc.mode+tc.want, func(t *testing.T) {
			calls := []string{}
			port := func(n string) RoutePort { return func(context.Context) error { calls = append(calls, n); return nil } }
			err := Route(context.Background(), RouteRequest{Platform: tc.platform, Mode: tc.mode, EnrollmentFlags: tc.enroll}, Routes{Enrollment: port("enrollment"), Wizard: port("wizard"), Fix: port("fix")})
			if exitcode.FromError(err) != tc.code {
				t.Fatal(err)
			}
			if tc.want == "" && len(calls) != 0 || tc.want != "" && (len(calls) != 1 || calls[0] != tc.want) {
				t.Fatal(calls)
			}
		})
	}
}

func TestRoutePreservesPortErrorAndCancelsBeforeEffects(t *testing.T) {
	sentinel := errors.New("callback failure")
	called := 0
	ports := Routes{Enrollment: func(context.Context) error { called++; return sentinel }}
	if err := Route(context.Background(), RouteRequest{Platform: "linux", Mode: "setup"}, ports); err != sentinel || called != 1 {
		t.Fatalf("%v %d", err, called)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Route(ctx, RouteRequest{Platform: "linux", Mode: "setup"}, ports); err != context.Canceled || called != 1 {
		t.Fatalf("%v %d", err, called)
	}
	if err := Route(context.Background(), RouteRequest{Platform: "linux", Mode: "setup"}, Routes{}); exitcode.FromError(err) != exitcode.Internal {
		t.Fatal(err)
	}
	if err := Route(context.Background(), RouteRequest{Platform: "darwin", Mode: "invalid"}, ports); exitcode.FromError(err) != exitcode.Usage || called != 1 {
		t.Fatalf("%v %d", err, called)
	}
}
