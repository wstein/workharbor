// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const pinnedBase = "docker.io/library/busybox@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The helper image pin is additive, optional, schema_version 2 only and must be
// digest-pinned (home clone/backup amendment, 2026-10-29).
func TestManifestHelperImagesBasePin(t *testing.T) {
	r := workspacecfg.CheckManifest(v2(`,"helper_images":{"base":"` + pinnedBase + `"}`))
	if len(r.Findings) != 0 || r.Manifest == nil || r.Manifest.HelperBaseImage != pinnedBase {
		t.Fatalf("valid pin: %+v %+v", r.Findings, r.Manifest)
	}
	if r := workspacecfg.CheckManifest(v2("")); len(r.Findings) != 0 || r.Manifest.HelperBaseImage != "" {
		t.Fatalf("absent pin: %+v", r)
	}
	tagged := "registry.example:5000/tools/busybox:1.37@sha256:" + strings.Repeat("a", 64)
	if r := workspacecfg.CheckManifest(v2(`,"helper_images":{"base":"` + tagged + `"}`)); len(r.Findings) != 0 || r.Manifest.HelperBaseImage != tagged {
		t.Fatalf("tag plus digest: %+v", r)
	}
	for name, extra := range map[string]string{
		"tag only":        `,"helper_images":{"base":"docker.io/library/busybox:latest"}`,
		"short digest":    `,"helper_images":{"base":"docker.io/library/busybox@sha256:abc"}`,
		"upper digest":    `,"helper_images":{"base":"docker.io/library/busybox@sha256:` + strings.Repeat("A", 64) + `"}`,
		"other algorithm": `,"helper_images":{"base":"docker.io/library/busybox@sha512:` + strings.Repeat("a", 64) + `"}`,
		"whitespace":      `,"helper_images":{"base":" ` + pinnedBase + `"}`,
		"option-like":     `,"helper_images":{"base":"--privileged@sha256:` + strings.Repeat("a", 64) + `"}`,
		"not a string":    `,"helper_images":{"base":1}`,
		"not an object":   `,"helper_images":"` + pinnedBase + `"`,
		"unknown member":  `,"helper_images":{"base":"` + pinnedBase + `","rsync":"` + pinnedBase + `"}`,
		"empty object ok": ``,
		"too long":        `,"helper_images":{"base":"` + strings.Repeat("a", 500) + `/r@sha256:` + strings.Repeat("a", 64) + `"}`,
	} {
		r := workspacecfg.CheckManifest(v2(extra))
		got := strings.Join(manifestCodes(r.Findings), ",")
		want := "manifest.schema"
		if name == "empty object ok" {
			want = ""
		}
		if got != want || (want != "" && r.Manifest != nil && r.Manifest.HelperBaseImage != "") {
			t.Errorf("%s: codes %q pin %q", name, got, r.Manifest.HelperBaseImage)
		}
	}
	v1 := []byte(`{"schema_version":1,"devcontainer_path":"a","projects_path":"b","bundles":[],"helper_images":{"base":"` + pinnedBase + `"}}`)
	r = workspacecfg.CheckManifest(v1)
	if got := strings.Join(manifestCodes(r.Findings), ","); got != "manifest.schema" || !strings.Contains(r.Findings[0].Message, "needs schema_version 2") {
		t.Fatalf("v1 pin: %+v", r.Findings)
	}
}

func TestDigestPinnedImageReference(t *testing.T) {
	if !workspacecfg.DigestPinnedImage(pinnedBase) || workspacecfg.DigestPinnedImage("busybox:latest") || workspacecfg.DigestPinnedImage("") {
		t.Fatal("digest-pinned reference grammar")
	}
}
