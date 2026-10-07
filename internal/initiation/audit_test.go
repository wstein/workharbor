package initiation_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// These are the complete human entry points, plus upgrades after an answer
// succeeds. Adding a background mint requires a reviewed change to this list.
func TestPinnedMintSitesAndAdapterBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	var mints []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if ok && pkg.Name == "initiation" && (sel.Sel.Name == "UserAction" || sel.Sel.Name == "DecisionAnswer") {
					mints = append(mints, filepath.ToSlash(rel)+":"+fn.Name.Name+":"+sel.Sel.Name)
				}
				if strings.HasPrefix(rel, "service/") {
					if recv, ok := sel.X.(*ast.SelectorExpr); ok && recv.Sel.Name == "ag" && (sel.Sel.Name == "Start" || sel.Sel.Name == "Resume") {
						want := "startAgent"
						if sel.Sel.Name == "Resume" {
							want = "launch"
						}
						if fn.Name.Name != want {
							t.Errorf("unreviewed send %s:%s", rel, fn.Name.Name)
						}
					}
					if rel == "service/reconcile.go" && sel.Sel.Name == "DueResumes" {
						t.Error("reset time must not schedule recovery")
					}
					if sel.Sel.Name == "Instruct" && fn.Name.Name != "Say" {
						t.Errorf("unreviewed Instruct %s:%s", rel, fn.Name.Name)
					}
				}
				return true
			})
		}
		if strings.HasPrefix(rel, "service/") {
			ast.Inspect(file, func(n ast.Node) bool {
				structure, ok := n.(*ast.StructType)
				if !ok {
					return true
				}
				for _, field := range structure.Fields.List {
					if sel, ok := field.Type.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "agent" && sel.Sel.Name == "Adapter" {
							t.Errorf("raw adapter field outside gate: %s", rel)
						}
					}
					for _, name := range field.Names {
						if name.Name == "ag" {
							p, ok := field.Type.(*ast.StarExpr)
							if !ok {
								t.Error("service adapter is not a concrete gate")
							} else if sel, ok := p.X.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Gate" {
								t.Error("service adapter bypasses gate")
							}
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"api/server.go:answer:UserAction", "api/server.go:resume:UserAction", "api/server.go:runTask:UserAction", "api/server.go:say:UserAction",
		"service/run.go:Answer:DecisionAnswer", "service/service.go:AnswerDecision:DecisionAnswer",
		"web/pages.go:answer:UserAction", "web/pages.go:resume:UserAction", "web/pages.go:say:UserAction", "web/pages.go:start:UserAction", "web/passkeys.go:stepUpFinish:UserAction",
	}
	sort.Strings(mints)
	sort.Strings(want)
	if !reflect.DeepEqual(mints, want) {
		t.Fatalf("mint sites:\n got %v\nwant %v", mints, want)
	}
}
