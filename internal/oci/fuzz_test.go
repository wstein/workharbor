package oci

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzExtract unpacks whatever archive a registry sends. Whatever the bytes, nothing is
// written outside the directory, nothing but regular files and directories exists
// afterwards, the limits hold, and it never panics.
func FuzzExtract(f *testing.F) {
	f.Add(makeTarForSeed("install.sh", "#!/bin/sh\n", 0o755))
	f.Add(makeTarForSeed("../evil", "x", 0o644))
	f.Add(makeTarForSeed("/abs", "x", 0o644))
	f.Add([]byte("not a tar"))
	f.Add([]byte{0x1f, 0x8b, 0x08, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		base := t.TempDir()
		dir := filepath.Join(base, "feature")
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		lim := ExtractLimits{Files: 20, Bytes: 1 << 16, Depth: 6}
		_ = Extract(bytes.NewReader(data), dir, lim)
		var files int
		var total int64
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || path == base || path == dir {
				return nil
			}
			if rel, rerr := filepath.Rel(dir, path); rerr != nil || !filepath.IsLocal(rel) {
				t.Fatalf("%q is outside the directory", path)
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				t.Fatalf("%q is not a regular file", path)
			}
			files++
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
			return nil
		})
		if files > lim.Files || total > lim.Bytes {
			t.Fatalf("%d files and %d bytes exceed the limits", files, total)
		}
	})
}

// FuzzParseRef: what is accepted round-trips and never carries a character that could
// become a path, an option or a second host.
func FuzzParseRef(f *testing.F) {
	for _, s := range []string{"ghcr.io/devcontainers/features/node:1", "ghcr.io/a/b@sha256:" + string(bytes.Repeat([]byte("a"), 64)), "x/y:z", "", "../x/y:1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseRef(s)
		if err != nil {
			return
		}
		again, err := ParseRef(r.String())
		if err != nil || again != r {
			t.Fatalf("%q does not round-trip: %+v %v", s, again, err)
		}
		// Check each field alone: joining them makes a false ".." out of "0", "0" and "0..".
		// A dot-dot is a traversal only as a whole path segment, and a tag is one segment
		// that starts with a letter, digit or underscore, so it may hold dots later on.
		for name, field := range map[string]string{"registry": r.Registry, "repo": r.Repo, "tag": r.Tag} {
			for _, bad := range []string{"//", " ", "\\", "?", "#", "@", "/"} {
				if name == "repo" && bad == "/" {
					continue
				}
				if strings.Contains(field, bad) {
					t.Fatalf("%q was accepted with %q in its %s", s, bad, name)
				}
			}
			if name != "tag" && strings.Contains(field, "..") {
				t.Fatalf("%q was accepted with .. in its %s", s, name)
			}
			if name == "tag" && (field == "." || field == "..") {
				t.Fatalf("%q was accepted with a dot segment as its tag", s)
			}
		}
	})
}
