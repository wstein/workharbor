//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"fmt"
	"io/fs"
	"os"
)

func createExclusive(root *os.Root, name string, mode fs.FileMode) (*os.File, error) {
	return nil, fmt.Errorf("safe init creation unsupported on this platform")
}
