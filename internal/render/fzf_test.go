package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemFzfIsMissingWhenNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	have, run := SystemFzf()
	if have() {
		t.Error("fzf found in an empty PATH")
	}
	if _, err := run([]string{"a"}); err == nil {
		t.Error("run without fzf succeeded")
	}
	// Pick with this missing fzf falls back to the numbered list
	in, out, w := asker("2\n")
	got, err := Pick(PickOptions{In: in, W: w, Items: []string{"a", "b"}, HaveFzf: have, Fzf: run, StdinTTY: true, StdoutTTY: true})
	if err != nil || got != 1 || out.Len() == 0 {
		t.Errorf("fallback: %d %v %q", got, err, out.String())
	}
}

func TestSystemFzfReadsTheChosenIndex(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncat >/dev/null\nprintf '1\\tb\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "fzf"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable stand-in in a test directory
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	have, run := SystemFzf()
	if !have() {
		t.Fatal("the stand-in fzf is not found")
	}
	if got, err := run([]string{"a", "b"}); err != nil || got != 1 {
		t.Errorf("got %d %v", got, err)
	}
}
