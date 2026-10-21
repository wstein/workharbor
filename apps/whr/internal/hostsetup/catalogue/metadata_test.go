// SPDX-License-Identifier: EUPL-1.2
package catalogue

import (
	"encoding/json"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func metadataCatalogue(t *testing.T) (map[string]any, map[string][]byte) {
	t.Helper()
	document, templates := syntheticCatalogue(t)
	item := first(document)
	item["id"] = "orbstack"
	item["kind"] = "runtime"
	delete(item, "config")
	probe := tool(document)
	delete(probe, "command")
	delete(probe, "version_args")
	probe["version_metadata"] = "orbstack_app"
	probe["version_regex"] = `^([0-9]+\.[0-9]+\.[0-9]+)\s*$`
	probe["min_version"] = "2.2.3"
	source(document)["type"] = "cask"
	source(document)["name"] = "orbstack"
	source(document)["tap"] = "homebrew/cask"
	return document, templates
}

func TestCatalogueOrbStackMetadataProbe(t *testing.T) {
	schema := catalogueSchema(t)
	cases := []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"measured_app_metadata", func(map[string]any) {}, true},
		{"both_routes", func(d map[string]any) { tool(d)["command"] = "orb"; tool(d)["version_args"] = []any{"version"} }, false},
		{"command_only", func(d map[string]any) { tool(d)["command"] = "orb" }, false},
		{"args_only", func(d map[string]any) { tool(d)["version_args"] = []any{"version"} }, false},
		{"unknown_route", func(d map[string]any) { tool(d)["version_metadata"] = "arbitrary_file" }, false},
		{"null_route", func(d map[string]any) { tool(d)["version_metadata"] = nil }, false},
		{"missing_route", func(d map[string]any) { delete(tool(d), "version_metadata") }, false},
		{"other_item", func(d map[string]any) { first(d)["id"] = "sample" }, false},
		{"other_kind", func(d map[string]any) { first(d)["kind"] = "tool" }, false},
		{"other_package", func(d map[string]any) { source(d)["name"] = "sample" }, false},
		{"formula", func(d map[string]any) { source(d)["type"] = "formula"; source(d)["tap"] = "homebrew/core" }, false},
		{"unknown_field", func(d map[string]any) { tool(d)["metadata_path"] = "/tmp/other" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, templates := metadataCatalogue(t)
			tc.change(doc)
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if schemaOK := schema.Validate(doc) == nil; schemaOK != tc.valid {
				t.Errorf("schema valid=%v want%v", schemaOK, tc.valid)
			}
			got, err := Load(raw, templates)
			if (err == nil) != tc.valid {
				t.Fatalf("loader valid=%v want%v: %v", err == nil, tc.valid, err)
			}
			if !tc.valid && exitcode.FromError(err) != exitcode.Internal {
				t.Errorf("exit=%d want74", exitcode.FromError(err))
			}
			if tc.valid && (got.Items[0].Tool.VersionMetadata != OrbStackAppMetadata || got.Items[0].Tool.Command != "" || len(got.Items[0].Tool.VersionArgs) != 0) {
				t.Fatal("metadata probe lost its discriminator or acquired a command")
			}
		})
	}
}

func TestCatalogueMetadataTypedRoundTrip(t *testing.T) {
	document, templates := metadataCatalogue(t)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Load(raw, templates)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(encoded, templates)
	if err != nil {
		t.Fatalf("typed metadata round trip refused: %v", err)
	}
	if second.Items[0].Tool.VersionMetadata != OrbStackAppMetadata {
		t.Fatal("metadata discriminator lost in round trip")
	}
}
