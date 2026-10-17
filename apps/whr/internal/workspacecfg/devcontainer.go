// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"bytes"
	"fmt"
	"regexp"
	"unicode/utf8"
)

const matrixDoc = "docs/development/devcontainer-matrix.md"

// providerKeys are the top-level devcontainer fields of the provider subset
// (specs/006-whr-cli/contracts/devcontainer-provider.md). initializeCommand and
// postAttachCommand are reported as host commands instead.
var providerKeys = map[string]bool{
	"name": true, "image": true, "build": true, "containerUser": true, "remoteUser": true,
	"containerEnv": true, "remoteEnv": true, "overrideCommand": true, "userEnvProbe": true,
	"updateRemoteUserUID": true, "waitFor": true, "shutdownAction": true,
	"onCreateCommand": true, "updateContentCommand": true, "postCreateCommand": true, "postStartCommand": true,
}

var (
	digestImage = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	numericUser = regexp.MustCompile(`^(0|[1-9][0-9]*):(0|[1-9][0-9]*)$`)
)

// CheckDevcontainer validates the raw bytes of the devcontainer file in memory:
// size, encoding, bounded strict JSONC (comments and trailing commas, duplicate
// keys and depth rejected) and the provider-subset warnings. It is a
// pre-flight, not the authority: the host agent parses again at trust time.
func CheckDevcontainer(raw []byte) []Finding {
	var c collector
	fail := func(code, msg string) []Finding {
		c.add(code, "", msg, "")
		return c.list
	}
	if len(raw) > MaxDevcontainerLen {
		return fail("devcontainer.too_large", fmt.Sprintf("the devcontainer file is larger than %d bytes", MaxDevcontainerLen))
	}
	switch {
	case bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")):
		return fail("devcontainer.encoding", "the devcontainer file begins with a byte-order mark")
	case bytes.IndexByte(raw, 0) >= 0:
		return fail("devcontainer.encoding", "the devcontainer file contains a NUL byte")
	case !utf8.Valid(raw):
		return fail("devcontainer.encoding", "the devcontainer file is not valid UTF-8")
	}
	doc, serr := parseStrict(raw, true)
	if serr != nil {
		code := "devcontainer.syntax"
		if serr.duplicate {
			code = "devcontainer.duplicate_key"
		}
		return fail(code, serr.msg)
	}
	if doc.kind != jObject {
		return fail("devcontainer.syntax", "the top-level value must be an object")
	}
	for _, m := range doc.obj {
		switch m.key {
		case "initializeCommand", "postAttachCommand":
			c.add("devcontainer.host_command", "", fmt.Sprintf("%s runs on the host and is outside the provider subset (%s)", m.key, matrixDoc), "")
		default:
			if !providerKeys[m.key] {
				c.add("devcontainer.unsupported_key", "", fmt.Sprintf("field %s is outside the provider subset (%s)", quoteKey(m.key), matrixDoc), "")
			}
		}
	}
	if img, ok := doc.get("image"); ok && !(img.kind == jString && digestImage.MatchString(img.str)) {
		c.add("devcontainer.image_not_digest_pinned", "", "image is not pinned by digest (name@sha256:...); the provider refuses a tag", "")
	}
	for _, k := range []string{"containerUser", "remoteUser"} {
		if u, ok := doc.get(k); ok && !(u.kind == jString && numericUser.MatchString(u.str)) {
			c.add("devcontainer.users_not_numeric", "", k+" is not an explicit numeric UID:GID pair", "")
		}
	}
	return c.list
}
