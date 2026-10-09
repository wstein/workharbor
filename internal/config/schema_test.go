package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/schematest"
)

const (
	configSchemaPath = "../../schemas/config.v0-provisional.schema.json"
	// The URL an instance names (editors) and the dialect a schema document
	// names are different things and must stay different.
	configSchemaURL = "https://wstein.github.io/workharbor/schemas/config.v0-provisional.schema.json"
	dialectURL      = "https://json-schema.org/draft/2020-12/schema"
)

// TestConfigSchemaMatchesTheStruct fails when a key (at any depth) exists on
// one side only, or its type or optionality differs.
func TestConfigSchemaMatchesTheStruct(t *testing.T) {
	s := schematest.Load(t, configSchemaPath)
	schematest.CompareStruct(t, s, reflect.TypeFor[Config](), map[string]string{
		"$.repositories[].clone_depth": "the loader reads a missing clone_depth as 0 (full history), and the manual's example omits it",
	})
}

func TestSchemaDocumentAndInstanceHintAreDistinct(t *testing.T) {
	s := schematest.Load(t, configSchemaPath)
	if s["$schema"] != dialectURL {
		t.Errorf("$schema of the schema document = %v, want the JSON Schema dialect %s", s["$schema"], dialectURL)
	}
	if s["$id"] != configSchemaURL {
		t.Errorf("$id = %v, want the published URL %s", s["$id"], configSchemaURL)
	}
}

// manualExample returns the config.json example of the host setup page.
func manualExample(t *testing.T) []byte {
	t.Helper()
	page, err := os.ReadFile("../../docs/content/docs/manual/host-setup.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(page), "```json\n")
	for ok && !strings.Contains(strings.SplitN(rest, "```", 2)[0], `"listen"`) {
		_, rest, ok = strings.Cut(rest, "```json\n")
	}
	if !ok {
		t.Fatal("no config.json example with a listen key in host-setup.md")
	}
	block, _, _ := strings.Cut(rest, "```")
	var lines []string
	for line := range strings.SplitSeq(block, "\n") {
		lines = append(lines, strings.TrimPrefix(line, "    "))
	}
	return []byte(strings.Join(lines, "\n"))
}

func TestSchemaAcceptsGoodExamplesAndRejectsWrongOnes(t *testing.T) {
	s := schematest.Load(t, configSchemaPath)
	example := manualExample(t)
	if p := schematest.Validate(s, example); len(p) != 0 {
		t.Fatalf("the manual's example does not validate: %v", p)
	}
	generated, _ := json.Marshal(newRig(t).cfg)
	if p := schematest.Validate(s, generated); len(p) != 0 {
		t.Fatalf("a marshalled Config does not validate: %v", p)
	}

	mutate := func(edit func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(example, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		raw, _ := json.Marshal(m)
		return raw
	}
	nested := func(m map[string]any, key string) map[string]any { return m[key].(map[string]any) }
	for name, tc := range map[string]struct {
		doc  []byte
		want string
	}{
		"nested typo":     {mutate(func(m map[string]any) { nested(m, "github")["app_idd"] = 1 }), `$.github: unknown key "app_idd"`},
		"top-level typo":  {mutate(func(m map[string]any) { m["listn"] = "x" }), `unknown key "listn"`},
		"wrong type":      {mutate(func(m map[string]any) { nested(m, "github")["app_id"] = "123456" }), "$.github.app_id: want integer"},
		"missing":         {mutate(func(m map[string]any) { delete(nested(m, "roots"), "tool_store") }), `missing required key "tool_store"`},
		"enum":            {mutate(func(m map[string]any) { m["account"] = "private" }), "$.account: "},
		"enum in a list":  {mutate(func(m map[string]any) { m["repositories"].([]any)[0].(map[string]any)["workflow"] = "yolo" }), "$.repositories[0].workflow: "},
		"range":           {mutate(func(m map[string]any) { m["preview"] = map[string]any{"first_port": 80, "last_port": 90} }), "$.preview.first_port: matches none of the alternatives"},
		"percent":         {mutate(func(m map[string]any) { m["budgets"] = map[string]any{"soft_percent": 100} }), "$.budgets.soft_percent: 100 violates maximum"},
		"list item type":  {mutate(func(m map[string]any) { m["agent_allowed_tools"] = []any{"Read", 7} }), "$.agent_allowed_tools[1]: want string"},
		"empty workspace": {mutate(func(m map[string]any) { nested(m, "roots")["workspaces"] = []any{} }), "fewer than 1 items"},
	} {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(schematest.Validate(s, tc.doc), "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want one containing %q", got, tc.want)
			}
		})
	}
}

