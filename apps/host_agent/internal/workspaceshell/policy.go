// SPDX-License-Identifier: EUPL-1.2
// Package workspaceshell plans initialization without executing or authorizing it.
package workspaceshell

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

func Prepare(environment map[string]string, toolbox *ToolboxDescription) (Plan, error) {
	return Plan{}, &Error{Code: "unsupported_attachment"}
}
