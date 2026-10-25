//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import "path/filepath"

const stageMaxSmokeBytes = 256 << 10

// Validate the exact buffer sent to Docker, rather than reopening the file after
// its cooperative inventory check. The digest comes from the admitted capture.
func readStageTrustedSmoke(root, digest string) ([]byte, error) {
	data, err := readStageInput(filepath.Join(root, ".devcontainer", "smoke-container.sh"), stageMaxSmokeBytes)
	if err != nil {
		return nil, err
	}
	if !stageImageID(digest) || stageBytesDigest(data) != digest {
		return nil, stageConflict("trusted smoke bytes changed")
	}
	return data, nil
}
