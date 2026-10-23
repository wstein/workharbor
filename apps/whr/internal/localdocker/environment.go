// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"errors"
	"os"
)

// ClientEnvironment supplies the shared minimal host-process allowlist. The
// private empty config prevents loading credentials, contexts or helper config.
// Callers must defer cleanup; no session content or selector is written here.
func ClientEnvironment() ([]string, func(), error) {
	dir, err := os.MkdirTemp("", "whr-docker-config-")
	if err != nil {
		return nil, nil, errors.New("cannot isolate Docker configuration")
	}
	return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "DOCKER_CONFIG=" + dir}, func() { _ = os.RemoveAll(dir) }, nil
}
