// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// candidateDisposalHelp is used only for receipt-bound managed candidate disposal.
const candidateDisposalHelp = "Confirmed disposal removes the candidate container, candidate home, clone/result tree, and receipts, journals and private metadata. Removal loses unfetched commits and uncommitted clone work. Back up the staging home with Docker before removal or promotion if needed.\n"

const globalFlagsHelp = `Global flags:
  --json            write one JSON envelope to standard output
  --color string    color mode: auto, always or never (default "auto")
  --no-color        disable color (same as --color never)
  -h, --help        show help
`

// renderHelp renders help for the root, a group or a command from the table.
func (a *app) renderHelp(c *cobra.Command) string {
	var b strings.Builder
	if id := c.Annotations[annotationID]; id != "" {
		a.leafHelp(&b, c, mustEntry(id))
		return b.String()
	}
	a.groupHelp(&b, groupPath(c))
	return b.String()
}

func (a *app) leafHelp(b *strings.Builder, c *cobra.Command, e Entry) {
	fmt.Fprintf(b, "%s\n\nUsage:\n  whr %s", Describe(e.ID), joinPath(e.Path))
	if e.ID != "version" && e.ID != "help" && e.ID != "completion" {
		b.WriteString(" [flags]")
	}
	switch e.ID {
	case "help":
		b.WriteString(" [command]")
	case "completion":
		b.WriteString(" bash|zsh|fish")
	}
	b.WriteString("\n\n")
	if e.Short != nil {
		fmt.Fprintf(b, "Short form: whr %s\n", joinPath(e.Short))
	} else {
		b.WriteString("Short form: none\n")
	}
	if g := groupAliases[e.Path[0]]; g != nil && len(e.Path) > 1 {
		fmt.Fprintf(b, "Group alias: whr %s %s\n", g[0], joinPath(e.Path[1:]))
	}
	if !a.available(e.ID) {
		fmt.Fprintf(b, "Status: not available yet; it waits for %s and exits 64\n", WaitsFor(e.ID))
	}
	if e.ID == "workspace.drift" {
		b.WriteString("Local: --local --source <dir> <exact-name|full-id> compares a running primary against its source pins (5 s total).\nExit 0 means reference alignment; exit 68 means drift or cannot_determine. Volume contents are not attested.\n")
	}
	if e.ID == "workspace.ls" {
		b.WriteString("Local: --local lists running and stopped labelled containers (5 s total); no runtime is started.\nWithout --local: waits for owner_api and exits 64.\n")
	}
	if e.ID == "workspace.rm" {
		b.WriteString("Local: --local requires one container id/name, --role staging|temp, or --label key[=value]. Docker inspection is bounded to 5 s.\nOther workspace home volumes are preserved by default; their deletion requires --volumes --force and confirmation. A receipt-bound whr-managed staging candidate is always disposed of completely with normal confirmation; unattended/JSON mutation requires --yes.\nUse --dry-run to preview without mutations. Without --local: waits for owner_api and exits 64.\n")
	}
	if e.ID == "workspace.rm" || e.ID == "workspace.promote" {
		b.WriteString(candidateDisposalHelp)
	}
	if e.ID == "workspace.promote" {
		b.WriteString("Local: --local <exact-name|full-id> replaces the primary with the verified candidate image and preserves the original primary home. Replacement ends exec sessions. Use --dry-run to preview without mutation; unattended/JSON mutation requires --yes.\n")
	}
	if e.ID == "workspace.shell" {
		b.WriteString("Local: --local <ws> opens one fresh login shell; terminal stdin/stdout required; no --json or trailing command.\n")
		b.WriteString("Docker owns direct terminal descriptors; the stream is not filtered or recorded by whr.\n")
		b.WriteString("Detach with ctrl-] ctrl-^; the container shell survives detaching. Closing a pane/connection does not guarantee process termination.\n")
		b.WriteString("Persistent identity: manually set 🟥 HOST or the workspace glyph + ws:<name> in both pane title and tab label (docs/container-runtime.md).\n")
		b.WriteString("WHR_LOCAL_WORKSPACE and WHR_LOCAL_MODE=1 are available to an optional prompt; whr does not configure the prompt or multiplexer.\n")
	}
	if e.ID == "workspace.console" {
		b.WriteString("console --local is refused; use whr shell --local <ws> in the host multiplexer session.\n")
	}
	if e.JSON == "rejected" {
		b.WriteString("JSON: not supported (--json is a usage error)\n")
	}
	if usages := c.LocalNonPersistentFlags().FlagUsages(); strings.TrimSpace(usages) != "" && (e.ID == "workspace.drift" || e.ID == "workspace.rm" || e.ID == "workspace.init" || e.ID == "workspace.ls" || e.ID == "workspace.shell" || e.ID == "workspace.console" || e.ID == "workspace.promote") {
		fmt.Fprintf(b, "\nFlags:\n%s", usages)
	}
	b.WriteString("\n" + globalFlagsHelp)
}

func (a *app) groupHelp(b *strings.Builder, path []string) {
	title := "whr - WorkHarbor command line"
	usage := "whr [--json] [--color auto|always|never] [--no-color] <command> [args]\n  whr --version"
	if len(path) > 0 {
		title = "whr " + joinPath(path)
		usage = "whr " + joinPath(path) + " <command> [args]"
	}
	fmt.Fprintf(b, "%s\n\nUsage:\n  %s\n\n", title, usage)
	var avail, later []Entry
	for _, e := range entries {
		if !under(e.Path, path) {
			continue
		}
		if a.available(e.ID) {
			avail = append(avail, e)
		} else {
			later = append(later, e)
		}
	}
	if len(avail) > 0 {
		b.WriteString("Available commands:\n")
		for _, e := range avail {
			fmt.Fprintf(b, "  %-24s %s%s\n", joinPath(e.Path), Describe(e.ID), shortHint(e))
		}
		b.WriteString("\n")
	}
	if len(later) > 0 {
		b.WriteString("Not yet available (registered; they refuse with exit 64):\n")
		for _, e := range later {
			fmt.Fprintf(b, "  %-24s %s%s\n", joinPath(e.Path), Describe(e.ID), shortHint(e))
		}
		b.WriteString("\n")
	}
	if len(path) == 0 {
		b.WriteString("The group workspace is also spelled ws.\n\n")
	}
	b.WriteString(globalFlagsHelp)
}

func shortHint(e Entry) string {
	if e.Short == nil {
		return ""
	}
	return " (short: " + joinPath(e.Short) + ")"
}

// under reports whether path is below the group prefix (all paths for the root).
func under(path, prefix []string) bool {
	return len(path) >= len(prefix) && equal(path[:len(prefix)], prefix)
}
