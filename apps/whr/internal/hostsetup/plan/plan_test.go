// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
)

func input() Input {
	return Input{Platform: "darwin", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Items: []catalogue.Item{{ID: "jq", Kind: "tool", SelectedByDefault: true}, {ID: "bat", Kind: "tool", SelectedByDefault: false}}}, Results: []detect.Result{{Item: "jq", State: "absent"}, {Item: "bat", State: "absent"}}, Config: map[string]ConfigObservation{}, Selected: map[string]bool{}, OptedOut: map[string]bool{}}
}
func TestPlanHashAndClasses(t *testing.T) {
	in := input()
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Item != "jq" || p.Steps[0].Kind != "brew_install" || p.Steps[0].Class != "create" {
		t.Fatalf("default steps=%#v", p.Steps)
	}
	if len(p.Hash) != 64 {
		t.Fatal("missing stable hash")
	}
	again, err := Build(in)
	if err != nil || again.Hash != p.Hash {
		t.Fatal("equal state hash differs")
	}
	in.Results[0] = detect.Result{Item: "jq", State: "present_outdated", Managed: true}
	upgrade, err := Build(in)
	if err != nil || len(upgrade.Steps) != 1 || upgrade.Steps[0].Class != "modify" || upgrade.Steps[0].Kind != "brew_upgrade" {
		t.Fatalf("upgrade=%#v err%v", upgrade, err)
	}
	if upgrade.Hash == p.Hash || Recheck(upgrade, p.Hash) == nil {
		t.Fatal("changed plan hash accepted")
	}
	if !ValidKind("git_config_move") || ValidKind("docker_vm_modify") || ValidKind("runtime_start") {
		t.Fatal("step kinds not closed")
	}
	in.OptedOut["jq"] = true
	skip, err := Build(in)
	if err != nil || len(skip.Steps) != 0 {
		t.Fatal("opted-out item touched")
	}
}
func TestPlanDependenciesAndConfigClasses(t *testing.T) {
	in := input()
	in.Catalogue.Items[0].Requires = []string{"bat"}
	in.Selected["bat"] = true
	in.Catalogue.Items[0].Config = &catalogue.Config{Target: ".config/jq/config"}
	in.Config["jq"] = ConfigObservation{State: "differs", SHA256: "old"}
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 3 || p.Steps[0].Item != "bat" || p.Steps[1].Item != "jq" || p.Steps[2].Kind != "dotfile_replace" || p.Steps[2].Class != "modify" {
		t.Fatalf("dependency/config order=%#v", p.Steps)
	}
	if len(p.Steps[1].DependsOn) != 1 || p.Steps[1].DependsOn[0] != "bat.brew_install" {
		t.Fatal("dependency edge missing")
	}
}

func TestPlanSealingRefusalsAndDynamicSelections(t *testing.T) {
	t.Run("sealed step tamper", func(t *testing.T) {
		in := input()
		p, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		if Recheck(p, p.Hash) != nil {
			t.Fatal("unchanged plan refused")
		}
		p.Steps[0].Kind = "docker_vm_modify"
		if Recheck(p, p.Hash) == nil {
			t.Fatal("tampered sealed step accepted")
		}
	})
	t.Run("effective selection and observed order", func(t *testing.T) {
		in := input()
		a, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		in.Selected["jq"] = true
		in.Results[0], in.Results[1] = in.Results[1], in.Results[0]
		b, err := Build(in)
		if err != nil || a.Hash != b.Hash {
			t.Fatal("equivalent selection/state hash differs")
		}
	})
	t.Run("empty config normalization", func(t *testing.T) {
		in := input()
		a, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		in.Config = nil
		b, err := Build(in)
		if err != nil || a.Hash != b.Hash {
			t.Fatal("nil/empty observations hash differs")
		}
	})
	t.Run("config create", func(t *testing.T) {
		in := input()
		in.Catalogue.Items[0].Config = &catalogue.Config{Target: ".config/jq/config"}
		in.Config["jq"] = ConfigObservation{State: "absent"}
		p, err := Build(in)
		if err != nil || len(p.Steps) != 2 || p.Steps[1].Kind != "dotfile_create" || p.Steps[1].Class != "create" {
			t.Fatal("create config not planned")
		}
	})
	t.Run("unsafe config is visibly blocked", func(t *testing.T) {
		in := input()
		in.Catalogue.Items[0].Config = &catalogue.Config{Target: ".config/jq/config"}
		in.Config["jq"] = ConfigObservation{State: "unsafe_target"}
		p, err := Build(in)
		if err != nil || len(p.Steps) != 2 || p.Steps[1].Blocked != "target_unsafe" {
			t.Fatal("unsafe config silently omitted")
		}
	})
	t.Run("opted out required item", func(t *testing.T) {
		in := input()
		in.Catalogue.Items[0].Requires = []string{"bat"}
		in.OptedOut["bat"] = true
		p, err := Build(in)
		if err != nil || len(p.Steps) != 1 || p.Steps[0].Blocked != "dependency_unavailable" {
			t.Fatal("opted-out dependency touched or ignored")
		}
	})
	t.Run("dynamic catalogue default", func(t *testing.T) {
		in := input()
		in.Catalogue.Items[1].SelectedByDefault = true
		p, err := Build(in)
		if err != nil || len(p.Steps) != 2 {
			t.Fatal("new owner-selected default ignored")
		}
	})
	t.Run("reserved git selection", func(t *testing.T) {
		in := input()
		in.Selected["git-config"] = true
		if _, err := Build(in); err != nil {
			t.Fatal("reserved profile id refused")
		}
		in.Selected["unknown"] = true
		if _, err := Build(in); err == nil {
			t.Fatal("unknown selected id accepted")
		}
	})
	t.Run("missing observation", func(t *testing.T) {
		in := input()
		in.Results = in.Results[:1]
		if _, err := Build(in); err == nil {
			t.Fatal("missing observation accepted")
		}
	})
	t.Run("cycle", func(t *testing.T) {
		in := input()
		in.Catalogue.Items[0].Requires = []string{"bat"}
		in.Catalogue.Items[1].Requires = []string{"jq"}
		if _, err := Build(in); err == nil {
			t.Fatal("cyclic dependencies accepted")
		}
	})
	t.Run("unknown state", func(t *testing.T) {
		in := input()
		in.Results[0].State = "invented"
		if _, err := Build(in); err == nil {
			t.Fatal("unknown state accepted")
		}
	})
	for _, state := range []string{"present_other_source", "present_unverifiable", "unknown", "present_ok"} {
		t.Run("report only "+state, func(t *testing.T) {
			in := input()
			in.Results[0].State = state
			p, err := Build(in)
			if err != nil || len(p.Steps) != 0 {
				t.Fatal("report-only state changed tool")
			}
		})
	}
}
