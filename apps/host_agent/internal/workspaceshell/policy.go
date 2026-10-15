// SPDX-License-Identifier: EUPL-1.2
// Package workspaceshell plans initialization without executing or authorizing it.
package workspaceshell

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// EntryKind describes caller-observed layout data; it proves no filesystem fact.
type EntryKind string

const (
	RegularFile EntryKind = "file"
	Directory   EntryKind = "directory"
)

// ToolboxDescription is caller-supplied data, never an approval or integrity token.
// The future trusted attacher must separately verify bytes, ownership, trust and
// actual read-only mount state before a launcher may apply the resulting plan.
type ToolboxDescription struct {
	MountPath, ArtifactDigest, SourceRevision, Platform string
	ReadOnly                                            bool
	Entries                                             map[string]EntryKind
}

// Plan is data only. Environment must precede all startup/autoload in a future
// new-session launcher. RC fragments belong only in approved respective system rc
// files. Reconnecting an existing console does not apply a new plan.
type Plan struct {
	Environment   map[string]string
	BashRC, ZshRC string
}

// Error is a stable redacted refusal; it never includes caller environment data.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

const root = "/opt/bundledock/toolbox"
const toolboxBin = root + "/bin"
const toolboxShare = root + "/share"

var digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var revision = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

var required = map[string]EntryKind{
	"bin":                   Directory,
	"share/shell/init.bash": RegularFile,
	"share/shell/init.zsh":  RegularFile,
	"share/fish/vendor_conf.d/bdock-shell.fish":    RegularFile,
	"share/nushell/vendor/autoload/bdock-shell.nu": RegularFile,
}

// Prepare validates data against the supported profile and computes a pure plan.
// Success does not establish actual attachment, integrity or execution authority.
// Starship is always disabled; there is deliberately no prompt-enable parameter.
func Prepare(environment map[string]string, toolbox *ToolboxDescription) (Plan, error) {
	if toolbox != nil {
		if toolbox.MountPath != root || !digest.MatchString(toolbox.ArtifactDigest) ||
			!revision.MatchString(toolbox.SourceRevision) || !toolbox.ReadOnly ||
			(toolbox.Platform != "linux/amd64" && toolbox.Platform != "linux/arm64") {
			return Plan{}, &Error{Code: "unsupported_attachment"}
		}
		for name, kind := range required {
			if toolbox.Entries[name] != kind {
				return Plan{}, &Error{Code: "unsupported_attachment"}
			}
		}
	}
	for key, value := range environment {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return Plan{}, &Error{Code: "invalid_environment"}
		}
	}
	bins, ok := searchEntries(environment["PATH"], toolboxBin)
	if !ok || len(bins) == 0 {
		return Plan{}, &Error{Code: "invalid_environment"}
	}
	var shares []string
	if value := environment["XDG_DATA_DIRS"]; value != "" {
		shares, ok = searchEntries(value, toolboxShare)
		if !ok {
			return Plan{}, &Error{Code: "invalid_environment"}
		}
	}
	if len(shares) == 0 {
		shares = []string{"/usr/local/share", "/usr/share"}
	}
	out := make(map[string]string, len(environment)+3)
	for key, value := range environment {
		out[key] = value
	}
	// Apply the deny fence regardless of attachment/opt-outs/inherited enablement.
	out["BDOCK_STARSHIP"] = "0"
	shortcuts := toolbox != nil && environment["BDOCK_SHORTCUTS"] != "0"
	if toolbox != nil {
		bins = append(bins, toolboxBin)
	}
	if shortcuts {
		shares = append([]string{toolboxShare}, shares...)
	}
	out["PATH"] = strings.Join(bins, ":")
	out["XDG_DATA_DIRS"] = strings.Join(shares, ":")
	plan := Plan{Environment: out}
	if shortcuts {
		plan.BashRC = "case $- in *i*) if [ -r /opt/bundledock/toolbox/share/shell/init.bash ]; then . /opt/bundledock/toolbox/share/shell/init.bash; fi ;; esac"
		plan.ZshRC = "if [[ -o interactive ]] && [[ -r /opt/bundledock/toolbox/share/shell/init.zsh ]]; then source /opt/bundledock/toolbox/share/shell/init.zsh; fi"
	}
	return plan, nil
}

// searchEntries preserves source spelling/order; it never canonicalizes paths or
// silently removes malformed/relative entries. Only the exact owned path is cut.
func searchEntries(value, owned string) ([]string, bool) {
	entries := strings.Split(value, ":")
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == "" || !path.IsAbs(entry) || strings.IndexFunc(entry, unicode.IsControl) >= 0 {
			return nil, false
		}
		if entry != owned {
			out = append(out, entry)
		}
	}
	return out, true
}
