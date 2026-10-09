package schematest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type nested struct {
	N int `json:"n"`
}

type sample struct {
	Name string   `json:"name"`
	Opt  []nested `json:"opt,omitempty"`
}

func schema(t *testing.T, s string) object {
	t.Helper()
	var o object
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestValidateFindsNestedAndTypeProblems(t *testing.T) {
	s := schema(t, `{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","enum":["a"]},"opt":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer","minimum":1}}}}}}`)
	if p := Validate(s, []byte(`{"name":"a","opt":[{"n":2}]}`)); len(p) != 0 {
		t.Fatalf("valid document: %v", p)
	}
	got := strings.Join(Validate(s, []byte(`{"name":"b","opt":[{"m":1,"n":0}]}`)), "\n")
	for _, want := range []string{"$.name: ", "$.opt[0]: unknown key \"m\"", "$.opt[0].n: 0 violates minimum"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestUnsupportedKeywordPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unsupported keyword must not pass silently")
		}
	}()
	Validate(schema(t, `{"type":"object","oneOf":[]}`), []byte(`{}`))
}

func TestCompareStructReportsBothDirections(t *testing.T) {
	rec := &recorder{}
	s := schema(t, `{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string"},"opt":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{}}},"extra":{"type":"string"}}}`)
	CompareStruct(rec, s, reflect.TypeFor[sample](), nil)
	got := strings.Join(rec.errs, "\n")
	for _, want := range []string{"$.opt[].n: the Go struct", "$.extra: the schema has this entry"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
