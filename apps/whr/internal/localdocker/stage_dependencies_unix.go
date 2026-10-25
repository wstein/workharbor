//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type stageDependencyAdmission struct {
	ctx                     context.Context
	root, source, workspace string
	paths, seen             map[string]bool
	catalogue               map[string]stageKnownDependency
	gitIdentity             bool
	gitStatus               bool
}

// This admits a deliberately small, non-expanding command language. It is not
// a shell parser or a claim to infer arbitrary program dependency closure.
// Opaque known Anchorage bodies require exact inspected bytes and recursively
// admitted helpers. Other opaque/dynamic programs refuse before allocation.
func admitStageDependencies(ctx context.Context, root, source, workspace string, config map[string]any, smoke bool, configuration string) ([]string, string, error) {
	d := &stageDependencyAdmission{ctx: ctx, root: root, source: source, workspace: workspace, paths: map[string]bool{}, seen: map[string]bool{}, catalogue: stageKnownDependencies}
	for _, path := range []string{".devcontainer", "Makefile", ".gitignore", ".gitmodules", "bundledock", "BundleDock", configuration} {
		d.paths[path] = true
	}
	for _, key := range []string{"initializeCommand", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand"} {
		if err := d.command(config[key]); err != nil {
			return nil, "", err
		}
	}
	if smoke {
		if err := d.file(".devcontainer/smoke-container.sh", true); err != nil {
			return nil, "", err
		}
	}
	paths := []string{}
	for path := range d.paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	mode := "none"
	if d.gitIdentity {
		mode = "known_identity"
	} else if d.gitStatus {
		mode = "status_success"
	}
	return paths, mode, nil
}
func stageDependencyUnsupported() error {
	return stageConflict("execution dependency capability unsupported: opaque or dynamic command")
}
func (d *stageDependencyAdmission) command(value any) error {
	switch value := value.(type) {
	case nil:
		return nil
	case string:
		if strings.Contains(value, "@workspace@") {
			return stageConflict("reserved workspace marker dependency capability unsupported")
		}
		logical := d.logical(value)
		if strings.Contains(logical, "${") {
			return stageDependencyUnsupported()
		}
		argv, err := stageLiteralCommand(logical)
		if err != nil {
			return err
		}
		return d.argv(argv)
	case []any:
		argv := []string{}
		for _, value := range value {
			arg, ok := value.(string)
			if !ok {
				return stageDependencyUnsupported()
			}
			if strings.Contains(arg, "@workspace@") {
				return stageConflict("reserved workspace marker dependency capability unsupported")
			}
			logical := d.logical(arg)
			if strings.Contains(logical, "${") {
				return stageDependencyUnsupported()
			}
			argv = append(argv, logical)
		}
		return d.argv(argv)
	case map[string]any:
		keys := []string{}
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := d.command(value[key]); err != nil {
				return err
			}
		}
		return nil
	default:
		return stageDependencyUnsupported()
	}
}
func (d *stageDependencyAdmission) logical(value string) string {
	for _, prefix := range []string{"${localWorkspaceFolder}", "${containerWorkspaceFolder}", d.root, d.source, d.workspace} {
		if prefix != "" {
			value = strings.ReplaceAll(value, prefix+"/", "@workspace@/")
		}
	}
	return value
}
func (d *stageDependencyAdmission) argv(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	switch argv[0] {
	case "printf":
		if len(argv) > 1 && (strings.HasPrefix(argv[1], "-") && argv[1] != "--" || strings.Contains(argv[1], "%n")) {
			return stageDependencyUnsupported()
		}
		return nil
	case "echo", "true", "false", ":":
		return nil
	case "exit":
		if len(argv) <= 2 {
			return nil
		}
		return stageDependencyUnsupported()
	case "set":
		if len(argv) == 2 && (argv[1] == "-e" || argv[1] == "-u" || argv[1] == "-eu" || argv[1] == "-ue") {
			return nil
		}
		return stageDependencyUnsupported()
	case "git", "/usr/bin/git", "/bin/git":
		if len(argv) == 3 && argv[1] == "status" && argv[2] == "--short" {
			d.gitStatus = true
			return nil
		}
		return stageConflict("Git execution identity capability unsupported")
	case "sh", "bash", "/bin/sh", "/bin/bash", "/usr/bin/sh", "/usr/bin/bash":
		args := argv[1:]
		for len(args) > 0 && (args[0] == "-e" || args[0] == "-u" || args[0] == "-eu" || args[0] == "-ue") {
			args = args[1:]
		}
		if len(args) == 2 && args[0] == "-c" {
			return d.command(args[1])
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return stageDependencyUnsupported()
		}
		return d.file(args[0], true)
	default:
		if strings.Contains(argv[0], "/") {
			return d.file(argv[0], true, true)
		}
		return stageDependencyUnsupported()
	}
}
func (d *stageDependencyAdmission) file(name string, executable bool, direct ...bool) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimPrefix(d.logical(name), "@workspace@/")
	name = strings.TrimPrefix(name, "./")
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0) {
		return stageDependencyUnsupported()
	}
	parts := strings.Split(name, string(filepath.Separator))
	for i := range parts {
		if stageOmit(filepath.Join(parts[:i+1]...)) {
			return stageConflict("required execution dependency was omitted")
		}
	}
	d.paths[name] = true
	resolved, err := filepath.EvalSymlinks(filepath.Join(d.root, name))
	if err != nil {
		return stageConflict("required execution dependency unavailable")
	}
	relative, err := filepath.Rel(d.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return stageConflict("execution dependency path outside workspace")
	}
	d.paths[relative] = true
	if checked, exists := d.seen[relative]; exists && (!executable || checked) {
		return nil
	}
	d.seen[relative] = executable
	data, err := readStageInput(resolved, stageMaxFileBytes)
	if err != nil {
		return stageConflict("required execution dependency unavailable")
	}
	if known, ok := d.catalogue[relative]; ok {
		if known.Digest == stageBytesDigest(data) {
			d.gitIdentity = d.gitIdentity || known.GitIdentity
			for _, dependency := range known.Dependencies {
				_, script := d.catalogue[dependency]
				if err = d.file(dependency, script); err != nil {
					return err
				}
			}
			return nil
		}
		if !executable {
			return stageConflict("known execution dependency body changed; capability unsupported")
		}
	}
	if !executable {
		return nil
	}
	if len(direct) != 0 && direct[0] && !strings.HasPrefix(string(data), "#!") {
		return stageConflict("script interpreter dependency capability unsupported")
	}
	if strings.HasPrefix(string(data), "#!") {
		interpreter, _, _ := strings.Cut(string(data), "\n")
		switch interpreter {
		case "#!/bin/sh", "#!/bin/bash", "#!/usr/bin/sh", "#!/usr/bin/bash":
		default:
			return stageConflict("script interpreter dependency capability unsupported")
		}
	}
	if !utf8.Valid(data) {
		return stageDependencyUnsupported()
	}
	for _, line := range strings.Split(string(data), "\n") {
		// Only ASCII shell indentation may precede a comment. Preserve every
		// other byte for the lexer; Unicode whitespace and CR are not shell trim.
		if strings.Contains(line, "@workspace@") {
			return stageConflict("reserved workspace marker dependency capability unsupported")
		}
		if line == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		argv, err := stageLiteralCommand(d.logical(line))
		if err != nil {
			return err
		}
		if err = d.argv(argv); err != nil {
			return err
		}
	}
	return nil
}

