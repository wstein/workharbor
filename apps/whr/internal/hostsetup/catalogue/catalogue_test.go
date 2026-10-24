// SPDX-License-Identifier: EUPL-1.2
package catalogue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// All fixture versions and destinations are synthetic; this is not the shipped
// catalogue and does not establish any owner's Mac minimum version.
func syntheticCatalogue(t *testing.T) (map[string]any, map[string][]byte) {
	t.Helper()
	content := []byte("# synthetic catalogue fixture\n")
	digest := sha256.Sum256(content)
	item := map[string]any{
		"id": "sample", "kind": "tool", "title": "Synthetic fixture",
		"platforms": []any{"darwin-arm64"}, "selected_by_default": true,
		"network": map[string]any{"hosts": []any{"fixture.invalid"}, "purpose": "Synthetic test", "lookup_urls": []any{"https://fixture.invalid/version"}},
		"tool":    map[string]any{"command": "sample", "version_args": []any{"--version"}, "version_regex": `([0-9]+\.[0-9]+)`, "min_version": "1.0", "source": map[string]any{"manager": "homebrew", "type": "formula", "name": "sample", "tap": "homebrew/core", "may_elevate": false}},
		"config":  map[string]any{"marker": "dotfile", "target": ".config/sample/config", "content_file": "sample.txt", "sha256": hex.EncodeToString(digest[:]), "mode": "0600"},
	}
	return map[string]any{"schema_version": 1, "catalogue_version": "2026.10.1", "items": []any{item}}, map[string][]byte{"sample.txt": content}
}

func TestCatalogueLoadsAndValidates(t *testing.T) {
	document, templates := syntheticCatalogue(t)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(raw, templates)
	if err != nil {
		t.Fatalf("valid synthetic catalogue refused: %v", err)
	}
	if got == nil {
		t.Fatal("nil loaded catalogue")
	}
}

func TestCatalogueRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any, map[string][]byte)
	}{
		{"unknown_root", func(d map[string]any, _ map[string][]byte) { d["unknown"] = true }},
		{"schema_boolean", func(d map[string]any, _ map[string][]byte) { d["schema_version"] = true }},
		{"unsupported_schema", func(d map[string]any, _ map[string][]byte) { d["schema_version"] = 2 }},
		{"catalogue_version", func(d map[string]any, _ map[string][]byte) { d["catalogue_version"] = "latest" }},
		{"missing_items", func(d map[string]any, _ map[string][]byte) { delete(d, "items") }},
		{"empty_items", func(d map[string]any, _ map[string][]byte) { d["items"] = []any{} }},
		{"unknown_item", func(d map[string]any, _ map[string][]byte) { first(d)["unknown"] = true }},
		{"duplicate_id", func(d map[string]any, _ map[string][]byte) { d["items"] = []any{first(d), first(d)} }},
		{"missing_dependency", func(d map[string]any, _ map[string][]byte) { first(d)["requires"] = []any{"missing"} }},
		{"self_cycle", func(d map[string]any, _ map[string][]byte) { first(d)["requires"] = []any{"sample"} }},
		{"parent_target", func(d map[string]any, _ map[string][]byte) { config(d)["target"] = ".config/../sample" }},
		{"double_slash_target", func(d map[string]any, _ map[string][]byte) { config(d)["target"] = ".config//sample" }},
		{"absolute_target", func(d map[string]any, _ map[string][]byte) { config(d)["target"] = "/tmp/sample" }},
		{"template_escape", func(d map[string]any, _ map[string][]byte) { config(d)["content_file"] = "../sample.txt" }},
		{"missing_template", func(_ map[string]any, templates map[string][]byte) { delete(templates, "sample.txt") }},
		{"hash_mismatch", func(_ map[string]any, templates map[string][]byte) { templates["sample.txt"] = []byte("changed") }},
		{"tap_type_mismatch", func(d map[string]any, _ map[string][]byte) { source(d)["tap"] = "homebrew/cask" }},
		{"unknown_manager", func(d map[string]any, _ map[string][]byte) { source(d)["manager"] = "other" }},
		{"agent_tool", func(d map[string]any, _ map[string][]byte) {
			first(d)["id"] = "codex"
			tool(d)["command"] = "codex"
			source(d)["name"] = "codex"
		}},
		{"undeclared_lookup_host", func(d map[string]any, _ map[string][]byte) {
			first(d)["network"].(map[string]any)["lookup_urls"] = []any{"https://elsewhere.invalid/version"}
		}},
		{"invalid_regexp", func(d map[string]any, _ map[string][]byte) { tool(d)["version_regex"] = "(" }},
		{"non_go_regexp", func(d map[string]any, _ map[string][]byte) { tool(d)["version_regex"] = "(?=v)" }},
		{"bad_minimum", func(d map[string]any, _ map[string][]byte) { tool(d)["min_version"] = "latest" }},
		{"unknown_tool_field", func(d map[string]any, _ map[string][]byte) { tool(d)["unknown"] = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			document, templates := syntheticCatalogue(t)
			tc.change(document, templates)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(raw, templates)
			if err == nil {
				t.Fatal("invalid catalogue accepted")
			}
			if got := exitcode.FromError(err); got != exitcode.Internal {
				t.Fatalf("invalid catalogue exit=%d, want74", got)
			}
		})
	}
	document, templates := syntheticCatalogue(t)
	valid, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"duplicate_root_key":          bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1),
		"duplicate_nested_key":        bytes.Replace(valid, []byte(`"command":"sample"`), []byte(`"command":"sample","command":"sample"`), 1),
		"fractional_schema_spelling":  bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":1.0`), 1),
		"exponential_schema_spelling": bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":1e0`), 1),
		"trailing_document":           append(bytes.Clone(valid), []byte(` {}`)...),
		"invalid_utf8":                bytes.Replace(valid, []byte("Synthetic fixture"), []byte{0xff}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(raw, templates); err == nil {
				t.Fatal("invalid JSON input accepted")
			}
		})
	}

}

