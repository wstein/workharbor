// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

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
	if e.ID == "workspace.ls" {
		b.WriteString("Local: --local lists running and stopped labelled containers (5 s total); no runtime is started.\nWithout --local: waits for owner_api and exits 64.\n")
	}
	if e.JSON == "rejected" {
		b.WriteString("JSON: not supported (--json is a usage error)\n")
	}
	if usages := c.LocalNonPersistentFlags().FlagUsages(); strings.TrimSpace(usages) != "" && (e.ID == "workspace.init" || e.ID == "workspace.ls") {
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