// Quotes and escapes are handled explicitly; unquoted expansion, control,
// substitution, globbing and redirection syntax is unsupported. No evaluation
// or subprocess is involved in this admission pass.
func stageLiteralCommand(command string) ([]string, error) {
	argv := []string{}
	var word strings.Builder
	quote := rune(0)
	active := false
	escaped := false
	flush := func() {
		if active {
			argv = append(argv, word.String())
			word.Reset()
			active = false
		}
	}
	for _, r := range command {
		if r == 0 {
			return nil, stageDependencyUnsupported()
		}
		if escaped {
			if r == '\n' || r == '\r' {
				return nil, stageConflict("command continuation dependency capability unsupported")
			}
			word.WriteRune(r)
			active = true
			escaped = false
			continue
		}
		if quote == '\'' {
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\\' {
			if quote == '"' {
				return nil, stageConflict("quoted command escape dependency capability unsupported")
			}
			escaped = true
			active = true
			continue
		}
		if quote == '"' {
			if r == '"' {
				quote = 0
				continue
			}
			if r == '$' || r == '`' || r == '\n' || r == '\r' || r == 0 {
				return nil, stageDependencyUnsupported()
			}
			word.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			active = true
		case ' ', '\t':
			flush()
		case '#':
			if !active {
				flush()
				return argv, nil
			}
			word.WriteRune(r)
		case '$', '`', ';', '|', '&', '<', '>', '(', ')', '{', '}', '*', '?', '[', ']', '~', '\n', '\r', 0:
			return nil, stageDependencyUnsupported()
		default:
			word.WriteRune(r)
			active = true
		}
	}
	if quote != 0 || escaped {
		return nil, stageDependencyUnsupported()
	}
	flush()
	return argv, nil
}

// Missing generated smoke facts cannot silently become a different dependency
// channel in the original workspace. Host facts have their own admitted channel.
func stageKnownSmokeOmittedInput(root string) error {
	if _, err := os.Lstat(filepath.Join(root, ".devcontainer", "smoke.env")); err == nil {
		return stageConflict("required omitted smoke facts capability unsupported")
	} else if !os.IsNotExist(err) {
		return stageConflict("required omitted smoke facts unavailable")
	}
	return nil
}

// The official 0.89 merged shape contains plural arrays, one command per
// source/image/Feature origin. This builds an admission-only union; execution
// retains the official command order and parallel command groups unchanged.
func (s *LocalStage) admitMergedDependencies(ctx context.Context, merged map[string]any) error {
	config := map[string]any{}
	for key, value := range s.config {
		config[key] = value
	}
	for _, key := range []string{"onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand"} {
		value, exists := merged[key+"s"]
		if !exists {
			continue
		}
		commands, ok := value.([]any)
		if !ok {
			return stageDependencyUnsupported()
		}
		origins := map[string]any{"source": config[key]}
		for i, command := range commands {
			origins[strconv.Itoa(i)] = command
		}
		config[key] = origins
	}
	paths, mode, err := admitStageDependencies(ctx, s.capture.Path, s.capture.Source, s.workspace, config, s.options.Smoke, s.configRelative)
	if err != nil {
		return err
	}
	if mode == "known_identity" {
		if err = stageKnownSmokeOmittedInput(s.capture.Source); err != nil {
			return err
		}
	}
	if mode != "none" {
		for _, repo := range s.git.Repositories {
			current, e := readStageGitSnapshotContext(ctx, repo.ClonePath, true)
			if e != nil {
				return e
			}
			if _, e = stageConsumedGitInputDigest(ctx, repo, current, mode == "known_identity"); e != nil {
				return e
			}
		}
	}
	s.executionPaths, s.gitInputMode = paths, mode
	return nil
}
