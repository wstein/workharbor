// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package wizard

import (
	"context"
	"io"
	"os"
)

func IsTerminal(*os.File) bool                       { return false }
func Reader(_ context.Context, f *os.File) io.Reader { return f }
