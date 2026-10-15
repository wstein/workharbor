// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOfficialPhaseResultIsClosedAndNeverAcceptsMarkersOrPartialSuccess(t *testing.T) {
	id := strings.Repeat("a", 64)
	valid := map[string]any{"outcome": "success", "containerId": id, "remoteUser": "1000", "remoteWorkspaceFolder": "/workspace", "configuration": map[string]any{}, "mergedConfiguration": map[string]any{}}
	raw, _ := json.Marshal(valid)
	if _, e := phaseResult(raw, Resources, id); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"outcome":"success"}`, `{"outcome":"success","outcome":"error"}`, `{"outcome":"success","result":"skipNonBlocking"}`, `{"outcome":"success","result":"done","unknown":true}`, `{"result":"done"}`, `marker done`, "{\"outcome\":\"success\",\"result\":\"done\"}\n{}"} {
		if _, e := phaseResult([]byte(raw), Hooks, id); e == nil {
			t.Fatal("incomplete result admitted", raw)
		}
	}
	if _, e := phaseResult([]byte(`{"outcome":"success","result":"done"}`), Hooks, id); e != nil {
		t.Fatal(e)
	}
}
