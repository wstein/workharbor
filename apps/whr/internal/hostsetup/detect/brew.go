// SPDX-License-Identifier: EUPL-1.2
package detect

import (
	"context"
	"errors"
	"io/fs"
)

// BrewExecutable reports metadata availability only at the two approved
// Homebrew paths. It reuses detection's componentwise link resolver and five-
// second observation budget, never invoking brew or reading file contents.
// This bool seam does not lease an executable identity for a later process.
// The package-manager FS interface has no context; its caller checks request
// cancellation after each observation. Slow OS metadata calls are checked when
// they return, as in other detector observations.
func (d Detector) BrewExecutable(p string) (bool, error) {
	if p != "/opt/homebrew/bin/brew" && p != "/usr/local/bin/brew" {
		return false, fs.ErrInvalid
	}
	if d.FS == nil || d.Clock == nil {
		return false, errors.New("brew availability ports unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeBudget)
	defer cancel()
	_, err := d.resolve(ctx, p, d.Clock.Now())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