func first(d map[string]any) map[string]any  { return d["items"].([]any)[0].(map[string]any) }
func tool(d map[string]any) map[string]any   { return first(d)["tool"].(map[string]any) }
func config(d map[string]any) map[string]any { return first(d)["config"].(map[string]any) }
func source(d map[string]any) map[string]any { return tool(d)["source"].(map[string]any) }

// realisticCatalogue13 is a TEST ONLY inline snapshot using the real thirteen
// program identities. Every minimum is synthetic 1.0, not a measured support floor.
// Unobserved version argv, template paths and shell-hook comments are fixture choices.
func realisticCatalogue13() ([]byte, map[string][]byte) {
	return []byte(`{"schema_version":1,"catalogue_version":"2026.10.1","items":[{"id":"orbstack","kind":"runtime","title":"OrbStack","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"orb","version_args":["version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"cask","name":"orbstack","tap":"homebrew/cask","may_elevate":false}}},{"id":"git","kind":"tool","title":"git","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"git","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"git","tap":"homebrew/core","may_elevate":false}}},{"id":"gh","kind":"tool","title":"GitHub CLI","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"gh","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"gh","tap":"homebrew/core","may_elevate":false}}},{"id":"jq","kind":"tool","title":"jq","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"jq","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"jq","tap":"homebrew/core","may_elevate":false}}},{"id":"fish","kind":"tool","title":"fish","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"fish","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"fish","tap":"homebrew/core","may_elevate":false}},"config":{"marker":"shell_hook","target":".config/fish/conf.d/whr-fish.fish","content_file":"fish.fish","sha256":"c8924206c471ca75f2c61a356550a4ee3b7a48cb3bb37c2b9ea9e13add8529be","mode":"0600"}},{"id":"starship","kind":"tool","title":"starship","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"starship","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"starship","tap":"homebrew/core","may_elevate":false}},"config":{"marker":"dotfile","target":".config/starship.toml","content_file":"starship.toml","sha256":"0fbf196b3612d0bafdaaa79b45efb1d03c813bedc53bbe3e6e0f5550ec14683f","mode":"0600"}},{"id":"ripgrep","kind":"tool","title":"ripgrep","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"rg","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"ripgrep","tap":"homebrew/core","may_elevate":false}}},{"id":"fd","kind":"tool","title":"fd","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"fd","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"fd","tap":"homebrew/core","may_elevate":false}}},{"id":"fzf","kind":"tool","title":"fzf","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"fzf","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"fzf","tap":"homebrew/core","may_elevate":false}},"config":{"marker":"shell_hook","target":".config/fish/conf.d/whr-fzf.fish","content_file":"fzf.fish","sha256":"76610ba70fe12821095ad10b08d1611523aa25fd8f179f50f233460cfe4d2cde","mode":"0600"}},{"id":"zoxide","kind":"tool","title":"zoxide","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"zoxide","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"zoxide","tap":"homebrew/core","may_elevate":false}},"config":{"marker":"shell_hook","target":".config/fish/conf.d/whr-zoxide.fish","content_file":"zoxide.fish","sha256":"28f96e85bcc74415d23b2345848443480aa26235784b2bedba0080c2396addce","mode":"0600"}},{"id":"bat","kind":"tool","title":"bat","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"bat","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"bat","tap":"homebrew/core","may_elevate":false}}},{"id":"jj","kind":"tool","title":"jj","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"jj","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"jj","tap":"homebrew/core","may_elevate":false}}},{"id":"herdr","kind":"tool","title":"herdr","platforms":["darwin-arm64","darwin-amd64"],"selected_by_default":true,"network":{"hosts":["fixture.invalid"],"purpose":"TEST ONLY declared destination; no lookup performed"},"tool":{"command":"herdr","version_args":["--version"],"version_regex":"([0-9]+\\.[0-9]+(?:\\.[0-9]+)?)","min_version":"1.0","source":{"manager":"homebrew","type":"formula","name":"herdr","tap":"homebrew/core","may_elevate":false}}}]}`), map[string][]byte{
		"starship.toml": []byte("add_newline = false\n"),
		"fish.fish":     []byte("# TEST ONLY fish drop-in bytes\n"),
		"fzf.fish":      []byte("# TEST ONLY fzf hook bytes\n"),
		"zoxide.fish":   []byte("# TEST ONLY zoxide hook bytes\n"),
	}
}

