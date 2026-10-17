// SPDX-License-Identifier: EUPL-1.2

// Package version reports build metadata and supported contract versions
// (FR-027).
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

// Build metadata. A release build overrides them with -ldflags -X; a plain
// development build reports the module's VCS revision when Go recorded one.
var (
	Version   = "0.1.0-dev"
	Revision  = ""
	BuildDate = ""
)

// Platform is the client platform whr runs on.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Result is the --json result of `whr version` (versionResult in
// contracts/cli/output.schema.json).
type Result struct {
	Version          string         `json:"version"`
	Revision         string         `json:"revision"`
	BuildDate        string         `json:"build_date"`
	ClientPlatform   Platform       `json:"client_platform"`
	ContractVersions map[string]int `json:"contract_versions"`
}

// ContractVersions are the schema versions of the language-neutral contracts
// this build implements.
func contractVersions() map[string]int {
	return map[string]int{"command_table": 1, "exit_codes": 1, "output_schema": 1}
}

// Current returns the metadata of this build. Unknown values read "unknown"
// so the fields are never empty.
func Current() Result {
	rev := Revision
	if rev == "" {
		rev = vcsRevision()
	}
	return Result{
		Version:          orUnknown(Version),
		Revision:         orUnknown(rev),
		BuildDate:        orUnknown(BuildDate),
		ClientPlatform:   Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		ContractVersions: contractVersions(),
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// Human renders the result for a terminal; every value is build-controlled.
func (r Result) Human() string {
	names := make([]string, 0, len(r.ContractVersions))
	for n := range r.ContractVersions {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", n, r.ContractVersions[n]))
	}
	return fmt.Sprintf("whr %s\nrevision: %s\nbuild date: %s\nplatform: %s/%s\ncontracts: %s\n",
		r.Version, r.Revision, r.BuildDate, r.ClientPlatform.OS, r.ClientPlatform.Arch, strings.Join(parts, " "))
}
