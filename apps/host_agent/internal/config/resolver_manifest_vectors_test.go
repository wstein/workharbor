// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type resolverManifestVector struct {
	ID            string          `json:"id"`
	Document      json.RawMessage `json:"document"`
	RawText       *string         `json:"raw_text"`
	BytesHex      *string         `json:"bytes_hex"`
	GenerateBytes *struct {
		Total    int    `json:"total"`
		Template string `json:"template"`
		Padding  string `json:"padding"`
		After    bool   `json:"padding_after_template"`
	} `json:"generate_bytes"`
	Codes []string `json:"expect_codes"`
}

func (v resolverManifestVector) bytes(t *testing.T) []byte {
	t.Helper()
	switch {
	case v.Document != nil:
		return v.Document
	case v.RawText != nil:
		return []byte(*v.RawText)
	case v.BytesHex != nil:
		b, e := hex.DecodeString(*v.BytesHex)
		if e != nil {
			t.Fatal(e)
		}
		return b
	case v.GenerateBytes != nil:
		g := v.GenerateBytes
		if g.Padding != "space" || !g.After || g.Total < len(g.Template) {
			t.Fatal("unsupported shared vector generator")
		}
		return []byte(g.Template + strings.Repeat(" ", g.Total-len(g.Template)))
	}
	t.Fatal("shared vector has no bytes")
	return nil
}
func resolverManifestError(t *testing.T, e error, want string) {
	t.Helper()
	var typed *Error
	if !errors.As(e, &typed) || typed.Code != want || e.Error() != want {
		t.Fatalf("manifest refusal: %T %v; want typed %s", e, e, want)
	}
}
func writeResolverManifest(t *testing.T, repo string, raw []byte) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(repo, "workharbor.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestResolverManifestVectors(t *testing.T) {
	raw, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "workspace", "vectors.json"))
	if e != nil {
		t.Fatal(e)
	}
	var vectors struct {
		Cases []resolverManifestVector `json:"cases"`
	}
	if e = json.Unmarshal(raw, &vectors); e != nil {
		t.Fatal(e)
	}
	if len(vectors.Cases) == 0 {
		t.Fatal("no manifest vectors")
	}

	seen := map[string]bool{}
	for _, v := range vectors.Cases {
		t.Run(v.ID, func(t *testing.T) {
			if v.ID == "" || seen[v.ID] {
				t.Fatal("duplicate or empty shared vector ID")
			}
			seen[v.ID] = true
			raw := v.bytes(t)
			_, _, e := decodeResolverManifest(raw)
			if len(v.Codes) == 0 {
				if e != nil {
					t.Fatalf("shared valid manifest refused: %v", e)
				}
				return
			}
			want := "invalid_configuration"
			if len(v.Codes) == 1 && v.Codes[0] == "manifest.bundles_deferred" {
				want = "unsupported_configuration"
			}
			resolverManifestError(t, e, want)
		})
	}
}
