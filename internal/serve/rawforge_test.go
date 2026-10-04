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
	// isAdapter walks a type expression: Adapter itself, or any composite that
	// holds one a caller can read out (pointer, slice, array, map, chan, a
	// func's results, an exported or embedded struct field, an interface
	// method's results, a generic argument). Parameters and unexported fields
	// do not hand the adapter out.
	var isAdapter func(e ast.Expr) bool
	fieldsHave := func(fl *ast.FieldList, exportedOnly bool) bool {
		if fl == nil {
			return false
		}
		for _, fd := range fl.List {
			visible := len(fd.Names) == 0 || !exportedOnly
			for _, n := range fd.Names {
				if n.IsExported() {
					visible = true
				}
			}
			if visible && isAdapter(fd.Type) {
				return true
			}
		}
		return false
	}
	isAdapter = func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name == "Adapter"
		case *ast.SelectorExpr:
			id, ok := t.X.(*ast.Ident)
			return ok && t.Sel.Name == "Adapter" && names[id.Name]
		case *ast.StarExpr:
			return isAdapter(t.X)
		case *ast.ParenExpr:
			return isAdapter(t.X)
		case *ast.ArrayType:
			return isAdapter(t.Elt)
		case *ast.Ellipsis:
			return isAdapter(t.Elt)
		case *ast.MapType:
			return isAdapter(t.Key) || isAdapter(t.Value)
		case *ast.ChanType:
			return isAdapter(t.Value)
		case *ast.IndexExpr:
			return isAdapter(t.X) || isAdapter(t.Index)
		case *ast.IndexListExpr:
			if isAdapter(t.X) {
				return true
			}
			for _, i := range t.Indices {
				if isAdapter(i) {
					return true
				}
			}
		case *ast.FuncType:
			return fieldsHave(t.Results, false)
		case *ast.StructType:
			return fieldsHave(t.Fields, true)
		case *ast.InterfaceType:
			return fieldsHave(t.Methods, true)
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
				if fieldsHave(fd.Type.Results, false) {
					found = true
				}
			}
			continue
		}
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			switch s := sp.(type) {
			case *ast.TypeSpec:
				if s.Name.Name != "Adapter" && isAdapter(s.Type) {
					found = true
				}
			case *ast.ValueSpec:
				// Only a declared type is visible to the scan.
				for _, n := range s.Names {
					if n.IsExported() && s.Type != nil && isAdapter(s.Type) {
						found = true
					}
				}
			}
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
		"dot import":        {"internal/x/x.go", "package x\nimport . \"" + forgePkg + "\"\nvar _ Adapter\n", true},
		"alias":             {"internal/forge/raw.go", "package forge\ntype Raw = Adapter\n", true},
		"definition":        {"internal/forge/raw.go", "package forge\ntype Raw Adapter\n", true},
		"interface":         {"internal/forge/raw.go", "package forge\ntype Raw interface{ Adapter; X() }\n", true},
		"struct":            {"internal/forge/raw.go", "package forge\ntype Raw struct{ *Adapter }\n", true},
		"selector alias":    {"internal/forge/github/raw.go", "package github\nimport f \"" + forgePkg + "\"\ntype Raw = f.Adapter\n", true},
		"selector embed":    {"internal/forge/github/raw.go", "package github\nimport \"" + forgePkg + "\"\ntype Raw struct{ forge.Adapter }\n", true},
		"func returns":      {"internal/forge/raw.go", "package forge\nfunc AsRaw(a any) Adapter { return nil }\n", true},
		"func returns ptr":  {"internal/forge/raw.go", "package forge\nfunc AsRaw(a any) (*Adapter, error) { return nil, nil }\n", true},
		"selector func":     {"internal/forge/github/raw.go", "package github\nimport f \"" + forgePkg + "\"\nfunc AsRaw() f.Adapter { return nil }\n", true},
		"method returns":    {"internal/forge/raw.go", "package forge\nfunc (g *Guard) Inner() Adapter { return nil }\n", true},
		"result second":     {"internal/forge/raw.go", "package forge\nfunc AsRaw() (error, Adapter) { return nil, nil }\n", true},
		"result third":      {"internal/forge/raw.go", "package forge\nfunc AsRaw() (int, error, *Adapter) { return 0, nil, nil }\n", true},
		"result named":      {"internal/forge/raw.go", "package forge\nfunc AsRaw() (n int, a Adapter) { return }\n", true},
		"exported var":      {"internal/forge/raw.go", "package forge\nvar Default Adapter\n", true},
		"exported var sel":  {"internal/forge/github/raw.go", "package github\nimport \"" + forgePkg + "\"\nvar A, Default forge.Adapter\n", true},
		"unexported var":    {"internal/forge/raw.go", "package forge\nvar def Adapter\n", false},
		"exported field":    {"internal/forge/raw.go", "package forge\ntype G struct{ Raw Adapter }\n", true},
		"exported field 2":  {"internal/forge/raw.go", "package forge\ntype G struct{ a, Raw Adapter }\n", true},
		"mixed fields":      {"internal/forge/raw.go", "package forge\ntype G struct{ a Adapter; N int }\n", false},
		"slice result":      {"internal/forge/raw.go", "package forge\nfunc All() []Adapter { return nil }\n", true},
		"array result":      {"internal/forge/raw.go", "package forge\nfunc All() [2]Adapter { return [2]Adapter{} }\n", true},
		"map value result":  {"internal/forge/raw.go", "package forge\nfunc All() map[string]Adapter { return nil }\n", true},
		"map key result":    {"internal/forge/raw.go", "package forge\nfunc All() map[Adapter]int { return nil }\n", true},
		"chan result":       {"internal/forge/raw.go", "package forge\nfunc All() <-chan Adapter { return nil }\n", true},
		"func result":       {"internal/forge/raw.go", "package forge\nfunc All() func() Adapter { return nil }\n", true},
		"nested result":     {"internal/forge/raw.go", "package forge\nfunc All() map[string][]*Adapter { return nil }\n", true},
		"selector slice":    {"internal/forge/github/raw.go", "package github\nimport f \"" + forgePkg + "\"\nfunc All() []f.Adapter { return nil }\n", true},
		"generic result":    {"internal/forge/raw.go", "package forge\nfunc All() List[Adapter] { return nil }\n", true},
		"generic base":      {"internal/forge/raw.go", "package forge\nfunc All() Adapter[int] { return nil }\n", true},
		"slice var":         {"internal/forge/raw.go", "package forge\nvar All []Adapter\n", true},
		"slice field":       {"internal/forge/raw.go", "package forge\ntype G struct{ All []Adapter }\n", true},
		"func field":        {"internal/forge/raw.go", "package forge\ntype G struct{ Get func() Adapter }\n", true},
		"nested struct":     {"internal/forge/raw.go", "package forge\ntype G struct{ In struct{ Raw Adapter } }\n", true},
		"slice type":        {"internal/forge/raw.go", "package forge\ntype L []Adapter\n", true},
		"iface method":      {"internal/forge/raw.go", "package forge\ntype I interface{ Get() Adapter }\n", true},
		"func param slice":  {"internal/forge/raw.go", "package forge\nfunc Set(a []Adapter) {}\n", false},
		"slice unexported":  {"internal/forge/raw.go", "package forge\ntype G struct{ all []Adapter }\nvar all []Adapter\nfunc all2() []Adapter { return nil }\n", false},
		"func param result": {"internal/forge/raw.go", "package forge\nfunc Hook() func(Adapter) { return nil }\n", false},
		"unexported func":   {"internal/forge/raw.go", "package forge\nfunc asRaw() Adapter { return nil }\n", false},
		"func takes":        {"internal/forge/raw.go", "package forge\nfunc NewGuard(a Adapter) *Guard { return nil }\n", false},
		"selector field":    {"internal/forge/github/raw.go", "package github\nimport \"" + forgePkg + "\"\ntype G struct{ a forge.Adapter }\n", false},
		"named field":       {"internal/forge/raw.go", "package forge\ntype G struct{ a Adapter }\n", false},
		"adapter itself":    {"internal/forge/raw.go", "package forge\ntype Adapter interface{ X() }\n", false},
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
