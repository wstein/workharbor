package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/skillset"
)

func skillFixture(t *testing.T) (skillset.Config, skillset.Binding) {
	t.Helper()
	source := t.TempDir()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "Alternative workflow instructions.\n"
	files := []skillset.File{{Path: "SKILL.md", SHA256: skillset.Digest([]byte(text))}}
	binding := skillset.Binding{Name: "test", Version: "1.0.0", Model: "model", Effort: "low"}
	manifest := skillset.Manifest{ContractVersion: 1, Identity: "alternative", Entrypoint: "SKILL.md", RequiredProjectInputs: []string{"project_policy"}, Adapters: []skillset.Binding{binding}, Files: files}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{skillset.ManifestName: data, "SKILL.md": []byte(text)} {
		if err := os.WriteFile(filepath.Join(source, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := skillset.InventoryDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	pin := skillset.Pin{Identity: "alternative", Source: "https://example.test/alternative", Commit: strings.Repeat("a", 40), ManifestSHA256: skillset.Digest(data), InventorySHA256: digest, ContractVersion: 1}
	if _, err := (skillset.Store{Root: root}).Install(source, pin); err != nil {
		t.Fatal(err)
	}
	return skillset.Config{Store: root, Selection: "package", Package: &pin}, binding
}

func TestExternalSkillsRestoreTheRecordedPin(t *testing.T) {
	selection, binding := skillFixture(t)
	project := ProjectInstructions{Revision: "reviewed:1", Text: "Project policy.", Inputs: map[string]bool{"project_policy": true}}
	svc := &Service{cfg: Config{
		SkillSet:            &selection,
		ProjectInstructions: func(context.Context, domain.Task, domain.Run) (ProjectInstructions, error) { return project, nil },
		SkillBinding:        func(context.Context, []skillset.Binding) (skillset.Binding, error) { return binding, nil },
	}}
	spec := agent.StartSpec{Prompt: "task", PermissionMode: agent.PermissionDontAsk, AllowedTools: []string{"Read"}}
	recorded, mount, err := svc.composeSkills(bg, domain.Task{}, domain.Run{}, &spec, true)
	if err != nil {
		t.Fatal(err)
	}
	if mount == nil || !mount.ReadOnly || mount.Target != "/skills/"+recorded.InventorySHA256 || !strings.Contains(spec.Prompt, "Alternative workflow") || spec.Mode() != agent.PermissionDontAsk || len(spec.AllowedTools) != 1 {
		t.Fatalf("composition changed controls or omitted skills: %+v", spec)
	}
	if recorded.ProjectSHA256 == recorded.InstructionSHA256 || recorded.InstructionSHA256 != skillset.Digest([]byte(spec.Prompt)) {
		t.Fatal("instruction provenance not separate or exact")
	}
	if err := svc.prepareSkills(bg, domain.Run{}, mount); err == nil {
		t.Fatal("package launched without runtime integration")
	}
	selection.Selection, selection.Package = "none", nil
	resumed := agent.StartSpec{Prompt: "resume"}
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{Skills: recorded}, &resumed, false); err != nil || !strings.Contains(resumed.Prompt, "Alternative workflow") {
		t.Fatalf("resume switched to changed default: %v", err)
	}
	project.Text = "changed"
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{Skills: recorded}, &resumed, false); err == nil {
		t.Fatal("changed project policy accepted on resume")
	}
	project.Text = "Project policy."
	binding.Model = "different"
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{Skills: recorded}, &resumed, false); err == nil {
		t.Fatal("model drift accepted")
	}
}

func TestSkillPrerequisitesAndTamperingRefuseComposition(t *testing.T) {
	selection, binding := skillFixture(t)
	project := ProjectInstructions{Revision: "reviewed:1", Text: "policy"}
	svc := &Service{cfg: Config{SkillSet: &selection, ProjectInstructions: func(context.Context, domain.Task, domain.Run) (ProjectInstructions, error) { return project, nil }, SkillBinding: func(context.Context, []skillset.Binding) (skillset.Binding, error) { return binding, nil }}}
	spec := agent.StartSpec{Prompt: "task"}
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{}, &spec, true); err == nil {
		t.Fatal("missing required policy input accepted")
	}
	project.Inputs = map[string]bool{"project_policy": true}
	installed, err := (skillset.Store{Root: selection.Store}).Load(*selection.Package)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(installed.Directory, "SKILL.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{}, &spec, true); err == nil {
		t.Fatal("tampered package accepted")
	}
}

