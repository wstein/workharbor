// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"reflect"
	"strings"
	"unicode/utf8"
)

type emptyBuildEnv struct{}

func (emptyBuildEnv) Get(string) (string, bool) { return "", false }
func (emptyBuildEnv) Keys() []string            { return nil }

type normalizedNode struct {
	Value      string
	Flags      []string
	Attributes map[string]bool
	Heredocs   []parser.Heredoc
	Next       *normalizedNode
	Children   []*normalizedNode
}

func normalized(n *parser.Node) *normalizedNode {
	if n == nil {
		return nil
	}
	out := &normalizedNode{Value: n.Value, Flags: n.Flags, Attributes: n.Attributes, Heredocs: n.Heredocs, Next: normalized(n.Next)}
	for _, child := range n.Children {
		out.Children = append(out.Children, normalized(child))
	}
	return out
}

// ValidateGeneratedDockerfile admits only the official no-Features wrapper,
// byte-preserved source semantics and a nonempty independently expected label.
// It runs before build forwarding, never as a post-build repair.
func ValidateGeneratedDockerfile(ctx context.Context, p config.JSONC, source, generated []byte, target string, metadata []map[string]any) error {
	plan, e := config.AnalyzeDockerfile(source, target)
	if e != nil {
		return e
	}
	if !utf8.Valid(generated) || bytes.IndexByte(generated, 0) >= 0 {
		return fail("configuration_changed")
	}
	if _, _, _, found := parser.DetectSyntax(generated); found {
		return fail("configuration_changed")
	}
	original, e := parser.Parse(bytes.NewReader(source))
	if e != nil {
		return fail("configuration_changed")
	}
	candidate, e := parser.Parse(bytes.NewReader(generated))
	if e != nil {
		return fail("configuration_changed")
	}
	a, b := original.AST.Children, candidate.AST.Children
	if len(b) != len(a)+3 {
		return fail("configuration_changed")
	}
	prefix, _ := parser.Parse(strings.NewReader("ARG _DEV_CONTAINERS_BASE_IMAGE=scratch\n"))
	wrapper, _ := parser.Parse(strings.NewReader("FROM $_DEV_CONTAINERS_BASE_IMAGE AS dev_containers_target_stage\n"))
	if !reflect.DeepEqual(normalized(b[0]), normalized(prefix.AST.Children[0])) || !reflect.DeepEqual(normalized(b[len(b)-2]), normalized(wrapper.AST.Children[0])) {
		return fail("configuration_changed")
	}
	lastFrom := -1
	for i, n := range a {
		if strings.EqualFold(n.Value, "from") {
			lastFrom = i
		}
	}
	for i, n := range a {
		expected := normalized(n)
		if i == lastFrom && plan.LastStageUnnamed && target == "" {
			expected.Next.Next = &normalizedNode{Value: "AS", Next: &normalizedNode{Value: plan.Stage}}
		}
		actual := normalized(b[i+1])
		if !reflect.DeepEqual(expected, actual) {
			return fail("configuration_changed")
		}
	}
	label := b[len(b)-1]
	if !strings.EqualFold(label.Value, "label") || len(label.Flags) != 0 || len(label.Heredocs) != 0 || label.Next == nil || label.Next.Value != "devcontainer.metadata" || label.Next.Next == nil || label.Next.Next.Next == nil || label.Next.Next.Next.Value != "=" || label.Next.Next.Next.Next != nil {
		return fail("configuration_changed")
	}
	value, unmatched, e := shell.NewLex(candidate.EscapeToken).ProcessWord(label.Next.Next.Value, emptyBuildEnv{})
	if e != nil || len(unmatched) != 0 {
		return fail("configuration_changed")
	}
	actual, e := ValidateImage(ctx, p, ImageConfig{Metadata: value})
	if e != nil {
		return e
	}
	if len(actual) == 0 || !reflect.DeepEqual(actual, metadata) {
		return fail("configuration_changed")
	}
	last := actual[len(actual)-1]
	if last["userEnvProbe"] != "none" || last["updateRemoteUserUID"] != false {
		return fail("configuration_changed")
	}
	return nil
}
