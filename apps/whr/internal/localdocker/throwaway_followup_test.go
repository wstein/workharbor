// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestThrowawayDisposalRefusesCandidateDriftAndMountedResidualHome(t *testing.T) {
	fixture := fixtureThrowawayBinding(t)
	for _, tc := range []struct{ name, reason string }{
		{"renamed_candidate", "candidate home identity or mount binding refused"},
		{"mounted_residual_home", "local stage candidate cleanup home remains mounted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.fresh(t)
			state := readFakeStageState(t, f.path)
			if tc.name == "renamed_candidate" {
				state.Candidate["Name"] = "/renamed-candidate"
			} else {
				other := copyThrowawayContainer(t, state.Candidate)
				other["Id"], other["Name"] = strings.Repeat("f", 64), "/unrelated-container"
				other["Labels"] = map[string]any{}
				state.ExtraContainers = []map[string]any{other}
				state.Candidate = nil
			}
			state.Calls = nil
			writeThrowawayBindingState(t, f.path, state)

			err := f.disposal.removeResources(context.Background())
			if err == nil || err.Error() != tc.reason || exitcode.FromError(err) != exitcode.Conflict {
				t.Fatalf("disposal refusal = %v (exit %d), want %q (exit %d)", err, exitcode.FromError(err), tc.reason, exitcode.Conflict)
			}
			final := readFakeStageState(t, f.path)
			assertThrowawayNoMutation(t, final.Calls)
			if final.Volume == nil || final.Primary["Running"] != true {
				t.Fatal("refused disposal removed the home or stopped the primary")
			}
			if tc.name == "renamed_candidate" && final.Candidate == nil {
				t.Fatal("refused disposal removed the renamed candidate")
			}
			if tc.name == "mounted_residual_home" && len(final.ExtraContainers) != 1 {
				t.Fatal("refused disposal removed the unrelated container")
			}
		})
	}
}
