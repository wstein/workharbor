// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"bytes"
	"github.com/moby/buildkit/util/gitutil"
	"github.com/tonistiigi/go-csvvalue"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// Descriptor is an exact normalized revision set; it confers no trust.
type Descriptor struct {
	RepositoryIdentity, Commit string
	Submodules                 []Submodule
}
type Submodule struct{ Path, Commit string }
type Approval struct {
	ID, OwnerID string
	Descriptor  Descriptor
}

var fullCommit = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var uuidValue = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var imageDigest = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}$`)
var hostName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.?$`)
var userName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func invalidConfiguration() error     { return &Error{Code: "invalid_configuration"} }
func unsupportedConfiguration() error { return &Error{Code: "unsupported_configuration"} }
func fields(m map[string]any, required []string, optional ...string) bool {
	if m == nil {
		return false
	}
	allowed := map[string]bool{}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return false
		}
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}

// DecodeDescriptor accepts only the materialized, sorted descriptor used by T007.
func DecodeDescriptor(value any) (Descriptor, error) {
	m, ok := value.(map[string]any)
	if !ok || !fields(m, []string{"repository_identity", "commit", "submodules"}) {
		return Descriptor{}, invalidConfiguration()
	}
	origin, ok := m["repository_identity"].(string)
	if !ok || !validOrigin(origin) {
		return Descriptor{}, invalidConfiguration()
	}
	commit, ok := m["commit"].(string)
	if !ok || !fullCommit.MatchString(commit) {
		return Descriptor{}, invalidConfiguration()
	}
	entries, ok := m["submodules"].([]any)
	if !ok {
		return Descriptor{}, invalidConfiguration()
	}
	d := Descriptor{origin, commit, make([]Submodule, 0, len(entries))}
	previous := ""
	for _, entry := range entries {
		s, ok := entry.(map[string]any)
		if !ok || !fields(s, []string{"path", "commit"}) {
			return Descriptor{}, invalidConfiguration()
		}
		p, e := CanonicalPath(s["path"])
		c, ok := s["commit"].(string)
		if e != nil || !ok || !fullCommit.MatchString(c) || (previous != "" && p <= previous) {
			return Descriptor{}, invalidConfiguration()
		}
		d.Submodules = append(d.Submodules, Submodule{p, c})
		previous = p
	}
	return d, nil
}
func (d Descriptor) Map() map[string]any {
	entries := make([]any, 0, len(d.Submodules))
	for _, s := range d.Submodules {
		entries = append(entries, map[string]any{"path": s.Path, "commit": s.Commit})
	}
	return map[string]any{"repository_identity": d.RepositoryIdentity, "commit": d.Commit, "submodules": entries}
}
func MatchApproval(a Approval, owner, reference string, d Descriptor) error {
	_, ae := DecodeDescriptor(a.Descriptor.Map())
	_, de := DecodeDescriptor(d.Map())
	if !uuidValue.MatchString(owner) || !uuidValue.MatchString(reference) || a.ID != reference || a.OwnerID != owner || ae != nil || de != nil || !reflect.DeepEqual(a.Descriptor.Map(), d.Map()) {
		return &Error{Code: "trust_required"}
	}
	return nil
}
func validOrigin(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r <= 32 || r == 127 || unicode.IsSpace(r) || strings.ContainsRune("\\?#", r) {
			return false
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) || !hexByte(s[i+1]) || !hexByte(s[i+2]) {
				return false
			}
			i += 2
		}
	}
	if i := strings.Index(s, "://"); i >= 0 {
		scheme := strings.ToLower(s[:i])
		if scheme != "https" && scheme != "ssh" {
			return false
		}
		rest := s[i+3:]
		slash := strings.IndexByte(rest, '/')
		return slash >= 0 && validAuthority(rest[:slash], scheme == "ssh", true)
	}
	colon := -1
	bracket := false
	for i, r := range s {
		if r == '[' {
			bracket = true
		}
		if r == ']' {
			bracket = false
		}
		if r == ':' && !bracket {
			colon = i
			break
		}
	}
	return colon > 0 && colon < len(s)-1 && validAuthority(s[:colon], true, false)
}
func hexByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
func validAuthority(s string, allowUser, allowPort bool) bool {
	if strings.Count(s, "@") > 1 {
		return false
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		if !allowUser || !userName.MatchString(s[:i]) {
			return false
		}
		s = s[i+1:]
	}
	host, port := s, ""
	if strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return false
		}
		host = s[:end+1]
		if len(s) > end+1 {
			if s[end+1] != ':' {
				return false
			}
			port = s[end+2:]
			if port == "" {
				return false
			}
		}
		literal := s[1:end]
		if strings.Contains(literal, "%") || net.ParseIP(literal) == nil || !strings.Contains(literal, ":") {
			return false
		}
	} else {
		if strings.Count(s, ":") > 1 {
			return false
		}
		if i := strings.IndexByte(s, ':'); i >= 0 {
			host, port = s[:i], s[i+1:]
			if port == "" {
				return false
			}
		}
		if !hostName.MatchString(host) {
			return false
		}
	}
	if port != "" {
		if !allowPort {
			return false
		}
		for _, r := range port {
			if r < '0' || r > '9' {
				return false
			}
		}
		p := strings.TrimLeft(port, "0")
		if len(p) < 1 || len(p) > 5 {
			return false
		}
		n, _ := strconv.Atoi(p)
		if n > 65535 {
			return false
		}
	}
	return host != ""
}
func reservedArgument(s string) bool {
	return strings.HasPrefix(s, "BUILDKIT_") || strings.HasPrefix(s, "_DEV_CONTAINERS_")
}
func noSubstitution(v any) bool {
	switch x := v.(type) {
	case string:
		return utf8.ValidString(x) && !strings.ContainsRune(x, 0) && !strings.Contains(x, "${")
	case map[string]any:
		for k, v := range x {
			if !noSubstitution(k) || !noSubstitution(v) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !noSubstitution(v) {
				return false
			}
		}
	}
	return true
}
func stringMap(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k, v := range m {
		s, ok := v.(string)
		if !ok || k == "" || strings.ContainsAny(k, "=\x00") || !utf8.ValidString(k) || !utf8.ValidString(s) {
			return false
		}
	}
	return true
}
func commandLeaf(v any) bool {
	switch x := v.(type) {
	case string:
		return utf8.ValidString(x)
	case []any:
		for _, v := range x {
			if _, ok := v.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}
func command(v any) bool {
	if commandLeaf(v) {
		return true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, v := range m {
		if !commandLeaf(v) {
			return false
		}
	}
	return true
}
func ValidateSource(m map[string]any) error {
	allowed := []string{"name", "image", "build", "containerUser", "remoteUser", "containerEnv", "remoteEnv", "overrideCommand", "userEnvProbe", "updateRemoteUserUID", "waitFor", "shutdownAction", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand"}
	if !fields(m, nil, allowed...) || !noSubstitution(m) {
		return unsupportedConfiguration()
	}
	_, image := m["image"]
	_, build := m["build"]
	if image == build {
		return unsupportedConfiguration()
	}
	for k, v := range m {
		valid := false
		switch k {
		case "image":
			s, ok := v.(string)
			valid = ok && imageDigest.MatchString(s)
		case "name", "containerUser", "remoteUser":
			_, valid = v.(string)
		case "containerEnv", "remoteEnv":
			valid = stringMap(v)
		case "overrideCommand":
			_, valid = v.(bool)
		case "userEnvProbe":
			valid = v == "none"
		case "waitFor":
			valid = v == "postStartCommand"
		case "shutdownAction":
			valid = v == "none"
		case "updateRemoteUserUID":
			b, ok := v.(bool)
			valid = ok && !b
		case "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand":
			valid = command(v)
		case "build":
			b, ok := v.(map[string]any)
			valid = ok && fields(b, []string{"dockerfile"}, "context", "args", "target")
			if valid {
				for key, value := range b {
					if key == "args" {
						valid = stringMap(value)
						if valid {
							for name := range value.(map[string]any) {
								if reservedArgument(name) {
									valid = false
									break
								}
							}
						}
					} else {
						_, valid = value.(string)
					}
					if !valid {
						break
					}
				}
			}
		}
		if !valid {
			return unsupportedConfiguration()
		}
	}
	return nil
}

// ValidateDockerfile uses the upstream AST only to reject unsupported capabilities.
// priorStageSources admits COPY/ADD --from and RUN --mount from= only for an
// earlier stage, so every external image enters through a digest-pinned FROM
// and its metadata gate. Remote ADD sources and dynamic flags refuse.
func priorStageSources(node *parser.Node, aliases map[string]bool, stages int) bool {
	prior := func(ref string) bool {
		if index, e := strconv.Atoi(ref); e == nil {
			return index >= 0 && index < stages-1 && strconv.Itoa(index) == ref
		}
		return aliases[strings.ToLower(ref)]
	}
	for _, flag := range node.Flags {
		name, value, _ := strings.Cut(strings.TrimPrefix(flag, "--"), "=")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "from":
			if strings.Contains(value, "$") || !prior(value) {
				return false
			}
		case "mount":
			// BuildKit parses mount options as CSV, so quoted fields count.
			fields, e := csvvalue.Fields(value, nil)
			if e != nil {
				return false
			}
			for _, field := range fields {
				key, ref, _ := strings.Cut(field, "=")
				if strings.EqualFold(strings.TrimSpace(key), "from") && (strings.Contains(ref, "$") || !prior(ref)) {
					return false
				}
			}
		}
	}
	if strings.EqualFold(node.Value, "add") {
		for argument := node.Next; argument != nil && argument.Next != nil; argument = argument.Next {
			source := argument.Value
			if strings.Contains(source, "://") || gitutil.IsGitTransport(source) {
				return false
			}
		}
	}
	return true
}

func ValidateDockerfile(raw []byte) ([]string, error) {
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, unsupportedConfiguration()
	}
	if _, _, _, found := parser.DetectSyntax(raw); found {
		return nil, unsupportedConfiguration()
	}
	result, err := parser.Parse(bytes.NewReader(raw))
	if err != nil || len(result.AST.Children) == 0 {
		return nil, unsupportedConfiguration()
	}
	if _, _, e := instructions.Parse(result.AST, linter.New(&linter.Config{})); e != nil {
		return nil, unsupportedConfiguration()
	}
	aliases := map[string]bool{}
	seen := map[string]bool{}
	refs := []string{}
	from := false
	stages := 0
	for _, node := range result.AST.Children {
		switch strings.ToLower(node.Value) {
		case "volume", "onbuild":
			return nil, unsupportedConfiguration()
		case "copy", "add", "run":
			if !priorStageSources(node, aliases, stages) {
				return nil, unsupportedConfiguration()
			}
		case "arg":
			if node.Next == nil {
				return nil, unsupportedConfiguration()
			}
			for argument := node.Next; argument != nil; argument = argument.Next {
				if reservedArgument(strings.SplitN(argument.Value, "=", 2)[0]) {
					return nil, unsupportedConfiguration()
				}
			}
		case "from":
			from = true
			stages++
			if len(node.Flags) > 0 || node.Next == nil {
				return nil, unsupportedConfiguration()
			}
			ref := node.Next.Value
			if !aliases[strings.ToLower(ref)] {
				if !imageDigest.MatchString(ref) {
					return nil, unsupportedConfiguration()
				}
				if !seen[ref] {
					seen[ref] = true
					refs = append(refs, ref)
				}
			}
			if tail := node.Next.Next; tail != nil {
				if !strings.EqualFold(tail.Value, "as") || tail.Next == nil || tail.Next.Next != nil {
					return nil, unsupportedConfiguration()
				}
				alias := strings.ToLower(tail.Next.Value)
				if alias == "" || aliases[alias] {
					return nil, unsupportedConfiguration()
				}
				aliases[alias] = true
			}
		}
	}
	if !from {
		return nil, unsupportedConfiguration()
	}
	sort.Strings(refs)
	return refs, nil
}
