// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"context"
	"fmt"
	"io/fs"
)

// ReadManifest reads only workharbor.json through the existing no-follow,
// bounded reader and validates it with CheckManifest. It does not inspect Git,
// devcontainer or project content. An absent manifest returns fs.ErrNotExist;
// an absent directory returns ErrNotFound. Rejected content is in Findings.
func ReadManifest(ctx context.Context, directory string) (ManifestResult, error) {
	if err := ctx.Err(); err != nil {
		return ManifestResult{}, err
	}
	d, err := openDir(directory)
	if err != nil {
		return ManifestResult{}, err
	}
	defer d.Close()
	raw, aerr := d.readFile(ManifestFile, MaxManifestBytes)
	if err := ctx.Err(); err != nil {
		return ManifestResult{}, err
	}
	if aerr != nil {
		if aerr.kind == accMissing {
			return ManifestResult{}, fmt.Errorf("%s: %w", ManifestFile, fs.ErrNotExist)
		}
		if aerr.kind == accTooLarge {
			return ManifestResult{}, fmt.Errorf("%s", tooLarge(ManifestFile, MaxManifestBytes))
		}
		return ManifestResult{}, fmt.Errorf("%s %s", ManifestFile, aerr.why)
	}
	result := CheckManifest(raw)
	if err := ctx.Err(); err != nil {
		return ManifestResult{}, err
	}
	return result, nil
}
