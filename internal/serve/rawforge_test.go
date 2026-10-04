package serve

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const forgePkg = "github.com/wstein/workharbor/internal/forge"

// rawForgeUses returns the Go files under modRoot (paths relative to it, with
// slashes) that name forge.Adapter, in code or tests, unless allowed (issue
// #247). It reads each file's imports, so an alias does not hide the type.
func rawForgeUses(t *testing.T, modRoot string, allowed func(rel string) bool) []string {
	t.Helper()
	var bad []string
	err := filepath.WalkDir(modRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(modRoot, path)
		rel = filepath.ToSlash(rel)
		if allowed(rel) {
			return nil
		}
		f, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == forgePkg {
				name := "forge"
				if im.Name != nil {
					name = im.Name.Name
				}
				names[name] = true
			}
		}
		if len(names) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if se, ok := n.(*ast.SelectorExpr); ok && se.Sel.Name == "Adapter" {
				if id, ok := se.X.(*ast.Ident); ok && names[id.Name] {
					bad = append(bad, rel)
					return false
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return bad
}

func rawForgeAllowed(rel string) bool {
	return strings.HasPrefix(rel, "internal/forge/") || rel == "internal/serve/forge.go"
}

// TestRawForgeStaysAtTheRoot: only forge/ and the composition root (forge.go
// narrows the adapter; real.go builds the GitHub client without naming the
// type) take forge.Adapter; every other package gets a narrow member.
func TestRawForgeStaysAtTheRoot(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if bad := rawForgeUses(t, root, rawForgeAllowed); len(bad) > 0 {
		t.Errorf("forge.Adapter is named outside forge/ and serve/forge.go (take a narrow interface, issue #247): %v", bad)
	}
}

// TestRawForgeScanCatchesAViolation shows the scan fails: a package that names
// the adapter type, under an alias, is reported.
func TestRawForgeScanCatchesAViolation(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/x/x.go", "package x\nimport f \""+forgePkg+"\"\ntype T struct{ A f.Adapter }\n")
	write("internal/y/y.go", "package y\nimport \""+forgePkg+"\"\ntype T struct{ G *forge.Guard }\n")
	write("internal/serve/forge.go", "package serve\nimport \""+forgePkg+"\"\nvar _ forge.Adapter\n")
	got := rawForgeUses(t, root, rawForgeAllowed)
	if len(got) != 1 || got[0] != "internal/x/x.go" {
		t.Errorf("violations = %v, want only internal/x/x.go", got)
	}
}
