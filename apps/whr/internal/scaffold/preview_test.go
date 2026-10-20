// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

func TestInitPreviewDeterministic(t *testing.T) {
	s := snapshot()
	o := defaults()
	p, err := scaffold.Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(p.Preview())
	files := p.Files()
	files["workharbor.json"][0] = 'x'
	e := s.Entries[".devcontainer/devcontainer.json"]
	e.Data[0] = 'x'
	*o.Name = "changed"
	detached := p.Preview()
	detached.Actions[0].Path = "changed"
	detached.Validation.Valid = false
	after, _ := json.Marshal(p.Preview())
	if !bytes.Equal(before, after) {
		t.Fatal("caller mutated retained preview")
	}
	if p.Files()["workharbor.json"][0] != '{' {
		t.Fatal("caller mutated retained file content")
	}
	for _, a := range p.Preview().Actions {
		if a.Applied {
			t.Fatal("a proposal cannot claim applied files")
		}
	}
	fp := p.Fingerprints()
	if !fp[".devcontainer/devcontainer.json"].Exists || fp[".devcontainer/devcontainer.json"].SHA256 == "" {
		t.Fatal("observed content hash missing")
	}
}

func TestInitRerunIsNoOp(t *testing.T) {
	s := snapshot()
	p, err := scaffold.Build(s, defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Preview().Actions {
		if a.Kind == "directory" {
			s.Entries[a.Path] = scaffold.Entry{Kind: "directory"}
		}
	}
	for name, data := range p.Files() {
		s.Entries[name] = scaffold.Entry{Kind: "file", Data: data}
	}
	next, err := scaffold.Build(s, defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range next.Preview().Actions {
		if a.Action == "create" || a.Action == "overwrite" {
			t.Fatalf("identical rerun would mutate %s", a.Path)
		}
	}
}

func TestInitPreviewTruncatesOnlyDisplay(t *testing.T) {
	s := snapshot()
	old := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	old = append(old, bytes.Repeat([]byte(" "), 65536-len(old))...)
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: old}
	o := defaults()
	o.Overwrite = []string{"workharbor.json"}
	p, err := scaffold.Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Preview().Actions {
		if a.Path == "workharbor.json" {
			if !a.DiffTruncated || len([]rune(a.Diff)) != 65536 || !strings.HasPrefix(a.Diff, "--- a/workharbor.json") {
				t.Fatal("bounded full-file diff missing")
			}
		}
	}
	if !json.Valid(p.Files()["workharbor.json"]) {
		t.Fatal("retained proposal was truncated")
	}
}

func TestInitJSONMatchesSchema(t *testing.T) {
	p, err := scaffold.Build(snapshot(), defaults())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../../contracts/cli/output.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("https://schema.test/output", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("https://schema.test/output")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := output.Writer{Out: &out, Err: &bytes.Buffer{}, JSON: true}
	if err := w.Success("workspace.init", p.Preview(), ""); err != nil {
		t.Fatal(err)
	}
	value, err := jsonschema.UnmarshalJSON(&out)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}
