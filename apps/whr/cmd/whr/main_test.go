// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestScaffoldRefusesProductCommands(t *testing.T) {
	for _, args := range [][]string{{"setup"}, {"ws", "create"}, {"--version", "setup"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code == 0 {
			t.Fatalf("run(%q) succeeded before product commands exist", args)
		}
		if out.Len() != 0 || !strings.Contains(errOut.String(), "not implemented") {
			t.Fatalf("run(%q): stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}
