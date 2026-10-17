// SPDX-License-Identifier: EUPL-1.2

package version_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/version"
)

func TestVersionFields(t *testing.T) {
	r := version.Current()
	if r.Version == "" || r.Revision == "" || r.BuildDate == "" {
		t.Fatalf("version, revision and build date must never be empty: %+v", r)
	}
	if r.ClientPlatform.OS != runtime.GOOS || r.ClientPlatform.Arch != runtime.GOARCH {
		t.Errorf("client platform = %+v, want %s/%s", r.ClientPlatform, runtime.GOOS, runtime.GOARCH)
	}
	for _, name := range []string{"output_schema", "command_table", "exit_codes"} {
		if v := r.ContractVersions[name]; v < 1 {
			t.Errorf("contract version %q = %d, want >= 1", name, v)
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "revision", "build_date", "client_platform", "contract_versions"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("JSON result lacks %q", k)
		}
	}
	if len(keys) != 5 {
		t.Errorf("JSON result has %d fields, want exactly 5: %s", len(keys), raw)
	}
}

func TestVersionBuildMetadataIsInjectable(t *testing.T) {
	oldV, oldR, oldD := version.Version, version.Revision, version.BuildDate
	t.Cleanup(func() { version.Version, version.Revision, version.BuildDate = oldV, oldR, oldD })
	version.Version, version.Revision, version.BuildDate = "9.9.9", "abc123", "2026-01-02"
	r := version.Current()
	if r.Version != "9.9.9" || r.Revision != "abc123" || r.BuildDate != "2026-01-02" {
		t.Fatalf("linker-set metadata ignored: %+v", r)
	}
	text := r.Human()
	for _, want := range []string{"whr 9.9.9", "revision: abc123", "build date: 2026-01-02", "platform: " + runtime.GOOS + "/" + runtime.GOARCH, "output_schema=1"} {
		if !strings.Contains(text, want) {
			t.Errorf("human text lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, "\n") || strings.Contains(strings.TrimSuffix(text, "\n"), "\n\n") {
		t.Errorf("human text must end with one newline and have no blank line: %q", text)
	}
}

func TestHumanEscapesInjectedFields(t *testing.T) {
	oldV, oldR, oldD := version.Version, version.Revision, version.BuildDate
	t.Cleanup(func() { version.Version, version.Revision, version.BuildDate = oldV, oldR, oldD })
	version.Version, version.Revision, version.BuildDate = "1\x1b[2J", "r\u202ev", "d\nwhr: forged"
	text := version.Current().Human()
	if strings.ContainsAny(text, "\x1b\u202e") || strings.Count(text, "\n") != 5 {
		t.Fatalf("injected fields reach the terminal raw: %q", text)
	}
}

func TestContractVersionsMatchContractFiles(t *testing.T) {
	files := map[string]string{"command_table": "commands.json", "exit_codes": "exit-codes.json", "output_schema": "output.schema.json"}
	got := version.Current().ContractVersions
	if len(got) != len(files) {
		t.Fatalf("contract versions %v, want exactly %d entries", got, len(files))
	}
	for name, file := range files {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "cli", file))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			SchemaVersion int `json:"schema_version"`
			Properties    struct {
				SchemaVersion struct {
					Const int `json:"const"`
				} `json:"schema_version"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		want := doc.SchemaVersion
		if want == 0 {
			want = doc.Properties.SchemaVersion.Const
		}
		if want == 0 || got[name] != want {
			t.Errorf("%s = %d, %s declares schema_version %d", name, got[name], file, want)
		}
	}
}
