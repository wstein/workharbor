// SPDX-License-Identifier: EUPL-1.2

// Package commands holds the whr command table (ADR 0014, FR-051), generates
// the Cobra command tree, help and shell completion from it, and runs a command
// through the shared envelope, error line, color and exit-code plumbing.
package commands

import "strings"

// Entry is one row of the command table, the Go mirror of
// contracts/cli/commands.json.
type Entry struct {
	// ID is the canonical id used in --json, logs and errors ("workspace.ls").
	ID string
	// Path is the canonical spelling; Short is the optional short form.
	Path, Short []string
	// Increment is "first" or "later"; Needs, Writes and JSON describe the command.
	Increment, Needs, Writes, JSON string
	// BlockedOn names what a later command waits for (see waitsFor).
	BlockedOn string
	// Flags lists the flags the specification fixes for the command.
	Flags []string
}

// Table returns a deep copy of the command table.
func Table() []Entry {
	out := make([]Entry, len(entries))
	for i, e := range entries {
		e.Path = append([]string(nil), e.Path...)
		e.Short = append([]string(nil), e.Short...)
		if len(e.Short) == 0 {
			e.Short = nil
		}
		e.Flags = append([]string(nil), e.Flags...)
		if len(e.Flags) == 0 {
			e.Flags = nil
		}
		out[i] = e
	}
	return out
}

// groupAliases are group aliases; ws is an alias of the group workspace only.
var groupAliases = map[string][]string{"workspace": {"ws"}}

// GroupAliases returns the group aliases of the table.
func GroupAliases() map[string][]string {
	out := map[string][]string{}
	for g, a := range groupAliases {
		out[g] = append([]string(nil), a...)
	}
	return out
}

// blockedOn explains the blocked_on values of the contract.
var blockedOn = map[string]string{
	"owner_api":     "the control plane owner API, which does not exist yet",
	"host_setup_wi": "the host setup and service work item (T015)",
	"owner_client":  "the typed TLS owner client work item (T011)",
}

// pending names the work item of first-increment commands whose behavior is
// delivered after this foundation.
var pending = map[string]string{
	"workspace.init": "work item T030",
}

// WaitsFor says what an unavailable command waits for.
func WaitsFor(id string) string {
	for _, e := range entries {
		if e.ID != id {
			continue
		}
		if e.BlockedOn != "" {
			return blockedOn[e.BlockedOn]
		}
		if p, ok := pending[id]; ok {
			return p
		}
	}
	return "a later work item"
}

var descriptions = map[string]string{
	"workspace.drift":        "Report local bundle reference drift without changing the workspace",
	"workspace.ls":           "List local workspaces with --local (remote listing waits for owner_api)",
	"workspace.show":         "Show one workspace",
	"workspace.create":       "Create a workspace",
	"workspace.start":        "Start a workspace",
	"workspace.stop":         "Stop a workspace",
	"workspace.rebuild":      "Rebuild a workspace",
	"workspace.rm":           "Remove a workspace",
	"workspace.shell":        "Open a local workspace login shell with --local (remote waits for owner_api)",
	"workspace.console":      "Attach to the persistent workspace console",
	"workspace.ssh":          "Connect to a workspace over SSH",
	"workspace.open":         "Open a workspace in the browser",
	"workspace.init":         "Author the workspace configuration in a directory",
	"workspace.validate":     "Validate a workspace configuration directory",
	"host.setup":             "Set up this machine as a workspace host",
	"host.doctor":            "Check this machine for problems",
	"host.offboard":          "Remove this machine as a workspace host",
	"host.service.install":   "Install the host agent service",
	"host.service.uninstall": "Uninstall the host agent service",
	"host.service.status":    "Show the host agent service status",
	"host.ls":                "List hosts",
	"host.show":              "Show one host",
	"auth.login":             "Log in to the control plane",
	"auth.status":            "Show the authentication state",
	"auth.logout":            "Log out of the control plane",
	"op.show":                "Show an operation",
	"op.wait":                "Wait for an operation to finish",
	"agent.ls":               "List agent sessions",
	"agent.attach":           "Attach to an agent session",
	"agent.status":           "Show an agent session status",
	"agent.send":             "Send input to an agent session",
	"agent.stop":             "Stop an agent session",
	"version":                "Print the version and supported contract versions",
	"completion":             "Print a shell completion script (bash, zsh or fish)",
	"help":                   "Show help for whr or one command",
}

// Describe returns the one-line help description of a command.
func Describe(id string) string { return descriptions[id] }

func joinPath(p []string) string { return strings.Join(p, " ") }