// TestSchemaAcceptsWhatTheLoaderAccepts pins the values the loader takes that
// a stricter schema would flag: 0 means "default" or "off" for the percents and
// the preview range, and url.Parse lowercases the scheme. Each document is also
// parsed by the loader, so the schema cannot drift from it. Out-of-range values
// must keep failing in the schema.
func TestSchemaAcceptsWhatTheLoaderAccepts(t *testing.T) {
	s := schematest.Load(t, configSchemaPath)
	base, _ := json.Marshal(newRig(t).cfg)
	with := func(edit func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(base, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		raw, _ := json.Marshal(m)
		return raw
	}
	ok := map[string]func(m map[string]any){
		"soft_percent 0":    func(m map[string]any) { m["budgets"] = map[string]any{"soft_percent": 0} },
		"warn_percent 0":    func(m map[string]any) { m["limits"] = map[string]any{"warn_percent": 0} },
		"preview off":       func(m map[string]any) { m["preview"] = map[string]any{"first_port": 0, "last_port": 0} },
		"public_url scheme": func(m map[string]any) { m["public_url"] = "HTTPS://whr.example.ts.net" },
		"board scheme": func(m map[string]any) {
			m["board"] = map[string]any{"owner": "octo", "number": 1, "public_url": "Https://whr.example.ts.net"}
		},
	}
	for name, edit := range ok {
		t.Run("accepts "+name, func(t *testing.T) {
			doc := with(edit)
			if p := schematest.Validate(s, doc); len(p) != 0 {
				t.Errorf("schema rejects a document the loader accepts: %v", p)
			}
			if _, err := Parse(doc); err != nil {
				t.Errorf("loader rejects the document: %v", err)
			}
		})
	}
	bad := map[string]struct {
		edit func(m map[string]any)
		want string
	}{
		"soft_percent -1":  {func(m map[string]any) { m["budgets"] = map[string]any{"soft_percent": -1} }, "violates minimum"},
		"warn_percent 100": {func(m map[string]any) { m["limits"] = map[string]any{"warn_percent": 100} }, "violates maximum"},
		"preview 1023":     {func(m map[string]any) { m["preview"] = map[string]any{"first_port": 1023, "last_port": 2000} }, "matches none"},
		"preview 65536":    {func(m map[string]any) { m["preview"] = map[string]any{"first_port": 2000, "last_port": 65536} }, "matches none"},
		"public_url http":  {func(m map[string]any) { m["public_url"] = "http://whr.example.ts.net" }, "does not match"},
	}
	for name, tc := range bad {
		t.Run("rejects "+name, func(t *testing.T) {
			got := strings.Join(schematest.Validate(s, with(tc.edit)), "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want one containing %q", got, tc.want)
			}
		})
	}
}

// TestSchemaKeyIsDataOnly pins the rule of issue #268. Parse decodes the file
// with the Go struct and nothing else: the only place that ever sees the value
// is Config.Schema, and no code in this package imports net/http or net, opens a
// connection or hands the value to anything. Asserting "no request is made"
// otherwise is impossible from a test, so the test pins the structure: the
// value does not change the result, and the field is read nowhere.
func TestSchemaKeyIsDataOnly(t *testing.T) {
	r := newRig(t)
	base, err := json.Marshal(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{configSchemaURL, "", "https://unreachable.invalid/x.json", "not a url at all", "file:///nonexistent", "http://127.0.0.1:1/never"} {
		var m map[string]any
		_ = json.Unmarshal(base, &m)
		m["$schema"] = url
		raw, _ := json.Marshal(m)
		c, err := Parse(raw)
		if err != nil {
			t.Fatalf("$schema %q broke loading: %v", url, err)
		}
		if c.Schema != url {
			t.Errorf("Schema = %q, want %q kept as data", c.Schema, url)
		}
	}

	var m map[string]any
	_ = json.Unmarshal(base, &m)
	m["$schema"] = 7
	raw, _ := json.Marshal(m)
	if _, err := Parse(raw); err == nil {
		t.Error("a non-string $schema must fail: it is an optional string key")
	}

	m["$schema"] = configSchemaURL
	m["$schemas"] = "typo"
	raw, _ = json.Marshal(m)
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "$schemas") {
		t.Errorf("an unknown key next to $schema = %v, want it refused by name", err)
	}

	// Nothing in the package may resolve the URL.
	entries, _ := filepath.Glob("*.go")
	for _, f := range entries {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f) //nolint:gosec // package source
		for _, banned := range []string{`"net/http"`, ".Schema"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s uses %s: a $schema value must never be resolved or read", f, banned)
			}
		}
	}
}
