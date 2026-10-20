// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// JSON Schema minLength counts Unicode code points, including each combining
// mark, rather than UTF-8 bytes, UTF-16 units or user-perceived graphemes.
func TestManifestProjectPathMinimumCountsUnicodeCharacters(t *testing.T) {
	for _, tc := range []struct{ name, raw, codes string }{
		{"empty", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":""}}`, "manifest.path_invalid,manifest.schema"},
		{"ascii1", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"a"}}`, "manifest.path_invalid,manifest.schema"},
		{"ascii2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"ab"}}`, "manifest.path_invalid,manifest.schema"},
		{"ascii3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"abc"}}`, "manifest.path_invalid"},
		{"cjk1", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"界"}}`, "manifest.path_invalid,manifest.schema"},
		{"cjk2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"界界"}}`, "manifest.path_invalid,manifest.schema"},
		{"cjk3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"界界界"}}`, "manifest.path_invalid"},
		{"emoji1", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"😀"}}`, "manifest.path_invalid,manifest.schema"},
		{"emoji2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"😀😀"}}`, "manifest.path_invalid,manifest.schema"},
		{"emoji3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"😀😀😀"}}`, "manifest.path_invalid"},
		{"latin1", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"é"}}`, "manifest.path_invalid,manifest.schema"},
		{"latin2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"éé"}}`, "manifest.path_invalid,manifest.schema"},
		{"latin3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"ééé"}}`, "manifest.path_invalid"},
		{"combining2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"é"}}`, "manifest.path_invalid,manifest.schema"},
		{"combining3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"é́"}}`, "manifest.path_invalid"},
		{"mixed2", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"a界"}}`, "manifest.path_invalid,manifest.schema"},
		{"mixed3", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"../x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"a界b"}}`, "manifest.path_invalid"},
		{"valid_ascii", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"x/a"}}`, ""},
		{"valid_cjk_root", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"界","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"界/a"}}`, ""},
		{"valid_emoji_root", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"😀","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"😀/a"}}`, ""},
		{"valid_combining_root", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"é","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"é/a"}}`, ""},
		{"valid_root_wrong_path", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"x","bundles":[],"project":{"url":"https://f.example/o/a.git","path":"界"}}`, "manifest.schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := workspacecfg.CheckManifest([]byte(tc.raw))
			got := strings.Join(manifestCodes(result.Findings), ",")
			if got != tc.codes {
				t.Fatalf("finding codes %q, want %q; findings=%+v", got, tc.codes, result.Findings)
			}
		})
	}
}
