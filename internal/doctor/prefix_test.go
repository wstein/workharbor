package doctor

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prefixHoldsDir compares by identity: the same directory under another
// spelling is held, a sibling is not, and an unreadable path fails closed.
func TestPrefixHoldsDir(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "Users", "werner")
	sibling := filepath.Join(root, "Users", "other")
	for _, d := range []string{home, sibling} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(root, "gone", "home") // does not exist; root is its parent's parent

	for _, c := range []struct {
		name        string
		prefix, dir string
		want        bool
	}{
		{"equal", home, home, true},
		{"parent", filepath.Dir(home), home, true},
		{"grandparent", root, home, true},
		{"inside the home", filepath.Join(home, ".local"), home, false},
		{"sibling", sibling, home, false},
		{"empty dir", home, "", false},
		{"relative dir fails closed", home, "relative/home", true},
		{"missing home, its existing parent", root, missing, true},
		{"missing home, unrelated prefix", sibling, missing, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Join(home, ".local"), 0o700); err != nil {
				t.Fatal(err)
			}
			if got := prefixHoldsDir(c.prefix, c.dir); got != c.want {
				t.Fatalf("prefixHoldsDir(%q, %q) = %v, want %v", c.prefix, c.dir, got, c.want)
			}
		})
	}

	t.Run("home is a symlink, prefix is the real parent", func(t *testing.T) {
		// The link lives outside Users, so only resolving it first reaches the
		// real parent chain; walking the link's own parents never would.
		linkDir := filepath.Join(root, "links")
		if err := os.MkdirAll(linkDir, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(linkDir, "home")
		if err := os.Symlink(home, link); err != nil {
			t.Skip("symlinks are unavailable:", err)
		}
		if !prefixHoldsDir(filepath.Dir(home), link) {
			t.Fatal("a symlinked home was not held by its real parent")
		}
		if prefixHoldsDir(sibling, link) {
			t.Fatal("a symlinked home was held by an unrelated prefix")
		}
	})

	t.Run("upper-cased spelling", func(t *testing.T) {
		if _, err := os.Stat(strings.ToUpper(home)); err != nil {
			t.Skip("the volume is case-sensitive")
		}
		if !prefixHoldsDir(strings.ToUpper(filepath.Dir(home)), home) || !prefixHoldsDir(strings.ToUpper(home), home) {
			t.Fatal("an upper-cased spelling of the home or its parent was not held")
		}
	})

	t.Run("another path to the same directory", func(t *testing.T) {
		// What /System/Volumes/Data/Users is to /Users: a name EvalSymlinks does
		// not resolve, that stat answers with the same directory.
		alias := "/alias/Users"
		fi, err := os.Stat(filepath.Dir(home))
		if err != nil {
			t.Fatal(err)
		}
		old := statDir
		statDir = func(p string) (fs.FileInfo, error) {
			if p == alias {
				return fi, nil
			}
			return old(p)
		}
		t.Cleanup(func() { statDir = old })
		if !prefixHoldsDir(alias, home) {
			t.Fatal("an alias of the home's parent was not held")
		}
		if !prefixHoldsDir(alias, sibling+"/x") { // sibling/x is missing; its parent chain reaches Users
			t.Fatal("the alias must hold a missing home below the same parent")
		}
	})

	t.Run("a stat error other than not-exist fails closed", func(t *testing.T) {
		old := statDir
		statDir = func(p string) (fs.FileInfo, error) {
			if p == home {
				return nil, fs.ErrPermission
			}
			return old(p)
		}
		t.Cleanup(func() { statDir = old })
		if !prefixHoldsDir(sibling, home) {
			t.Fatal("a permission error was not treated as held")
		}
	})

	t.Run("a prefix that cannot be examined fails closed", func(t *testing.T) {
		if !prefixHoldsDir(filepath.Join(root, "no", "such", "prefix"), home) {
			t.Fatal("an unreadable prefix was not treated as held")
		}
	})
}
