// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

const basePin = "docker.io/library/busybox@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func helperPin(t *testing.T, p *scaffold.Proposal) any {
	t.Helper()
	var manifest map[string]any
	if err := json.Unmarshal(p.Files()["workharbor.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest["helper_images"]
}

// --helper-base-image is written only when given, and only through the normal proposal.
func TestInitHelperBaseImageIsProposedOnlyWhenGiven(t *testing.T) {
	p, err := scaffold.Build(snapshot(), defaults())
	if err != nil {
		t.Fatal(err)
	}
	if pin := helperPin(t, p); pin != nil {
		t.Fatalf("pin written without --helper-base-image: %v", pin)
	}
	if _, ok := p.Preview().Settings["helper_images"]; ok {
		t.Fatal("setting reported without a pin")
	}
	o := defaults()
	o.HelperBaseImage = str(basePin)
	p, err = scaffold.Build(snapshot(), o)
	if err != nil {
		t.Fatal(err)
	}
	if pin, _ := helperPin(t, p).(map[string]any); pin["base"] != basePin || len(pin) != 1 {
		t.Fatalf("pin %v", pin)
	}
	s := p.Preview().Settings["helper_images"]
	if s.Origin != "flag" || s.Value.(map[string]any)["base"] != basePin {
		t.Fatalf("setting %+v", s)
	}
	if !strings.Contains(p.HumanPreview(), basePin) {
		t.Fatal("human preview hides the pinned digest")
	}
}

func TestInitHelperBaseImageRequiresDigestPin(t *testing.T) {
	for _, bad := range []string{"", "docker.io/library/busybox:latest", "busybox@sha256:" + strings.Repeat("a", 64), "--privileged"} {
		o := defaults()
		o.HelperBaseImage = str(bad)
		if _, err := scaffold.Build(snapshot(), o); exitcode.FromError(err) != exitcode.Usage {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestInitHelperBaseImagePreservedAndOverwriteConsent(t *testing.T) {
	existing := `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"start_dir":".","human_shell":"fish","console_mode":"plain","shortcuts":"on","helper_images":{"base":"` + basePin + `"}}` + "\n"
	s := snapshot()
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: []byte(existing)}
	p, err := scaffold.Build(s, scaffold.Options{Name: str("demo"), DryRun: true, Overwrite: []string{"workharbor.json"}})
	if err != nil {
		t.Fatal(err)
	}
	if pin, _ := helperPin(t, p).(map[string]any); pin["base"] != basePin {
		t.Fatalf("existing pin dropped by another edit: %v", pin)
	}
	if got := p.Preview().Settings["helper_images"]; got.Origin != "existing" {
		t.Fatalf("origin %+v", got)
	}
	plain := snapshot()
	plain.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: []byte(strings.Replace(existing, `,"helper_images":{"base":"`+basePin+`"}`, "", 1))}
	o := scaffold.Options{HelperBaseImage: str(basePin), DryRun: true}
	if _, err := scaffold.Build(plain, o); exitcode.FromError(err) != exitcode.NeedsHuman {
		t.Fatalf("existing manifest edited without --overwrite: %v", err)
	}
	o.Overwrite = []string{"workharbor.json"}
	if p, err = scaffold.Build(plain, o); err != nil {
		t.Fatal(err)
	}
	if pin, _ := helperPin(t, p).(map[string]any); pin["base"] != basePin {
		t.Fatalf("pin %v", pin)
	}
}
