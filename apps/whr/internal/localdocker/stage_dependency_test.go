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

func TestLocalStageAmbiguousLiteralDependenciesRefuseBeforeEffects(t *testing.T) {
	t.Run("escaped_line_break", func(t *testing.T) {
		_, err := stageLiteralCommand("sh scripts/policy\\\nname.sh")
		if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "command continuation dependency capability unsupported") {
			t.Fatalf("escaped line break: %v", err)
		}
	})
	t.Run("scalar_path_substitution", func(t *testing.T) {
		execution := t.TempDir()
		if err := os.WriteFile(filepath.Join(execution, "simple.sh"), []byte("true\n"), 0700); err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(t.TempDir(), "original with space")
		_, _, err := admitStageDependencies(context.Background(), execution, original, "/workspaces/test", map[string]any{"postCreateCommand": "sh ${localWorkspaceFolder}/simple.sh"}, false, ".devcontainer/devcontainer.json")
		if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "workspace path substitution dependency capability unsupported") {
			t.Fatalf("scalar phase path: %v", err)
		}
		spaced := filepath.Join(t.TempDir(), "execution with space")
		if err = os.Mkdir(spaced, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(spaced, "simple.sh"), []byte("true\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, _, err = admitStageDependencies(context.Background(), spaced, original, "/workspaces/test", map[string]any{"initializeCommand": []any{"sh", "${localWorkspaceFolder}/simple.sh"}}, false, ".devcontainer/devcontainer.json"); err != nil {
			t.Fatalf("literal argv path: %v", err)
		}
	})
	t.Run("plain_interpreter", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "simple.sh"), []byte("#!/bin/sh\ntrue\n"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"sh simple.sh", "./simple.sh"} {
			paths, _, err := admitStageDependencies(context.Background(), root, root, "/workspaces/test", map[string]any{"postCreateCommand": command}, false, ".devcontainer/devcontainer.json")
			if err != nil {
				t.Fatalf("plain interpreter: %v", err)
			}
			found := false
			for _, path := range paths {
				found = found || path == "simple.sh"
			}
			if !found {
				t.Fatal("plain script missing execution manifest path")
			}
		}
	})
	for _, form := range []string{"quoted_escape", "shebang_arguments", "shebang_python", "direct_without_interpreter", "direct_after_explicit_shell", "carriage_return", "literal_marker", "literal_marker_argv", "literal_marker_script", "literal_marker_concat", "literal_marker_escape"} {
		t.Run(form, func(t *testing.T) {
			c, opts, ports, statePath := fixtureStageLifecycle(t, false)
			if err := os.Mkdir(filepath.Join(opts.Source, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			var command any = `sh "scripts/policy\name.sh"`
			files := map[string]string{"scripts/policyname.sh": "true\n", "scripts/policy\\name.sh": "true\n"}
			reason := "quoted command escape dependency capability unsupported"
			if form == "shebang_arguments" {
				command = "./scripts/entry"
				files = map[string]string{"scripts/entry": "#!/usr/bin/env -S sh scripts/child.sh\ntrue\n", "scripts/child.sh": "true\n"}
				reason = "script interpreter dependency capability unsupported"
			}
			if form == "shebang_python" {
				command = "./scripts/entry"
				files = map[string]string{"scripts/entry": "#!/usr/bin/python3\ntrue\n"}
				reason = "script interpreter dependency capability unsupported"
			}
			if form == "direct_without_interpreter" {
				command = "./scripts/entry"
				files = map[string]string{"scripts/entry": "true\n"}
				reason = "script interpreter dependency capability unsupported"
			}
			if form == "direct_after_explicit_shell" {
				command = map[string]any{"a": "sh scripts/entry", "b": "./scripts/entry"}
				files = map[string]string{"scripts/entry": "true\n"}
				reason = "script interpreter dependency capability unsupported"
			}
			if form == "carriage_return" {
				command = "sh scripts/entry"
				files = map[string]string{"scripts/entry": "#!/bin/sh\nsh scripts/child.sh\r\n", "scripts/child.sh": "true\n", "scripts/child.sh\r": "true\n"}
				reason = "execution dependency capability unsupported"
			}
			if strings.HasPrefix(form, "literal_marker") {
				command = "sh @workspace@/scripts/actual.sh"
				files = map[string]string{"scripts/actual.sh": "true\n", "@workspace@/scripts/actual.sh": "true\n"}
				reason = "reserved workspace marker dependency capability unsupported"
				if form == "literal_marker_concat" {
					command = `sh "@""workspace@/scripts/actual.sh"`
				}
				if form == "literal_marker_escape" {
					command = `sh \@workspace\@/scripts/actual.sh`
				}
				if form == "literal_marker_argv" {
					command = []any{"sh", "@workspace@/scripts/actual.sh"}
				}
				if form == "literal_marker_script" {
					command = "sh scripts/entry"
					files["scripts/entry"] = "#!/bin/sh\nsh @workspace@/scripts/actual.sh\n"
				}
			}
			for name, data := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(opts.Source, name)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(opts.Source, name), []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			setStageFixtureHook(t, opts.Source, command)
			stage, err := c.prepareStage(context.Background(), opts, ports)
			if stage != nil {
				stage.Close()
			}
			if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("%s ambiguous dependency: %v", form, err)
			}
			state := readFakeStageState(t, statePath)
			if state.Candidate != nil || state.Volume != nil {
				t.Fatal("unsupported dependency allocated lifecycle resources")
			}
			if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
				t.Fatal("unsupported dependency wrote retained state")
			}
		})
	}
}

