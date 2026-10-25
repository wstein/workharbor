//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func setStageFixtureHook(t *testing.T, source string, hook any) {
	t.Helper()
	path := filepath.Join(source, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["postCreateCommand"] = hook
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLocalStageNamedHookDependencyDriftRefusesPromotion(t *testing.T) {
	for _, side := range []string{"candidate", "original"} {
		t.Run(side, func(t *testing.T) {
			c, opts, ports, path := fixturePromotionLifecycle(t)
			opts.Smoke = true
			if err := os.Mkdir(filepath.Join(opts.Source, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opts.Source, "scripts", "setup.sh"), []byte("printf admitted-hook\\n\n"), 0600); err != nil {
				t.Fatal(err)
			}
			setStageFixtureHook(t, opts.Source, "sh scripts/setup.sh")
			s, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if err != nil || result.Status != "staged_verified" {
				t.Fatalf("stage: %+v %v", result, err)
			}
			root := s.clonePath
			reason := "promotion candidate execution inputs changed"
			if side == "original" {
				root = opts.Source
				reason = "promotion original execution inputs changed"
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "scripts", "setup.sh"), []byte("printf changed-hook\\n\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
				t.Fatal(err)
			}
			before := readFakePromotionState(t, path)
			p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64)}, ports)
			if p != nil {
				p.Close()
			}
			if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("%s named dependency drift: %v", side, err)
			}
			after := readFakePromotionState(t, path)
			if stageDigest(before.Primary) != stageDigest(after.Primary) || stageDigest(before.Candidate) != stageDigest(after.Candidate) || stageDigest(before.Volume) != stageDigest(after.Volume) {
				t.Fatal("dependency drift caused native lifecycle effect")
			}
		})
	}
}
func TestLocalStageUnknownGitExecutionIdentityRefusesBeforeEffects(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	setStageFixtureHook(t, opts.Source, "git rev-parse HEAD")
	s, err := c.prepareStage(context.Background(), opts, ports)
	if s != nil {
		s.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "Git execution identity capability unsupported") {
		t.Fatalf("unbound actual Git consumption: %v", err)
	}
	state := readFakeStageState(t, path)
	if state.Candidate != nil || state.Volume != nil {
		t.Fatal("unsupported Git identity caused allocation")
	}
	if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
		t.Fatal("unsupported Git identity created durable operation")
	}
}

