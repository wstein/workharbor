// SPDX-License-Identifier: EUPL-1.2
package devcontainer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func installed(t *testing.T) Tools {
	t.Helper()
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	root, e := filepath.Abs("../../tools/devcontainer")
	if e != nil {
		t.Fatal(e)
	}
	return Tools{NodePath: node, Root: root}
}
func TestExactCompanionIntegrityAndCLIVersion(t *testing.T) {
	p := installed(t)
	if e := p.Verify(context.Background(), true); e != nil {
		t.Fatal(e)
	}
}
func TestCompanionHashTamperAndSymlinkRefuseBeforeExecution(t *testing.T) {
	original := installed(t)
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "symlink"}[symlink], func(t *testing.T) {
			p := Tools{NodePath: original.NodePath, Root: t.TempDir()}
			if e := os.CopyFS(p.Root, os.DirFS(original.Root)); e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(p.Root, "node_modules/jsonc-parser/lib/umd/main.js")
			if symlink {
				os.Remove(target)
				os.Symlink(filepath.Join(original.Root, "node_modules/jsonc-parser/lib/umd/main.js"), target)
			} else {
				os.WriteFile(target, []byte("throw new Error('CANARY')"), 0600)
			}
			if e := p.Verify(context.Background(), false); e == nil {
				t.Fatal("mutated dependency accepted")
			}
		})
	}
}
