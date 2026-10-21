// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitClaudeSettingsExactDefault(t *testing.T) {
	s, err := Discover(context.Background(), fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = "{\n  \"attribution\": {\n    \"sessionUrl\": false\n  }\n}\n"
	if got := string(p.Files()[".claude/settings.json"]); got != want {
		t.Fatalf("settings bytes = %q; want %q", got, want)
	}
}

func claudeAction(t *testing.T, p *Proposal) Action {
	t.Helper()
	var found []Action
	for _, a := range p.Preview().Actions {
		if a.Path == claudeSettingsPath {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("settings actions=%v", found)
	}
	return found[0]
}

func TestInitClaudeSettingsCases(t *testing.T) {
	for _, tc := range []struct {
		name, data, action  string
		existing, overwrite bool
	}{
		{name: "absent", action: "create"},
		{name: "identical", data: claudeSettings, action: "skip_existing", existing: true},
		{name: "different", data: `{"owner": true}`, action: "left_unchanged", existing: true},
		{name: "invalid_json", data: "not json\n", action: "left_unchanged", existing: true},
		{name: "oversized", data: strings.Repeat("x", 2<<20), action: "left_unchanged", existing: true},
		{name: "overwrite", data: `{"owner": true}`, action: "overwrite", existing: true, overwrite: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, claudeSettingsPath)
			if tc.existing {
				if err := os.Mkdir(filepath.Join(dir, ".claude"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(tc.data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			o := Options{NonInteractive: true, Yes: true}
			if tc.overwrite {
				o.Overwrite = []string{claudeSettingsPath}
			}
			s, err := DiscoverWithOptions(context.Background(), dir, o)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(s.Entries[claudeSettingsPath].Data); got > len(claudeSettings)+1 {
				t.Fatalf("unbounded discovery: %d", got)
			}
			p, err := Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			if a := claudeAction(t, p); a.Action != tc.action || a.Applied {
				t.Fatalf("action=%+v", a)
			}
			if tc.action == "left_unchanged" {
				if _, ok := p.Fingerprints()[claudeSettingsPath]; ok {
					t.Fatal("unchanged file must not be rechecked")
				}
				count := 0
				for _, step := range p.Preview().NextSteps {
					if strings.Contains(step, "attribution.sessionUrl: false") && strings.Contains(step, "--overwrite .claude/settings.json") && strings.Contains(step, "whole file") {
						count++
					}
				}
				if count != 1 || !strings.Contains(p.HumanPreview(), "left_unchanged file .claude/settings.json\n") {
					t.Fatal("missing remediation/output")
				}
			}
			result, err := p.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := claudeSettings
			if tc.action == "left_unchanged" {
				want = tc.data
			}
			if string(got) != want {
				t.Fatal("wrong written/preserved bytes")
			}
			info, _ := os.Stat(target)
			if info.Mode().Perm() != 0644 {
				t.Fatalf("mode=%v", info.Mode())
			}
			for _, a := range result.Actions {
				if a.Path == claudeSettingsPath && a.Applied != (tc.action == "create" || tc.action == "overwrite") {
					t.Fatalf("applied=%+v", a)
				}
			}
			s, err = Discover(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			p, err = Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			result, err = p.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range result.Actions {
				if a.Applied {
					t.Fatalf("rerun mutated %+v", a)
				}
			}
		})
	}
}

func TestInitClaudeSettingsDryRunAndStale(t *testing.T) {
	for _, mode := range []string{"dry_run", "appeared", "appeared_overwrite", "exclusive_create", "exclusive_create_overwrite", "overwrite_changed", "unchanged_changed", "identical_changed"} {
		t.Run(mode, func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, claudeSettingsPath)
			if err := os.Mkdir(filepath.Join(dir, ".claude"), 0755); err != nil {
				t.Fatal(err)
			}
			o := Options{NonInteractive: true, Yes: true, DryRun: mode == "dry_run"}
			if mode == "overwrite_changed" || mode == "unchanged_changed" || mode == "identical_changed" {
				data := "original\n"
				if mode == "identical_changed" {
					data = claudeSettings
				}
				if err := os.WriteFile(target, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "overwrite_changed" || strings.HasSuffix(mode, "_overwrite") {
				o.Overwrite = []string{claudeSettingsPath}
			}
			s, err := Discover(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "dry_run" && !strings.HasPrefix(mode, "exclusive_create") {
				if err := os.WriteFile(target, []byte("concurrent\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var hook func(string) error
			if strings.HasPrefix(mode, "exclusive_create") {
				hook = func(event string) error {
					if event == "create:"+claudeSettingsPath {
						return os.WriteFile(target, []byte("concurrent\n"), 0644)
					}
					return nil
				}
			}
			_, err = p.apply(context.Background(), hook)
			if mode == "dry_run" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("dry run wrote")
				}
			} else if mode == "unchanged_changed" {
				if err != nil {
					t.Fatalf("unchanged rechecked: %v", err)
				}
			} else if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("stale accepted: %v", err)
			}
		})
	}
}

func TestInitClaudeSettingsLargeOverwriteAndRollback(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[rollback], func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, claudeSettingsPath)
			if err := os.Mkdir(filepath.Join(dir, ".claude"), 0755); err != nil {
				t.Fatal(err)
			}
			old := []byte(strings.Repeat("owner-settings\n", 160000))
			if err := os.WriteFile(target, old, 0640); err != nil {
				t.Fatal(err)
			}
			o := Options{NonInteractive: true, Yes: true, Overwrite: []string{claudeSettingsPath}}
			s, err := DiscoverWithOptions(context.Background(), dir, o)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			var hook func(string) error
			if rollback {
				hook = func(event string) error {
					if event == "after:"+claudeSettingsPath {
						return context.Canceled
					}
					return nil
				}
			}
			_, err = p.apply(context.Background(), hook)
			if rollback {
				if int(exitcode.FromError(err)) != 72 {
					t.Fatalf("rollback=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := []byte(claudeSettings)
			if rollback {
				want = old
			}
			if !bytes.Equal(got, want) {
				t.Fatal("wrong overwritten/restored bytes")
			}
			info, _ := os.Stat(target)
			if info.Mode().Perm() != 0640 {
				t.Fatalf("mode=%v", info.Mode())
			}
			matches, _ := filepath.Glob(filepath.Join(dir, ".claude/.whr-init-tmp-*"))
			if len(matches) != 0 {
				t.Fatalf("leftover backups=%v", matches)
			}
		})
	}
}

func TestInitClaudeSettingsTailChangeWithRestoredMtime(t *testing.T) {
	dir := fixture(t)
	target := filepath.Join(dir, claudeSettingsPath)
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	old := []byte(strings.Repeat("x", 512))
	if err := os.WriteFile(target, old, 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(target)
	o := Options{NonInteractive: true, Yes: true, Overwrite: []string{claudeSettingsPath}}
	s, err := DiscoverWithOptions(context.Background(), dir, o)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	old[len(old)-1] = 'y'
	if err := os.WriteFile(target, old, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = p.Apply(context.Background())
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("tail mutation accepted: %v", err)
	}
}
