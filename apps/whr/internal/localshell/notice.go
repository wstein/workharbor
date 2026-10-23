// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"io"
)

// emitStart runs only after validation, before the child owns the terminal.
func emitStart(w io.Writer, tty bool, m Metadata) error {
	if tty {
		if _, err := fmt.Fprintf(w, "whr: local shell in %s (no grant; host Docker access; detach keys ctrl-] ctrl-^)\n", terminal.Escape(m.Name)); err != nil {
			return err
		}
	}
	if m.DirectorySource == directoryFallback {
		_, err := fmt.Fprintf(w, "whr: warning: no workspace folder mount in %s; starting in %s\n", terminal.Escape(m.Name), terminal.Escape(m.Directory))
		return err
	}
	return nil
}
