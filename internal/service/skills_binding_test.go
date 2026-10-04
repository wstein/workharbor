package service

import (
	"context"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/skillset"
)

func TestSkillBindingCannotRewriteReviewedDeclarations(t *testing.T) {
	selection, binding := skillFixture(t)
	svc := &Service{cfg: Config{
		SkillSet: &selection,
		ProjectInstructions: func(context.Context, domain.Task, domain.Run) (ProjectInstructions, error) {
			return ProjectInstructions{Revision: "reviewed:1", Text: "policy", Inputs: map[string]bool{"project_policy": true}}, nil
		},
		SkillBinding: func(_ context.Context, declared []skillset.Binding) (skillset.Binding, error) {
			binding.Version = "unmeasured"
			declared[0] = binding
			return binding, nil
		},
	}}
	spec := agent.StartSpec{Prompt: "task"}
	if _, _, err := svc.composeSkills(context.Background(), domain.Task{}, domain.Run{}, &spec, true); err == nil || !strings.Contains(err.Error(), "exact declared") {
		t.Fatalf("provider rewrote the reviewed binding: %v", err)
	}
	if spec.Prompt != "task" {
		t.Fatal("refused binding changed the start instruction")
	}
}
