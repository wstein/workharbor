// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRawRegistryManifestIntegerLexemesAreNotRoundedByJSONC(t *testing.T) {
	for _, kind := range []string{"schema-version-fraction", "size-fraction", "size-exponent"} {
		t.Run(kind, func(t *testing.T) {
			version, size := "2", "100"
			switch kind {
			case "schema-version-fraction":
				version = "2.0000000000000001"
			case "size-fraction":
				size = "100.0000000000000001"
			case "size-exponent":
				size = "1e2"
			}
			raw := []byte(`{"schemaVersion":` + version + `,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + strings.Repeat("b", 64) + `","size":` + size + `},"layers":[]}`)
			sum := sha256.Sum256(raw)
			ref := "example.test/fixture@sha256:" + hex.EncodeToString(sum[:])
			if _, e := selectRegistryManifest(context.Background(), registryParser(t), ref, "arm64", raw); e == nil {
				t.Fatal("raw noninteger lexeme normalized into accepted descriptor", kind)
			}
		})
	}
}
