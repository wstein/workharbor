// SPDX-License-Identifier: EUPL-1.2
package dotfiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

type node struct {
	info Info
	data []byte
}
type fakeFS struct {
	nodes        map[string]node
	calls        []string
	seq          int
	fail         string
	failAlso     string
	failAfter    int
	afterRemove  bool
	beforeRead   func(string)
	afterRename  bool
	corrupt      string
	corruptReads int
	beforeRename func(string, string)
}

func newFS() *fakeFS {
	return &fakeFS{nodes: map[string]node{".": {info: Info{ID: "home", Mode: fs.ModeDir | 0700}}}}
}
func (f *fakeFS) put(p string, b []byte, m fs.FileMode) {
	f.seq++
	f.nodes[p] = node{Info{ID: fmt.Sprint(f.seq), Mode: m, Size: int64(len(b))}, bytes.Clone(b)}
}
func (f *fakeFS) call(op, p string) error {
	f.calls = append(f.calls, op+":"+p)
	if f.failAlso == op+":"+p {
		f.failAlso = ""
		return errors.New("injected sensitive details")
	}
	if f.fail == op+":"+p {
		if f.failAfter > 0 {
			f.failAfter--
			return nil
		}
		f.fail = ""
		return errors.New("injected sensitive details")
	}
	return nil
}
func (f *fakeFS) Stat(p string) (Info, error) {
	if err := f.call("stat", p); err != nil {
		return Info{}, err
	}
	n, ok := f.nodes[p]
	if !ok {
		return Info{}, fs.ErrNotExist
	}
	return n.info, nil
}
func (f *fakeFS) Read(p, id string, limit int64) ([]byte, error) {
	if f.beforeRead != nil {
		f.beforeRead(p)
	}
	if err := f.call("read", p); err != nil {
		return nil, err
	}
	n, ok := f.nodes[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if n.info.ID != id || !n.info.Mode.IsRegular() || int64(len(n.data)) > limit {
		return nil, fs.ErrPermission
	}
	b := bytes.Clone(n.data)
	if f.corrupt == p && len(b) > 0 {
		// Preserve length and metadata so only byte/hash integrity can refuse.
		b[0] ^= 1
		f.corruptReads++
	}
	return b, nil
}
func (f *fakeFS) Mkdir(p string, m fs.FileMode) error {
	if err := f.call("mkdir", p); err != nil {
		return err
	}
	if _, ok := f.nodes[p]; ok {
		return fs.ErrExist
	}
	f.put(p, nil, fs.ModeDir|m)
	return nil
}
func (f *fakeFS) Create(p string, b []byte, m fs.FileMode) (Info, error) {
	if err := f.call("create", p); err != nil {
		return Info{}, err
	}
	if _, ok := f.nodes[p]; ok {
		return Info{}, fs.ErrExist
	}
	f.put(p, b, m)
	return f.nodes[p].info, nil
}
func (f *fakeFS) Rename(from, to, fromID, toID string) error {
	if f.beforeRename != nil {
		f.beforeRename(from, to)
	}
	err := f.call("rename", to)
	if err != nil && !f.afterRename {
		return err
	}
	n, ok := f.nodes[from]
	if !ok || n.info.ID != fromID {
		return fs.ErrPermission
	}
	if old, ok := f.nodes[to]; (ok && old.info.ID != toID) || (!ok && toID != "") {
		return fs.ErrPermission
	}
	f.nodes[to] = n
	delete(f.nodes, from)
	return err
}
func (f *fakeFS) Remove(p, id string) error {
	err := f.call("remove", p)
	if err != nil && !f.afterRemove {
		return err
	}
	n, ok := f.nodes[p]
	if !ok {
		return fs.ErrNotExist
	}
	if n.info.ID != id {
		return fs.ErrPermission
	}
	delete(f.nodes, p)
	return err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }
func fixture(t *testing.T, old string) (*Engine, *fakeFS, catalogue.Config, map[string][]byte) {
	t.Helper()
	f := newFS()
	f.put(".config", nil, fs.ModeDir|0755)
	if old != "" {
		f.put(".config/starship.toml", []byte(old), 0644)
	}
	b := []byte("new = true\n")
	c := catalogue.Config{Marker: "dotfile", Target: ".config/starship.toml", ContentFile: "starship.toml", SHA256: digest(b), Mode: "0644"}
	return New(f, fixedClock{}), f, c, map[string][]byte{c.ContentFile: b}
}
func preview(t *testing.T, e *Engine, c catalogue.Config, m map[string][]byte) *Preview {
	t.Helper()
	p, err := e.Preview(c, m)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func apply(t *testing.T, e *Engine, p *Preview) Result {
	t.Helper()
	r, err := e.Apply(p, Options{Decision: Replace, Target: p.Target(), RunID: "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func writes(f *fakeFS) []string {
	var out []string
	for _, v := range f.calls {
		if !strings.HasPrefix(v, "stat:") && !strings.HasPrefix(v, "read:") {
			out = append(out, v)
		}
	}
	return out
}
func cloneTree(f *fakeFS) map[string]node {
	out := map[string]node{}
	for p, n := range f.nodes {
		n.data = bytes.Clone(n.data)
		out[p] = n
	}
	return out
}
func TestDotfileDiffBackupAtomic(t *testing.T) {
	e, f, c, m := fixture(t, "old = true\n")
	p := preview(t, e, c, m)
	if p.State() != Differs || p.Class() != "modify" || !strings.Contains(p.Diff(), "-old = true\n+new = true\n") || !strings.HasPrefix(p.Diff(), "--- ") {
		t.Fatalf("missing unified preview: %s", p.Diff())
	}
	r := apply(t, e, p)
	if r.Status != "applied" || r.Backup == nil || !strings.Contains(r.Restore, "manual") {
		t.Fatalf("result: %#v", r)
	}
	if string(f.nodes[c.Target].data) != "new = true\n" {
		t.Fatal("target bytes")
	}
	backup := strings.TrimPrefix(r.Backup.Path, "~/")
	if string(f.nodes[backup].data) != "old = true\n" || f.nodes[backup].info.Mode.Perm() != 0600 || f.nodes[c.Target].info.Mode.Perm() != 0644 {
		t.Fatal("backup bytes/modes")
	}
	for path, n := range f.nodes {
		if strings.HasPrefix(path, ".local/") && n.info.Mode.IsDir() && n.info.Mode.Perm() != 0700 {
			t.Fatal("directory mode", path)
		}
	}
	manifest := ".local/state/whr/backups/0123456789abcdef/manifest.json"
	var records []BackupRecord
	if err := json.Unmarshal(f.nodes[manifest].data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].SHA256 != digest([]byte("old = true\n")) || records[0].Original != "~/.config/starship.toml" || records[0].Mode != 0644 || records[0].Time != "2026-10-20T12:00:00Z" || f.nodes[manifest].info.Mode.Perm() != 0600 {
		t.Fatalf("manifest %+v", records)
	}
	order := strings.Join(f.calls, "\n")
	verify := strings.Index(order, "read:"+backup)
	replace := strings.LastIndex(order, "rename:"+c.Target)
	if verify < 0 || replace < verify {
		t.Fatal("backup unverified before replace")
	}
	for path := range f.nodes {
		if strings.Contains(path, ".whr-stage-") {
			t.Fatal("temp remains", path)
		}
	}
}
func TestDotfileBoundsAndCatalogueBinding(t *testing.T) {
	e, f, c, m := fixture(t, strings.Repeat("old\n", 20000))
	p := preview(t, e, c, m)
	if len(p.Diff()) > MaxDiffBytes || !strings.Contains(p.Diff(), "truncated") {
		t.Fatal("unbounded diff")
	}
	for _, target := range []string{"/tmp/x", "../x", ".config/../x", ".config//x", "", ".config/x/", ".config/./x", "~/.config/x"} {
		cc := c
		cc.Target = target
		if _, err := e.Preview(cc, m); err == nil {
			t.Fatal("unsafe target", target)
		}
	}
	cc := c
	cc.SHA256 = strings.Repeat("0", 64)
	if _, err := e.Preview(cc, m); err == nil {
		t.Fatal("bad hash")
	}
	cc = c
	cc.ContentFile = "missing"
	if _, err := e.Preview(cc, m); err == nil {
		t.Fatal("missing template")
	}
	cc = c
	cc.Mode = "0777"
	if _, err := e.Preview(cc, m); err == nil {
		t.Fatal("unsafe mode")
	}
	if len(writes(f)) != 0 {
		t.Fatal(writes(f))
	}
	e, _, c, m = fixture(t, "")
	p = preview(t, e, c, m)
	if !strings.Contains(p.Diff(), string(m[c.ContentFile])) {
		t.Fatal("new content incomplete")
	}
	m[c.ContentFile][0] = 'X'
	r, err := e.Apply(p, Options{Decision: Create, Target: c.Target, RunID: "r"})
	if err != nil || r.Status != "applied" {
		t.Fatal(r, err)
	}
	b := bytes.Repeat([]byte{'x'}, MaxContentBytes+1)
	m[c.ContentFile] = b
	c.SHA256 = digest(b)
	if _, err := e.Preview(c, m); err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestExistingDifferentNeedsConfirmation(t *testing.T) {
	for _, decision := range []Decision{"", Create, Skip, Replace} {
		e, f, c, m := fixture(t, "original\n")
		p := preview(t, e, c, m)
		r, err := e.Apply(p, Options{Decision: decision, Target: "another/path", RunID: "r"})
		if decision == Skip {
			if err != nil || r.Status != "declined" {
				t.Fatal(r, err)
			}
		} else if err == nil {
			t.Fatal("missing path consent accepted", decision)
		}
		if len(writes(f)) != 0 || string(f.nodes[c.Target].data) != "original\n" {
			t.Fatal("changed without consent")
		}
	}
	e, f, c, m := fixture(t, "new = true\n")
	r, err := e.Apply(preview(t, e, c, m), Options{})
	if err != nil || r.Status != "unchanged" || len(writes(f)) != 0 {
		t.Fatal(r, err, writes(f))
	}
}
func TestSymlinkTargetRefused(t *testing.T) {
	for _, mode := range []fs.FileMode{fs.ModeSymlink, fs.ModeDir, fs.ModeNamedPipe, fs.ModeSocket, fs.ModeDevice} {
		e, f, c, m := fixture(t, "")
		f.put(c.Target, nil, mode)
		if _, err := e.Preview(c, m); err == nil {
			t.Fatal("special accepted", mode)
		}
		if len(writes(f)) != 0 {
			t.Fatal(writes(f))
		}
	}
	for _, path := range []string{".", ".config", ".config/fish", ".config/fish/conf.d"} {
		e, f, c, m := fixture(t, "")
		c.Target = ".config/fish/conf.d/whr.fish"
		f.put(".config/fish", nil, fs.ModeDir|0700)
		f.put(".config/fish/conf.d", nil, fs.ModeDir|0700)
		f.put(path, nil, fs.ModeSymlink)
		if _, err := e.Preview(c, m); err == nil {
			t.Fatal("followed parent", path)
		}
	}
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	f.put(".local", nil, fs.ModeSymlink)
	_, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
	if err == nil || string(f.nodes[c.Target].data) != "original\n" {
		t.Fatal("backup symlink followed")
	}
}
func TestChangedSincePreviewRefused(t *testing.T) {
	for _, change := range []string{"bytes", "identity", "parent", "appeared", "config"} {
		old := "original\n"
		if change == "appeared" {
			old = ""
		}
		e, f, c, m := fixture(t, old)
		p := preview(t, e, c, m)
		switch change {
		case "bytes":
			n := f.nodes[c.Target]
			n.data = []byte("modified\n")
			if int64(len(n.data)) != n.info.Size {
				t.Fatal("stale fixture changed size")
			}
			f.nodes[c.Target] = n
		case "identity":
			f.put(c.Target, []byte(old), 0644)
		case "parent":
			f.put(".config", nil, fs.ModeDir|0755)
		case "appeared":
			f.put(c.Target, []byte("intruder\n"), 0644)
		case "config":
			c.Target = ".config/other"
		}
		d := Replace
		if change == "appeared" {
			d = Create
		}
		before := cloneTree(f)
		r, err := e.Apply(p, Options{Decision: d, Target: p.Target(), RunID: "r"})
		if change == "config" {
			if err != nil || r.Status != "applied" || string(f.nodes[".config/starship.toml"].data) != "new = true\n" {
				t.Fatal("config alias", r, err)
			}
			continue
		}
		if err == nil || len(writes(f)) != 0 || !reflect.DeepEqual(before, cloneTree(f)) {
			t.Fatal("stale accepted/written", change, err, writes(f))
		}
	}
}
func TestDryRunWritesNothing(t *testing.T) {
	for _, old := range []string{"", "original\n", "new = true\n"} {
		e, f, c, m := fixture(t, old)
		before := cloneTree(f)
		_, err := e.Apply(preview(t, e, c, m), Options{DryRun: true})
		if err != nil || !reflect.DeepEqual(before, f.nodes) || len(writes(f)) != 0 {
			t.Fatal(err, writes(f))
		}
	}
	e, f, c, m := fixture(t, "")
	delete(f.nodes, ".config")
	c.Target = ".config/fish/conf.d/whr.fish"
	p := preview(t, e, c, m)
	before := cloneTree(f)
	if _, err := e.Apply(p, Options{DryRun: true}); err != nil || !reflect.DeepEqual(before, f.nodes) {
		t.Fatal("dry run parents", err)
	}
	if _, err := e.Apply(p, Options{Decision: Create, Target: c.Target, RunID: "r"}); err != nil {
		t.Fatal(err)
	}
	if string(f.nodes[c.Target].data) != "new = true\n" || f.nodes[".config/fish/conf.d"].info.Mode.Perm() != 0700 {
		t.Fatal("safe parents missing")
	}
}
func TestDotfileFailuresPreserveOriginal(t *testing.T) {
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	f.calls = nil
	apply(t, e, p)
	calls := append([]string(nil), f.calls...)
	seen := map[string]int{}
	for _, op := range calls {
		occurrence := seen[op]
		seen[op]++
		t.Run(op, func(t *testing.T) {
			e, f, c, m := fixture(t, "original\n")
			p := preview(t, e, c, m)
			f.fail = op
			f.failAfter = occurrence
			r, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "0123456789abcdef"})
			if f.fail != "" {
				t.Fatal("fault not reached", op)
			}
			if err == nil && r.Status != "applied" {
				t.Fatal("failure hidden")
			}
			if err != nil && string(f.nodes[c.Target].data) != "original\n" {
				t.Fatal("original lost", r, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("error leaked")
			}
		})
	}
	for _, after := range []bool{false, true} {
		e, f, c, m := fixture(t, "original\n")
		p := preview(t, e, c, m)
		f.fail = "rename:" + c.Target
		f.afterRename = after
		r, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
		if err == nil || string(f.nodes[c.Target].data) != "original\n" || (after && !r.Restored) {
			t.Fatal("rename recovery", after, r, err)
		}
	}
}

func TestDotfileCorruptionAndRecoveryFailure(t *testing.T) {
	for _, which := range []string{"backup", "stage"} {
		e, f, c, m := fixture(t, "original\n")
		p := preview(t, e, c, m)
		if which == "backup" {
			f.corrupt = ".local/state/whr/backups/r/" + digest([]byte(c.Target))[:32] + ".bak"
		} else {
			f.corrupt = ".config/.whr-stage-" + digest([]byte(c.Target))[:16] + "-r"
		}
		r, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
		if f.corruptReads == 0 {
			t.Fatal("corruption fixture was not exercised", which)
		}
		for _, op := range writes(f) {
			if op == "rename:"+c.Target {
				t.Fatal("corruption reached target rename", which)
			}
		}
		if err == nil || string(f.nodes[c.Target].data) != "original\n" || r.Restored {
			t.Fatal("corruption accepted", which, r, err)
		}
	}
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	f.fail = "rename:" + c.Target
	f.afterRename = true
	f.failAlso = "create:.config/.whr-stage-" + digest([]byte(c.Target))[:16] + "-r-restore"
	r, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
	var failure *Error
	if !errors.As(err, &failure) || !failure.RecoveryFailed || r.Restored || r.Backup == nil || !strings.Contains(err.Error(), "manual restore") {
		t.Fatal("recovery failure hidden", r, err)
	}
	if string(f.nodes[strings.TrimPrefix(r.Backup.Path, "~/")].data) != "original\n" {
		t.Fatal("recovery backup lost")
	}
}
func TestDotfileManifestRetainsEachOriginal(t *testing.T) {
	e, f, c, m := fixture(t, "original\n")
	first := apply(t, e, preview(t, e, c, m))
	c.Target = ".config/other.toml"
	c.Mode = "0600"
	f.put(c.Target, []byte("second\n"), 0600)
	second := apply(t, e, preview(t, e, c, m))
	if first.Backup.Path == second.Backup.Path || f.nodes[c.Target].info.Mode.Perm() != 0600 {
		t.Fatal("backup collision or mode")
	}
	var records []BackupRecord
	b := f.nodes[".local/state/whr/backups/0123456789abcdef/manifest.json"].data
	if err := json.Unmarshal(b, &records); err != nil || len(records) != 2 || records[0].Original == records[1].Original {
		t.Fatal("manifest lost record", string(b), err)
	}
	if string(f.nodes[strings.TrimPrefix(first.Backup.Path, "~/")].data) != "original\n" || string(f.nodes[strings.TrimPrefix(second.Backup.Path, "~/")].data) != "second\n" {
		t.Fatal("backup overwritten")
	}
}
func TestNewFilePreviewContainsEveryLine(t *testing.T) {
	e, _, c, m := fixture(t, "")
	b := []byte(strings.Repeat("\n", 40000))
	m[c.ContentFile] = b
	c.SHA256 = digest(b)
	p, err := e.Preview(c, m)
	if err == nil && (strings.Contains(p.Diff(), "truncated") || strings.Count(p.Diff(), "+\n") != 40000) {
		t.Fatal("new file preview truncated instead of refused")
	}
}

func TestDotfileConsentAndConcurrentReplacement(t *testing.T) {
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	if _, err := e.Apply(p, Options{Decision: Create, Target: c.Target, RunID: "r"}); err == nil || len(writes(f)) != 0 {
		t.Fatal("create consent approved replacement")
	}
	f.beforeRename = func(_, to string) {
		if to == c.Target {
			f.put(c.Target, []byte("other writer\n"), 0644)
			f.beforeRename = nil
		}
	}
	r, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
	var failure *Error
	if !errors.As(err, &failure) || failure.RecoveryFailed || r.Restored || string(f.nodes[c.Target].data) != "other writer\n" {
		t.Fatal("foreign target overwritten or misreported", r, err)
	}
}

func TestFailedManifestStageDoesNotBlockNextFile(t *testing.T) {
	for _, fault := range []struct {
		op    string
		after int
	}{{"read", 0}, {"stat", 0}, {"stat", 1}} {
		t.Run(fmt.Sprintf("%s-%d", fault.op, fault.after), func(t *testing.T) {
			e, f, c, m := fixture(t, "original\n")
			p := preview(t, e, c, m)
			stage := ".local/state/whr/backups/review/manifest.json.whr-stage-review"
			f.fail = fault.op + ":" + stage
			f.failAfter = fault.after
			_, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "review"})
			if err == nil || f.fail != "" || string(f.nodes[c.Target].data) != "original\n" {
				t.Fatal("first failure missing", err)
			}
			if _, ok := f.nodes[stage]; ok {
				t.Fatal("owned failed stage retained")
			}
			c.Target = ".config/second.toml"
			f.put(c.Target, []byte("second original\n"), 0644)
			r, err := e.Apply(preview(t, e, c, m), Options{Decision: Replace, Target: c.Target, RunID: "review"})
			if err != nil || r.Status != "applied" {
				t.Fatal("independent target blocked", r, err)
			}
		})
	}
}
func TestFailedStageCleanupPreservesForeignObject(t *testing.T) {
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	stage := ".local/state/whr/backups/review/manifest.json.whr-stage-review"
	f.beforeRead = func(p string) {
		if p == stage {
			f.put(stage, []byte("foreign stage\n"), 0600)
			f.fail = "read:" + stage
			f.beforeRead = nil
		}
	}
	if _, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "review"}); err == nil {
		t.Fatal("foreign stage accepted")
	}
	if string(f.nodes[stage].data) != "foreign stage\n" || string(f.nodes[c.Target].data) != "original\n" {
		t.Fatal("cleanup deleted unowned object")
	}
}
func TestRollbackReconcilesAmbiguousMutations(t *testing.T) {
	for _, old := range []string{"original\n", ""} {
		e, f, c, m := fixture(t, old)
		p := preview(t, e, c, m)
		f.beforeRename = func(_, to string) {
			if to == c.Target {
				f.fail = "rename:" + to
				f.afterRename = true
			}
		}
		decision := Replace
		if old == "" {
			decision = Create
			f.failAlso = "remove:" + c.Target
			f.afterRemove = true
		}
		r, err := e.Apply(p, Options{Decision: decision, Target: c.Target, RunID: "review"})
		var failure *Error
		if !errors.As(err, &failure) || failure.RecoveryFailed || !r.Restored || r.Status != "rolled_back" {
			t.Fatal("verified rollback misreported", r, err)
		}
		n, ok := f.nodes[c.Target]
		if old == "" {
			if ok {
				t.Fatal("new target not removed")
			}
		} else if !ok || string(n.data) != old || n.info.Mode.Perm() != 0644 {
			t.Fatal("wrong restored bytes/mode")
		}
	}
}

func TestPreviewRefusesAnotherEngineWithSameFS(t *testing.T) {
	e, f, c, m := fixture(t, "original\n")
	p := preview(t, e, c, m)
	other := New(f, fixedClock{})
	before := cloneTree(f)
	_, err := other.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "r"})
	if err == nil || len(writes(f)) != 0 || !reflect.DeepEqual(before, cloneTree(f)) {
		t.Fatal("foreign-engine preview accepted or wrote", err, writes(f))
	}
}