func TestNoneAndLegacyComposition(t *testing.T) {
	missing := &Service{}
	unconfigured := agent.StartSpec{Prompt: "task"}
	if _, _, err := missing.composeSkills(bg, domain.Task{}, domain.Run{}, &unconfigured, true); err == nil {
		t.Fatal("absent provider silently marked a fresh run legacy")
	}
	selection := skillset.Config{Selection: "none"}
	svc := &Service{cfg: Config{SkillSet: &selection}}
	spec := agent.StartSpec{Prompt: "task", PermissionMode: agent.PermissionManual}
	recorded, mount, err := svc.composeSkills(bg, domain.Task{}, domain.Run{}, &spec, true)
	if err != nil || recorded.Mode != "none" || mount != nil || !strings.Contains(spec.Prompt, "Supervisor policy") || spec.Mode() != agent.PermissionManual {
		t.Fatalf("none composition: %+v, %v", recorded, err)
	}
	selection.Selection = ""
	legacy := agent.StartSpec{Prompt: "protected legacy briefing", PermissionMode: agent.PermissionManual}
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{Skills: domain.SkillSelection{Mode: "legacy"}}, &legacy, false); err != nil || legacy.Prompt != "protected legacy briefing" {
		t.Fatalf("legacy changed: %v", err)
	}
	if _, _, err := svc.composeSkills(bg, domain.Task{}, domain.Run{}, &spec, true); err == nil {
		t.Fatal("absent default became none")
	}
}

func TestNoneSelectionIsSavedBeforeLaunchAndSurvivesResume(t *testing.T) {
	r := newWsRig(t)
	r.svc.cfg.SkillSet = &skillset.Config{Selection: "none"}
	_, profile := r.create("skills-none")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: profile.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.svc.Show(bg, task)
	if err != nil || len(view.Runs) != 1 || view.Runs[0].Skills.Mode != "none" || view.Runs[0].Skills.InstructionSHA256 == "" {
		t.Fatalf("missing durable provenance: %+v, %v", view, err)
	}
	events, err := r.store.EventsSince(bg, task, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == domain.EventRunSkills {
			found = true
		}
	}
	if !found {
		t.Fatal("selection was not audited")
	}
	eventually(t, func() bool { view, err := r.svc.Show(bg, task); return err == nil && view.Runs[0].SessionID != "" })
	if err := r.svc.Pause(bg, task); err != nil {
		t.Fatal(err)
	}
	r.svc.Wait()
	r.svc.cfg.SkillSet.Selection = "default"
	if got, err := r.svc.Resume(bg, task); err != nil || got != run {
		t.Fatalf("recorded none failed after default changed: %s, %v", got, err)
	}
}

func TestAlternativeSelectionIsDurableAndTamperedResumeLaunchesNothing(t *testing.T) {
	r := newWsRig(t)
	selection, binding := skillFixture(t)
	r.svc.cfg.SkillSet = &selection
	r.svc.cfg.ProjectInstructions = func(context.Context, domain.Task, domain.Run) (ProjectInstructions, error) {
		return ProjectInstructions{Revision: "reviewed:1", Text: "Project policy.", Inputs: map[string]bool{"project_policy": true}}, nil
	}
	r.svc.cfg.SkillBinding = func(context.Context, []skillset.Binding) (skillset.Binding, error) { return binding, nil }
	prepared := false
	r.svc.cfg.PrepareSkills = func(ctx context.Context, run domain.Run, mount SkillMount) error {
		aggregate, err := r.store.LoadTask(ctx, run.TaskID)
		if err != nil {
			return err
		}
		stored, ok := aggregate.Run(run.ID)
		if !ok || stored.Skills != run.Skills || mount.Target != "/skills/"+stored.Skills.InventorySHA256 || !mount.ReadOnly {
			t.Error("mount called before durable exact selection")
		}
		prepared = true
		return nil
	}
	_, profile := r.create("skills-alternative")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: profile.ID, Issue: "#1"})
	if err != nil || !prepared {
		t.Fatalf("alternative start: %v", err)
	}
	eventually(t, func() bool { view, err := r.svc.Show(bg, task); return err == nil && view.Runs[0].SessionID != "" })
	view, err := r.svc.Show(bg, task)
	if err != nil {
		t.Fatal(err)
	}
	recorded := view.Runs[0].Skills
	if err := r.svc.Pause(bg, task); err != nil {
		t.Fatal(err)
	}
	r.svc.Wait()
	selection.Selection, selection.Package = "none", nil
	installed, err := (skillset.Store{Root: selection.Store}).Load(skillset.Pin{Identity: recorded.Identity, Source: recorded.Source, Commit: recorded.Commit, ManifestSHA256: recorded.ManifestSHA256, InventorySHA256: recorded.InventorySHA256, ContractVersion: recorded.ContractVersion})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(installed.Directory, "SKILL.md")
	if err := os.Chmod(filename, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := r.agent.Started()
	if _, err := r.svc.Resume(bg, task); err == nil {
		t.Fatal("tampered recorded package resumed")
	}
	if r.agent.Started() != before {
		t.Fatal("agent launched after failed revalidation")
	}
	view, err = r.svc.Show(bg, task)
	if err != nil || view.Runs[0].ID != run || view.Runs[0].Skills != recorded {
		t.Fatalf("failure overwrote provenance: %+v, %v", view, err)
	}
}
