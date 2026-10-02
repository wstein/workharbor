package devcontainer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParse feeds devcontainer.json as a repository (and so an agent) may write it.
// Whatever the bytes, Parse never panics, and a file it accepts never carries what
// D38 refuses: no host command, mount, runtime argument, privilege, capability or
// security option, no root user, no reserved variable, no absolute build path; its
// ports are real ports and its egress requests are host names.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`{"image":"golang:1.26"}`,
		"// c\n{\"image\":\"x\", /* y */ \"forwardPorts\":[3000, 8080,],}",
		`{"build":{"dockerfile":"Dockerfile","context":".."},"postCreateCommand":["make","dep"]}`,
		`{"image":"x","mounts":["a"]}`,
		`{"image":"x","remoteUser":"root"}`,
		`{"image":"x","mounts":[]}`,
		`{"image":"x","containerEnv":{"HOME":"/"}}`,
		`{"image":"x","customizations":{"workharbor":{"egress":["proxy.golang.org","1.2.3.4"],"previewPorts":[3000,99999]}}}`,
		`{"image":"x","features":{"ghcr.io/devcontainers/features/go:1":{}}}`,
		`{"image":"x","build":{"dockerfile":"/etc/passwd"}}`,
		`{"image":"x","image":"y","privileged":true}`,
		``, `{`, `[]`, `null`, `"x"`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			return
		}
		clean, cerr := stripJSONC(data)
		if cerr != nil {
			t.Fatalf("Parse accepted what stripJSONC refuses: %v", cerr)
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(clean, &raw) != nil {
			t.Fatal("Parse accepted what is not an object")
		}
		for key := range refused {
			if _, ok := raw[key]; ok {
				t.Fatalf("an accepted file carries %q", key)
			}
		}
		for _, key := range []string{"remoteUser", "containerUser"} {
			if v, ok := raw[key]; ok {
				var user string
				if json.Unmarshal(v, &user) != nil || isRoot(user) {
					t.Fatalf("an accepted file has %s %s", key, v)
				}
			}
		}
		for name := range c.Env {
			if isReservedEnv(name) {
				t.Fatalf("an accepted file sets the reserved variable %q", name)
			}
		}
		for name := range c.BuildArgs {
			if isReservedEnv(name) {
				t.Fatalf("an accepted file passes the reserved build argument %q", name)
			}
		}
		for _, p := range append(append([]int(nil), c.ForwardPorts...), c.Hints.PreviewPorts...) {
			if p < 1 || p > 65535 {
				t.Fatalf("port %d is not a port", p)
			}
		}
		for _, h := range c.EgressRequests {
			if !ValidHost(h) {
				t.Fatalf("egress request %q is not a valid host name", h)
			}
		}
		if (c.Image == "") == (c.Dockerfile == "") {
			t.Fatalf("exactly one of image and build.dockerfile must be set: %q %q", c.Image, c.Dockerfile)
		}
		if strings.HasPrefix(c.Dockerfile, "/") || strings.HasPrefix(c.Context, "/") || strings.Contains(c.Dockerfile+c.Context, "\\") {
			t.Fatalf("an absolute or backslash build path was accepted: %q %q", c.Dockerfile, c.Context)
		}
	})
}

// treeRunner answers git with a fixed ls-tree output and blob.
type treeRunner struct{ ls, blob []byte }

func (r treeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "ls-tree":
		return r.ls, nil
	case "cat-file":
		return r.blob, nil
	}
	return nil, errors.New("unexpected git command")
}

// FuzzExport hands Export an ls-tree listing and blobs a hostile repository could
// produce. Whatever they hold, nothing is written outside the destination, nothing
// but regular files is written, the limits hold, and file() returns a blob only
// for one regular file at exactly the path asked for.
func FuzzExport(f *testing.F) {
	f.Add([]byte("100644 blob "+strings.Repeat("a", 40)+"\tDockerfile\x00"), []byte("FROM x\n"), "Dockerfile", ".")
	f.Add([]byte("100644 blob "+strings.Repeat("a", 40)+"\t../evil\x00"), []byte("x"), "../evil", ".")
	f.Add([]byte("120000 blob "+strings.Repeat("b", 40)+"\tlink\x00"), []byte("/etc/passwd"), "link", ".")
	f.Add([]byte("100755 blob "+strings.Repeat("c", 40)+"\ta/b/c.sh\x00160000 commit "+strings.Repeat("d", 40)+"\tsub\x00"), []byte("#!/bin/sh"), "a/b/c.sh", "a")
	f.Add([]byte("100644 blob "+strings.Repeat("a", 40)+"\ta\x00100644 blob "+strings.Repeat("a", 40)+"\ta/b\x00"), []byte("x"), "a", ".")
	f.Add([]byte("100644 blob "+strings.Repeat("a", 40)+"\t/abs\x00100644 blob "+strings.Repeat("a", 40)+"\t.\x00"), []byte("x"), "/abs", "/")
	f.Add([]byte("garbage"), []byte(""), "", "..")
	f.Fuzz(func(t *testing.T, ls, blob []byte, p, dir string) {
		base := t.TempDir()
		dest := filepath.Join(base, "dest")
		r := treeRunner{ls: ls, blob: blob}
		lim := Limits{Files: 40, Bytes: 1 << 16}
		res, _ := Export(context.Background(), r, "main", dir, dest, lim)
		if res.Files > lim.Files+1 || res.Bytes > lim.Bytes+int64(len(blob)) {
			t.Fatalf("limits exceeded: %d files, %d bytes", res.Files, res.Bytes)
		}
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || path == base {
				return nil
			}
			rel, rerr := filepath.Rel(dest, path)
			if path != dest && (rerr != nil || !filepath.IsLocal(rel)) {
				t.Fatalf("%q was written outside the destination", path)
			}
			if d.Type()&os.ModeSymlink != 0 || (!d.IsDir() && !d.Type().IsRegular()) {
				t.Fatalf("%q is not a directory or a regular file", path)
			}
			return nil
		})
		// the single-file reader: a blob only for one regular file at exactly p
		data, err := file(context.Background(), r, "main", p)
		if err == nil {
			list, lerr := lsTree(context.Background(), r, "main", p, false)
			if lerr != nil || len(list) != 1 || list[0].Path != p || !list[0].regular() {
				t.Fatalf("file(%q) returned data without exactly one regular entry at that path: %+v", p, list)
			}
			if len(data) > maxFileBytes || !bytes.Equal(data, blob) {
				t.Fatalf("file(%q) returned %d bytes", p, len(data))
			}
		}
	})
}