func TestCatalogueLoadsAndValidatesRealistic13Fixture(t *testing.T) {
	raw, templates := realisticCatalogue13()
	got, err := Load(raw, templates)
	if err != nil || got == nil {
		t.Fatalf("TEST ONLY thirteen-entry fixture failed: %v", err)
	}
	if len(got.Items) != 13 {
		t.Fatalf("TEST ONLY initial fixture has %d entries, want 13", len(got.Items))
	}
	for _, item := range got.Items {
		if !item.SelectedByDefault {
			t.Errorf("owner's initial thirteen-entry default missing for %s", item.ID)
		}
	}
}

// Existing test-only schema dependency checks the mirror against the actual
// contract. This neither installs a dependency nor fetches schema references.
func catalogueSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "specs", "009-host-setup-wizard", "contracts", "host-tool-catalogue.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	const url = "https://workharbor.invalid/host-tool-catalogue.schema.json"
	if err = c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestCatalogueSchemaConformance(t *testing.T) {
	schema := catalogueSchema(t)
	type mutation func(map[string]any)
	cases := map[string]mutation{
		"unicode_title_limit":   func(d map[string]any) { first(d)["title"] = strings.Repeat("é", 81) },
		"invalid_id":            func(d map[string]any) { first(d)["id"] = "X" },
		"unknown_kind":          func(d map[string]any) { first(d)["kind"] = "agent" },
		"no_platform":           func(d map[string]any) { first(d)["platforms"] = []any{} },
		"duplicate_platform":    func(d map[string]any) { first(d)["platforms"] = []any{"darwin-arm64", "darwin-arm64"} },
		"unsupported_platform":  func(d map[string]any) { first(d)["platforms"] = []any{"linux-amd64"} },
		"bad_requires_id":       func(d map[string]any) { first(d)["requires"] = []any{"X"} },
		"network_hosts_empty":   func(d map[string]any) { first(d)["network"].(map[string]any)["hosts"] = []any{} },
		"network_host_syntax":   func(d map[string]any) { first(d)["network"].(map[string]any)["hosts"] = []any{"fixture.invalid:443"} },
		"network_purpose_limit": func(d map[string]any) { first(d)["network"].(map[string]any)["purpose"] = strings.Repeat("a", 121) },
		"network_url_http": func(d map[string]any) {
			first(d)["network"].(map[string]any)["lookup_urls"] = []any{"http://fixture.invalid/version"}
		},
		"command_path":         func(d map[string]any) { tool(d)["command"] = "/bin/sample" },
		"version_args_empty":   func(d map[string]any) { tool(d)["version_args"] = []any{} },
		"version_args_shell":   func(d map[string]any) { tool(d)["version_args"] = []any{"--version;id"} },
		"regex_length":         func(d map[string]any) { tool(d)["version_regex"] = strings.Repeat("a", 129) },
		"minimum_components":   func(d map[string]any) { tool(d)["min_version"] = "1.2.3.4.5" },
		"source_type":          func(d map[string]any) { source(d)["type"] = "script" },
		"source_name":          func(d map[string]any) { source(d)["name"] = "../sample" },
		"source_tap":           func(d map[string]any) { source(d)["tap"] = "custom/tap" },
		"config_marker":        func(d map[string]any) { config(d)["marker"] = "merge" },
		"config_target_prefix": func(d map[string]any) { config(d)["target"] = ".ssh/config" },
		"config_filename_path": func(d map[string]any) { config(d)["content_file"] = "dir/sample.txt" },
		"config_sha_case":      func(d map[string]any) { config(d)["sha256"] = strings.Repeat("A", 64) },
		"config_mode":          func(d map[string]any) { config(d)["mode"] = "0777" },
		"privileged_empty_argv": func(d map[string]any) {
			first(d)["privileged"] = []any{map[string]any{"purpose": "test", "argv": []any{}}}
		},
		"privileged_shell_argv": func(d map[string]any) {
			first(d)["privileged"] = []any{map[string]any{"purpose": "test", "argv": []any{"echo secret"}}}
		},
		"items_limit": func(d map[string]any) {
			items := []any{}
			for i := 0; i < 65; i++ {
				item := make(map[string]any, len(first(d)))
				for key, value := range first(d) {
					item[key] = value
				}
				item["id"] = "sample-" + strconv.Itoa(i)
				items = append(items, item)
			}
			d["items"] = items
		},
	}
	// Exercise missing/null/unknown at every nested object, including optional ones.
	objects := map[string]func(map[string]any) map[string]any{
		"root": func(d map[string]any) map[string]any { return d }, "item": first, "tool": tool, "source": source, "config": config,
		"network": func(d map[string]any) map[string]any { return first(d)["network"].(map[string]any) },
		"privileged": func(d map[string]any) map[string]any {
			p := map[string]any{"purpose": "test", "argv": []any{"/usr/bin/true"}}
			first(d)["privileged"] = []any{p}
			return p
		},
	}
	for name, selectObject := range objects {
		document, _ := syntheticCatalogue(t)
		obj := selectObject(document)
		for key := range obj {
			// Optional fields may be absent, but may never be null.
			if key != "lookup_urls" && !(name == "item" && key == "config") {
				cases[name+"_missing_"+key] = func(d map[string]any) { delete(selectObject(d), key) }
			}
			cases[name+"_null_"+key] = func(d map[string]any) { selectObject(d)[key] = nil }
		}
		cases[name+"_unknown"] = func(d map[string]any) { selectObject(d)["unknown"] = true }
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			document, templates := syntheticCatalogue(t)
			change(document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(value); err == nil {
				t.Fatal("negative fixture unexpectedly satisfies authoritative schema")
			}
			if _, err = Load(raw, templates); err == nil {
				t.Fatal("schema-invalid fixture accepted by loader")
			}
		})
	}
	raw, templates := realisticCatalogue13()
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(value); err != nil {
		t.Fatalf("TEST ONLY thirteen-entry fixture does not match schema: %v", err)
	}
	if _, err = Load(raw, templates); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogueCopiesTemplatesAndPermitsReviewedFutureEntries(t *testing.T) {
	document, templates := syntheticCatalogue(t)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Load(raw, templates)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(result.Templates["sample.txt"])
	templates["sample.txt"][0] = 'X'
	if !bytes.Equal(original, result.Templates["sample.txt"]) {
		t.Fatal("loader retained caller template alias")
	}
	if len(result.Items) != 1 || result.Items[0].ID != "sample" {
		t.Fatal("loader incorrectly restricts future owner-reviewed entries to initial thirteen")
	}
}

