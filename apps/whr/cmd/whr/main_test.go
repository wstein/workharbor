// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// TestScaffoldRefusesProductCommands keeps its original name because the CI
// acceptance manifest lists it; it now proves that commands whose work items
// are not delivered yet refuse with the usage code and write no data.
func TestScaffoldRefusesProductCommands(t *testing.T) {
	for _, args := range [][]string{{"setup"}, {"ws", "create"}, {"--version", "setup"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 64 {
			t.Fatalf("run(%q) exit = %d, want usage exit 64", args, code)
		}
		if out.Len() != 0 || !strings.HasPrefix(errOut.String(), "whr:") {
			t.Fatalf("run(%q): stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}

func TestRunVersionAndHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"version"}, {"--version"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 0 || out.Len() == 0 || errOut.Len() != 0 {
			t.Fatalf("run(%q) = %d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer null.Close()
	if isTerminal(null) {
		t.Error("/dev/null is not a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(w) {
		t.Error("a pipe is not a terminal")
	}
}
