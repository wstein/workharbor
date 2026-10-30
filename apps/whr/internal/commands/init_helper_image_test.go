// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const initBasePin = "docker.io/library/busybox@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// init --helper-base-image previews and writes the pin through the normal
// consent and never contacts Docker; the JSON result stays inside the schema.
func TestInitHelperBaseImageFlagPreviewAndWrite(t *testing.T) {
	dir := initFixture(t)
	t.Setenv("PATH", t.TempDir()) // no docker executable is reachable
	preview := run(t, nil, "init", dir, "--non-interactive", "--dry-run", "--helper-base-image", initBasePin, "--json")
	if preview.code != 0 || !strings.Contains(preview.err, initBasePin) {
		t.Fatalf("preview %+v", preview)
	}
	doc, err := jsonUnmarshal(preview.stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	setting := decodeEnvelope(t, preview.stdout)["result"].(map[string]any)["settings"].(map[string]any)["helper_images"].(map[string]any)
	if setting["origin"] != "flag" || setting["value"].(map[string]any)["base"] != initBasePin {
		t.Fatalf("setting %v", setting)
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote the manifest")
	}
	apply := run(t, nil, "workspace", "init", dir, "--non-interactive", "--yes", "--helper-base-image", initBasePin)
	if apply.code != 0 {
		t.Fatalf("apply %+v", apply)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err != nil || !strings.Contains(string(raw), `"helper_images"`) || !strings.Contains(string(raw), initBasePin) {
		t.Fatalf("manifest %s %v", raw, err)
	}
	bad := run(t, nil, "init", dir, "--non-interactive", "--dry-run", "--helper-base-image", "docker.io/library/busybox:latest")
	if bad.code != 64 || !strings.Contains(bad.err, "helper-base-image") {
		t.Fatalf("unpinned reference %+v", bad)
	}
}
