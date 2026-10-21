// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
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
					if a.Action != want {
						t.Fatalf("action=%+v", a)
					}
				}
			}
			if found != 1 {
				t.Fatalf("rows=%d", found)
			}
		})
	}
}
func TestClaudeSettingsUnattendedOverwriteNeedsYes(t *testing.T) {
	s := existing()
	s.Entries[".claude/settings.json"] = scaffold.Entry{Kind: "file", Data: []byte("owner")}
	_, err := scaffold.Build(s, scaffold.Options{NonInteractive: true, Overwrite: []string{".claude/settings.json"}})
	if int(exitcode.FromError(err)) != 70 {
		t.Fatalf("unattended consent=%v", err)
	}
}

func TestWizardClaudeSettingsFlaggedSpecialRefusedBeforePrompts(t *testing.T) {
	s := fixture()
	s.Entries[".claude"] = scaffold.Entry{Kind: "special"}
	var diag bytes.Buffer
	_, err := wizard.Run(context.Background(), s, scaffold.Options{Overwrite: []string{".claude/settings.json"}}, strings.NewReader(""), &diag)
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("flagged special=%v", err)
	}
	if strings.Contains(diag.String(), "Workspace name") {
		t.Fatal("refused edit prompted")
	}
}
