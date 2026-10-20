// SPDX-License-Identifier: EUPL-1.2
package version

import (
	"runtime/debug"
	"testing"
)

func TestVCSRevisionModifiedState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"missing", nil, ""},
		{"clean", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "false"}}, "abc"},
		{"unspecified", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
		{"dirty", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "true"}}, "abc-modified"},
		{"dirty setting first", []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: "abc"}}, "abc-modified"},
		{"dirty without revision", []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := revisionFromSettings(tc.settings); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
