// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
	"strings"
	"testing"
)

func TestWizardClaudeSettingsOnlyFlagPreconfirms(t *testing.T) {
	for _, flag := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "preconfirmed"}[flag], func(t *testing.T) {
			s := existing()
			s.Entries[".claude"] = scaffold.Entry{Kind: "directory"}
			s.Entries[".claude/settings.json"] = scaffold.Entry{Kind: "file", Data: []byte("invalid json")}
			o := scaffold.Options{}
			if flag {
				o.Overwrite = []string{".claude/settings.json"}
			}
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, o, strings.NewReader("write\n"), &diag)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(diag.String(), "Modify .claude/settings.json?") {
				t.Fatal("settings prompted")
			}
			want := "left_unchanged"
			if flag {
				want = "overwrite"
			}
			found := 0
			for _, a := range p.Preview().Actions {
				if a.Path == ".claude/settings.json" {
					found++
					if a.Action != want || a.Applied {
						t.Fatalf("action=%+v", a)
					}
				}
			}
			if found != 1 {
				t.Fatalf("rows=%d", found)
			}
			steps := 0
			for _, step := range p.Preview().NextSteps {
				if strings.Contains(step, "attribution.sessionUrl: false") && strings.Contains(step, "--overwrite .claude/settings.json") && strings.Contains(step, "whole file") {
					steps++
				}
			}
			wantSteps := 1
			if flag {
				wantSteps = 0
				const authored = "{\n  \"attribution\": {\n    \"sessionUrl\": false\n  }\n}\n"
				if string(p.Files()[".claude/settings.json"]) != authored {
					t.Fatal("preconfirmed replacement did not retain exact authored bytes")
				}
			}
			if steps != wantSteps || !bytes.Equal(s.Entries[".claude/settings.json"].Data, []byte("invalid json")) {
				t.Fatal("wrong remediation count or modified input snapshot")
			}
		})
	}
}
func TestClaudeSettingsUnattendedOverwriteNeedsYes(t *testing.T) {
	s := existing()
	s.Entries[".claude/settings.json"] = scaffold.Entry{Kind: "file", Data: []byte("owner")}
	_, err := scaffold.Build(s, scaffold.Options{NonInteractive: true, Overwrite: []string{".claude/settings.json"}})
	var typed *exitcode.Error
	if !errors.As(err, &typed) || exitcode.FromError(err) != exitcode.NeedsHuman || typed.Path != exitcode.PathHumanDecisionMissing || typed.Message != "--non-interactive requires --yes (or --dry-run)" {
		t.Fatalf("wrong unattended consent refusal: %v", err)
	}
}

func TestWizardClaudeSettingsFlaggedSpecialRefusedBeforePrompts(t *testing.T) {
	s := fixture()
	s.Entries[".claude"] = scaffold.Entry{Kind: "special"}
	var diag bytes.Buffer
	_, err := wizard.Run(context.Background(), s, scaffold.Options{Overwrite: []string{".claude/settings.json"}}, strings.NewReader(""), &diag)
	var typed *exitcode.Error
	if !errors.As(err, &typed) || exitcode.FromError(err) != exitcode.Conflict || typed.Path != exitcode.PathConflict || typed.Message != "Claude settings target must be a regular file under a real .claude directory" {
		t.Fatalf("wrong flagged-special refusal: %v", err)
	}
	if strings.Contains(diag.String(), "Workspace name") {
		t.Fatal("refused edit prompted")
	}
}
