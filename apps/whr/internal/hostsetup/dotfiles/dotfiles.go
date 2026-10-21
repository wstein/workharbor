// SPDX-License-Identifier: EUPL-1.2

// Package dotfiles implements the offline preview/backup/replace engine over a
// rooted no-follow filesystem port. It has no native adapter and never discovers
// HOME. A real adapter and its race/symlink acceptance remain separate work.
package dotfiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
)

const (
	MaxContentBytes  = 1024 * 1024
	MaxDiffBytes     = 64 * 1024
	maxManifestBytes = 1024 * 1024
)

// Info identifies one inode/object, never a path alias. ID must remain stable
// across a rename and must change when a different object occupies the path.
type Info struct {
	ID   string
	Mode fs.FileMode
	Size int64
}

// FS is rooted at an explicitly supplied home, never the process HOME. All
// paths are slash-separated root-relative paths ("." denotes the root).
// Stat reports no-follow metadata, including link/special-file kinds. All other
// operations MUST reject links and special files in any component, hold parent
// identities during the operation, and never escape the root. Read binds
// a regular file's ID and bounds its bytes. Create and Mkdir are exclusive, with
// exact modes independent of umask. Create returns the identity held during
// exclusive creation, including on an ambiguous error when ownership is known;
// it never returns an identity for a pre-existing object. Rename atomically moves a regular staged
// file only if both identities match; empty toID means destination must be absent.
// Remove only removes the regular file with the given ID, never a backup.
// Errors may be ambiguous after a mutation; Apply inspects the target to recover.
// These are adapter obligations, not guarantees proved by fake-FS tests.
type FS interface {
	Stat(string) (Info, error)
	Read(string, string, int64) ([]byte, error)
	Mkdir(string, fs.FileMode) error
	Create(string, []byte, fs.FileMode) (Info, error)
	Rename(string, string, string, string) error
	Remove(string, string) error
}
type Clock interface{ Now() time.Time }
type State string

const (
	Absent    State = "absent"
	Identical State = "identical"
	Differs   State = "differs"
)

type Decision string

const (
	Create  Decision = "create"
	Replace Decision = "replace"
	Skip    Decision = "skip"
)

// Options carries consent for exactly the previewed target; create consent never
// approves replacement. RunID comes from the run owner; this engine owns no lock.
type Options struct {
	Decision      Decision
	Target, RunID string
	DryRun        bool
}
type BackupRecord struct {
	Path     string      `json:"path"`
	Original string      `json:"original"`
	SHA256   string      `json:"sha256"`
	Reason   string      `json:"reason"`
	Time     string      `json:"time"`
	Size     int64       `json:"size"`
	Mode     fs.FileMode `json:"mode"`
}
type Result struct {
	Status, Restore string
	Backup          *BackupRecord
	Restored        bool
}

// Error deliberately excludes raw filesystem errors, paths and file contents.
// Command/result integration, including AP-6 placement, is not implemented here.
type Error struct {
	Reason         string
	RecoveryFailed bool
}

func (e *Error) Error() string {
	if e.RecoveryFailed {
		return e.Reason + ": recovery incomplete; retain backup for manual restore"
	}
	return e.Reason
}
func refusal(reason string) error { return &Error{Reason: "host_setup." + reason} }

type snapshot struct {
	info    Info
	hash    string
	parents map[string]Info
	data    []byte
}

// Preview is immutable to callers and bound to its creating engine. Observation
// methods expose values for plan conversion without importing the plan package.
type Preview struct {
	owner   *Engine
	config  catalogue.Config
	content []byte
	before  snapshot
	state   State
	diff    string
}

func (p *Preview) Target() string       { return p.config.Target }
func (p *Preview) State() State         { return p.state }
func (p *Preview) BeforeSHA256() string { return p.before.hash }
func (p *Preview) Class() string {
	switch p.state {
	case Absent:
		return "create"
	case Differs:
		return "modify"
	default:
		return "read_only"
	}
}
func (p *Preview) Diff() string { return p.diff }

type Engine struct {
	fs    FS
	clock Clock
}

func New(f FS, c Clock) *Engine { return &Engine{fs: f, clock: c} }

func safeTarget(s string) bool {
	if !strings.HasPrefix(s, ".config/") || len(s) > 128 || strings.Contains(s, "..") || strings.Contains(s, "//") || strings.HasSuffix(s, "/") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
				return false
			}
		}
	}
	return true
}

var runPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func same(a, b Info) bool    { return a.ID == b.ID && a.Mode == b.Mode && a.Size == b.Size }
func dirs(target string) []string {
	out := []string{"."}
	parts := strings.Split(path.Dir(target), "/")
	cur := ""
	for _, part := range parts {
		if part == "." {
			continue
		}
		if cur != "" {
			cur += "/"
		}
		cur += part
		out = append(out, cur)
	}
	return out
}

