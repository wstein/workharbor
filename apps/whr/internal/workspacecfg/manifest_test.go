// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type vectorCase struct {
	ID            string          `json:"id"`
	Document      json.RawMessage `json:"document"`
	RawText       *string         `json:"raw_text"`
	BytesHex      *string         `json:"bytes_hex"`
	GenerateBytes *struct {
		Total               int    `json:"total"`
		Template            string `json:"template"`
		Padding             string `json:"padding"`
		PaddingAfterTemplte bool   `json:"padding_after_template"`
	} `json:"generate_bytes"`
	ExpectCodes []string `json:"expect_codes"`
}

func (c vectorCase) bytes(t *testing.T) []byte {
	t.Helper()
	switch {
	case c.BytesHex != nil:
		b, err := hex.DecodeString(*c.BytesHex)
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		return b
	case c.RawText != nil:
		return []byte(*c.RawText)
	case c.GenerateBytes != nil:
		g := c.GenerateBytes
		if g.Padding != "space" || !g.PaddingAfterTemplte || len(g.Template) > g.Total {
			t.Fatalf("%s: unsupported generator %+v", c.ID, g)
		}
		return []byte(g.Template + strings.Repeat(" ", g.Total-len(g.Template)))
	case c.Document != nil:
		return []byte(c.Document)
	}
	t.Fatalf("%s: no input", c.ID)
	return nil
}

func manifestCodes(fs []workspacecfg.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if strings.HasPrefix(f.Code, "manifest.") && !seen[f.Code] {
			seen[f.Code] = true
			out = append(out, f.Code)
		}
	}
	sort.Strings(out)
	return out
}

func loadVectors(t *testing.T) []vectorCase {
	t.Helper()
	var v struct {
		Cases []vectorCase `json:"cases"`
	}
	readJSON(t, contractPath("workspace", "vectors.json"), &v)
	if len(v.Cases) < 70 {
		t.Fatalf("vectors.json holds %d cases", len(v.Cases))
	}
	return v.Cases
}

// TestManifestVectors runs every case of the shared vectors.
func TestManifestVectors(t *testing.T) {
	for _, c := range loadVectors(t) {
		t.Run(c.ID, func(t *testing.T) {
			got := manifestCodes(workspacecfg.CheckManifest(c.bytes(t)).Findings)
			want := append([]string{}, c.ExpectCodes...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("codes %v, want %v", got, want)
			}
		})
	}
}

// TestExampleManifestsValid holds the committed example manifests to the checker.
func TestExampleManifestsValid(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "examples", "workspace")
	for _, p := range []string{"workharbor.json", "admitted-image/workharbor.json", "admitted-dockerfile/workharbor.json"} {
		raw, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		if r := workspacecfg.CheckManifest(raw); len(r.Findings) != 0 || r.Manifest == nil {
			t.Errorf("%s: %+v", p, r.Findings)
		}
	}
}

func v2(extra string) []byte {
	return []byte(`{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]` + extra + `}`)
}

func TestManifestParsedValues(t *testing.T) {
	r := workspacecfg.CheckManifest(v2(`,"name":"my-ws","start_dir":"projects/app","human_shell":"nu","console_mode":"plain","shortcuts":"off","project":{"url":"https://f.example/o/app.git","path":"projects/app"}`))
	if len(r.Findings) != 0 {
		t.Fatal(r.Findings)
	}
	m := r.Manifest
	if m.SchemaVersion != 2 || m.DevcontainerPath != ".devcontainer/devcontainer.json" || m.ProjectsPath != "projects" ||
		m.Name != "my-ws" || m.StartDir != "projects/app" || m.HumanShell != "nu" || m.ConsoleMode != "plain" || m.Shortcuts != "off" ||
		m.Project == nil || m.Project.Path != "projects/app" {
		t.Errorf("%+v", m)
	}
}

func TestManifestDuplicateKeysAreDecoded(t *testing.T) {
	raw := []byte(`{"schema_version":1,"schema_version":1}`)
	if got := manifestCodes(workspacecfg.CheckManifest(raw).Findings); strings.Join(got, ",") != "manifest.duplicate_key" {
		t.Errorf("%v", got)
	}
}

func TestManifestDepthBoundary(t *testing.T) {
	ok := strings.Repeat("[", 127) + strings.Repeat("]", 127)
	raw := []byte(`{"schema_version":1,"devcontainer_path":"a","projects_path":"b","bundles":[],"x":` + ok + `}`)
	// 128 levels in total (object plus 127 arrays) parse; the unknown field is the only finding.
	if got := manifestCodes(workspacecfg.CheckManifest(raw).Findings); strings.Join(got, ",") != "manifest.schema" {
		t.Errorf("128 levels: %v", got)
	}
	deep := strings.Repeat("[", 128) + strings.Repeat("]", 128)
	raw = []byte(`{"schema_version":1,"devcontainer_path":"a","projects_path":"b","bundles":[],"x":` + deep + `}`)
	if got := manifestCodes(workspacecfg.CheckManifest(raw).Findings); strings.Join(got, ",") != "manifest.syntax" {
		t.Errorf("129 levels: %v", got)
	}
}

