//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"errors"
	"os"
)

func openObservedFile(*os.Root, string) (*os.File, error) {
	return nil, errors.New("safe init discovery is unsupported on this platform")
}
