// SPDX-License-Identifier: EUPL-1.2
package wizard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/wizard"
	"io"
	"reflect"
	"strings"
	"testing"
)

func fixture() scaffold.Snapshot {
	return scaffold.Snapshot{Directory: "workspace", Entries: map[string]scaffold.Entry{".devcontainer": {Kind: "directory"}, ".devcontainer/devcontainer.json": {Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}}}
}
func ptr(s string) *string { return &s }
func TestWizardScriptedAnswersMatchFlags(t *testing.T) {
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), fixture(), scaffold.Options{}, strings.NewReader("demo\nhttps://example.com/app.git\nprojects/app\n\nnu\nplain\noff\nwrite\n"), &diag)
	if err != nil {
		t.Fatal(err)
	}
	want, err := scaffold.Build(fixture(), scaffold.Options{Name: ptr("demo"), ProjectURL: ptr("https://example.com/app.git"), StartDir: ptr("projects/app"), HumanShell: ptr("nu"), ConsoleMode: ptr("plain"), Shortcuts: ptr("off")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Files(), want.Files()) {
		t.Fatalf("wizard ignored answers: %s", p.Files()["workharbor.json"])
	}
	if !strings.Contains(diag.String(), "not creation-ready") {
		t.Fatal("missing shared preview warning")
	}
}

