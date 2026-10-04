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
		f, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if allowed(rel) {
			if strings.HasPrefix(rel, "internal/forge/") && reExportsAdapter(f) {
				bad = append(bad, rel)
			}
			return nil
		}
		names := map[string]bool{}
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == forgePkg {
				name := "forge"
				if im.Name != nil && im.Name.Name == "." {
					bad = append(bad, rel) // a dot import hides every bare Adapter
					return nil
				}
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

// reExportsAdapter reports a type in package forge that aliases, redefines or
// embeds Adapter: used elsewhere as forge.<Name>, it would hand out the adapter
// without naming forge.Adapter (issue #247). A named field or parameter of type
// Adapter (the Guard's) is fine.
func reExportsAdapter(f *ast.File) bool {
	isAdapter := func(e ast.Expr) bool {
		if st, ok := e.(*ast.StarExpr); ok {
			e = st.X
		}
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "Adapter"
	}
	found := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name == "Adapter" {
				continue
			}
			if isAdapter(ts.Type) {
				found = true
			}
			ast.Inspect(ts.Type, func(n ast.Node) bool {
				var fl *ast.FieldList
				switch t := n.(type) {
				case *ast.InterfaceType:
					fl = t.Methods
				case *ast.StructType:
					fl = t.Fields
				}
				if fl != nil {
					for _, fd := range fl.List {
						if len(fd.Names) == 0 && isAdapter(fd.Type) {
							found = true
						}
					}
				}
				return true
			})
		}
	}
	return found
}

func rawForgeAllowed(rel string) bool {
	return strings.HasPrefix(rel, "internal/forge/") || rel == "internal/serve/forge.go" || rel == "internal/serve/forge_test.go"
}

// TestRawForgeStaysAtTheRoot: only forge/, forge_test.go (it asserts against the type) and the composition root (forge.go
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

// TestRawForgeScanCatchesDotImportAndReExport: a dot import of forge outside the
// allowlist and an alias or embedding of Adapter inside forge/ are reported.
func TestRawForgeScanCatchesDotImportAndReExport(t *testing.T) {
	cases := map[string]struct {
		rel, src string
		want     bool
	}{
		"dot import":     {"internal/x/x.go", "package x\nimport . \"" + forgePkg + "\"\nvar _ Adapter\n", true},
		"alias":          {"internal/forge/raw.go", "package forge\ntype Raw = Adapter\n", true},
		"definition":     {"internal/forge/raw.go", "package forge\ntype Raw Adapter\n", true},
		"interface":      {"internal/forge/raw.go", "package forge\ntype Raw interface{ Adapter; X() }\n", true},
		"struct":         {"internal/forge/raw.go", "package forge\ntype Raw struct{ *Adapter }\n", true},
		"named field":    {"internal/forge/raw.go", "package forge\ntype G struct{ a Adapter }\n", false},
		"adapter itself": {"internal/forge/raw.go", "package forge\ntype Adapter interface{ X() }\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, c.rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(c.src), 0o600); err != nil {
				t.Fatal(err)
			}
			got := rawForgeUses(t, root, rawForgeAllowed)
			if (len(got) > 0) != c.want {
				t.Errorf("violations = %v, want violation %v", got, c.want)
			}
		})
	}
}