func TestManifestSchemaVersionSpellings(t *testing.T) {
	for _, tc := range []struct {
		lit  string
		want string
	}{
		{"1", ""}, {"2", ""}, {"1.0", "manifest.schema"}, {"1e0", "manifest.schema"}, {"1E0", "manifest.schema"},
		{"10e-1", "manifest.schema"}, {"-1", "manifest.schema"}, {"0", "manifest.schema"}, {"true", "manifest.schema"},
		{`"1"`, "manifest.schema"}, {"null", "manifest.schema"}, {"[1]", "manifest.schema"}, {"01", "manifest.syntax"},
		{"1.", "manifest.syntax"}, {"+1", "manifest.syntax"}, {".5", "manifest.syntax"}, {"NaN", "manifest.syntax"},
		{"99999999999999999999999999", "manifest.schema"},
	} {
		raw := []byte(`{"schema_version":` + tc.lit + `,"devcontainer_path":"a","projects_path":"b","bundles":[]}`)
		got := strings.Join(manifestCodes(workspacecfg.CheckManifest(raw).Findings), ",")
		if got != tc.want {
			t.Errorf("schema_version %s: %q, want %q", tc.lit, got, tc.want)
		}
	}
}

func TestManifestSyntaxCorpus(t *testing.T) {
	for name, raw := range map[string]string{
		"empty": "", "whitespace": " \n", "trailing": `{} x`, "second document": `{}{}`, "comment": `{/*x*/}`,
		"trailing comma": `{"a":1,}`, "single quotes": `{'a':1}`, "unterminated": `{"a":`, "bad escape": `{"a":"\q"}`,
		"raw newline in string": "{\"a\":\"x\ny\"}", "control in string": "{\"a\":\"x\x01\"}", "bare word": `{"a":nul}`,
	} {
		if got := manifestCodes(workspacecfg.CheckManifest([]byte(raw)).Findings); strings.Join(got, ",") != "manifest.syntax" {
			t.Errorf("%s: %v", name, got)
		}
	}
	if got := manifestCodes(workspacecfg.CheckManifest([]byte(" \t\r\n{\"schema_version\":1,\"devcontainer_path\":\"a\",\"projects_path\":\"b\",\"bundles\":[]}\n")).Findings); len(got) != 0 {
		t.Errorf("surrounding whitespace: %v", got)
	}
}

func TestManifestEveryDefectItsOwnFinding(t *testing.T) {
	r := workspacecfg.CheckManifest([]byte(`{"schema_version":1,"projects_path":"/etc","bundles":[1],"x":1,"y":2}`))
	count := map[string]int{}
	for _, f := range r.Findings {
		count[f.Code]++
	}
	// missing devcontainer_path, two unknown fields; one bad path; non-empty bundles.
	if count["manifest.schema"] != 3 || count["manifest.path_invalid"] != 1 || count["manifest.bundles_deferred"] != 1 {
		t.Errorf("%v", count)
	}
}

func TestManifestMessagesNeverEchoSecrets(t *testing.T) {
	for _, extra := range []string{
		`,"project":{"url":"https://user:CANARYsecret@f.example/o/a.git","path":"projects/a"}`,
		`,"project":{"url":"https://CANARYsecret@f.example/o/a.git","path":"projects/a"}`,
		`,"project":{"url":"ssh://git:CANARYsecret@f.example/o/a.git","path":"projects/a"}`,
	} {
		r := workspacecfg.CheckManifest(v2(extra))
		if len(r.Findings) != 1 || r.Findings[0].Code != "manifest.url_credentials" {
			t.Fatalf("%v", r.Findings)
		}
		f := r.Findings[0]
		if strings.Contains(f.Message+f.Remediation+f.Subject, "CANARY") {
			t.Errorf("credential echoed: %+v", f)
		}
	}
}

func TestManifestKeyEchoIsTruncated(t *testing.T) {
	key := strings.Repeat("k", 500)
	r := workspacecfg.CheckManifest([]byte(`{"` + key + `":1,"` + key + `":2}`))
	if len(r.Findings) != 1 || len([]rune(r.Findings[0].Message)) > 300 {
		t.Errorf("%d findings, message %d runes", len(r.Findings), len([]rune(r.Findings[0].Message)))
	}
}

func TestManifestProjectURLDerivation(t *testing.T) {
	for _, tc := range []struct {
		url, path string
		ok        bool
	}{
		{"https://f.example/o/app.git", "projects/app", true},
		{"https://f.example/o/app", "projects/app", true},
		{"https://f.example/app.git.git", "projects/app.git", true},
		{"https://f.example/o/.git", "projects/", false},
		{"https://f.example/o/_x", "projects/_x", false},
		{"https://f.example/o/a%zz", "projects/a%zz", false},
		{"https://f.example/o/a%41", "projects/a%41", false},
		{"ssh://git@f.example:22/o/app.git", "projects/app", true},
		{"ssh://-oProxyCommand@f.example/o/app.git", "projects/app", false},
		{"https://f.example/o/app.git", "projects/APP", false},
	} {
		r := workspacecfg.CheckManifest(v2(`,"project":{"url":"` + tc.url + `","path":"` + tc.path + `"}`))
		if (len(r.Findings) == 0) != tc.ok {
			t.Errorf("%s -> %s: %v", tc.url, tc.path, r.Findings)
		}
	}
}
