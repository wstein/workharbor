// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type stageGitSnapshot struct {
	Source, GitDir, CommonDir, ObjectDir, IndexPath, Head, ObjectFormat string
	Refs                                                                map[string]string
	Index                                                               []byte
	IndexDigest                                                         string
}
type stageGitCapture struct {
	stageGitSnapshot
	Clone        string
	Repositories []stageGitSnapshot
}

func stageOID(v string, format string) bool {
	n := 40
	if format == "sha256" {
		n = 64
	}
	if len(v) != n {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func stageRef(v string) bool {
	if !strings.HasPrefix(v, "refs/") || strings.HasSuffix(v, ".") || strings.HasSuffix(v, "/") || strings.Contains(v, "..") || strings.Contains(v, "@{") || strings.ContainsAny(v, " ~^:?*[\\\x00\r\n\t") {
		return false
	}
	for _, p := range strings.Split(v, "/") {
		if p == "" || strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".lock") {
			return false
		}
		for _, c := range p {
			if c < 32 || c == 127 {
				return false
			}
		}
	}
	return true
}
func stageAdminFile(root, path string, limit int64) ([]byte, error) {
	r, e := os.OpenRoot(root)
	if e != nil {
		return nil, stageConflict("Git administration path unavailable")
	}
	defer r.Close()
	// Ref and administrative directories must not be redirected through symlinks.
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		st, err := r.Lstat(parent)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, stageConflict("Git administration path is symlinked or unavailable")
		}
	}
	f, e := r.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, stageConflict("Git administration file type or quota")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, stageConflict("Git administration read quota")
	}
	return b, nil
}
func stageGitPaths(source string) (stageGitSnapshot, error) {
	s := stageGitSnapshot{Source: source, ObjectFormat: "sha1", Refs: map[string]string{}}
	p := filepath.Join(source, ".git")
	st, e := os.Lstat(p)
	if e != nil {
		return s, stageConflict("source is not a populated Git repository")
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return s, stageConflict("Git administration path is symlinked")
	}
	if st.IsDir() {
		s.GitDir = p
	} else {
		b, e := stageAdminFile(source, ".git", 4096)
		if e != nil || !strings.HasPrefix(string(b), "gitdir: ") {
			return s, stageConflict("Git directory pointer unsupported")
		}
		v := strings.TrimSuffix(strings.TrimPrefix(string(b), "gitdir: "), "\n")
		if strings.ContainsAny(v, "\x00\r\n") || v == "" {
			return s, stageConflict("Git directory pointer malformed")
		}
		if !filepath.IsAbs(v) {
			v = filepath.Join(source, v)
		}
		s.GitDir = filepath.Clean(v)
	}
	physical, e := filepath.EvalSymlinks(s.GitDir)
	if e != nil || physical != s.GitDir {
		return s, stageConflict("Git administration path is symlinked or unavailable")
	}
	s.CommonDir = s.GitDir
	if b, e := stageAdminFile(s.GitDir, "commondir", 4096); e == nil {
		v := strings.TrimSuffix(string(b), "\n")
		if strings.ContainsAny(v, "\x00\r\n") || v == "" {
			return s, stageConflict("Git common directory pointer malformed")
		}
		if !filepath.IsAbs(v) {
			v = filepath.Join(s.GitDir, v)
		}
		s.CommonDir = filepath.Clean(v)
	} else if !os.IsNotExist(e) {
		return s, e
	}
	physical, e = filepath.EvalSymlinks(s.CommonDir)
	if e != nil || physical != s.CommonDir {
		return s, stageConflict("Git common administration path is symlinked or unavailable")
	}
	s.ObjectDir = filepath.Join(s.CommonDir, "objects")
	s.IndexPath = filepath.Join(s.GitDir, "index")
	physical, e = filepath.EvalSymlinks(s.ObjectDir)
	if e != nil || physical != s.ObjectDir {
		return s, stageConflict("Git object directory is symlinked or unavailable")
	}
	// Parse only the object-format declaration as inert data. Includes, helpers,
	// hooks and transport configuration never enter the generated reader.
	if b, e := stageAdminFile(s.CommonDir, "config", 256<<10); e == nil {
		section := ""
		for _, line := range strings.Split(string(b), "\n") {
			v := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(v, "[") {
				section = v
				continue
			}
			if section == "[extensions]" {
				k, val, ok := strings.Cut(v, "=")
				if ok && strings.TrimSpace(k) == "objectformat" {
					s.ObjectFormat = strings.TrimSpace(val)
					if s.ObjectFormat != "sha1" && s.ObjectFormat != "sha256" {
						return s, stageConflict("Git object format unsupported")
					}
				}
			}
		}
	} else if !os.IsNotExist(e) {
		return s, e
	}
	return s, nil
}
func readStageGitSnapshot(source string) (stageGitSnapshot, error) {
	s, e := stageGitPaths(source)
	if e != nil {
		return s, e
	}
	add := func(name, oid string) error {
		if !stageRef(name) || !stageOID(oid, s.ObjectFormat) {
			return stageConflict("Git ref identity unsupported")
		}
		if strings.HasPrefix(name, "refs/replace/") {
			return stageConflict("Git replacement refs unsupported")
		}
		if _, ok := s.Refs[name]; ok && s.Refs[name] != oid {
			return stageConflict("Git duplicate ref identity")
		}
		s.Refs[name] = oid
		return nil
	}
	if b, e := stageAdminFile(s.CommonDir, "packed-refs", stageMaxEntries*(4096+128)); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line == "" || line[0] == '#' || line[0] == '^' {
				continue
			}
			oid, name, ok := strings.Cut(line, " ")
			if !ok {
				return s, stageConflict("Git packed refs malformed")
			}
			if e = add(name, oid); e != nil {
				return s, e
			}
		}
	} else if !os.IsNotExist(e) {
		return s, e
	}
	refsRoot := filepath.Join(s.CommonDir, "refs")
	if st, e := os.Lstat(refsRoot); e == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return s, stageConflict("Git refs path is symlinked")
		}
		n := 0
		e = filepath.WalkDir(refsRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return stageConflict("Git refs enumeration unavailable")
			}
			n++
			if n > stageMaxEntries {
				return stageConflict("Git ref entry quota")
			}
			if d.Type()&os.ModeSymlink != 0 {
				return stageConflict("Git symbolic export ref unsupported")
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(s.CommonDir, path)
			b, err := stageAdminFile(s.CommonDir, rel, 4096)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(rel)
			oid := strings.TrimSuffix(string(b), "\n")
			if !stageRef(name) || !stageOID(oid, s.ObjectFormat) {
				return stageConflict("Git symbolic or malformed export ref unsupported")
			}
			s.Refs[name] = oid
			return nil
		})
		if e != nil {
			return s, e
		}
	} else if !os.IsNotExist(e) {
		return s, stageConflict("Git refs unavailable")
	}
	for name := range s.Refs {
		if strings.HasPrefix(name, "refs/replace/") {
			return s, stageConflict("Git replacement refs unsupported")
		}
	}
	head, e := stageAdminFile(s.GitDir, "HEAD", 4096)
	if e != nil {
		return s, stageConflict("Git HEAD unavailable")
	}
	v := strings.TrimSuffix(string(head), "\n")
	if strings.HasPrefix(v, "ref: ") {
		name := strings.TrimPrefix(v, "ref: ")
		if !stageRef(name) {
			return s, stageConflict("Git HEAD symbolic identity unsupported")
		}
		s.Head = s.Refs[name]
	} else {
		s.Head = v
	}
	if !stageOID(s.Head, s.ObjectFormat) {
		return s, stageConflict("Git HEAD object identity unavailable")
	}
	index, e := stageAdminFile(s.GitDir, "index", stageMaxWorkingBytes)
	if e != nil && !os.IsNotExist(e) {
		return s, e
	}
	sum := sha256.Sum256(index)
	s.IndexDigest = "sha256:" + hex.EncodeToString(sum[:])
	return s, nil
}

