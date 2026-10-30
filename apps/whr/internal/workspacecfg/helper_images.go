// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"fmt"
	"regexp"
)

// MaxImageReference bounds a helper image reference in bytes.
const MaxImageReference = 512

// digestPinned is a lowercase registry/repository path, an optional tag and a
// mandatory sha256 digest. It cannot start with "-", so it is never an option.
var digestPinned = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?@sha256:[0-9a-f]{64}$`)

// DigestPinnedImage reports whether ref is an admitted digest-pinned image
// reference (home clone/backup amendment, 2026-10-29).
func DigestPinnedImage(ref string) bool {
	return len(ref) <= MaxImageReference && digestPinned.MatchString(ref)
}

// checkHelperImages validates the closed helper_images object and returns the
// rsync pin when it is valid.
func checkHelperImages(c *collector, h jvalue) string {
	schema := func(format string, args ...any) {
		c.add("manifest.schema", ManifestFile, fmt.Sprintf(format, args...), "")
	}
	if h.kind != jObject {
		schema("helper_images must be an object")
		return ""
	}
	ok := true
	for _, m := range h.obj {
		if m.key != "rsync" {
			schema("unknown helper_images field %s", quoteKey(m.key))
			ok = false
		}
	}
	v, found := h.get("rsync")
	switch {
	case !found:
		return ""
	case v.kind != jString:
		schema("helper_images.rsync must be a string")
		return ""
	case !DigestPinnedImage(v.str):
		schema("helper_images.rsync must be an image reference pinned by digest (name@sha256:<64 hex>)")
		return ""
	}
	if !ok {
		return ""
	}
	return v.str
}
