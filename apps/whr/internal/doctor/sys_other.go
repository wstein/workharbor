// SPDX-License-Identifier: EUPL-1.2

//go:build !(linux || darwin || freebsd)

package doctor

import (
	"errors"
	"os"
)

var errUnsupportedOS = errors.New("not measured on this platform")

func freeBytes(string) (uint64, error) { return 0, errUnsupportedOS }

func openNoFollow(string) (*os.File, error) { return nil, errUnsupportedOS }
