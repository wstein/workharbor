// SPDX-License-Identifier: EUPL-1.2
package catalogue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

func productionBytes(t *testing.T) ([]byte, map[string][]byte) {
	t.Helper()
	raw, err := os.ReadFile("assets/catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	templates := make(map[string][]byte)
	for _, name := range []string{"fish.fish", "starship.toml", "fzf.fish", "zoxide.fish"} {
		templates[name], err = os.ReadFile(filepath.Join("assets", "templates", name))
		if err != nil {
			t.Fatal(err)
		}
	}
	return raw, templates
}

func TestInitialCatalogueEntries(t *testing.T) {
	raw, templates := productionBytes(t)
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if err := catalogueSchema(t).Validate(document); err != nil {
		t.Fatal(err)
	}
	got, err := Load(raw, templates)
	if err != nil {
		t.Fatal(err)
	}
	floors := map[string]string{"orbstack": "2.2.3", "git": "2.56.0", "gh": "2.102.1", "jq": "1.8.2", "fish": "4.9.3", "starship": "1.27.0", "ripgrep": "15.2.0", "fd": "10.5.0", "fzf": "0.74.5", "zoxide": "0.10.0", "bat": "0.26.1", "jj": "0.46.0", "herdr": "0.9.5"}
	configs := map[string]Config{
		"fish":     {Marker: "shell_hook", Target: ".config/fish/conf.d/whr-fish.fish", ContentFile: "fish.fish", Mode: "0600"},
		"starship": {Marker: "dotfile", Target: ".config/starship.toml", ContentFile: "starship.toml", Mode: "0600"},
		"fzf":      {Marker: "shell_hook", Target: ".config/fish/conf.d/whr-fzf.fish", ContentFile: "fzf.fish", Mode: "0600"},
		"zoxide":   {Marker: "shell_hook", Target: ".config/fish/conf.d/whr-zoxide.fish", ContentFile: "zoxide.fish", Mode: "0600"},
	}
	if len(got.Items) != len(floors) {
		t.Fatalf("items=%d want13", len(got.Items))
	}
	for _, item := range got.Items {
		floor, ok := floors[item.ID]
		if !ok || item.Tool.MinVersion != floor {
			t.Errorf("%s floor=%s want%s", item.ID, item.Tool.MinVersion, floor)
		}
		if !item.SelectedByDefault || len(item.Requires) != 0 {
			t.Errorf("%s must be independently selectable", item.ID)
		}
		if !reflect.DeepEqual(item.Platforms, []string{"darwin-arm64", "darwin-amd64"}) {
			t.Errorf("%s platforms=%v", item.ID, item.Platforms)
		}
		if len(item.Privileged) != 0 {
			t.Errorf("%s unexpected privileged commands", item.ID)
		}
		expected, configured := configs[item.ID]
		if configured != (item.Config != nil) {
			t.Errorf("%s config marker mismatch", item.ID)
			continue
		}
		if configured {
			actual := *item.Config
			actual.SHA256 = ""
			if actual != expected {
				t.Errorf("%s config=%+v want%+v", item.ID, actual, expected)
			}
		}
		wantSource := Source{Manager: "homebrew", Type: "formula", Name: item.ID, Tap: "homebrew/core"}
		if item.ID == "orbstack" {
			wantSource.Type = "cask"
			wantSource.Tap = "homebrew/cask"
			wantSource.MayElevate = true
		}
		if item.Tool.Source != wantSource {
			t.Errorf("%s source=%+v want%+v", item.ID, item.Tool.Source, wantSource)
		}
	}
}

func TestInitialCatalogueKnownVersionOutputs(t *testing.T) {
	raw, templates := productionBytes(t)
	got, err := Load(raw, templates)
	if err != nil {
		t.Fatal(err)
	}
	// Version values are owner observations; surrounding text exercises the fixed
	// parser shapes of the owner-run read-only script, not captured raw stdout.
	outputs := map[string]string{"orbstack": "2.2.4\n", "git": "git version 2.56.0", "gh": "gh version 2.102.1 (date)", "jq": "jq-1.8.2", "fish": "fish, version 4.9.3", "starship": "starship 1.27.0", "ripgrep": "ripgrep 15.2.0", "fd": "fd 10.5.0", "fzf": "0.74.5 (brew)", "zoxide": "zoxide 0.10.0", "bat": "bat 0.26.1", "jj": "jj 0.46.0", "herdr": "herdr 0.9.5"}
	for _, item := range got.Items {
		if item.ID == "orbstack" {
			if item.Kind != "runtime" || item.Tool.VersionMetadata != OrbStackAppMetadata || item.Tool.Command != "" || len(item.Tool.VersionArgs) != 0 {
				t.Error("OrbStack must use the closed application metadata route")
			}
		} else {
			if item.Kind != "tool" || item.Tool.VersionMetadata != "" {
				t.Errorf("%s must use the command route", item.ID)
			}
			command := item.ID
			if item.ID == "ripgrep" {
				command = "rg"
			}
			if item.Tool.Command != command || !reflect.DeepEqual(item.Tool.VersionArgs, []string{"--version"}) {
				t.Errorf("%s argv=%s %v", item.ID, item.Tool.Command, item.Tool.VersionArgs)
			}
		}
		pattern := regexp.MustCompile(item.Tool.VersionRegex)
		match := pattern.FindStringSubmatch(outputs[item.ID])
		expected := item.Tool.MinVersion
		if item.ID == "orbstack" {
			expected = "2.2.4"
		}
		if len(match) != 2 || match[1] != expected {
			t.Errorf("%s parsed=%v want%s", item.ID, match, expected)
		}
		if pattern.MatchString("unrelated 99.99.99") {
			t.Errorf("%s accepts unrelated output", item.ID)
		}
		if item.ID == "orbstack" && pattern.MatchString("2.2.3,20963") {
			t.Error("Homebrew registration accepted as application metadata")
		}
	}
}

func TestInitialCatalogueSnapshotIsIndependent(t *testing.T) {
	raw, templates := productionBytes(t)
	expected, err := Load(raw, templates)
	if err != nil {
		t.Fatal(err)
	}
	first, err := LoadInitial()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, expected) {
		t.Fatal("embedded snapshot differs from production assets")
	}
	first.Items[0].Tool.MinVersion = "0"
	first.Items[0].Network.Hosts[0] = "mutated.invalid"
	first.Templates["fish.fish"][0] ^= 1
	delete(first.Templates, "fzf.fish")
	second, err := LoadInitial()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, expected) {
		t.Fatal("caller mutation changed embedded snapshot")
	}
}
