// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"bytes"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"strings"
)

type DockerfilePlan struct {
	Bases               []string
	SelectedBase, Stage string
	LastStageUnnamed    bool
}

// AnalyzeDockerfile selects only a literal named stage or the final stage. The
// maintained AST remains the source of syntax; this does not execute a Dockerfile.
func AnalyzeDockerfile(raw []byte, target string) (DockerfilePlan, error) {
	refs, e := ValidateDockerfile(raw)
	if e != nil {
		return DockerfilePlan{}, e
	}
	parsed, e := parser.Parse(bytes.NewReader(raw))
	if e != nil {
		return DockerfilePlan{}, unsupportedConfiguration()
	}
	aliases := map[string]string{}
	selected := DockerfilePlan{Bases: refs}
	found := target == ""
	for _, node := range parsed.AST.Children {
		if !strings.EqualFold(node.Value, "from") {
			continue
		}
		ref := node.Next.Value
		base := ref
		if inherited, ok := aliases[strings.ToLower(ref)]; ok {
			base = inherited
		}
		name := ""
		if node.Next.Next != nil {
			name = strings.ToLower(node.Next.Next.Next.Value)
		}
		if name == "dev_containers_target_stage" || name == "dev_container_auto_added_stage_label" {
			return DockerfilePlan{}, unsupportedConfiguration()
		}
		if name != "" {
			aliases[strings.ToLower(name)] = base
		}
		if target == "" || strings.EqualFold(name, target) {
			stage := name
			if stage == "" {
				stage = "dev_container_auto_added_stage_label"
			}
			selected.SelectedBase = base
			selected.Stage = stage
			selected.LastStageUnnamed = name == ""
			found = true
		}
	}
	if !found || selected.SelectedBase == "" {
		return DockerfilePlan{}, unsupportedConfiguration()
	}
	return selected, nil
}
