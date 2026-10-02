// Package feature applies devcontainer features (design D38, issue #108): it reads a
// feature's devcontainer-feature.json, refuses what the safe subset does not allow,
// orders the features, and writes the image layers that install them. A feature is code
// from a registry that runs as root in the builder, which is the accepted risk of §7.2
// for a repository's own Dockerfile; what this package guarantees is what that code
// cannot ask for: no privilege, mount, capability, security option, init, entrypoint or
// lifecycle command, and no environment variable that points the agent past the egress
// proxy, preloads a library, moves its home or carries a supervisor or vendor name.
package feature

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/runtime"
)

// ErrRefused is matched by the error for a feature the safe subset does not allow.
var ErrRefused = errors.New("devcontainer feature: outside the safe subset")

// Request is one feature the devcontainer.json asks for.
type Request struct {
	// ID is the reference as written, for example ghcr.io/devcontainers/features/node:1.
	ID string
	// Options are the options the file sets, as the strings the install script reads.
	Options map[string]string
}

// Option is one declared option of a feature.
type Option struct {
	Type    string // "string" or "boolean"; empty counts as string
	Default string
	// Enum lists the allowed values of a string option, when the feature gives them.
	Enum []string
}

// Metadata is what the build reads of a feature's devcontainer-feature.json.
type Metadata struct {
	ID            string
	Version       string
	Options       map[string]Option
	ContainerEnv  map[string]string
	InstallsAfter []string
	Notes         []string
}

// refusedKeys are the keys a feature may not set to a non-empty value.
var refusedKeys = []string{
	"privileged", "mounts", "capAdd", "securityOpt", "init", "entrypoint",
	"onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand", "initializeCommand",
	"dependsOn",
}

// emptyValue says a JSON value is absent, null, false, zero, empty or an empty
// collection: declaring "privileged": false asks for nothing.
func emptyValue(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case float64:
		return t == 0
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// refusedEnv says a feature's containerEnv may not set a name: the proxy variables,
// the loader's, HOME and the supervisor's and vendors' prefixes (D38). PATH may change,
// since a feature puts its tool on it that way.
func refusedEnv(name string) bool {
	if strings.EqualFold(name, "PATH") {
		return false
	}
	if runtime.ReservedEnv(name) {
		return true
	}
	u := strings.ToUpper(name)
	return strings.HasPrefix(u, "LD_") || strings.HasSuffix(u, "_PROXY")
}

// ParseMetadata reads devcontainer-feature.json and refuses a feature that asks for
// more than the safe subset allows. The error names every refused key.
func ParseMetadata(data []byte) (Metadata, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Metadata{}, fmt.Errorf("devcontainer-feature.json: %w", err)
	}
	var problems []string
	for _, k := range refusedKeys {
		if v, ok := raw[k]; ok && !emptyValue(v) {
			problems = append(problems, k)
		}
	}
	m := Metadata{Options: map[string]Option{}, ContainerEnv: map[string]string{}}
	_ = json.Unmarshal(raw["id"], &m.ID)
	_ = json.Unmarshal(raw["version"], &m.Version)
	if v, ok := raw["containerEnv"]; ok {
		var env map[string]string
		if err := json.Unmarshal(v, &env); err != nil {
			problems = append(problems, "containerEnv (not a map of strings)")
		}
		for name, val := range env {
			switch {
			case !validName(name):
				problems = append(problems, "containerEnv."+name+" (not a variable name)")
			case refusedEnv(name):
				problems = append(problems, "containerEnv."+name+" (the supervisor sets it)")
			case strings.ContainsAny(val, "\x00\n\r") || len(val) > 4096:
				problems = append(problems, "containerEnv."+name+" (a control character or over 4096 bytes)")
			default:
				m.ContainerEnv[name] = val
			}
		}
	}
	if v, ok := raw["options"]; ok {
		var opts map[string]struct {
			Type    string          `json:"type"`
			Default json.RawMessage `json:"default"`
			Enum    []string        `json:"enum"`
		}
		if err := json.Unmarshal(v, &opts); err != nil {
			return Metadata{}, fmt.Errorf("devcontainer-feature.json: options: %w", err)
		}
		for name, o := range opts {
			if !validName(name) {
				problems = append(problems, "options."+name+" (not a name)")
				continue
			}
			m.Options[name] = Option{Type: o.Type, Default: optionString(o.Default), Enum: o.Enum}
		}
	}
	if v, ok := raw["installsAfter"]; ok {
		if err := json.Unmarshal(v, &m.InstallsAfter); err != nil {
			problems = append(problems, "installsAfter (not a list of strings)")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return Metadata{}, fmt.Errorf("%w: %s", ErrRefused, strings.Join(problems, "; "))
	}
	return m, nil
}

func validName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

func optionString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return fmt.Sprint(b)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// EnvName is the variable an option becomes: upper case, with every character that is
// not a letter or digit turned into an underscore, as the devcontainer spec says.
func EnvName(option string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(option) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ResolveOptions merges the options a file sets over the feature's defaults and checks
// them: an option the feature does not declare is refused, a boolean must be true or
// false, an enum value must be one of the listed ones, and a value may hold no
// control character. It returns the environment variables the install script reads,
// sorted by name.
func (m Metadata) ResolveOptions(set map[string]string) ([][2]string, error) {
	vars := map[string]string{}
	for name, o := range m.Options {
		vars[EnvName(name)] = o.Default
	}
	var problems []string
	for name, val := range set {
		o, ok := m.Options[name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("option %q is not one the feature declares", name))
			continue
		case strings.ContainsAny(val, "\x00\n\r") || len(val) > 1024:
			problems = append(problems, fmt.Sprintf("option %q has a control character or is over 1024 bytes", name))
			continue
		case o.Type == "boolean" && val != "true" && val != "false":
			problems = append(problems, fmt.Sprintf("option %q is a boolean: %q is not true or false", name, val))
			continue
		case len(o.Enum) > 0 && !contains(o.Enum, val):
			problems = append(problems, fmt.Sprintf("option %q must be one of %s", name, strings.Join(o.Enum, ", ")))
			continue
		}
		vars[EnvName(name)] = val
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("devcontainer feature: %s", strings.Join(problems, "; "))
	}
	out := make([][2]string, 0, len(vars))
	for k, v := range vars {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
