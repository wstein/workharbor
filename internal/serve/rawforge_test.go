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
			if strings.HasPrefix(rel, "internal/forge/") && reExportsAdapter(rel, f) {
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

// adapterReturners names exported functions and methods under internal/forge
// that may return Adapter, as "<rel path>:<Func or Recv.Method>". None does
// today: NewGuard takes the adapter and returns *Guard. A caller of such a
// function would hold the adapter without naming the type, so each entry needs
// a reason here.
var adapterReturners = map[string]bool{}

// reExportsAdapter reports a type in forge or one of its subpackages that
// aliases, redefines or embeds Adapter, or an exported function or method that
// returns it (not in adapterReturners): used elsewhere as forge.<Name>, each
// would hand out the adapter without naming forge.Adapter (issue #247). Adapter
// is the bare name inside package forge and <alias>.Adapter elsewhere, under
// any import alias of the forge package. A named field or parameter of type
// Adapter (the Guard's) is fine.
func reExportsAdapter(rel string, f *ast.File) bool {
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
	isAdapter := func(e ast.Expr) bool {
		if st, ok := e.(*ast.StarExpr); ok {
			e = st.X
		}
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name == "Adapter"
		case *ast.SelectorExpr:
			id, ok := t.X.(*ast.Ident)
			return ok && t.Sel.Name == "Adapter" && names[id.Name]
		}
		return false
	}
	found := false
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				r := fd.Recv.List[0].Type
				if st, ok := r.(*ast.StarExpr); ok {
					r = st.X
				}
				if id, ok := r.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			if fd.Name.IsExported() && fd.Type.Results != nil && !adapterReturners[rel+":"+name] {
				for _, res := range fd.Type.Results.List {
					if isAdapter(res.Type) {
						found = true
					}
				}
			}
			continue
		}
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
		"dot import":       {"internal/x/x.go", "package x\nimport . \"" + forgePkg + "\"\nvar _ Adapter\n", true},
		"alias":            {"internal/forge/raw.go", "package forge\ntype Raw = Adapter\n", true},
		"definition":       {"internal/forge/raw.go", "package forge\ntype Raw Adapter\n", true},
		"interface":        {"internal/forge/raw.go", "package forge\ntype Raw interface{ Adapter; X() }\n", true},
		"struct":           {"internal/forge/raw.go", "package forge\ntype Raw struct{ *Adapter }\n", true},
		"selector alias":   {"internal/forge/github/raw.go", "package github\nimport f \"" + forgePkg + "\"\ntype Raw = f.Adapter\n", true},
		"selector embed":   {"internal/forge/github/raw.go", "package github\nimport \"" + forgePkg + "\"\ntype Raw struct{ forge.Adapter }\n", true},
		"func returns":     {"internal/forge/raw.go", "package forge\nfunc AsRaw(a any) Adapter { return nil }\n", true},
		"func returns ptr": {"internal/forge/raw.go", "package forge\nfunc AsRaw(a any) (*Adapter, error) { return nil, nil }\n", true},
		"selector func":    {"internal/forge/github/raw.go", "package github\nimport f \"" + forgePkg + "\"\nfunc AsRaw() f.Adapter { return nil }\n", true},
		"method returns":   {"internal/forge/raw.go", "package forge\nfunc (g *Guard) Inner() Adapter { return nil }\n", true},
		"unexported func":  {"internal/forge/raw.go", "package forge\nfunc asRaw() Adapter { return nil }\n", false},
		"func takes":       {"internal/forge/raw.go", "package forge\nfunc NewGuard(a Adapter) *Guard { return nil }\n", false},
		"selector field":   {"internal/forge/github/raw.go", "package github\nimport \"" + forgePkg + "\"\ntype G struct{ a forge.Adapter }\n", false},
		"named field":      {"internal/forge/raw.go", "package forge\ntype G struct{ a Adapter }\n", false},
		"adapter itself":   {"internal/forge/raw.go", "package forge\ntype Adapter interface{ X() }\n", false},
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

// TestAdapterReturnersAllowlist: a listed function or method is let through,
// by "<file>:<Recv.Method>" or "<file>:<Func>", and its neighbour is not.
func TestAdapterReturnersAllowlist(t *testing.T) {
	adapterReturners["internal/forge/raw.go:Guard.Inner"] = true
	adapterReturners["internal/forge/raw.go:AsRaw"] = true
	t.Cleanup(func() { clear(adapterReturners) })
	check := func(src string, want bool) {
		t.Helper()
		f, err := parser.ParseFile(gotoken.NewFileSet(), "raw.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := reExportsAdapter("internal/forge/raw.go", f); got != want {
			t.Errorf("%q: got %v, want %v", src, got, want)
		}
	}
	check("package forge\nfunc (g *Guard) Inner() Adapter { return nil }\n", false)
	check("package forge\nfunc AsRaw() Adapter { return nil }\n", false)
	check("package forge\nfunc (g *Guard) Other() Adapter { return nil }\n", true)
}
