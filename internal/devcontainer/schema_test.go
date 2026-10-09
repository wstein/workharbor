package devcontainer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/schematest"
)

const blockSchemaPath = "../../schemas/devcontainer-workharbor.v0-provisional.schema.json"

// TestBlockSchemaMatchesTheStruct fails when a key of customizations.workharbor
// exists in the Go reader or in the schema only, or their types differ.
func TestBlockSchemaMatchesTheStruct(t *testing.T) {
	s := schematest.Load(t, blockSchemaPath)
	// previewPorts is read leniently (numbers or numeric strings), so the Go
	// field is a json.RawMessage and only needs a schema entry.
	schematest.CompareStruct(t, s, reflect.TypeFor[workharborBlock](), nil, "$.previewPorts")
}

func TestBlockSchemaAcceptsAGoodBlockAndRejectsWrongOnes(t *testing.T) {
	s := schematest.Load(t, blockSchemaPath)
	good := `{"egress":["registry.npmjs.org"],"check":"make check","previewPorts":[3000,"8080"],"agent":"claude","tools":["Read","Edit"]}`
	if p := schematest.Validate(s, []byte(good)); len(p) != 0 {
		t.Fatalf("a good block does not validate: %v", p)
	}
	// The same block must be read by the product without notes.
	if _, _, notes := customizations([]byte(`{"workharbor":`+good+`}`), nil); len(notes) != 0 {
		t.Fatalf("the good example produced notes: %v", notes)
	}
	for name, tc := range map[string]struct{ doc, want string }{
		"typo":            {`{"cmd_check":"make check"}`, `unknown key "cmd_check"`},
		"wrong type":      {`{"check":["make","check"]}`, "$.check: want string"},
		"list item type":  {`{"egress":["a.example", 3]}`, "$.egress[1]: want string"},
		"port out":        {`{"previewPorts":[70000]}`, "$.previewPorts[0]: matches none"},
		"port not number": {`{"previewPorts":["http"]}`, "$.previewPorts[0]: matches none"},
		"not an object":   {`["check"]`, "want object"},
	} {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(schematest.Validate(s, []byte(tc.doc)), "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want one containing %q", got, tc.want)
			}
		})
	}
}