// parents inspects every component without creating it, unless explicitly asked
// during confirmed apply. Missing components are retained in preview identity.
func (e *Engine) parents(target string, create bool) (map[string]Info, error) {
	out := map[string]Info{}
	missing := false
	for _, p := range dirs(target) {
		info, err := e.fs.Stat(p)
		if errors.Is(err, fs.ErrNotExist) && p != "." {
			if !create {
				missing = true
				out[p] = Info{}
				continue
			}
			if err = e.fs.Mkdir(p, 0700); err != nil {
				return nil, refusal("target_unsafe")
			}
			info, err = e.fs.Stat(p)
		}
		if err != nil || missing || !info.Mode.IsDir() || info.Mode&fs.ModeSymlink != 0 || info.ID == "" {
			return nil, refusal("target_unsafe")
		}
		out[p] = info
	}
	return out, nil
}
func parentsEqual(a, b map[string]Info) bool {
	if len(a) != len(b) {
		return false
	}
	for p, x := range a {
		y, ok := b[p]
		if !ok || x.ID != y.ID || x.Mode != y.Mode {
			return false
		}
	}
	return true
}
func (e *Engine) read(target string, limit int64) (Info, []byte, error) {
	info, err := e.fs.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, nil, nil
	}
	if err != nil || info.ID == "" || !info.Mode.IsRegular() || info.Size < 0 || info.Size > limit {
		return Info{}, nil, refusal("target_unsafe")
	}
	b, err := e.fs.Read(target, info.ID, limit)
	if err != nil || int64(len(b)) != info.Size {
		return Info{}, nil, refusal("target_unsafe")
	}
	now, err := e.fs.Stat(target)
	if err != nil || !same(info, now) {
		return Info{}, nil, refusal("changed_since_preview")
	}
	return info, b, nil
}
func (e *Engine) snapshot(target string) (snapshot, error) {
	parents, err := e.parents(target, false)
	if err != nil {
		return snapshot{}, err
	}
	info, b, err := e.read(target, MaxContentBytes)
	if err != nil {
		return snapshot{}, err
	}
	after, err := e.parents(target, false)
	if err != nil || !parentsEqual(parents, after) {
		return snapshot{}, refusal("changed_since_preview")
	}
	hash := ""
	if info.ID != "" {
		hash = digest(b)
	}
	return snapshot{info: info, hash: hash, parents: parents, data: bytes.Clone(b)}, nil
}
func (e *Engine) Preview(c catalogue.Config, templates map[string][]byte) (*Preview, error) {
	if e.fs == nil || e.clock == nil {
		return nil, refusal("catalogue_invalid")
	}
	b, ok := templates[c.ContentFile]
	if !safeTarget(c.Target) || !ok || c.ContentFile == "" || (c.Marker != "dotfile" && c.Marker != "shell_hook") || (c.Mode != "0600" && c.Mode != "0644") || len(b) > MaxDiffBytes-1024 || digest(b) != c.SHA256 {
		return nil, refusal("catalogue_invalid")
	}
	s, err := e.snapshot(c.Target)
	if err != nil {
		return nil, err
	}
	state := Differs
	if s.info.ID == "" {
		state = Absent
	} else if bytes.Equal(s.data, b) {
		state = Identical
	}
	p := &Preview{owner: e, config: c, content: bytes.Clone(b), before: s, state: state}
	if state != Identical {
		var truncated bool
		p.diff, truncated = unified(c.Target, s.data, b, state == Absent)
		if state == Absent && truncated {
			return nil, refusal("target_unsafe")
		}
	}
	return p, nil
}