func writeStageKnownDependencyFixture(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(stageDependencyBodyFixtures[name]), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLocalStageKnownBodyCatalogueRequiresRecursiveExactBytes(t *testing.T) {
	root := t.TempDir()
	writeStageKnownDependencyFixture(t, root, ".devcontainer/initialize.sh", "bundledock/scripts/bundle-volume.sh")
	config := map[string]any{"initializeCommand": []any{"sh", "${localWorkspaceFolder}/.devcontainer/initialize.sh", "image@sha256:" + strings.Repeat("a", 64), "bundle-volume", "toolbox"}}
	paths, mode, err := admitStageDependencies(context.Background(), root, root, "/workspaces/test", config, false, ".devcontainer/devcontainer.json")
	if err != nil || mode != "none" {
		t.Fatalf("known exact initializer: %s %v", mode, err)
	}
	found := false
	for _, path := range paths {
		found = found || path == "bundledock/scripts/bundle-volume.sh"
	}
	if !found {
		t.Fatal("known initializer omitted its transitive executable helper")
	}
	for _, name := range []string{".devcontainer/initialize.sh", "bundledock/scripts/bundle-volume.sh"} {
		known := stageKnownDependencies[name]
		if known.Digest != stageBytesDigest([]byte(stageDependencyBodyFixtures[name])) || known.Repository == "" || len(known.Revision) != 40 {
			t.Fatal("known body lost immutable provenance")
		}
	}
	if err = os.WriteFile(filepath.Join(root, "bundledock/scripts/bundle-volume.sh"), []byte(stageDependencyBodyFixtures["bundledock/scripts/bundle-volume.sh"]+"\neval altered_helper\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = admitStageDependencies(context.Background(), root, root, "/workspaces/test", config, false, ".devcontainer/devcontainer.json")
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "execution dependency capability unsupported") {
		t.Fatalf("changed opaque helper: %v", err)
	}
}
func TestLocalStageOpaqueAndDynamicDependencyFormsRefuse(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/opaque.sh"), []byte("cat $SELECTED_INPUT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []any{"printf \"$(cat scripts/input)\"", "eval 'printf literal'", "git rev-parse HEAD", "git status --short | grep file", []any{"sh", "scripts/opaque.sh"}, []any{"sh", "-c", "git rev-parse HEAD"}, "node scripts/app.js"} {
		_, _, err := admitStageDependencies(context.Background(), root, root, "/workspaces/test", map[string]any{"postCreateCommand": command}, false, ".devcontainer/devcontainer.json")
		if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "capability unsupported") {
			t.Fatalf("opaque %v: %v", command, err)
		}
	}
	for _, command := range []any{"printf '%s\\n' 'literal words'", []any{"printf", "%s", "literal argument"}, "echo literal", "true"} {
		if _, _, err := admitStageDependencies(context.Background(), root, root, "/workspaces/test", map[string]any{"postCreateCommand": command, "postAttachCommand": "opaque unused attach"}, false, ".devcontainer/devcontainer.json"); err != nil {
			t.Fatalf("literal %v: %v", command, err)
		}
	}
}
func TestLocalStageKnownGitConsumptionBindsReceiptThenCandidateCommit(t *testing.T) {
	c, opts, ports, path := fixturePromotionLifecycle(t)
	opts.Smoke = true
	writeStageKnownDependencyFixture(t, opts.Source, ".devcontainer/greeting-info.sh", ".devcontainer/pins-hash.sh")
	setStageFixtureHook(t, opts.Source, []any{"sh", ".devcontainer/greeting-info.sh", "commit"})
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	if s.gitInputMode != "known_identity" {
		t.Fatal("known consumed Git identity not classified")
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("known Git stage: %+v %v", result, err)
	}
	if len(s.execution.GitInputs) == 0 || s.execution.GitInputs[0].Predicate != "known_git_identity" {
		t.Fatal("receipt assigned exit-only predicate to actual Git identity")
	}
	clone := s.clonePath
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(clone, "file"), []byte("normal result commit consumes different HEAD\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, clone, "add", "file")
	fixtureGit(t, clone, "commit", "-m", "candidate result")
	if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	before := readFakePromotionState(t, path)
	p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64)}, ports)
	if p != nil {
		p.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "known Git execution version differs between original and candidate") {
		t.Fatalf("changed consumed HEAD: %v", err)
	}
	after := readFakePromotionState(t, path)
	if stageDigest(before.Primary) != stageDigest(after.Primary) || stageDigest(before.Candidate) != stageDigest(after.Candidate) || stageDigest(before.Volume) != stageDigest(after.Volume) {
		t.Fatal("consumed HEAD drift caused lifecycle effects")
	}
}

func TestLocalStageMergedOriginsCannotHideGitConsumption(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	state := readFakeStageState(t, path)
	state.MergedLifecycle = map[string][]any{"postCreateCommands": {"git rev-parse HEAD"}, "postAttachCommands": {"unexecuted opaque attach"}}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := c.prepareStage(context.Background(), opts, ports)
	if s != nil {
		s.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "Git execution identity capability unsupported") {
		t.Fatalf("merged image/Feature Git hook bypassed admission: %v", err)
	}
	if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
		t.Fatal("unknown merged hook created durable state")
	}
}
func TestLocalStageLateMergedOpaqueHookRetainsPartialBeforeHooks(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	state := readFakeStageState(t, path)
	state.LateMergedLifecycle = map[string][]any{"postCreateCommands": {"git rev-parse HEAD"}}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || err == nil || !strings.Contains(err.Error(), "lifecycle execution dependency capability unsupported") || result.Status != "partial" {
		t.Fatalf("late unknown hook: %+v %v", result, err)
	}
	state = readFakeStageState(t, path)
	if state.Candidate == nil || state.Volume == nil || result.Candidate == nil || result.Candidate.ID == nil {
		t.Fatal("late capability refusal lost observed resource inventory")
	}
	for _, call := range state.Calls {
		if len(call) > 0 && call[0] == "exec" {
			t.Fatal("opaque late hook or smoke ran before refusal")
		}
	}
}
