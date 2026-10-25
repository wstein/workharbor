// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalLifecycleEffectTargetsAreClosedAndDiscriminated(t *testing.T) {
	id := strings.Repeat("a", 64)
	name := "candidate"
	v := StageTarget{Resource: &StageResource{Kind: "container", Name: &name, ID: &id, DaemonID: "daemon"}}
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var round StageTarget
	if e = json.Unmarshal(raw, &round); e != nil || round.Resource == nil || *round.Resource.ID != id {
		t.Fatalf("roundtrip: %s %v", raw, e)
	}
	for _, raw := range []string{`{"kind":"container","name":null,"id":null,"daemon_id":"d","script":"secret"}`, `{"kind":"container","name":null,"id":null}`, `{"kind":"container","name":null,"id":null,"Daemon_id":"d"}`, `{"kind":"container","name":null,"id":null,"daemon_id":"d","kind":"volume"}`, `{"kind":"unknown"}`} {
		if e = json.Unmarshal([]byte(raw), &round); e == nil {
			t.Fatalf("open/ambiguous target admitted: %s", raw)
		}
	}
	v.Objects = &StageGitObjectsTarget{Kind: "git_objects"}
	if _, e = json.Marshal(v); e == nil {
		t.Fatal("multiple target kinds admitted")
	}
}
