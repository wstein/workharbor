// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"os"
	"strings"
	"testing"
)

func TestStartersMatchContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/workspace/starters.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Starters []struct{ ID, Status string } }
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	text := scaffold.UnsupportedStarters()
	if len(catalog.Starters) != 2 {
		t.Fatal("catalog changed: review the unsupported display")
	}
	for _, s := range catalog.Starters {
		if s.Status != "unsupported" || !strings.Contains(text, s.ID) {
			t.Fatalf("starter not displayed unsupported: %+v", s)
		}
	}
	p, err := scaffold.Build(snapshot(), defaults())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.HumanPreview(), text) {
		t.Fatal("catalog absent from preview")
	}
}
func TestTemplateRefusedWhileUnsupported(t *testing.T) {
	o := defaults()
	o.Template = str("buildpack-deps-image")
	if _, err := scaffold.Build(snapshot(), o); err == nil || !strings.Contains(err.Error(), "starters are unsupported") {
		t.Fatalf("template admitted: %v", err)
	}
}
