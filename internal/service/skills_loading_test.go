package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/skillset"
)

func TestPrepareSkillsRevalidatesRecordedMount(t *testing.T) {
	for _, scenario := range []string{"valid", "writable", "source", "target", "none", "tampered", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			selection, binding := skillFixture(t)
			called := false
			svc := &Service{cfg: Config{
				SkillSet: &selection,
				ProjectInstructions: func(context.Context, domain.Task, domain.Run) (ProjectInstructions, error) {
					return ProjectInstructions{Revision: "reviewed:1", Text: "policy", Inputs: map[string]bool{"project_policy": true}}, nil
				},
				SkillBinding:  func(context.Context, []skillset.Binding) (skillset.Binding, error) { return binding, nil },
				PrepareSkills: func(context.Context, domain.Run, SkillMount) error { called = true; return nil },
			}}
			spec := agent.StartSpec{Prompt: "task"}
			recorded, mount, err := svc.composeSkills(context.Background(), domain.Task{}, domain.Run{}, &spec, true)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "writable":
				mount.ReadOnly = false
			case "source":
				mount.Source = t.TempDir()
			case "target":
				mount.Target = "/workspace/skills"
			case "none":
				recorded.Mode = "none"
			case "tampered":
				filename := filepath.Join(mount.Source, "SKILL.md")
				if err := os.Chmod(filename, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(mount.Source, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			}
			selection.Selection, selection.Package = "none", nil
			err = svc.prepareSkills(context.Background(), domain.Run{Skills: recorded}, mount)
			valid := scenario == "valid"
			if (err == nil) != valid || called != valid {
				t.Fatalf("prepare: err=%v called=%v, want success=%v", err, called, valid)
			}
		})
	}
}
