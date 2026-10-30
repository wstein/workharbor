// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	_ "embed"

	"crypto/sha256"
	"encoding/hex"
)

// toolsDockerfile defines the small local image that carries rsync and tar for
// home copies. It is built on the developer's machine on first use; the tag is
// the hash of this file, so editing it yields a new image.
//
//go:embed tools.Dockerfile
var toolsDockerfile []byte

// ToolsImage is the local tag of the tooling image for the embedded Dockerfile.
func ToolsImage() string {
	sum := sha256.Sum256(toolsDockerfile)
	return "whr-tools:" + hex.EncodeToString(sum[:])[:12]
}