func stageGitEnv(home, objects, index string, transport bool) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_COUNT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL="}
	if transport {
		env[len(env)-1] = "GIT_ALLOW_PROTOCOL=file"
	}
	if objects != "" {
		env = append(env, "GIT_OBJECT_DIRECTORY="+objects)
	}
	if index != "" {
		env = append(env, "GIT_INDEX_FILE="+index)
	}
	return env
}
func initStageGit(ctx context.Context, home, path, format string) error {
	cmd := stageGitCommand(ctx, home, "", "", "", false, "init", "--bare", "--template=", "--object-format="+format, "--", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e := runStageGit(cmd); e != nil {
		return stageConflict("private Git initialization failed")
	}
	return nil
}
func captureStageGit(ctx context.Context, source, captured, clone string) (stageGitCapture, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var result stageGitCapture
	ctx, tool, pinErr := pinStageGit(ctx)
	if pinErr != nil {
		return result, pinErr
	}
	_ = tool
	if !filepath.IsAbs(source) || !filepath.IsAbs(captured) || !filepath.IsAbs(clone) {
		return result, stageConflict("Git capture paths must be physical absolute paths")
	}
	s, e := readStageGitSnapshot(source)
	if e != nil {
		return result, e
	}
	private, e := os.MkdirTemp(filepath.Dir(clone), "git-capture-")
	if e != nil {
		return result, stageConflict("private Git capture storage unavailable")
	}
	defer os.RemoveAll(private)
	if e = os.Chmod(private, 0700); e != nil {
		return result, e
	}
	reader := filepath.Join(private, "reader.git")
	export := filepath.Join(private, "export.git")
	if e = initStageGit(ctx, private, reader, s.ObjectFormat); e != nil {
		return result, e
	}
	if e = initStageGit(ctx, private, export, s.ObjectFormat); e != nil {
		return result, e
	}
	names := make([]string, 0, len(s.Refs))
	for name := range s.Refs {
		if strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") || strings.HasPrefix(name, "refs/remotes/") || strings.HasPrefix(name, "refs/notes/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var refs, roots strings.Builder
	roots.WriteString(s.Head + "\n")
	for _, name := range names {
		refs.WriteString("create " + name + " " + s.Refs[name] + "\n")
		roots.WriteString(s.Refs[name] + "\n")
	}
	if _, e = stageGitRead(ctx, private, reader, s.ObjectDir, "", []byte(refs.String()), "update-ref", "--no-deref", "--stdin"); e != nil {
		return result, e
	}
	if e = os.WriteFile(filepath.Join(reader, "HEAD"), []byte(s.Head+"\n"), 0600); e != nil {
		return result, e
	}
	index, e := stageGitRead(ctx, private, reader, s.ObjectDir, s.IndexPath, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return result, e
	}
	s.Index = index
	for _, entry := range bytes.Split(index, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		header, _, ok := bytes.Cut(entry, []byte{'\t'})
		parts := strings.Fields(string(header))
		if !ok || len(parts) != 3 || !stageOID(parts[1], s.ObjectFormat) {
			return result, stageConflict("Git index representation unsupported")
		}
		if parts[0] != "160000" {
			roots.WriteString(parts[1] + "\n")
		}
	}
	objects, e := stageGitReadLimit(ctx, private, reader, s.ObjectDir, "", []byte(roots.String()), 66*1000000, "rev-list", "--objects", "--no-object-names", "--stdin")
	if e != nil {
		return result, e
	}
	ids := strings.Fields(string(objects))
	if len(ids) > 1000000 {
		return result, stageConflict("Git object count quota exceeded")
	}
	for _, id := range ids {
		if !stageOID(id, s.ObjectFormat) {
			return result, stageConflict("Git object listing malformed")
		}
	}
	sizes, e := stageGitReadLimit(ctx, private, reader, s.ObjectDir, "", objects, 128*1000000, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if e != nil {
		return result, e
	}
	var expanded int64
	lines := strings.Split(strings.TrimSuffix(string(sizes), "\n"), "\n")
	if len(lines) != len(ids) {
		return result, stageConflict("Git object inventory incomplete")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || !stageOID(fields[0], s.ObjectFormat) {
			return result, stageConflict("Git object inventory malformed")
		}
		n, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || n < 0 || n > 8<<30-expanded {
			return result, stageConflict("Git expanded object byte quota exceeded")
		}
		expanded += n
	}
	pack, e := os.OpenFile(filepath.Join(private, "objects.pack"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return result, e
	}
	defer pack.Close()
	packCmd := stageGitCommand(ctx, private, reader, s.ObjectDir, "", false, "pack-objects", "--stdout", "--revs")
	packCmd.Stdin = strings.NewReader(roots.String())
	packCmd.Stdout = &stageBoundedWriter{Writer: pack, Remaining: 8 << 30}
	packCmd.Stderr = io.Discard
	if e = runStageGit(packCmd); e != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, stageConflict("Git verified object export failed")
	}
	if _, e = pack.Seek(0, 0); e != nil {
		return result, e
	}
	unpack := stageGitCommand(ctx, private, export, "", "", false, "index-pack", "--strict", "--stdin")
	unpack.Stdin = pack
	unpack.Stdout = io.Discard
	unpack.Stderr = io.Discard
	if e = runStageGit(unpack); e != nil {
		return result, stageConflict("Git exported object verification failed")
	}
	if _, e = stageGitRead(ctx, private, export, "", "", []byte(refs.String()), "update-ref", "--no-deref", "--stdin"); e != nil {
		return result, e
	}
	if e = os.WriteFile(filepath.Join(export, "HEAD"), []byte(s.Head+"\n"), 0600); e != nil {
		return result, e
	}
	cloneCmd := stageGitCommand(ctx, private, "", "", "", true, "clone", "--no-local", "--no-hardlinks", "--no-checkout", "--template=", "--", export, clone)
	cloneCmd.Stdout = io.Discard
	cloneCmd.Stderr = io.Discard
	if e = runStageGit(cloneCmd); e != nil {
		return result, stageConflict("independent candidate clone failed")
	}
	// Clone transports only advertised refs; staged orphan blobs have no ref.
	// Import the already verified complete pack into the independent clone too.
	if _, e = pack.Seek(0, 0); e != nil {
		return result, e
	}
	importAll := stageGitCommand(ctx, private, filepath.Join(clone, ".git"), "", "", false, "index-pack", "--strict", "--stdin")
	importAll.Stdin, importAll.Stdout, importAll.Stderr = pack, io.Discard, io.Discard
	if e = runStageGit(importAll); e != nil {
		return result, stageConflict("candidate complete object import failed")
	}
	// Rebuild the raw index without copying executable configuration or index
	// extensions. The clone has no checkout, so captured working bytes remain
	// independent from both HEAD and the staged index.
	if _, e = stageGitRead(ctx, private, filepath.Join(clone, ".git"), "", "", index, "update-index", "-z", "--index-info"); e != nil {
		return result, e
	}
	if e = copyStageWorking(captured, clone); e != nil {
		return result, e
	}
	current, e := readStageGitSnapshot(source)
	if e != nil || current.Head != s.Head || current.IndexDigest != s.IndexDigest || !stageSameRefs(current.Refs, s.Refs) {
		return result, stageConflict("source Git state changed during capture")
	}
	if e = os.Chmod(clone, 0700); e != nil {
		return result, e
	}
	result = stageGitCapture{stageGitSnapshot: s, Clone: clone, Repositories: []stageGitSnapshot{s}}
	return result, nil
}

type stageBoundedWriter struct {
	io.Writer
	Remaining int64
}

func (w *stageBoundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.Remaining {
		return 0, fmt.Errorf("object export byte quota")
	}
	n, e := w.Writer.Write(p)
	w.Remaining -= int64(n)
	return n, e
}
func stageSameRefs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func copyStageWorking(source, destination string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return stageConflict("captured working tree unavailable")
		}
		rel, _ := filepath.Rel(source, path)
		if rel == "." {
			return nil
		}
		target := filepath.Join(destination, rel)
		st, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if d.IsDir() {
			return os.Mkdir(target, st.Mode().Perm())
		}
		if st.Mode()&os.ModeSymlink != 0 {
			v, e := os.Readlink(path)
			if e != nil {
				return e
			}
			return os.Symlink(v, target)
		}
		if !st.Mode().IsRegular() {
			return stageConflict("captured working file type changed")
		}
		in, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, st.Mode().Perm())
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e == nil {
			e = ce
		}
		return e
	})
}
