// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// Human renders the report for standard output in human mode. Every dynamic
// string is escaped for a terminal (FR-022).
func (r Report) Human() string {
	var b strings.Builder
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "%-7s %s  %s\n", c.Status, c.ID, terminal.Escape(c.Message))
		if c.Reason != "" {
			fmt.Fprintf(&b, "        reason: %s\n", terminal.Escape(c.Reason))
		}
		if len(c.Evidence) > 0 {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if enc.Encode(c.Evidence) == nil { // keys sorted
				fmt.Fprintf(&b, "        evidence: %s\n", terminal.Escape(strings.TrimSpace(buf.String())))
			}
		}
		if c.Remediation != "" {
			fmt.Fprintf(&b, "        hint: %s\n", terminal.Escape(c.Remediation))
		}
	}
	fmt.Fprintf(&b, "verdict: %s (%d pass, %d fail, %d skipped, %d unknown)\n",
		r.Verdict, r.Counts.Pass, r.Counts.Fail, r.Counts.Skipped, r.Counts.Unknown)
	return b.String()
}