// A single full-file hunk is a valid unified diff. It avoids quadratic diff work
// on large input. Existing-file output is bounded; new-file contents are complete.
func unified(target string, old, new []byte, absent bool) (string, bool) {
	var b strings.Builder
	from := "~/" + target
	if absent {
		from = "/dev/null"
	}
	fmt.Fprintf(&b, "--- %s\n+++ ~/%s\n@@ -%d,%d +%d,%d @@\n", from, target, start(old), lines(old), start(new), lines(new))
	truncated := false
	for _, group := range []struct {
		prefix byte
		data   []byte
	}{{'-', old}, {'+', new}} {
		for _, line := range bytes.SplitAfter(group.data, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			extra := 2
			if line[len(line)-1] != '\n' {
				extra += len("\\ No newline at end of file\n")
			}
			if b.Len()+len(line)+extra > MaxDiffBytes-64 {
				truncated = true
				break
			}
			b.WriteByte(group.prefix)
			b.Write(line)
			if line[len(line)-1] != '\n' {
				b.WriteString("\n\\ No newline at end of file\n")
			}
		}
		if truncated {
			break
		}
	}
	if truncated {
		b.WriteString("[diff truncated]\n")
	}
	return b.String(), truncated
}
func start(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return 1
}
func lines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
func (e *Engine) check(p *Preview, parents map[string]Info) error {
	s, err := e.snapshot(p.config.Target)
	if err != nil {
		return err
	}
	if !same(s.info, p.before.info) || s.hash != p.before.hash || !parentsEqual(s.parents, parents) {
		return refusal("changed_since_preview")
	}
	return nil
}
func (e *Engine) Apply(p *Preview, o Options) (Result, error) {
	r := Result{}
	if p == nil || p.owner != e || e.fs == nil || e.clock == nil {
		return r, refusal("catalogue_invalid")
	}
	if err := e.check(p, p.before.parents); err != nil {
		return r, err
	}
	if o.DryRun {
		r.Status = "previewed"
		return r, nil
	}
	if p.state == Identical {
		r.Status = "unchanged"
		return r, nil
	}
	if o.Decision == Skip {
		r.Status = "declined"
		return r, nil
	}
	expected := Create
	if p.state == Differs {
		expected = Replace
	}
	if o.Decision != expected || o.Target != p.config.Target {
		return r, refusal("confirmation_missing")
	}
	if !runPattern.MatchString(o.RunID) {
		return r, refusal("target_unsafe")
	}
	if p.state == Differs {
		record, err := e.backup(p, o.RunID)
		if err != nil {
			return r, refusal("backup_failed")
		}
		r.Backup = &record
		r.Restore = "Restore by manual copy from " + record.Path
	}
	parents, err := e.parents(p.config.Target, true)
	if err != nil {
		return r, err
	}
	// Existing parent identities may not be exchanged while absent parents are made.
	for name, old := range p.before.parents {
		if old.ID != "" {
			now := parents[name]
			if old.ID != now.ID || old.Mode != now.Mode {
				return r, refusal("changed_since_preview")
			}
		}
	}
	if err = e.check(p, parents); err != nil {
		return r, err
	}
	mode := fs.FileMode(0600)
	if p.config.Mode == "0644" {
		mode = 0644
	}
	stage := path.Join(path.Dir(p.config.Target), ".whr-stage-"+digest([]byte(p.config.Target))[:16]+"-"+o.RunID)
	stageInfo, err := e.stage(stage, p.content, mode)
	if err != nil {
		return r, err
	}
	defer e.clean(stage, stageInfo.ID)
	// Re-hash immediately before the sole atomic replacement.
	if err = e.check(p, parents); err != nil {
		return r, err
	}
	err = e.fs.Rename(stage, p.config.Target, stageInfo.ID, p.before.info.ID)
	if err == nil {
		now, b, readErr := e.read(p.config.Target, MaxContentBytes)
		if readErr == nil && now.ID == stageInfo.ID && now.Mode.Perm() == mode && digest(b) == p.config.SHA256 {
			r.Status = "applied"
			return r, nil
		}
	}
	// A rename can have succeeded before returning an error. Restore only our
	// staged object, never a different object installed by another writer.
	now, statErr := e.fs.Stat(p.config.Target)
	if statErr == nil && same(now, p.before.info) {
		r.Status = "failed"
		return r, refusal("target_unsafe")
	}
	if errors.Is(statErr, fs.ErrNotExist) && p.before.info.ID == "" {
		r.Status = "failed"
		return r, refusal("target_unsafe")
	}
	// An intact source proves a refused rename made no target mutation.
	// A different writer owns the new target; do not attempt to restore over it.
	if err != nil {
		intact, inspectErr := e.fs.Stat(stage)
		if inspectErr == nil && same(intact, stageInfo) {
			r.Status = "failed"
			return r, refusal("changed_since_preview")
		}
	}
	if statErr != nil || now.ID != stageInfo.ID {
		r.Status = "failed"
		return r, &Error{Reason: "host_setup.target_unsafe", RecoveryFailed: true}
	}
	if p.before.info.ID == "" {
		_ = e.fs.Remove(p.config.Target, stageInfo.ID)
		// Removal can complete before reporting an error; verify the absence.
		if _, verifyErr := e.fs.Stat(p.config.Target); errors.Is(verifyErr, fs.ErrNotExist) {
			r.Restored = true
			r.Status = "rolled_back"
			return r, refusal("target_unsafe")
		}
	} else if e.restore(p, r.Backup, stageInfo.ID, stage+"-restore") == nil {
		r.Restored = true
		r.Status = "rolled_back"
		return r, refusal("target_unsafe")
	}
	r.Status = "failed"
	return r, &Error{Reason: "host_setup.target_unsafe", RecoveryFailed: true}
}
func (e *Engine) clean(p, id string) {
	if id != "" {
		_ = e.fs.Remove(p, id)
	}
}
func (e *Engine) stage(p string, b []byte, mode fs.FileMode) (Info, error) {
	created, err := e.fs.Create(p, b, mode)
	if err != nil {
		e.clean(p, created.ID)
		return Info{}, refusal("target_unsafe")
	}
	if created.ID == "" || !created.Mode.IsRegular() {
		return Info{}, refusal("target_unsafe")
	}
	info, read, err := e.read(p, MaxContentBytes)
	if err != nil || !same(created, info) || !bytes.Equal(b, read) || info.Mode.Perm() != mode {
		e.clean(p, created.ID)
		return Info{}, refusal("target_unsafe")
	}
	return info, nil
}
func (e *Engine) backup(p *Preview, run string) (BackupRecord, error) {
	dir := ".local/state/whr/backups/" + run
	filename := digest([]byte(p.config.Target))[:32] + ".bak"
	backup := dir + "/" + filename
	if _, err := e.parents(backup, true); err != nil {
		return BackupRecord{}, err
	}
	for _, private := range []string{".local/state/whr", ".local/state/whr/backups", dir} {
		info, err := e.fs.Stat(private)
		if err != nil || info.Mode.Perm() != 0700 {
			return BackupRecord{}, refusal("backup_failed")
		}
	}
	if err := e.check(p, p.before.parents); err != nil {
		return BackupRecord{}, err
	}
	created, err := e.fs.Create(backup, p.before.data, 0600)
	if err != nil {
		return BackupRecord{}, err
	}
	info, b, err := e.read(backup, MaxContentBytes)
	if err != nil || created.ID == "" || !same(created, info) || info.Mode.Perm() != 0600 || digest(b) != p.before.hash {
		return BackupRecord{}, refusal("backup_failed")
	}
	record := BackupRecord{Path: "~/" + backup, Original: "~/" + p.config.Target, SHA256: p.before.hash, Size: int64(len(b)), Mode: p.before.info.Mode.Perm(), Reason: "dotfile_replace", Time: e.clock.Now().UTC().Format(time.RFC3339Nano)}
	manifest := dir + "/manifest.json"
	old, data, err := e.read(manifest, maxManifestBytes)
	if err != nil {
		return BackupRecord{}, err
	}
	var records []BackupRecord
	if old.ID != "" {
		if old.Mode.Perm() != 0600 || json.Unmarshal(data, &records) != nil {
			return BackupRecord{}, refusal("backup_failed")
		}
	}
	records = append(records, record)
	data, err = json.Marshal(records)
	if err != nil || len(data) > maxManifestBytes {
		return BackupRecord{}, refusal("backup_failed")
	}
	staged := manifest + ".whr-stage-" + run
	si, err := e.stage(staged, data, 0600)
	if err != nil {
		return BackupRecord{}, err
	}
	defer e.clean(staged, si.ID)
	if err = e.fs.Rename(staged, manifest, si.ID, old.ID); err != nil {
		return BackupRecord{}, err
	}
	mi, actual, err := e.read(manifest, maxManifestBytes)
	if err != nil || mi.ID != si.ID || mi.Mode.Perm() != 0600 || !bytes.Equal(actual, data) {
		return BackupRecord{}, refusal("backup_failed")
	}
	return record, nil
}
func (e *Engine) restore(p *Preview, record *BackupRecord, targetID, staged string) error {
	if record == nil {
		return refusal("backup_failed")
	}
	backup := strings.TrimPrefix(record.Path, "~/")
	if _, err := e.parents(backup, false); err != nil {
		return err
	}
	info, b, err := e.read(backup, MaxContentBytes)
	if err != nil || info.Mode.Perm() != 0600 || digest(b) != record.SHA256 {
		return refusal("backup_failed")
	}
	if _, err = e.parents(p.config.Target, false); err != nil {
		return err
	}
	si, err := e.stage(staged, b, p.before.info.Mode.Perm())
	if err != nil {
		return err
	}
	defer e.clean(staged, si.ID)
	// Reconcile an ambiguous rename by checking the destination even on error.
	// Only our staged identity with the original hash and mode proves recovery.
	_ = e.fs.Rename(staged, p.config.Target, si.ID, targetID)
	now, actual, err := e.read(p.config.Target, MaxContentBytes)
	if err != nil || now.ID != si.ID || digest(actual) != p.before.hash || now.Mode.Perm() != p.before.info.Mode.Perm() {
		return refusal("target_unsafe")
	}
	return nil
}
