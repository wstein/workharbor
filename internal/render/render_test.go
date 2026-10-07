package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// scene is one run of the vocabulary: the fixed example of issue #320.
func scene(s Style) string {
	var b bytes.Buffer
	w := Writer{W: &b, S: s}
	w.Legend()
	w.Header(2, 14, `standard user "workharbor"`)
	w.Report(LevelFail, `there is no user workharbor`)
	w.Action("create the account")
	w.Command("sudo sysadminctl -addUser workharbor -fullName WorkHarbor -password -")
	w.Tool("2026-10-06 10:00:00.1 sysadminctl[1:2] Creating user record")
	w.Rule()
	w.Header(3, 14, "no automatic log-out")
	w.Report(LevelOK, "key not set: the system default applies (default off)")
	w.Report(LevelNotVerified, "tailscale did not answer")
	w.Report(LevelWarn, "weaker than recommended")
	w.Report(LevelSkipped, "left out")
	w.Question("Ready to create the account?", DefaultYes)
	b.WriteString("\n")
	w.Summary(Counts{OK: 1, Fail: 1, NotVerified: 1, Warn: 1, Skipped: 1})
	w.Todo([]TodoItem{{Text: "create the account", Commands: []string{"sudo sysadminctl -addUser workharbor"}}, {Text: "log in as workharbor and run whr setup"}})
	return b.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) //nolint:gosec // a golden file of this package
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file; got:\n%s", name, got)
	}
}

func TestGoldenTTYWithColour(t *testing.T) {
	golden(t, "tty", scene(Detect(true, "", false)))
}

func TestGoldenNonTTYIsPlainASCII(t *testing.T) {
	got := scene(Detect(false, "", false))
	golden(t, "plain", got)
	if strings.Contains(got, "\x1b") {
		t.Error("escape sequence in plain output")
	}
	for _, r := range got {
		if r > 127 {
			t.Fatalf("non-ASCII rune %q in plain output", r)
		}
	}
}

func TestGoldenNoColorIsPlain(t *testing.T) {
	got := scene(Detect(true, "1", false))
	golden(t, "nocolor", got)
	if got != scene(Detect(false, "", false)) {
		t.Error("NO_COLOR on a terminal differs from non-terminal output")
	}
	if Detect(true, "", true) != Detect(false, "", false) {
		t.Error("--plain is not plain")
	}
}

func TestEveryColourHasATextLabel(t *testing.T) {
	if len(Palette) == 0 {
		t.Fatal("empty palette")
	}
	s := Detect(true, "", false)
	for role, c := range Palette {
		if c.Label == "" || c.SGR == "" {
			t.Errorf("role %v has no label or no colour: %+v", role, c)
		}
		// the same text in colour and plain: nothing depends on the colour
		plain := Detect(false, "", false)
		for _, out := range []string{renderRole(s, role), renderRole(plain, role)} {
			if !strings.Contains(ansi.ReplaceAllString(out, ""), c.Label) {
				t.Errorf("role %v: label %q missing from %q", role, c.Label, out)
			}
		}
	}
	// every escape sequence in the TTY scene is one of the palette's
	known := map[string]bool{"\x1b[0m": true}
	for _, c := range Palette {
		known["\x1b["+c.SGR+"m"] = true
	}
	for _, seq := range ansi.FindAllString(scene(s), -1) {
		if !known[seq] {
			t.Errorf("colour %q is not in the palette, so it has no label", seq)
		}
	}
}

func TestStrippedColourEqualsPlainLayoutOfLabels(t *testing.T) {
	// colour never changes the words, only decorates them
	c := ansi.ReplaceAllString(scene(Detect(true, "", false)), "")
	for _, word := range []string{"ACTION", "FAIL", "ok", "WARN", "Step 2 of 14", "What you need to do now"} {
		if !strings.Contains(c, word) {
			t.Errorf("%q missing in the TTY scene without colour", word)
		}
	}
}

func TestToolOutputIsIndentedApart(t *testing.T) {
	var b bytes.Buffer
	Writer{W: &b, S: Detect(false, "", false)}.Tool("a\n\nb")
	if b.String() != "    tool output:\n    | a\n    | b\n" {
		t.Errorf("tool block = %q", b.String())
	}
}

func TestMultilineReportIndentsTheContinuation(t *testing.T) {
	got := Report(Detect(false, "", false), LevelFail, "one\ntwo")
	if got != " x FAIL   one\n          two\n" {
		t.Errorf("got %q", got)
	}
}

