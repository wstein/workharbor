// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
)

// JSONC executes only the installed product companion, never repository executables.
type JSONC struct{ NodePath, CompanionDir string }

func (p JSONC) Parse(ctx context.Context, raw []byte, strict bool) (any, error) {
	if !utf8.Valid(raw) {
		return nil, invalidConfiguration()
	}
	tools := devcontainer.Tools{NodePath: p.NodePath, Root: p.CompanionDir}
	if tools.Verify(ctx, false) != nil {
		return nil, &Error{Code: "tool_unavailable"}
	}
	mode := "jsonc"
	if strict {
		mode = "strict"
	}
	cmd := exec.CommandContext(ctx, p.NodePath, "-e", devcontainer.ParserScript, mode)
	cmd.Env = tools.Environment()
	cmd.Dir = p.CompanionDir
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil {
		return nil, invalidConfiguration()
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, invalidConfiguration()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, invalidConfiguration()
	}
	return value, nil
}