func TestCatalogueDependencyCycleAndVersionedAgentIdentity(t *testing.T) {
	document, templates := syntheticCatalogue(t)
	raw, err := json.Marshal(first(document))
	if err != nil {
		t.Fatal(err)
	}
	var second map[string]any
	if err = json.Unmarshal(raw, &second); err != nil {
		t.Fatal(err)
	}
	first(document)["requires"] = []any{"second"}
	second["id"] = "second"
	second["requires"] = []any{"sample"}
	document["items"] = []any{first(document), second}
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Load(raw, templates); err == nil {
		t.Fatal("two-item cycle accepted")
	}
	document, templates = syntheticCatalogue(t)
	source(document)["name"] = "claude-code@1"
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Load(raw, templates); err == nil {
		t.Fatal("version-suffixed known agent source accepted")
	}
}

// Valid controls isolate each guard from unrelated schema and graph failures.
func TestCatalogueIsolatedGuards(t *testing.T) {
	schema := catalogueSchema(t)
	cases := []struct {
		name          string
		prepare       func(map[string]any)
		invalidate    func(map[string]any)
		schemaInvalid bool
	}{
		{name: "agent_id", invalidate: func(d map[string]any) { first(d)["id"] = "codex" }},
		{name: "agent_command", invalidate: func(d map[string]any) { tool(d)["command"] = "codex" }},
		{name: "cask_core_mismatch", prepare: func(d map[string]any) {
			source(d)["type"] = "cask"
			source(d)["tap"] = "homebrew/cask"
		}, invalidate: func(d map[string]any) { source(d)["tap"] = "homebrew/core" }},
		{name: "privileged_limit", prepare: func(d map[string]any) {
			steps := []any{}
			for i := 0; i < 4; i++ {
				steps = append(steps, map[string]any{"purpose": "Synthetic step " + strconv.Itoa(i), "argv": []any{"/usr/bin/true"}})
			}
			first(d)["privileged"] = steps
		}, invalidate: func(d map[string]any) {
			first(d)["privileged"] = append(first(d)["privileged"].([]any), map[string]any{"purpose": "Synthetic fifth step", "argv": []any{"/usr/bin/true"}})
		}, schemaInvalid: true},
		{name: "network_hosts_limit", prepare: func(d map[string]any) {
			hosts := []any{"fixture.invalid"}
			for i := 1; i < 8; i++ {
				hosts = append(hosts, "fixture-"+strconv.Itoa(i)+".invalid")
			}
			first(d)["network"].(map[string]any)["hosts"] = hosts
		}, invalidate: func(d map[string]any) {
			network := first(d)["network"].(map[string]any)
			network["hosts"] = append(network["hosts"].([]any), "fixture-8.invalid")
		}, schemaInvalid: true},
		{name: "duplicate_requires", prepare: func(d map[string]any) {
			other, _ := syntheticCatalogue(t)
			second := first(other)
			second["id"] = "second"
			d["items"] = append(d["items"].([]any), second)
			first(d)["requires"] = []any{"second"}
		}, invalidate: func(d map[string]any) { first(d)["requires"] = []any{"second", "second"} }, schemaInvalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			document, templates := syntheticCatalogue(t)
			if tc.prepare != nil {
				tc.prepare(document)
			}
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(document); err != nil {
				t.Fatalf("valid control fails schema: %v", err)
			}
			if got, err := Load(raw, templates); err != nil || got == nil {
				t.Fatalf("valid control refused: %v", err)
			}
			tc.invalidate(document)
			raw, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(document); (err != nil) != tc.schemaInvalid {
				t.Fatalf("schema rejection=%v, want %v: %v", err != nil, tc.schemaInvalid, err)
			}
			_, err = Load(raw, templates)
			if err == nil {
				t.Fatal("isolated invalid catalogue accepted")
			}
			if got := exitcode.FromError(err); got != exitcode.Internal {
				t.Fatalf("invalid catalogue exit=%d, want74", got)
			}
		})
	}
}
