// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
)

var d2Prompts = []string{
	"Workspace name (optional) [", "Project URL (optional) [", "Starting directory [",
	"Reuse existing devcontainer [", "Human shell (fish/bash/zsh/nu) [",
	"Console mode (herdr/plain) [", "Shared shortcuts (on/off) [",
}

// Check each optional field independently: a complete manifest must not hide a
// missing setting, and answering it must not invite edits to the other settings.
func TestWizardD2EachMissingSettingPrompted(t *testing.T) {
	for _, tc := range []struct {
		field, answer string
		prompt        int
	}{
		{"name", "new-name", 0}, {"project", "https://example.com/new.git", 1},
		{"start_dir", ".", 2}, {"human_shell", "fish", 4},
		{"console_mode", "plain", 5}, {"shortcuts", "on", 6},
	} {
		t.Run(tc.field, func(t *testing.T) {
			s := existing()
			var manifest map[string]json.RawMessage
			if err := json.Unmarshal(s.Entries["workharbor.json"].Data, &manifest); err != nil {
				t.Fatal(err)
			}
			delete(manifest, tc.field)
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: raw}
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, scaffold.Options{}, strings.NewReader(tc.answer+"\nwrite\nyes\n"), &diag)
			if err != nil || p == nil {
				t.Fatalf("missing %s: %v; %s", tc.field, err, diag.String())
			}
			for i, prompt := range d2Prompts {
				want := 0
				if i == tc.prompt {
					want = 1
				}
				if got := strings.Count(diag.String(), prompt); got != want {
					t.Fatalf("%q count = %d, want %d: %s", prompt, got, want, diag.String())
				}
			}
			if !strings.Contains(diag.String(), "Modify workharbor.json? [y/N]: ") {
				t.Fatal("answer bypassed existing-file consent")
			}
			var authored map[string]json.RawMessage
			if err := json.Unmarshal(p.Files()["workharbor.json"], &authored); err != nil {
				t.Fatal(err)
			}
			var gotAnswer any
			if err := json.Unmarshal(authored[tc.field], &gotAnswer); err != nil {
				t.Fatal(err)
			}
			var wantAnswer any = tc.answer
			if tc.field == "project" {
				wantAnswer = map[string]any{"url": tc.answer, "path": "projects/new"}
			}
			if !reflect.DeepEqual(gotAnswer, wantAnswer) {
				t.Fatalf("answer = %v, want %v", gotAnswer, wantAnswer)
			}
			for key, value := range manifest {
				if !bytes.Equal(authored[key], value) {
					var got, want any
					if json.Unmarshal(authored[key], &got) != nil || json.Unmarshal(value, &want) != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("existing %s changed: %s -> %s", key, value, authored[key])
					}
				}
			}
		})
	}
}

// Cancellation can occur before any answer, between choices, or at review.
// Run returns a proposal only after consent and must never mutate its input.
func TestWizardD2CancelAtEveryStepPreservesSnapshot(t *testing.T) {
	for stage := 0; stage <= len(d2Prompts); stage++ {
		t.Run(strconv.Itoa(stage), func(t *testing.T) {
			s := fixture()
			before := fixture()
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, scaffold.Options{Yes: true}, strings.NewReader(strings.Repeat("\n", stage)+"cancel\n"), &diag)
			if p != nil || int(exitcode.FromError(err)) != 72 {
				t.Fatalf("stage %d: proposal=%v error=%v; %s", stage, p, err, diag.String())
			}
			if stage < len(d2Prompts) && !strings.Contains(diag.String(), d2Prompts[stage]) {
				t.Fatal("did not reach cancellation step")
			}
			if stage == len(d2Prompts) && !strings.Contains(diag.String(), "Review:") {
				t.Fatal("did not reach review")
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("cancel mutated snapshot")
			}
		})
	}
}

func TestWizardD2EachExplicitFlagSkipsItsPrompt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt int
		set    func(*scaffold.Options)
	}{
		{"name", 0, func(o *scaffold.Options) { o.Name = ptr("flag-name") }},
		{"project-url", 1, func(o *scaffold.Options) { o.ProjectURL = ptr("https://example.com/flag.git") }},
		{"start-dir", 2, func(o *scaffold.Options) { o.StartDir = ptr("projects/custom") }},
		{"devcontainer", 3, func(o *scaffold.Options) { o.Devcontainer = ptr("chosen.json") }},
		{"human-shell", 4, func(o *scaffold.Options) { o.HumanShell = ptr("zsh") }},
		{"console-mode", 5, func(o *scaffold.Options) { o.ConsoleMode = ptr("herdr") }},
		{"shortcuts", 6, func(o *scaffold.Options) { o.Shortcuts = ptr("off") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			s.Entries["chosen.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}
			o := scaffold.Options{}
			tc.set(&o)
			var diag bytes.Buffer
			p, err := wizard.Run(context.Background(), s, o, strings.NewReader(strings.Repeat("\n", len(d2Prompts)-1)+"write\n"), &diag)
			if err != nil || p == nil {
				t.Fatalf("%v; %s", err, diag.String())
			}
			for i, prompt := range d2Prompts {
				want := 1
				if i == tc.prompt {
					want = 0
				}
				if got := strings.Count(diag.String(), prompt); got != want {
					t.Fatalf("%q count = %d, want %d", prompt, got, want)
				}
			}
			want, err := scaffold.Build(s, o)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Files(), want.Files()) {
				t.Fatal("flag and wizard generated different files")
			}
		})
	}
}
