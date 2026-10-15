// SPDX-License-Identifier: EUPL-1.2
// Package devcontainer owns installed official tools; repository paths never select code.
package devcontainer

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed tool-files.json
var fileManifest []byte

//go:embed parse.cjs
var ParserScript string

type Tools struct{ NodePath, Root string }

func (p Tools) Environment() []string {
	return []string{"PATH=" + filepath.Dir(p.NodePath), "LANG=C.UTF-8"}
}
func unavailable() error { return errors.New("tool_unavailable") }
func (p Tools) Verify(ctx context.Context, cli bool) error {
	if !filepath.IsAbs(p.NodePath) || !filepath.IsAbs(p.Root) {
		return unavailable()
	}
	for _, base := range []string{p.Root, filepath.Join(p.Root, "node_modules")} {
		info, e := os.Lstat(base)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return unavailable()
		}
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(fileManifest, &manifest) != nil || len(manifest.Files) == 0 {
		return unavailable()
	}
	for relative, expected := range manifest.Files {
		if !cli && !strings.HasPrefix(relative, "jsonc-parser/") {
			continue
		}
		path := filepath.Join(p.Root, "node_modules")
		for _, part := range strings.Split(relative, "/") {
			path = filepath.Join(path, part)
			info, e := os.Lstat(path)
			if e != nil || info.Mode()&os.ModeSymlink != 0 {
				return unavailable()
			}
		}
		f, e := os.Open(path)
		if e != nil {
			return unavailable()
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return unavailable()
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		ce := f.Close()
		if e != nil || ce != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return unavailable()
		}
	}
	version := exec.CommandContext(ctx, p.NodePath, "--version")
	version.Env = p.Environment()
	version.Stderr = io.Discard
	raw, e := version.Output()
	if e != nil || strings.TrimSpace(string(raw)) != "v26.11.1" {
		return unavailable()
	}
	if cli {
		c := exec.CommandContext(ctx, p.NodePath, filepath.Join(p.Root, "node_modules/@devcontainers/cli/dist/spec-node/devContainersSpecCLI.js"), "--version")
		c.Env = p.Environment()
		c.Stderr = io.Discard
		raw, e := c.Output()
		if e != nil || strings.TrimSpace(string(raw)) != "0.89.0" {
			return unavailable()
		}
	}
	return nil
}
