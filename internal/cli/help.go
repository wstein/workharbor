package cli

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/render"
)

// descColumn finds where a flag or command description starts on a line like
// "      --listen   use a ...": text, two or more spaces, then more text.
var descColumn = regexp.MustCompile(`^( *\S.*?\S {2,})\S`)

// hangWrap wraps every line over 80 columns at spaces, except a command line
// ("$ ..."), which is copied whole and so is never broken. A flag or command
// description continues under its own column; other text keeps the indent of
// its first line.
func hangWrap(text string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if utf8.RuneCountInString(line) <= 80 || strings.HasPrefix(strings.TrimSpace(line), "$ ") {
			out = append(out, line)
			continue
		}
		col := len(line) - len(strings.TrimLeft(line, " "))
		head := strings.Repeat(" ", col)
		if m := descColumn.FindStringSubmatch(line); m != nil && utf8.RuneCountInString(m[1]) < 40 {
			col, head = utf8.RuneCountInString(m[1]), m[1]
		}
		pad := strings.Repeat(" ", col)
		out = append(out, head+strings.Join(strings.Split(render.Wrap(line[len(head):], col), "\n"), "\n"+pad))
	}
	return strings.Join(out, "\n") + "\n"
}

// helpFunc prints a command's help like cobra does, wrapped to 80 columns.
func helpFunc(c *cobra.Command, _ []string) {
	desc := c.Long
	if desc == "" {
		desc = c.Short
	}
	text := strings.TrimRight(desc, " \n")
	if c.Runnable() || c.HasSubCommands() {
		text += "\n\n" + c.UsageString()
	}
	fmt.Fprint(c.OutOrStdout(), hangWrap(text))
}
