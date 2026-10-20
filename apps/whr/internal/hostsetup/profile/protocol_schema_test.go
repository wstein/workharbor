// SPDX-License-Identifier: EUPL-1.2

// Contract tests only: there is no profile writer or apply engine yet.
package profile_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type protocolCase struct {
	ID     string          `json:"id"`
	Record json.RawMessage `json:"record"`
}
type protocolFixtures struct {
	SchemaVersion int            `json:"schema_version"`
	Description   string         `json:"description"`
	Positive      []protocolCase `json:"positive"`
	Negative      []protocolCase `json:"negative"`
}

func protocolContract(t *testing.T) (*jsonschema.Schema, protocolFixtures) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "..", "specs", "009-host-setup-wizard", "contracts")
	raw, err := os.ReadFile(filepath.Join(dir, "protocol.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	const address = "https://workharbor.invalid/protocol.schema.json"
	if err = compiler.AddResource(address, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(address)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "protocol.fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures protocolFixtures
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("fixture has trailing data")
	}
	if fixtures.SchemaVersion != 1 || len(fixtures.Positive) != 6 || len(fixtures.Negative) < 6 {
		t.Fatal("invalid or incomplete protocol fixture corpus")
	}
	return schema, fixtures
}

func TestProtocolAcceptsRequiredAuditPayload(t *testing.T) {
	schema, fixtures := protocolContract(t)
	seen := map[string]bool{}
	for _, test := range fixtures.Positive {
		t.Run(test.ID, func(t *testing.T) {
			if seen[test.ID] {
				t.Fatal("duplicate positive case")
			}
			seen[test.ID] = true
			value, err := jsonschema.UnmarshalJSON(bytes.NewReader(test.Record))
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(value); err != nil {
				t.Fatalf("valid audit payload refused: %v", err)
			}
			if value.(map[string]any)["type"] != test.ID {
				t.Fatal("positive case ID does not match its record kind")
			}
		})
	}
	for _, kind := range []string{"run_start", "selection", "step", "backup", "diff", "run_end"} {
		if !seen[kind] {
			t.Fatalf("missing positive record kind %s", kind)
		}
	}
}

func TestProtocolRejectsMissingAuditPayloadAndInvalidValues(t *testing.T) {
	schema, fixtures := protocolContract(t)
	seen := map[string]bool{}
	for _, test := range fixtures.Negative {
		t.Run(test.ID, func(t *testing.T) {
			if seen[test.ID] {
				t.Fatal("duplicate negative case")
			}
			seen[test.ID] = true
			value, err := jsonschema.UnmarshalJSON(bytes.NewReader(test.Record))
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(value); err == nil {
				t.Fatal("invalid audit record accepted")
			}
		})
	}
	for _, kind := range []string{"run_start", "selection", "step", "backup", "diff", "run_end"} {
		if !seen[kind+"_payload_absent"] {
			t.Fatalf("missing absent-payload control for %s", kind)
		}
	}
	rejectIndividualProtocolFields(t, schema, fixtures)
}

// Each negative changes one field of an independently valid positive record.
// Required-field controls use the fixture payload, not the schema's required
// lists, so weakening one required list cannot weaken the test oracle with it.
func rejectIndividualProtocolFields(t *testing.T, schema *jsonschema.Schema, fixtures protocolFixtures) {
	t.Helper()
	positive := map[string]json.RawMessage{}
	for _, test := range fixtures.Positive {
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(test.Record))
		if err != nil {
			t.Fatal(err)
		}
		record := value.(map[string]any)
		if record["type"] != test.ID || positive[test.ID] != nil {
			t.Fatal("invalid positive record identity")
		}
		if err = schema.Validate(value); err != nil {
			t.Fatal(err)
		}
		positive[test.ID] = test.Record
		for field := range record {
			t.Run(test.ID+"_missing_"+field, func(t *testing.T) {
				value, err := jsonschema.UnmarshalJSON(bytes.NewReader(test.Record))
				if err != nil {
					t.Fatal(err)
				}
				delete(value.(map[string]any), field)
				if schema.Validate(value) == nil {
					t.Fatalf("missing %s accepted", field)
				}
			})
		}
	}
	for _, test := range []struct {
		name, kind, field string
		value             any
	}{
		{"invalid_calendar_utc", "run_start", "time", "2026-02-30T18:00:00Z"},
		{"invalid_clock_utc", "run_start", "time", "2026-10-20T25:61:61Z"},
		{"run_id_not_hex", "run_start", "run_id", "gggggggggggggggg"},
		{"run_id_short", "run_start", "run_id", "0123456789abcde"},
		{"hash_not_hex", "backup", "sha256", "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"},
		{"hash_short", "backup", "sha256", "aaaaaaaa"},
		{"selected_component_id", "selection", "selected", []any{"INVALID"}},
		{"opted_out_component_id", "selection", "opted_out", []any{"INVALID"}},
		{"step_component_id", "step", "item", "INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := jsonschema.UnmarshalJSON(bytes.NewReader(positive[test.kind]))
			if err != nil {
				t.Fatal(err)
			}
			value.(map[string]any)[test.field] = test.value
			if schema.Validate(value) == nil {
				t.Fatalf("malformed %s accepted", test.field)
			}
		})
	}
}
