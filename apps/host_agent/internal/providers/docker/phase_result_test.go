// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestOfficialPhaseResultIsClosedAndNeverAcceptsMarkersOrPartialSuccess(t *testing.T) {
	id := strings.Repeat("a", 64)
	valid := map[string]any{"outcome": "success", "containerId": id, "remoteUser": "1000", "remoteWorkspaceFolder": "/workspace", "configuration": map[string]any{}, "mergedConfiguration": map[string]any{}}
	raw, _ := json.Marshal(valid)
	if _, e := phaseResult(context.Background(), registryParser(t), raw, Resources, id); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"outcome":"success"}`, `{"outcome":"success","outcome":"error"}`, `{"outcome":"success","result":"skipNonBlocking"}`, `{"outcome":"success","result":"done","unknown":true}`, `{"result":"done"}`, `marker done`, "{\"outcome\":\"success\",\"result\":\"done\"}\n{}"} {
		if _, e := phaseResult(context.Background(), registryParser(t), []byte(raw), Hooks, id); e == nil {
			t.Fatal("incomplete result admitted", raw)
		}
	}
	if _, e := phaseResult(context.Background(), registryParser(t), []byte(`{"outcome":"success","result":"done"}`), Hooks, id); e != nil {
		t.Fatal(e)
	}
}

func TestResourceResultNumericUsersMatchPinnedCLIDisplay(t *testing.T) {
	for _, row := range []struct {
		pair, display string
		valid         bool
	}{
		{"0:0", "root", true}, {"1000:1000", "1000", true},
		{"0:0", "0", false}, {"0:0", "1000", false}, {"1000:1000", "root", false},
		{"1000:1000", "vscode", false}, {"1000", "1000", false},
	} {
		t.Run(row.pair+"-"+row.display, func(t *testing.T) {
			result := map[string]any{"remoteUser": row.display}
			if e := validateResourceUser(result, row.pair); (e == nil) != row.valid {
				t.Fatalf("pair=%s display=%s error=%v wantValid=%v", row.pair, row.display, e, row.valid)
			}
		})
	}
}
