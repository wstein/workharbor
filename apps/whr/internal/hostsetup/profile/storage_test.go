// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package profile_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

var instant = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func start(id string) profile.Start {
	return profile.Start{RunID: id, Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "customize", Selected: []string{"jq"}, OptedOut: []string{"git-config", "fish"}}
}
func openStore(t *testing.T, home string) *profile.Store {
	t.Helper()
	s, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func begin(t *testing.T, s *profile.Store, id string) *profile.Run {
	t.Helper()
	r, err := s.Begin(start(id), instant)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func hostProfile() profile.HostProfile {
	return profile.HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: instant.Format(time.RFC3339Nano), Items: map[string]string{"jq": "selected", "fish": "opted_out", "git-config": "opted_out"}}
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func private(t *testing.T, p string, want fs.FileMode) {
	t.Helper()
	i, err := os.Lstat(p)
	if err != nil || i.Mode().Perm() != want {
		t.Fatalf("private mode: %s %v %v", p, i, err)
	}
}
func TestOptOutPersistsAndHonored(t *testing.T) {
	home := t.TempDir()
	s := openStore(t, home)
	if _, err := s.ReadProfile(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing profile misclassified", err)
	}
	r := begin(t, s, "0123456789abcdef")
	p := hostProfile()
	if changed, err := r.StoreProfile(p); !changed || err != nil {
		t.Fatal(changed, err)
	}
	got, err := s.ReadProfile()
	if err != nil || !reflect.DeepEqual(p, got) {
		t.Fatal(got, err)
	}
	selected, off := got.Selection()
	if selected["fish"] || selected["git-config"] || !off["git-config"] || !selected["jq"] {
		t.Fatal("opt-outs lost", selected, off)
	}
	path := filepath.Join(home, ".config/whr/host-profile.json")
	original := mustRead(t, path)
	info, _ := os.Stat(path)
	r.Close()
	r = begin(t, s, "1123456789abcdef")
	p.Updated = instant.Add(time.Hour).Format(time.RFC3339Nano)
	if changed, err := r.StoreProfile(p); changed || err != nil {
		t.Fatal("rerun changed profile", changed, err)
	}
	after, _ := os.Stat(path)
	if !bytes.Equal(original, mustRead(t, path)) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("profile no-op rewrote bytes/time")
	}
	private(t, path, 0600)
	private(t, filepath.Dir(path), 0700)
}
func TestProtocolContentModesAppendOnly(t *testing.T) {
	home := t.TempDir()
	s := openStore(t, home)
	r := begin(t, s, "0123456789abcdef")
	path := filepath.Join(home, ".local/state/whr/protocols/0123456789abcdef.jsonl")
	prefix := mustRead(t, path)
	zero := 0
	if err := r.Step(profile.StepAudit{ID: "jq.brew_install", Kind: "brew_install", Item: "jq", Argv: []string{"/opt/homebrew/bin/brew", "install", "jq"}, Network: true, ExitStatus: &zero, Result: "applied"}, instant.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Backup("~/.local/state/whr/backups/0123456789abcdef/config.bak", strings.Repeat("a", 64), instant.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Diff("fish.dotfile_replace", "fish", 3, 1, instant.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(0, []string{"jq.brew_install"}, nil, instant.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	full := mustRead(t, path)
	if !bytes.HasPrefix(full, prefix) {
		t.Fatal("prefix overwritten")
	}
	schema, _ := protocolContract(t)
	var rendered strings.Builder
	for _, line := range bytes.Split(bytes.TrimSpace(full), []byte{'\n'}) {
		var v map[string]any
		if err := json.Unmarshal(line, &v); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(v); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&rendered, "%s: %s\n", v["type"], line)
	}
	if rendered.String() != string(mustRead(t, strings.TrimSuffix(path, ".jsonl")+".txt")) {
		t.Fatal("text is not rendered JSON")
	}
	private(t, path, 0600)
	private(t, strings.TrimSuffix(path, ".jsonl")+".txt", 0600)
	private(t, filepath.Dir(path), 0700)
	private(t, filepath.Join(home, ".local/state/whr"), 0700)
	private(t, filepath.Join(home, ".local/state/whr/run.lock"), 0600)
	if _, err := s.Begin(start("0123456789abcdef"), instant); err == nil {
		t.Fatal("overwrote existing protocol")
	}
	if !bytes.Equal(full, mustRead(t, path)) {
		t.Fatal("collision modified protocol")
	}
	if err := r.Step(profile.StepAudit{}, instant); err == nil {
		t.Fatal("closed run accepted step")
	}
}
func TestProtocolRejectsSecretsWithoutLogging(t *testing.T) {
	home := t.TempDir()
	r := begin(t, openStore(t, home), "0123456789abcdef")
	path := filepath.Join(home, ".local/state/whr/protocols/0123456789abcdef.jsonl")
	prefix := mustRead(t, path)
	canaries := []string{"password-CANARY", "token-CANARY", "git-value-CANARY", "ENV=CANARY"}
	for _, secret := range canaries {
		a := profile.StepAudit{ID: "jq.brew_install", Kind: "brew_install", Item: "jq", Argv: []string{"/opt/homebrew/bin/brew", "install", secret}, Result: "failed"}
		if err := r.Step(a, instant); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("unsafe argv accepted/leaked")
		}
		a.Argv = []string{"/opt/homebrew/bin/brew", "install", "jq"}
		a.Result = secret
		if err := r.Step(a, instant); err == nil {
			t.Fatal("unsafe status accepted")
		}
		if err := r.Backup("~/"+secret, strings.Repeat("a", 64), instant); err == nil {
			t.Fatal("unsafe backup accepted")
		}
	}
	if !bytes.Equal(prefix, mustRead(t, path)) {
		t.Fatal("rejected metadata was logged")
	}
}
func TestNoAndReadOnlyModesWriteNothing(t *testing.T) {
	home := t.TempDir()
	s := openStore(t, home)
	for _, change := range []func(*profile.Start){func(s *profile.Start) { s.Answer = "no" }, func(s *profile.Start) { s.Command = "doctor" }, func(s *profile.Start) { s.Command = "dry-run" }} {
		v := start("0123456789abcdef")
		change(&v)
		if _, err := s.Begin(v, instant); err == nil {
			t.Fatal("unconfirmed/read-only run accepted")
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only request wrote", entries, err)
	}
}
func TestProtocolUnwritableBlocksProfileMutation(t *testing.T) {
	for _, scenario := range []string{"directory_symlink", "protocol_symlink", "protocol_collision", "text_collision", "lock_symlink", "lock_fifo", "state_mode", "protocol_mode"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			outside := t.TempDir()
			dir := filepath.Join(home, ".local/state/whr/protocols")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "directory_symlink":
				os.Remove(dir)
				os.Symlink(outside, dir)
			case "protocol_symlink":
				os.Symlink(filepath.Join(outside, "canary"), filepath.Join(dir, "0123456789abcdef.jsonl"))
			case "protocol_collision":
				os.WriteFile(filepath.Join(dir, "0123456789abcdef.jsonl"), []byte("CANARY"), 0600)
			case "text_collision":
				os.WriteFile(filepath.Join(dir, "0123456789abcdef.txt"), []byte("CANARY"), 0600)
			case "lock_symlink":
				os.Symlink(filepath.Join(outside, "canary"), filepath.Join(filepath.Dir(dir), "run.lock"))
			case "lock_fifo":
				makeFIFO(t, filepath.Join(filepath.Dir(dir), "run.lock"))
			case "state_mode":
				os.Chmod(filepath.Dir(dir), 0755)
			case "protocol_mode":
				os.Chmod(dir, 0755)
			}
			s := openStore(t, home)
			if r, err := s.Begin(start("0123456789abcdef"), instant); err == nil {
				r.Close()
				t.Fatal("unsafe storage accepted")
			}
			if _, err := os.Stat(filepath.Join(home, ".config")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("managed profile mutated")
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 0 {
				t.Fatal("followed symlink outside root")
			}
		})
	}
}
func TestProfileRejectsCurrentSchemaViolations(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":2,"catalogue_version":"2026.10.1","updated":"2026-10-20T12:00:00Z","items":{}}`,
		`{"schema_version":1,"catalogue_version":"2026.10.1","updated":"2026-10-20T12:00:00Z","items":{"git-config":"secret"}}`,
		`{"schema_version":1,"catalogue_version":"2026.10.1","updated":"2026-10-20T12:00:00Z","items":{},"old_version":true}`,
	} {
		home := t.TempDir()
		os.MkdirAll(filepath.Join(home, ".config/whr"), 0700)
		os.WriteFile(filepath.Join(home, ".config/whr/host-profile.json"), []byte(raw), 0600)
		s := openStore(t, home)
		if _, err := s.ReadProfile(); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}
func TestProtocolWarningAfter500(t *testing.T) {
	for _, count := range []int{499, 500} {
		home := t.TempDir()
		dir := filepath.Join(home, ".local/state/whr/protocols")
		os.MkdirAll(dir, 0700)
		for i := 0; i < count; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%016x.jsonl", i)), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		r := begin(t, openStore(t, home), "0123456789abcdef")
		if r.Warning != (count == 500) {
			t.Fatal("warning threshold", count, r.Warning)
		}
	}
}
