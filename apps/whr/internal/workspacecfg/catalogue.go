// SPDX-License-Identifier: EUPL-1.2

// Package workspacecfg checks a workspace configuration directory: the manifest
// workharbor.json, the devcontainer file, the projects directory and, for a
// directory that holds a Git repository, the Git state and submodules. It is the
// code behind `whr validate`; `init` and `doctor` reuse its Git-free profile.
//
// The directory is hostile (FR-055). Nothing in it is executed, sourced,
// interpolated or followed; every file is reached by a no-follow walk and read
// with a size cap, and Git runs only through the fixed read-only invocation of
// git.go.
package workspacecfg

// Severity is the severity of a finding.
type Severity string

// The three severities of contracts/cli/validate-findings.json.
const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// CodeInfo is one row of the finding catalogue.
type CodeInfo struct {
	Code     string
	Severity Severity
}

// catalogue mirrors contracts/cli/validate-findings.json; the freeze test
// TestFindingCatalogueMatchesContract fails on any difference.
var catalogue = []CodeInfo{
	{"manifest.missing", SevError},
	{"manifest.unsafe_path", SevError},
	{"manifest.too_large", SevError},
	{"manifest.encoding", SevError},
	{"manifest.syntax", SevError},
	{"manifest.duplicate_key", SevError},
	{"manifest.schema", SevError},
	{"manifest.path_invalid", SevError},
	{"manifest.url_credentials", SevError},
	{"manifest.bundles_deferred", SevError},
	{"devcontainer.missing", SevError},
	{"devcontainer.unsafe_path", SevError},
	{"devcontainer.too_large", SevError},
	{"devcontainer.encoding", SevError},
	{"devcontainer.syntax", SevError},
	{"devcontainer.duplicate_key", SevError},
	{"devcontainer.unsupported_key", SevWarning},
	{"devcontainer.image_not_digest_pinned", SevWarning},
	{"devcontainer.users_not_numeric", SevWarning},
	{"devcontainer.host_command", SevWarning},
	{"projects.missing", SevError},
	{"projects.unsafe_path", SevError},
	{"projects.not_a_submodule", SevWarning},
	{"git.not_repository", SevWarning},
	{"git.unavailable", SevError},
	{"git.unsafe_path", SevError},
	{"git.head_missing", SevWarning},
	{"git.listing_failed", SevError},
	{"git.limit_exceeded", SevError},
	{"git.tree_path_invalid", SevError},
	{"gitmodules.unsafe", SevError},
	{"gitmodules.too_large", SevError},
	{"gitmodules.syntax", SevError},
	{"gitmodules.duplicate_field", SevError},
	{"gitmodules.unsupported_key", SevError},
	{"gitmodules.uncommitted_change", SevWarning},
	{"submodule.url_invalid", SevError},
	{"submodule.url_credentials", SevError},
	{"submodule.update_command", SevError},
	{"submodule.unlisted", SevError},
	{"submodule.not_pinned", SevError},
	{"submodule.outside_projects", SevError},
	{"submodule.nested", SevError},
	{"submodule.not_committed", SevWarning},
	{"submodule.pinned", SevInfo},
	{"findings.truncated", SevInfo},
	{"project.declared_not_pinned", SevWarning},
	{"findings.time_budget_exhausted", SevError},
}

var severityOf = func() map[string]Severity {
	m := make(map[string]Severity, len(catalogue))
	for _, e := range catalogue {
		m[e.Code] = e.Severity
	}
	return m
}()

// Catalogue returns a copy of the finding catalogue.
func Catalogue() []CodeInfo { return append([]CodeInfo(nil), catalogue...) }
