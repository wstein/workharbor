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

// recoverDockerfile defines the base of whr workspace recover: git and a few
// rescue tools around the mounted BundleDock toolbox. Built the same way.
//
//go:embed recover.Dockerfile
var recoverDockerfile []byte

func localImageTag(prefix string, dockerfile []byte) string {
	sum := sha256.Sum256(dockerfile)
	return prefix + ":" + hex.EncodeToString(sum[:])[:12]
}

// ToolsImage is the local tag of the tooling image for the embedded Dockerfile.
func ToolsImage() string { return localImageTag("whr-tools", toolsDockerfile) }

// RecoverImage is the local tag of the recovery base image.
func RecoverImage() string { return localImageTag("whr-recover", recoverDockerfile) }