// renderRole renders the smallest output that uses a palette role.
func renderRole(s Style, r Role) string {
	switch r {
	case RoleOK:
		return Report(s, LevelOK, "x")
	case RoleFail:
		return Report(s, LevelFail, "x")
	case RoleNotVerified:
		return Report(s, LevelNotVerified, "x")
	case RoleWarn:
		return Report(s, LevelWarn, "x")
	case RoleSkipped:
		return Report(s, LevelSkipped, "x")
	case RoleAction:
		return Action(s, "x")
	case RoleCommand:
		return Command(s, "x")
	case RoleTool:
		return ToolOutput(s, "x")
	case RoleHeader:
		return Header(s, 1, 2, "x")
	case RoleTodo:
		return Todo(s, []TodoItem{{Text: "x"}})
	}
	return ""
}

func TestSplitToolSeparatesWhrsReasonFromTheToolsRawText(t *testing.T) {
	for in, want := range map[string][2]string{
		"defaults did not answer, so the setting is not known: exit status 1: Could not find key 'x'": {"defaults did not answer, so the setting is not known", "exit status 1: Could not find key 'x'"},
		"dscl did not say whether operator exists: exit status 1":                                     {"dscl did not say whether operator exists", "exit status 1"},
		"killed: signal: killed": {"killed", "signal: killed"},
		"the file is missing":    {"the file is missing", ""},
	} {
		if r, tool := SplitTool(in); r != want[0] || tool != want[1] {
			t.Errorf("%q = %q, %q; want %q", in, r, tool, want)
		}
	}
}

func TestToolWriterIndentsEveryLineAndNeverHoldsAPromptBack(t *testing.T) {
	var b bytes.Buffer
	w := NewToolWriter(&b, Detect(false, "", false))
	_, _ = w.Write([]byte("2026 log one\nCont"))
	if got := b.String(); got != "    tool output:\n    | 2026 log one\n    | Cont" {
		t.Errorf("a partial line (a prompt) must appear at once: %q", got)
	}
	_, _ = w.Write([]byte("inue? \nnext\n"))
	if got := b.String(); got != "    tool output:\n    | 2026 log one\n    | Continue? \n    | next\n" {
		t.Errorf("got %q", got)
	}
	w.End()
	_, _ = w.Write([]byte("again\n"))
	if !strings.HasSuffix(b.String(), "    tool output:\n    | again\n") {
		t.Errorf("a new block starts with its label: %q", b.String())
	}
}

func TestToolWriterEndClosesAnOpenLine(t *testing.T) {
	var b strings.Builder
	tw := NewToolWriter(&b, Style{})
	_, _ = tw.Write([]byte("no newline"))
	tw.End()
	if !strings.HasSuffix(b.String(), "no newline\n") {
		t.Errorf("End did not add the newline: %q", b.String())
	}
	n := b.Len()
	tw.End()
	if b.Len() != n {
		t.Errorf("a second End wrote: %q", b.String())
	}
}

func TestToolWriterEscapesControlBytes(t *testing.T) {
	var b strings.Builder
	tw := NewToolWriter(&b, Style{})
	_, _ = tw.Write([]byte("a\x1b[2Jb\rc\u009bd\te\r\nnext\x07\n"))
	got := b.String()
	for _, r := range got {
		if r == 0x1b || r == '\r' || r == 0x07 || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("a control byte reached the output: %q", got)
		}
	}
	want := "    tool output:\n    | a\\x1b[2Jb\\rc\\u009bd\te\n    | next\\x07\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestToolWriterKeepsASplitMultibyteCharacterWhole(t *testing.T) {
	var b bytes.Buffer
	tw := NewToolWriter(&b, Style{})
	_, _ = tw.Write([]byte("a\xe2\x82"))
	_, _ = tw.Write([]byte("\xacb\n\xe2\x82"))
	tw.End()
	got := b.String()
	if !strings.Contains(got, "a\u20acb\n") {
		t.Fatalf("split character broken: %q", got)
	}
	if !strings.Contains(got, "| \ufffd") && !strings.Contains(got, "\\x") {
		t.Fatalf("dangling partial sequence dropped: %q", got)
	}
}

func TestToolWriterCRLFSplitAcrossWritesMatchesUnsplit(t *testing.T) {
	var whole, split bytes.Buffer
	w := NewToolWriter(&whole, Style{})
	_, _ = w.Write([]byte("a\r\nb\r\n"))
	w.End()
	s := NewToolWriter(&split, Style{})
	for _, c := range []string{"a\r", "\nb\r", "\n"} {
		_, _ = s.Write([]byte(c))
	}
	s.End()
	if whole.String() != split.String() {
		t.Fatalf("split %q != whole %q", split.String(), whole.String())
	}
}

