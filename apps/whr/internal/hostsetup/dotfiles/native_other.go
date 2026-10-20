//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package dotfiles

import "io/fs"

// Open refuses platforms without this adapter; it never falls back to path I/O.
func Open(home string, clock Clock) (*Engine, func() error, error) { return nil, nil, fs.ErrInvalid }
