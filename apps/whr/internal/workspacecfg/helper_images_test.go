// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const pinnedRsync = "quay.io/bundledock/rsync@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The helper image pin is additive, optional, schema_version 2 only and must be
// digest-pinned (home clone/backup amendment, 2026-10-29).
func TestManifestHelperImagesRsyncPin(t *testing.T) {
	r := workspacecfg.CheckManifest(v2(`,"helper_images":{"rsync":"` + pinnedRsync + `"}`))
	if len(r.Findings) != 0 || r.Manifest == nil || r.Manifest.RsyncImage != pinnedRsync {
		t.Fatalf("valid pin: %+v %+v", r.Findings, r.Manifest)
	}
	if r := workspacecfg.CheckManifest(v2("")); len(r.Findings) != 0 || r.Manifest.RsyncImage != "" {
		t.Fatalf("absent pin: %+v", r)
	}
	tagged := "registry.example:5000/tools/rsync:3.4.1@sha256:" + strings.Repeat("a", 64)
	if r := workspacecfg.CheckManifest(v2(`,"helper_images":{"rsync":"` + tagged + `"}`)); len(r.Findings) != 0 || r.Manifest.RsyncImage != tagged {
		t.Fatalf("tag plus digest: %+v", r)
	}
	for name, extra := range map[string]string{
		"tag only":        `,"helper_images":{"rsync":"quay.io/bundledock/rsync:latest"}`,
		"short digest":    `,"helper_images":{"rsync":"quay.io/bundledock/rsync@sha256:abc"}`,
		"upper digest":    `,"helper_images":{"rsync":"quay.io/bundledock/rsync@sha256:` + strings.Repeat("A", 64) + `"}`,
		"other algorithm": `,"helper_images":{"rsync":"quay.io/bundledock/rsync@sha512:` + strings.Repeat("a", 64) + `"}`,
		"whitespace":      `,"helper_images":{"rsync":" ` + pinnedRsync + `"}`,
		"option-like":     `,"helper_images":{"rsync":"--privileged@sha256:` + strings.Repeat("a", 64) + `"}`,
		"not a string":    `,"helper_images":{"rsync":1}`,
		"not an object":   `,"helper_images":"` + pinnedRsync + `"`,
		"unknown member":  `,"helper_images":{"rsync":"` + pinnedRsync + `","tar":"` + pinnedRsync + `"}`,
		"empty object ok": ``,
		"too long":        `,"helper_images":{"rsync":"` + strings.Repeat("a", 500) + `/r@sha256:` + strings.Repeat("a", 64) + `"}`,
	} {
		r := workspacecfg.CheckManifest(v2(extra))
		got := strings.Join(manifestCodes(r.Findings), ",")
		want := "manifest.schema"
		if name == "empty object ok" {
			want = ""
		}
		if got != want || (want != "" && r.Manifest != nil && r.Manifest.RsyncImage != "") {
			t.Errorf("%s: codes %q pin %q", name, got, r.Manifest.RsyncImage)
		}
	}
	v1 := []byte(`{"schema_version":1,"devcontainer_path":"a","projects_path":"b","bundles":[],"helper_images":{"rsync":"` + pinnedRsync + `"}}`)
	r = workspacecfg.CheckManifest(v1)
	if got := strings.Join(manifestCodes(r.Findings), ","); got != "manifest.schema" || !strings.Contains(r.Findings[0].Message, "needs schema_version 2") {
		t.Fatalf("v1 pin: %+v", r.Findings)
	}
}

func TestDigestPinnedImageReference(t *testing.T) {
	if !workspacecfg.DigestPinnedImage(pinnedRsync) || workspacecfg.DigestPinnedImage("rsync:latest") || workspacecfg.DigestPinnedImage("") {
		t.Fatal("digest-pinned reference grammar")
	}
}
