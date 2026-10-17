// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func contractPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", "..", "contracts"}, parts...)...)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

type findingsContract struct {
	Codes []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
	} `json:"codes"`
}

// TestFindingCatalogueMatchesContract freezes the Go catalogue against
// contracts/cli/validate-findings.json: same codes, same severities, no extras.
func TestFindingCatalogueMatchesContract(t *testing.T) {
	var c findingsContract
	readJSON(t, contractPath("cli", "validate-findings.json"), &c)
	want := map[string]string{}
	for _, e := range c.Codes {
		if _, dup := want[e.Code]; dup {
			t.Fatalf("contract lists %s twice", e.Code)
		}
		want[e.Code] = e.Severity
	}
	got := map[string]string{}
	for _, e := range workspacecfg.Catalogue() {
		if _, dup := got[e.Code]; dup {
			t.Errorf("catalogue lists %s twice", e.Code)
		}
		got[e.Code] = string(e.Severity)
	}
	var diffs []string
	for code, sev := range want {
		if g, ok := got[code]; !ok {
			diffs = append(diffs, "missing "+code)
		} else if g != sev {
			diffs = append(diffs, code+": severity "+g+", contract "+sev)
		}
	}
	for code := range got {
		if _, ok := want[code]; !ok {
			diffs = append(diffs, "extra "+code)
		}
	}
	sort.Strings(diffs)
	for _, d := range diffs {
		t.Error(d)
	}
	if len(want) == 0 {
		t.Fatal("contract holds no codes")
	}
}

// TestFindingCodesMatchOutputSchemaPattern holds every code to the finding
// code pattern of contracts/cli/output.schema.json.
func TestFindingCodesMatchOutputSchemaPattern(t *testing.T) {
	var schema struct {
		Defs struct {
			Finding struct {
				Properties struct {
					Code struct {
						Pattern string `json:"pattern"`
					} `json:"code"`
				} `json:"properties"`
			} `json:"finding"`
		} `json:"$defs"`
	}
	readJSON(t, contractPath("cli", "output.schema.json"), &schema)
	re := regexp.MustCompile(schema.Defs.Finding.Properties.Code.Pattern)
	if schema.Defs.Finding.Properties.Code.Pattern == "" {
		t.Fatal("schema has no finding code pattern")
	}
	for _, e := range workspacecfg.Catalogue() {
		if !re.MatchString(e.Code) {
			t.Errorf("%s does not match %s", e.Code, re)
		}
	}
}