func setStageFixtureInitialize(t *testing.T, source string, command any) {
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
	cfg["initializeCommand"] = command
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLocalStageExecutionRootsMatchActualPhase(t *testing.T) {
	t.Run("captured_initializer_override_and_labels", func(t *testing.T) {
		c, opts, ports, statePath := fixtureStageLifecycle(t, false)
		if err := os.Mkdir(filepath.Join(opts.Source, "scripts"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(opts.Source, "scripts", "host.sh"), []byte("true\n"), 0600); err != nil {
			t.Fatal(err)
		}
		setStageFixtureInitialize(t, opts.Source, []any{"sh", "${localWorkspaceFolder}/scripts/host.sh"})
		stage, err := c.prepareStage(context.Background(), opts, ports)
		if err != nil {
			t.Fatal(err)
		}
		defer stage.Close()
		data, err := os.ReadFile(stage.invocation.Override)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err = json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		command := cfg["initializeCommand"].([]any)
		if len(command) != 2 || command[1] != "${localWorkspaceFolder}/scripts/host.sh" {
			t.Fatalf("override changed source command: %v", command)
		}
		expected := "Run: sh " + filepath.Join(stage.capture.Path, "scripts", "host.sh")
		if len(stage.initializeLabels) != 1 || stage.initializeLabels[0] != expected || strings.Contains(stage.initializeLabels[0], opts.Source) {
			t.Fatalf("observed initializer did not resolve captured E: %v", stage.initializeLabels)
		}
		state := readFakeStageState(t, statePath)
		if state.Candidate != nil || state.Volume != nil {
			t.Fatal("preview executed initializer resources")
		}
		if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
			t.Fatal("preview wrote retained state")
		}
	})
	t.Run("exact_phase_argv_with_spaces", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "captured space")
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "child.sh"), []byte("true\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg := map[string]any{"initializeCommand": []any{"sh", "${localWorkspaceFolder}/child.sh"}, "postCreateCommand": []any{"sh", "${containerWorkspaceFolder}/child.sh"}}
		if _, _, err := admitStageDependencies(context.Background(), root, "/original space", "/workspaces/container space", cfg, false, ".devcontainer/devcontainer.json"); err != nil {
			t.Fatalf("exact phase argv: %v", err)
		}
	})
	t.Run("nested_shell_is_not_configuration", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "child.sh"), []byte("true\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "entry.sh"), []byte("sh -c 'sh ${localWorkspaceFolder}/child.sh'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err := admitStageDependencies(context.Background(), root, "/original", "/workspaces/test", map[string]any{"initializeCommand": []any{"sh", filepath.Join(root, "entry.sh")}}, false, ".devcontainer/devcontainer.json")
		if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "execution dependency capability unsupported") {
			t.Fatalf("script payload treated as configuration substitution: %v", err)
		}
	})
	for _, form := range []string{"host_original", "host_nested_original", "host_container", "container_original", "container_nested_original"} {
		t.Run(form, func(t *testing.T) {
			c, opts, ports, statePath := fixtureStageLifecycle(t, false)
			if err := os.Mkdir(filepath.Join(opts.Source, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(opts.Source, "scripts", "host.sh")
			if err := os.WriteFile(original, []byte("true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var command any = []any{"sh", original}
			if strings.Contains(form, "nested") {
				if err := os.WriteFile(filepath.Join(opts.Source, "scripts", "entry.sh"), []byte("sh "+stageShellLiteral(original)+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				command = []any{"sh", "scripts/entry.sh"}
			}
			reason := "host initialization execution root capability unsupported"
			if form == "host_container" {
				command = []any{"sh", "/workspaces/Anchorage/scripts/host.sh"}
			}
			if strings.HasPrefix(form, "container") {
				setStageFixtureHook(t, opts.Source, command)
				reason = "container execution root capability unsupported"
			} else {
				setStageFixtureInitialize(t, opts.Source, command)
			}
			stage, err := c.prepareStage(context.Background(), opts, ports)
			if stage != nil {
				stage.Close()
			}
			if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("%s phase alias: %v", form, err)
			}
			state := readFakeStageState(t, statePath)
			if state.Candidate != nil || state.Volume != nil {
				t.Fatal("phase alias caused lifecycle resource effects")
			}
			if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
				t.Fatal("phase alias wrote retained state")
			}
		})
	}
	t.Run("phase_qualified_cache", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "child.sh"), []byte("true\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "entry.sh"), []byte("sh "+stageShellLiteral(filepath.Join(root, "child.sh"))+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg := map[string]any{"initializeCommand": []any{"sh", filepath.Join(root, "entry.sh")}, "postCreateCommand": []any{"sh", "entry.sh"}}
		_, _, err := admitStageDependencies(context.Background(), root, "/original", "/workspaces/test", cfg, false, ".devcontainer/devcontainer.json")
		if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "container execution root capability unsupported") {
			t.Fatalf("host proof reused for container script: %v", err)
		}
	})
}
