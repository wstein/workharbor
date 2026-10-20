// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"bytes"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"strings"
	"testing"
)

func TestInitDevcontainerSelectionValidated(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		code int
	}{
		{`{ // custom JSONC
 "image": "debian:bookworm", }`, 0},
		{`{"image":"first","image":"second"}`, 68}, {`{"image":`, 68},
	} {
		s := snapshot()
		s.Entries["custom.json"] = scaffold.Entry{Kind: "file", Data: []byte(tc.raw)}
		p, err := scaffold.Build(s, scaffold.Options{Devcontainer: str("custom.json"), DryRun: true})
		if int(exitcode.FromError(err)) != tc.code {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if tc.code == 0 && (!bytes.Contains(p.Files()["workharbor.json"], []byte(`"devcontainer_path": "custom.json"`)) || !bytes.Equal(p.Files()["custom.json"], []byte(tc.raw))) {
			t.Fatal("selection not reused verbatim")
		}
	}
}
func TestInitDevcontainerSameSelectionPreservesManifest(t *testing.T) {
	s := snapshot()
	raw := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: raw}
	p, err := scaffold.Build(s, scaffold.Options{Devcontainer: str(".devcontainer/devcontainer.json"), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Files()["workharbor.json"], raw) {
		t.Fatal("same selection rewrote manifest")
	}
}
func TestInitDevcontainerSelectionNeedsManifestConsent(t *testing.T) {
	s := snapshot()
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)}
	s.Entries["custom.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}
	_, err := scaffold.Build(s, scaffold.Options{Devcontainer: str("custom.json"), DryRun: true})
	if int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--overwrite workharbor.json") {
		t.Fatalf("missing path consent: %v", err)
	}
}