func TestWizardBackAndReview(t *testing.T) {
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), fixture(), scaffold.Options{}, strings.NewReader("first\nback\nsecond\nreview\nedit human-shell\nnu\nwrite\n"), &diag)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(p.Files()["workharbor.json"], []byte(`"name": "second"`)) || !bytes.Contains(p.Files()["workharbor.json"], []byte(`"human_shell": "nu"`)) {
		t.Fatal("back/edit did not update choices")
	}
	if strings.Count(diag.String(), "create file workharbor.json") < 2 {
		t.Fatal("review omitted proposal")
	}
}
func TestWizardCancelChangesNothing(t *testing.T) {
	for _, answers := range []string{"cancel\n", "", "demo", "review\ncancel\n", "review\nwrite"} {
		s := fixture()
		before := append([]byte(nil), s.Entries[".devcontainer/devcontainer.json"].Data...)
		var diag bytes.Buffer
		p, err := wizard.Run(context.Background(), s, scaffold.Options{Yes: true}, strings.NewReader(answers), &diag)
		if p != nil || exitcode.FromError(err) != exitcode.Interrupted {
			t.Fatalf("%q: proposal=%v error=%v", answers, p, err)
		}
		if !bytes.Equal(before, s.Entries[".devcontainer/devcontainer.json"].Data) || len(s.Entries) != 2 {
			t.Fatal("cancel altered snapshot")
		}
	}
}
func existing() scaffold.Snapshot {
	s := fixture()
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"name":"old","start_dir":".","human_shell":"bash","console_mode":"plain","shortcuts":"off","project":{"url":"https://example.com/app.git","path":"projects/app"}}`)}
	return s
}
func TestWizardExistingSettingsOnlyMissingPrompted(t *testing.T) {
	var diag bytes.Buffer
	s := existing()
	p, err := wizard.Run(context.Background(), s, scaffold.Options{}, strings.NewReader("write\n"), &diag)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Files()["workharbor.json"], s.Entries["workharbor.json"].Data) {
		t.Fatal("existing manifest changed")
	}
	for _, prompt := range []string{"Workspace name (optional) [", "Human shell (", "Project URL (optional) [", "Reuse existing devcontainer ["} {
		if strings.Contains(diag.String(), prompt) {
			t.Fatal("prompted existing setting:", prompt)
		}
	}
}
func TestWizardPerPathEditConfirmation(t *testing.T) {
	for _, tc := range []struct {
		answer string
		code   exitcode.Code
	}{{"no\n", exitcode.Interrupted}, {"yes\n", exitcode.OK}, {"", exitcode.Interrupted}} {
		var diag bytes.Buffer
		p, err := wizard.Run(context.Background(), existing(), scaffold.Options{Name: ptr("new"), Yes: true, Overwrite: []string{"workharbor.json"}}, strings.NewReader("write\n"+tc.answer), &diag)
		if exitcode.FromError(err) != tc.code {
			t.Fatalf("%q: %v", tc.answer, err)
		}
		if !strings.Contains(diag.String(), "Modify workharbor.json? [y/N]: ") {
			t.Fatal("missing per-path confirmation")
		}
		if tc.code == exitcode.OK && (p == nil || !bytes.Contains(p.Files()["workharbor.json"], []byte(`"name": "new"`))) {
			t.Fatal("confirmed edit absent")
		}
	}
}
func TestWizardInvalidAnswersAreRetried(t *testing.T) {
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), fixture(), scaffold.Options{}, strings.NewReader("BAD!\ndemo\nhttps://user:secret@example.com/a.git\n\n../escape\n.\n\nsh\nfish\nwrong\nherdr\nwrong\non\nwrite\n"), &diag)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	if strings.Contains(diag.String(), "secret") {
		t.Fatal("credential leaked")
	}
	if !bytes.Contains(p.Files()["workharbor.json"], []byte(`"name": "demo"`)) {
		t.Fatal("valid retry ignored")
	}
}
func TestWizardContextAndOutputFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wizard.Run(ctx, fixture(), scaffold.Options{}, strings.NewReader(""), io.Discard); exitcode.FromError(err) != exitcode.Interrupted {
		t.Fatal(err)
	}
	if _, err := wizard.Run(context.Background(), fixture(), scaffold.Options{}, strings.NewReader("review\nwrite\n"), brokenWriter{}); exitcode.FromError(err) != exitcode.Internal {
		t.Fatal(err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWizardPerPathSecondDeclineCancels(t *testing.T) {
	s := existing()
	s.Entries["projects"] = scaffold.Entry{Kind: "directory"}
	s.Entries["projects/.gitkeep"] = scaffold.Entry{Kind: "file", Data: []byte("owner content")}
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), s, scaffold.Options{Name: ptr("new")}, strings.NewReader("write\nyes\nno\n"), &diag)
	if p != nil || exitcode.FromError(err) != exitcode.Interrupted {
		t.Fatalf("%v %v", p, err)
	}
	if !strings.Contains(diag.String(), "Modify workharbor.json?") || !strings.Contains(diag.String(), "Modify projects/.gitkeep?") {
		t.Fatal("not every edited path was confirmed")
	}
	if string(s.Entries["projects/.gitkeep"].Data) != "owner content" {
		t.Fatal("decline altered old file")
	}
}
func TestWizardExplicitFlagsNotPrompted(t *testing.T) {
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), fixture(), scaffold.Options{Name: ptr("flag-name"), HumanShell: ptr("zsh"), ConsoleMode: ptr("plain"), Shortcuts: ptr("off"), StartDir: ptr(".")}, strings.NewReader("\n\nwrite\n"), &diag)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diag.String(), "Workspace name (optional) [") || strings.Contains(diag.String(), "Human shell (") {
		t.Fatal("prompted explicit flag")
	}
	if !bytes.Contains(p.Files()["workharbor.json"], []byte(`"human_shell": "zsh"`)) {
		t.Fatal("flag lost precedence")
	}
}
func TestWizardAnswerBound(t *testing.T) {
	_, err := wizard.Run(context.Background(), fixture(), scaffold.Options{}, strings.NewReader(strings.Repeat("x", 5000)+"\n"), io.Discard)
	if exitcode.FromError(err) != exitcode.Usage {
		t.Fatal(err)
	}
}

// After the devcontainer step, a bare Enter on the review prompt takes its documented default, cancel; a
// later "write" in the input must never be reached.
func TestWizardBareEnterOnReviewCancels(t *testing.T) {
	o := scaffold.Options{Name: ptr("demo"), ProjectURL: ptr("https://example.com/app.git"), StartDir: ptr("projects/app"), HumanShell: ptr("nu"), ConsoleMode: ptr("plain"), Shortcuts: ptr("off")}
	var diag bytes.Buffer
	p, err := wizard.Run(context.Background(), fixture(), o, strings.NewReader("\n\nwrite\n"), &diag)
	if p != nil || exitcode.FromError(err) != exitcode.Interrupted {
		t.Fatalf("bare Enter must cancel: proposal=%v error=%v", p, err)
	}
	if !strings.Contains(diag.String(), "Review: write, back, edit <flag>, review or cancel [cancel]: ") {
		t.Fatalf("review prompt was not reached: %q", diag.String())
	}
}

func TestWizardDefaultsPlainAndRetainsExplicitHerdr(t *testing.T) {
	for _, source := range []string{"default", "answer", "flag", "existing", "existing-missing"} {
		s := fixture()
		o := scaffold.Options{}
		answers := "\n\n\n\n\n\n\nwrite\n"
		want := "plain"
		switch source {
		case "answer":
			answers = "\n\n\n\n\nherdr\n\nwrite\n"
			want = "herdr"
		case "flag":
			o.ConsoleMode = ptr("herdr")
			answers = "\n\n\n\n\n\nwrite\n"
			want = "herdr"
		case "existing-missing":
			s = existing()
			e := s.Entries["workharbor.json"]
			e.Data = bytes.ReplaceAll(e.Data, []byte(`"console_mode":"plain",`), nil)
			s.Entries["workharbor.json"] = e
			answers = "review\nwrite\n"
		case "existing":
			s = existing()
			e := s.Entries["workharbor.json"]
			e.Data = bytes.ReplaceAll(e.Data, []byte(`"console_mode":"plain"`), []byte(`"console_mode":"herdr"`))
			s.Entries["workharbor.json"] = e
			answers = "write\n"
			want = "herdr"
		}
		var diag bytes.Buffer
		p, err := wizard.Run(context.Background(), s, o, strings.NewReader(answers), &diag)
		if err != nil {
			t.Fatalf("%s: %v (%s)", source, err, diag.String())
		}
		if got := p.Preview().Settings["console_mode"].Value; got != want {
			t.Fatalf("%s: %v, want %s", source, got, want)
		}
		var manifest map[string]any
		if err := json.Unmarshal(p.Files()["workharbor.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		if source != "existing-missing" && manifest["console_mode"] != want {
			t.Fatalf("%s: stored mode %v, want %s", source, manifest["console_mode"], want)
		}
		if source == "default" && !strings.Contains(diag.String(), "Console mode (herdr/plain) [plain]:") {
			t.Fatal("prompt default differs")
		}
		if (source == "existing" || source == "existing-missing") && !bytes.Equal(p.Files()["workharbor.json"], s.Entries["workharbor.json"].Data) {
			t.Fatal("existing manifest rewritten")
		}
	}
}
