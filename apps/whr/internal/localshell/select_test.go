// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"strings"
	"testing"
)

func TestLocalShellExactNameWins(t *testing.T) {
	name := strings.Repeat("a", 12)
	all := []localdocker.Workspace{{ID: strings.Repeat("b", 64), Name: name, State: "running", Labels: map[string]string{"workharbor.workspace": "x"}}, {ID: strings.Repeat("a", 64), Name: "other", State: "running", Labels: map[string]string{"workharbor.workspace": "x"}}}
	got, err := Select(all, name)
	if err != nil || got.Name != name {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestLocalShellIDPrefixAndAmbiguity(t *testing.T) {
	all := []localdocker.Workspace{{ID: strings.Repeat("a", 64), Name: "one", State: "running", Labels: map[string]string{"workharbor.workspace": "x"}}, {ID: strings.Repeat("a", 63) + "b", Name: "two", State: "running", Labels: map[string]string{"workharbor.workspace": "x"}}}
	for _, tc := range []struct {
		sel  string
		code int
	}{{"one", 0}, {strings.Repeat("a", 11), 65}, {strings.Repeat("a", 12), 64}, {strings.Repeat("a", 64), 0}, {"on", 65}, {"absent", 65}} {
		_, err := Select(all, tc.sel)
		if int(exitcode.FromError(err)) != tc.code {
			t.Fatalf("selector=%q: %v", tc.sel, err)
		}
	}
	all[1].Name = "evil\x1b[31m\u202e"
	_, err := Select(all, strings.Repeat("a", 12))
	if err == nil || !strings.Contains(err.Error(), "<U+001B>") || strings.ContainsAny(err.Error(), "\x1b\u202e") {
		t.Fatalf("unsafe candidates: %v", err)
	}
}
func TestLocalShellSelectsBeforeCheckingState(t *testing.T) {
	for _, state := range []string{"exited", "paused", "restarting", "created", "unknown"} {
		all := []localdocker.Workspace{{ID: strings.Repeat("a", 64), Name: "one", State: state, Labels: map[string]string{"workharbor.workspace": "x"}}, {ID: strings.Repeat("a", 63) + "b", Name: "two", State: "running", Labels: map[string]string{"workharbor.workspace": "x"}}}
		if _, err := Select(all, strings.Repeat("a", 12)); exitcode.FromError(err) != 64 {
			t.Fatalf("state=%s ambiguous: %v", state, err)
		}
		if _, err := Select(all, "one"); exitcode.FromError(err) != 68 {
			t.Fatalf("state=%s selected: %v", state, err)
		}
	}
}

func TestLocalShellNeverSelectsForeign(t *testing.T) {
	_, err := Select([]localdocker.Workspace{{ID: strings.Repeat("a", 64), Name: "foreign", State: "running", Labels: map[string]string{"project": "x"}}}, "foreign")
	if exitcode.FromError(err) != 65 {
		t.Fatalf("foreign selected: %v", err)
	}
}
