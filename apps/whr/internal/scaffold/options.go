// SPDX-License-Identifier: EUPL-1.2
// Package scaffold plans local configuration without applying it.
package scaffold

import (
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// Pointers distinguish an omitted setting from an explicitly supplied value.
// The same input model is used by flags and the later wizard.
type Options struct {
	Name, ProjectURL, StartDir, HumanShell, ConsoleMode, Shortcuts *string
	Devcontainer, Template                                         *string
	NonInteractive, Yes, DryRun, NoAgentsBlock                     bool
	Overwrite                                                      []string
}

// Entry is one bounded, no-follow observation. Info retains identity for the
// later apply stage; callers must not infer an authorization from a snapshot.
type Entry struct {
	Kind string
	Data []byte
	Mode fs.FileMode
	Info fs.FileInfo
}
type Snapshot struct {
	Directory string
	Entries   map[string]Entry
	RootInfo  fs.FileInfo
}

func (o Options) validate() error {
	if o.Template != nil {
		return exitcode.Errorf(exitcode.PathBadArguments, "--template: starters are unsupported")
	}
	if o.Devcontainer != nil && !relativePath(*o.Devcontainer) {
		return exitcode.Errorf(exitcode.PathBadArguments, "--devcontainer needs a safe relative file path")
	}
	for _, setting := range []struct {
		name  string
		value *string
	}{{"name", o.Name}, {"project-url", o.ProjectURL}, {"start-dir", o.StartDir}, {"human-shell", o.HumanShell}, {"console-mode", o.ConsoleMode}, {"shortcuts", o.Shortcuts}} {
		if setting.value != nil && (*setting.value == "" || !utf8.ValidString(*setting.value)) {
			return exitcode.Errorf(exitcode.PathBadArguments, "--%s needs a nonempty UTF-8 value", setting.name)
		}
	}

	if o.NonInteractive && !o.Yes && !o.DryRun {
		return exitcode.Errorf(exitcode.PathHumanDecisionMissing, "--non-interactive requires --yes (or --dry-run)")
	}
	for _, path := range o.Overwrite {
		if !relativePath(path) {
			return exitcode.Errorf(exitcode.PathBadArguments, "--overwrite needs a safe relative file path")
		}
		if o.NoAgentsBlock && path == "AGENTS.md" {
			return exitcode.Errorf(exitcode.PathBadArguments, "--no-agents-block conflicts with --overwrite AGENTS.md")
		}
	}
	return nil
}

func (o Options) requestsEdit() bool {
	return o.Name != nil || o.ProjectURL != nil || o.StartDir != nil || o.HumanShell != nil || o.ConsoleMode != nil || o.Shortcuts != nil
}
func (o Options) permits(path string) bool {
	for _, p := range o.Overwrite {
		if p == path {
			return true
		}
	}
	return false
}
func projectName(url string) string {
	if strings.HasSuffix(url, "/") {
		return ""
	}
	return strings.TrimSuffix(url[strings.LastIndex(url, "/")+1:], ".git")
}
