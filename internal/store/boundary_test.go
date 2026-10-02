package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Snapshot, Restore and MarkSaved are exported because the store, in another
// package, must call them. No other production code may: a caller that forged
// a snapshot could skip every guard of the aggregate (design §4.1). The store
// also refuses a change that no event accounts for (ErrNoEvents), so this is
// the second line, and this test is the first.
func TestOnlyTheStoreUsesSnapshotAndRestore(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "spikes") {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slash := filepath.ToSlash(path)
		if strings.Contains(slash, "internal/store/") || strings.Contains(slash, "internal/domain/") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Snapshot", "Restore", "MarkSaved":
				// golang.org/x/term has a Restore of its own (a terminal's mode).
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "term" && sel.Sel.Name == "Restore" {
					return true
				}
				// A Snapshot method on another type would be a false alarm; the
				// names are reserved in this code base, so report them all.
				t.Errorf("%s: %s is for the store only", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