func TestToolWriterLoneCRIsHeldNotDropped(t *testing.T) {
	var whole, split bytes.Buffer
	w := NewToolWriter(&whole, Style{})
	_, _ = w.Write([]byte("a\rb"))
	w.End()
	s := NewToolWriter(&split, Style{})
	_, _ = s.Write([]byte("a\r"))
	_, _ = s.Write([]byte("b"))
	s.End()
	if whole.String() != split.String() {
		t.Fatalf("split %q != whole %q", split.String(), whole.String())
	}
	var tail bytes.Buffer
	e := NewToolWriter(&tail, Style{})
	_, _ = e.Write([]byte("50%\r"))
	e.End()
	if !strings.Contains(tail.String(), `50%\r`) { // shown escaped, not dropped
		t.Fatalf("trailing CR lost at End: %q", tail.String())
	}
}

func TestDetectEnvColourRules(t *testing.T) {
	cases := []struct {
		name string
		e    Env
		want bool
	}{
		{"tty", Env{TTY: true, Term: "xterm"}, true},
		{"not a tty", Env{Term: "xterm"}, false},
		{"NO_COLOR", Env{TTY: true, Term: "xterm", NoColor: "1"}, false},
		{"TERM=dumb", Env{TTY: true, Term: "dumb"}, false},
		{"--no-color", Env{TTY: true, Term: "xterm", NoColorFlag: true}, false},
		{"FORCE_COLOR", Env{Term: "dumb", ForceColor: "1"}, true},
		{"FORCE_COLOR=0", Env{ForceColor: "0"}, false},
		{"--color=always", Env{ColorAlways: true}, true},
		{"--no-color beats force", Env{ColorAlways: true, ForceColor: "1", NoColorFlag: true}, false},
		{"--plain beats force", Env{ColorAlways: true, Plain: true}, false},
	}
	for _, c := range cases {
		st := DetectEnv(c.e)
		if st.Color != c.want {
			t.Errorf("%s: Color = %v, want %v", c.name, st.Color, c.want)
		}
		if c.e.TTY == false && st.Unicode {
			t.Errorf("%s: forced colour must not turn on Unicode symbols", c.name)
		}
		if got := strings.Contains(scene(st), "\x1b"); got != c.want {
			t.Errorf("%s: escape byte present = %v, want %v", c.name, got, c.want)
		}
	}
}

// Issue #378: a tool's prompt for a secret is never relayed, whole or split
// across writes, with or without its newline; the text typed after it is not
// the relay's to show either.
func TestToolWriterDropsSecretPrompts(t *testing.T) {
	for name, chunks := range map[string][]string{
		"whole":      {"hello\nUser password:"},
		"newline":    {"hello\nPassword:\n"},
		"split":      {"hello\nUser pass", "word: "},
		"typed echo": {"hello\nUser password:", "\n"},
		"passphrase": {"hello\nEnter passphrase for key '/k': "},
	} {
		var b strings.Builder
		tw := NewToolWriter(&b, Style{})
		for _, c := range chunks {
			_, _ = tw.Write([]byte(c))
		}
		tw.End()
		got := b.String()
		if strings.Contains(strings.ToLower(got), "password:") || strings.Contains(got, "passphrase for") {
			t.Errorf("%s: a secret prompt was relayed: %q", name, got)
		}
		if !strings.Contains(got, NeutralPromptLine) || !strings.Contains(got, "| hello\n") {
			t.Errorf("%s: want the neutral line and the other output: %q", name, got)
		}
		if strings.Contains(got, "| \n") {
			t.Errorf("%s: stray empty line: %q", name, got)
		}
	}
}

func TestToolWriterPromptDetection(t *testing.T) {
	for in, hidden := range map[string]bool{
		"Password:":                  true,
		"\x1b[1mPassword:\x1b[0m ":   true,
		"Passwort:":                  true,
		"Kennwort: ":                 true,
		"invalid password for user:": false,
		"Falsches Passwort, Fehler:": false,
		"Set the password":           false,
	} {
		var b strings.Builder
		tw := NewToolWriter(&b, Style{})
		_, _ = tw.Write([]byte(in))
		tw.End()
		if got := strings.Contains(b.String(), NeutralPromptLine); got != hidden {
			t.Errorf("%q: hidden = %v, want %v (%q)", in, got, hidden, b.String())
		}
	}
}
