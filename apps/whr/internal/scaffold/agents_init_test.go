// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func agentsAction(t *testing.T, p *Proposal) Action {
	t.Helper()
	var found []Action
	for _, a := range p.Preview().Actions {
		if a.Path == "AGENTS.md" {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("AGENTS.md has %d actions, want exactly one", len(found))
	}
	return found[0]
}
func TestInitAgentsDefaultProposalAndWarningShape(t *testing.T) {
	s, err := Discover(context.Background(), fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := agentsAction(t, p); a.Action != "create" || a.Applied {
		t.Fatalf("default action: %+v", a)
	}
	if !bytes.Equal(p.Files()["AGENTS.md"], renderAgentsBlock()) {
		t.Fatal("default does not use shipped template")
	}
	raw, _ := json.Marshal(p.Preview())
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	warnings, ok := result["agents_block_warnings"].([]any)
	if !ok || len(warnings) != 0 {
		t.Fatalf("missing always-present warning array: %s", raw)
	}
	s.Entries["AGENTS.md"] = Entry{Kind: "file", Data: []byte("owner\r\n")}
	p, err = Build(s, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := agentsAction(t, p); a.Action != "left_unchanged" || a.Diff != "" {
		t.Fatalf("unrequested edit: %+v", a)
	}
	if _, ok := p.Fingerprints()["AGENTS.md"]; ok {
		t.Fatal("unchanged path must not be stale-rechecked")
	}
	raw, _ = json.Marshal(p.Preview())
	_ = json.Unmarshal(raw, &result)
	warnings, ok = result["agents_block_warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("expected one warning: %s", raw)
	}
	warning := warnings[0].(map[string]any)
	if warning["code"] != "agents_block.missing" || warning["path"] != "AGENTS.md" {
		t.Fatalf("warning: %v", warning)
	}
}
func TestInitAgentsApplyConsentAndStaleBeforeEffects(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "edited"}[edit], func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, "AGENTS.md")
			old := []byte("owner\r\n")
			if err := os.WriteFile(target, old, 0644); err != nil {
				t.Fatal(err)
			}
			o := Options{NonInteractive: true, Yes: true}
			if edit {
				o.Overwrite = []string{"AGENTS.md"}
			}
			s, err := DiscoverWithOptions(context.Background(), dir, o)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			expected := "left_unchanged"
			if edit {
				expected = "append_block"
			}
			if a := agentsAction(t, p); a.Action != expected {
				t.Fatalf("action: %+v", a)
			}
			if err := os.WriteFile(target, []byte("changed since preview\n"), 0644); err != nil {
				t.Fatal(err)
			}
			result, err := p.Apply(context.Background())
			if edit {
				if int(exitcode.FromError(err)) != 68 {
					t.Fatalf("stale edit accepted: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
					t.Fatal("stale edit had effects")
				}
			} else if err != nil {
				t.Fatalf("unchanged AGENTS rechecked: %v, %+v", err, result)
			}
		})
	}
}
func TestInitAgentsSkipNoReadAndRefusedKinds(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			dir := fixture(t)
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
			o := Options{DryRun: true, NoAgentsBlock: disabled}
			s, err := DiscoverWithOptions(context.Background(), dir, o)
			if !disabled {
				if int(exitcode.FromError(err)) != 68 {
					t.Fatalf("symlink not refused: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p, err := Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range p.Preview().Actions {
				if a.Path == "AGENTS.md" {
					t.Fatal("disabled block emitted action")
				}
			}
		})
	}
}
func TestInitAgentsPreviewMatchesWriteAndRerun(t *testing.T) {
	dir := fixture(t)
	target := filepath.Join(dir, "AGENTS.md")
	original := []byte("\xef\xbb\xbfowner\r\n")
	if err := os.WriteFile(target, original, 0644); err != nil {
		t.Fatal(err)
	}
	o := Options{NonInteractive: true, Yes: true, Overwrite: []string{"AGENTS.md"}}
	s, err := DiscoverWithOptions(context.Background(), dir, o)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(raw, p.Files()["AGENTS.md"]) {
		t.Fatalf("preview differs from disk: %v", err)
	}
	for _, a := range result.Actions {
		if a.Path == "AGENTS.md" && !a.Applied {
			t.Fatal("edit not reported applied")
		}
	}
	s, err = DiscoverWithOptions(context.Background(), dir, o)
	if err != nil {
		t.Fatal(err)
	}
	p, err = Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	if a := agentsAction(t, p); a.Action != "skip_existing" || a.Applied {
		t.Fatalf("rerun: %+v", a)
	}
	before, _ := os.Stat(dir)
	result, err = p.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(dir)
	if before.ModTime() != after.ModTime() {
		t.Fatal("no-op rerun created a journal")
	}
	for _, a := range result.Actions {
		if a.Applied {
			t.Fatalf("no-op applied %+v", a)
		}
	}
	// Current blocks also remain outside stale-preview checks when untouched.
	if err := os.WriteFile(target, []byte("changed current block after preview\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(context.Background()); err != nil {
		t.Fatalf("current block unnecessarily rechecked: %v", err)
	}
}

func TestInitAgentsSC022Table(t *testing.T) {
	current := renderAgentsBlock()
	older := fixtureAgentsBlock(agentsBlockVersion-1, "old\n")
	modified := bytes.Replace(current, []byte("## Agent workflow"), []byte("## Owner workflow"), 1)
	newer := fixtureAgentsBlock(agentsBlockVersion+1, "new\n")
	cases := []struct {
		name            string
		data            []byte
		action, warning string
		refused         bool
	}{
		{"absent", nil, "create", "", false},
		{"missing", []byte("owner\n"), "append_block", "missing", false},
		{"CRLF BOM missing", []byte("\xef\xbb\xbfowner\r\n"), "append_block", "missing", false},
		{"current", current, "skip_existing", "", false},
		{"lower intact", older, "replace_block", "different_template", false},
		{"same version foreign intact", fixtureAgentsBlock(agentsBlockVersion, "foreign workflow\n"), "replace_block", "different_template", false},
		{"modified", modified, "replace_block", "modified", false},
		{"higher intact", newer, "replace_block", "different_template", false},
		{"higher modified", bytes.Replace(newer, []byte("new\n"), []byte("edit\n"), 1), "replace_block", "modified", false},
		{"size malformed", bytes.Repeat([]byte("<!-- whr:agents-block end -->\n"), 3000), "", "too_large", true},
		{"result size", bytes.Repeat([]byte("x"), agentsBlockMaxBytes-1), "", "too_large", true},
		{"duplicate", append(copyBytes(current), current...), "", "malformed", true},
		{"unclosed", []byte(strings.Split(string(current), "\n")[0] + "\n"), "", "malformed", true},
		{"orphan", []byte("<!-- whr:agents-block end -->\n"), "", "malformed", true},
		{"nested", append([]byte(strings.Split(string(current), "\n")[0]+"\n"), current...), "", "malformed", true},
		{"CRLF markers", bytes.ReplaceAll(current, []byte("\n"), []byte("\r\n")), "", "malformed", true},
		{"indented", append([]byte("  "), current...), "", "malformed", true},
		{"BOM marker", append([]byte("\xef\xbb\xbf"), current...), "", "malformed", true},
		{"fenced marker", []byte("```\n\t<!-- whr:agents-block unknown -->\n```\n"), "", "malformed", true},
	}
	for _, tt := range cases {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edit=%v", tt.name, edit), func(t *testing.T) {
				dir := fixture(t)
				target := filepath.Join(dir, "AGENTS.md")
				if tt.data != nil {
					if err := os.WriteFile(target, tt.data, 0644); err != nil {
						t.Fatal(err)
					}
				}
				o := Options{NonInteractive: true, Yes: true}
				if edit {
					o.Overwrite = []string{"AGENTS.md"}
				}
				s, err := DiscoverWithOptions(context.Background(), dir, o)
				if err != nil {
					t.Fatal(err)
				}
				if len(tt.data) > agentsBlockMaxBytes && len(s.Entries["AGENTS.md"].Data) != 0 {
					t.Fatal("oversized file was read")
				}
				p, err := Build(s, o)
				if edit && tt.refused {
					if int(exitcode.FromError(err)) != 68 {
						t.Fatalf("refusal %v", err)
					}
					if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
						t.Fatal("refusal wrote files")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := tt.action
				warning := tt.warning
				if !edit && warning != "" {
					want = "left_unchanged"
				}
				if edit && want != "left_unchanged" {
					warning = ""
				}
				a := agentsAction(t, p)
				if a.Action != want || a.Applied {
					t.Fatalf("action %+v want %s", a, want)
				}
				warnings := p.Preview().AgentsBlockWarnings
				if warning == "" {
					if len(warnings) != 0 {
						t.Fatalf("unexpected warnings %v", warnings)
					}
				} else if len(warnings) != 1 || warnings[0].Code != "agents_block."+warning {
					t.Fatalf("warnings %v", warnings)
				}
				result, err := p.Apply(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				actual, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if want == "left_unchanged" || want == "skip_existing" {
					if !bytes.Equal(actual, tt.data) {
						t.Fatal("unchanged file changed")
					}
				} else if !bytes.Equal(actual, p.Files()["AGENTS.md"]) {
					t.Fatal("write differs from retained preview")
				}
				for _, a := range result.Actions {
					if a.Path == "AGENTS.md" && a.Applied != (want != "left_unchanged" && want != "skip_existing") {
						t.Fatalf("applied %+v", a)
					}
				}
			})
		}
	}
}

func TestInitAgentsTamperedRetainedOutputRefusedBeforeEffects(t *testing.T) {
	dir := fixture(t)
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{NonInteractive: true, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	p.files["AGENTS.md"] = []byte("<!-- whr:agents-block invalid -->\n")
	if _, err := p.Apply(context.Background()); int(exitcode.FromError(err)) != 68 {
		t.Fatalf("invalid retained output accepted: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("invalid retained marker wrote files")
	}
}

func TestInitAgentsReadOnlyUnrequestedFilePreserved(t *testing.T) {
	dir := fixture(t)
	target := filepath.Join(dir, "AGENTS.md")
	old := []byte("owner policy\r\n")
	if err := os.WriteFile(target, old, 0444); err != nil {
		t.Fatal(err)
	}
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{NonInteractive: true, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(raw, old) {
		t.Fatalf("read-only file changed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0444 {
		t.Fatalf("read-only mode changed: %v", err)
	}
}
