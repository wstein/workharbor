package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/skillset"
)

func TestSkillSetSelectionAndRootValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		selection skillset.Config
		problem   string
	}{
		{"none", skillset.Config{Selection: "none"}, ""},
		{"absent explicit default", skillset.Config{Selection: "default"}, "crewbook default is unconfigured"},
		{"unknown", skillset.Config{Selection: "latest"}, "unknown skill_set selection"},
		{"alternative without pin", skillset.Config{Selection: "package"}, "needs an external pin"},
		{"none with alternative", skillset.Config{Selection: "none", Package: &skillset.Pin{}}, "none cannot select a package"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rig := newRig(t)
			rig.cfg.SkillSet = test.selection
			err := rig.cfg.Validate()
			if test.problem == "" && err != nil || test.problem != "" && (err == nil || !strings.Contains(err.Error(), test.problem)) {
				t.Fatalf("configuration: %v", err)
			}
		})
	}
	rig := newRig(t)
	root := filepath.Join(rig.dir, "skills")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rig.cfg.SkillSet = skillset.Config{Selection: "none", Store: root}
	if err := rig.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	rig.cfg.SkillSet.Store = filepath.Dir(rig.cfg.AgentAPIKeyEnvFile)
	if err := rig.cfg.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps forbidden root") {
		t.Fatalf("credential directory accepted: %v", err)
	}
	rig.cfg.SkillSet.Store = rig.cfg.Roots.Workspaces[0]
	if err := rig.cfg.Validate(); err == nil {
		t.Fatal("workspace skill store accepted")
	}
}
