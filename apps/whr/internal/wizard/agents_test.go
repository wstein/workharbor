// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
)

func TestWizardAgentsPerPathConsent(t *testing.T) {
	for _, answer := range []string{"yes", "no"} {
		t.Run(answer, func(t *testing.T) {
			s := existing()
			s.Entries["AGENTS.md"] = scaffold.Entry{Kind: "file", Data: []byte("owner\r\n")}
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, scaffold.Options{}, strings.NewReader("write\n"+answer+"\n"), &diag)
			if err != nil || p == nil {
				t.Fatalf("decline must not cancel: %v", err)
			}
			if !strings.Contains(diag.String(), "Modify AGENTS.md? [y/N]: ") {
				t.Fatal("missing per-path offer")
			}
			want := "append_block"
			if answer == "no" {
				want = "left_unchanged"
			}
			for _, a := range p.Preview().Actions {
				if a.Path == "AGENTS.md" && a.Action != want {
					t.Fatalf("action %+v", a)
				}
			}
		})
	}
}
func TestWizardAgentsRefusedEditNotOfferedAndFlagPreconfirms(t *testing.T) {
	for _, flag := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrequested", true: "flagged"}[flag], func(t *testing.T) {
			s := existing()
			s.Entries["AGENTS.md"] = scaffold.Entry{Kind: "file", Data: []byte("<!-- whr:agents-block end -->\n")}
			o := scaffold.Options{}
			if flag {
				o.Overwrite = []string{"AGENTS.md"}
			}
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, o, strings.NewReader("write\n"), &diag)
			if flag {
				if exitcode.FromError(err) != exitcode.Conflict {
					t.Fatalf("flagged malformed edit: %v", err)
				}
				return
			}
			if err != nil || p == nil {
				t.Fatal(err)
			}
			if strings.Contains(diag.String(), "Modify AGENTS.md?") {
				t.Fatal("refused edit offered")
			}
			if !strings.Contains(diag.String(), "agents_block.malformed") {
				t.Fatal("missing refusal warning")
			}
		})
	}
	s := existing()
	s.Entries["AGENTS.md"] = scaffold.Entry{Kind: "file", Data: []byte("owner\n")}
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), s, scaffold.Options{Overwrite: []string{"AGENTS.md"}}, strings.NewReader("write\n"), &diag)
	if err != nil || p == nil {
		t.Fatalf("flag did not preconfirm: %v", err)
	}
	if strings.Contains(diag.String(), "Modify AGENTS.md?") {
		t.Fatal("preconfirmed path asked twice")
	}
}

func TestWizardAgentsFlaggedRefusalReturnsConflictDuringSettings(t *testing.T) {
	s := fixture()
	s.Entries["AGENTS.md"] = scaffold.Entry{Kind: "file", Data: []byte("<!-- whr:agents-block end -->\n")}
	var diag bytes.Buffer
	_, err := wizard.Run(context.Background(), s, scaffold.Options{Overwrite: []string{"AGENTS.md"}}, strings.NewReader("demo\n"), &diag)
	if exitcode.FromError(err) != exitcode.Conflict {
		t.Fatalf("preconfirmed refused edit must be 68, got %v", err)
	}
}

func TestWizardAgentsDifferentTemplateConsentR1(t *testing.T) {
	expected, err := scaffold.Build(fixture(), scaffold.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	shipped := expected.Files()["AGENTS.md"]
	for _, version := range []int{0, 1, 2} {
		for _, consent := range []string{"yes", "no", "flag"} {
			t.Run(fmt.Sprintf("version=%d/%s", version, consent), func(t *testing.T) {
				body := "foreign workflow\n"
				block := fmt.Sprintf("<!-- whr:agents-block begin v=%d sha256=%x -->\n%s<!-- whr:agents-block end -->\n", version, sha256.Sum256([]byte(body)), body)
				original := []byte("owner\r\n" + block + "tail\r\n")
				s := existing()
				s.Entries["AGENTS.md"] = scaffold.Entry{Kind: "file", Data: original}
				options := scaffold.Options{}
				answers := "write\n" + consent + "\n"
				if consent == "flag" {
					options.Overwrite = []string{"AGENTS.md"}
					answers = "write\n"
				}
				var diag bytes.Buffer
				p, err := wizard.Run(context.Background(), s, options, strings.NewReader(answers), &diag)
				if err != nil || p == nil {
					t.Fatalf("consent failed: %v", err)
				}
				if strings.Contains(diag.String(), "Modify AGENTS.md?") != (consent != "flag") {
					t.Fatal("wrong path-confirmation behavior")
				}
				wantAction := "replace_block"
				wantData := append(append([]byte("owner\r\n"), shipped...), []byte("tail\r\n")...)
				if consent == "no" {
					wantAction = "left_unchanged"
					wantData = original
				}
				found := 0
				for _, a := range p.Preview().Actions {
					if a.Path == "AGENTS.md" {
						found++
						if a.Action != wantAction {
							t.Fatalf("action %+v", a)
						}
					}
				}
				if found != 1 || !bytes.Equal(p.Files()["AGENTS.md"], wantData) {
					t.Fatal("accepted proposal differs from path consent")
				}
				if consent == "no" {
					warnings := p.Preview().AgentsBlockWarnings
					if len(warnings) != 1 || warnings[0].Code != "agents_block.different_template" {
						t.Fatalf("declined warning %v", warnings)
					}
				}
			})
		}
	}
}
