// Package render is the one vocabulary for what `whr setup` and `whr doctor`
// print for a human, and for the web status page and the mock to reuse
// (issues #320, #322, #323). It has three kinds of text with fixed rendering:
//
//   - a report says what whr checked: a symbol, a status word (ok, FAIL, ?,
//     WARN, skip) and one reason line;
//   - an ACTION is anything the person must do or answer, behind a bar and the
//     fixed label ACTION;
//   - a command is text to copy, indented behind the same bar after a "$".
//
// Every function here is pure: it takes a Style and returns text. Colour is only
// decoration: every colour in Palette has a text label that is always printed,
// so nothing depends on colour alone. Colour and non-ASCII symbols appear only
// on a terminal without NO_COLOR and without --plain (Detect). There is no
// cursor control and no dependency: output stays safe for pipes, agents and sudo
// prompts. The layout was not tested in the author's own terminal (fsh).
package render

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/textsafe"
)

// Level is the outcome a report line shows.
type Level int

// The levels, in the order of the legend.
const (
	LevelOK Level = iota
	LevelFail
	LevelNotVerified
	LevelWarn
	LevelSkipped
)

// Role is something that may be coloured. Each has an entry in Palette.
type Role int

// The roles.
const (
	RoleOK Role = iota
	RoleFail
	RoleNotVerified
	RoleWarn
	RoleSkipped
	RoleAction
	RoleCommand
	RoleTool
	RoleHeader
	RoleTodo
)

// Colour is a role's SGR code and the text label that always accompanies it.
type Colour struct {
	SGR   string
	Label string
}

// Palette maps each role to its colour and its text label. The pairs are chosen
// to stay apart for the common colour blindnesses (blue and orange, not red and
// green), and red is never used without the symbol and the word FAIL.
var Palette = map[Role]Colour{
	RoleOK:          {"34", "ok"},
	RoleFail:        {"1;31", "FAIL"},
	RoleNotVerified: {"35", "?"},
	RoleWarn:        {"33", "WARN"},
	RoleSkipped:     {"2", "skip"},
	RoleAction:      {"1;33", "ACTION"},
	RoleCommand:     {"36", "$"},
	RoleTool:        {"2", "tool output"},
	RoleHeader:      {"1", "Step"},
	RoleTodo:        {"1", "What you need to do now"},
}

// Style says how to draw. The zero value is plain ASCII.
type Style struct {
	Color   bool // ANSI colour
	Unicode bool // ✓ ✗ ▌ ─ instead of + x | -
	// Width is the columns to wrap at: the terminal width capped at 80. Zero
	// (not a terminal, or unknown) means 80.
	Width int
}

// cols is the width to wrap at.
func (s Style) cols() int {
	if s.Width > 0 && s.Width < wrapWidth {
		return s.Width
	}
	return wrapWidth
}

// Detect chooses the style: colour and symbols only when the output is a
// terminal, NO_COLOR is empty (no.color.org: any non-empty value turns colour
// off) and --plain is not given.
func Detect(tty bool, noColor string, plain bool) Style {
	return DetectEnv(Env{TTY: tty, NoColor: noColor, Plain: plain})
}

// Env is what decides the style. Colour is decoration only: the words are
// always printed.
type Env struct {
	TTY         bool   // the output is a terminal
	Term        string // $TERM
	NoColor     string // $NO_COLOR; non-empty turns colour off
	ForceColor  string // $FORCE_COLOR; non-empty and not "0" turns colour on
	NoColorFlag bool   // --no-color
	ColorAlways bool   // --color=always
	Plain       bool   // --plain
	Cols        int    // terminal columns; 0 when unknown
}

// DetectEnv is Detect with the rest of the rules. --no-color and --plain win;
// then --color=always and FORCE_COLOR force colour on; otherwise colour needs a
// terminal, a TERM that is not "dumb" and an empty NO_COLOR. Symbols follow
// the automatic rule only.
func DetectEnv(e Env) Style {
	auto := e.TTY && e.Term != "dumb" && e.NoColor == "" && !e.Plain && !e.NoColorFlag
	force := (e.ColorAlways || (e.ForceColor != "" && e.ForceColor != "0")) && !e.Plain && !e.NoColorFlag
	st := Style{Color: auto || force, Unicode: auto}
	if e.TTY && e.Cols > 0 && e.Cols < wrapWidth {
		st.Width = e.Cols
	}
	return st
}

func (s Style) paint(r Role, text string) string {
	if !s.Color || text == "" {
		return text
	}
	return "\x1b[" + Palette[r].SGR + "m" + text + "\x1b[0m"
}

func (s Style) bar() string {
	if s.Unicode {
		return "▌"
	}
	return "|"
}

var levelRole = map[Level]Role{LevelOK: RoleOK, LevelFail: RoleFail, LevelNotVerified: RoleNotVerified, LevelWarn: RoleWarn, LevelSkipped: RoleSkipped}

func (s Style) symbol(l Level) string {
	uni := map[Level]string{LevelOK: "✓", LevelFail: "✗", LevelNotVerified: "?", LevelWarn: "▲", LevelSkipped: "–"}
	asc := map[Level]string{LevelOK: "+", LevelFail: "x", LevelNotVerified: "?", LevelWarn: "!", LevelSkipped: "-"}
	if s.Unicode {
		return uni[l]
	}
	return asc[l]
}

// wrapWidth is the column the report and ACTION text wraps at; a word longer
// than the room (a path, a URL) is never broken.
const wrapWidth = 80

var padded = regexp.MustCompile(`^\S+ {2,}`)

// wrap breaks each line of text at spaces so that padWidth plus the line stays
// within wrapWidth columns.
func wrap(text string, padWidth int) string { return wrapAt(text, padWidth, wrapWidth) }

// wrapAt is wrap for a given total width.
func wrapAt(text string, padWidth, width int) string {
	room := width - padWidth
	var out []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if utf8.RuneCountInString(line) <= room {
			out = append(out, line)
			continue
		}
		cur := ""
		if m := padded.FindString(line); m != "" { // a padded name column stays whole
			cur, line = m, line[len(m):]
		}
		for _, w := range strings.Fields(line) {
			switch {
			case cur == "":
				cur = w
			case strings.HasSuffix(cur, " "):
				cur += w
			case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) <= room:
				cur += " " + w
			default:
				out = append(out, cur)
				cur = w
			}
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n")
}

// Wrap breaks text at spaces so that padWidth plus a line stays within 80
// columns.
func Wrap(text string, padWidth int) string { return wrap(text, padWidth) }

func indent(text, pad string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines, "\n"+pad)
}

// Report is one report line: " <symbol> <WORD>  <text>". The word is padded so
// the text aligns. Further lines of text are indented under it.
func Report(s Style, l Level, text string) string {
	role := levelRole[l]
	word := Palette[role].Label
	pad := strings.Repeat(" ", 5-len([]rune(word)))
	head := s.paint(role, s.symbol(l)+" "+word)
	return " " + head + pad + "  " + indent(wrapAt(text, 12, s.cols()), strings.Repeat(" ", 10)) + "\n"
}

// Action is a line the person must act on: a bar and the label ACTION.
func Action(s Style, text string) string {
	return s.paint(RoleAction, s.bar()+" ACTION") + "  " + indent(wrapAt(text, 10, s.cols()), "          ") + "\n"
}

// Command is text to copy, behind the action bar and a "$".
//
// A command is never wrapped, so a copy gets it whole. One that does not fit
// goes on its own line behind a line that says so.
func Command(s Style, cmd string) string {
	line := s.paint(RoleAction, s.bar()) + "   " + s.paint(RoleCommand, "$ "+cmd) + "\n"
	if utf8.RuneCountInString(cmd)+6 > s.cols() {
		line = s.paint(RoleAction, s.bar()) + "   (one long line, copy it whole)\n" + line
	}
	return line
}

// Cmd is Command under the name of the other typed helpers.
func Cmd(s Style, cmd string) string { return Command(s, cmd) }

// KV is a key and its value, two spaces apart; a long value wraps with the
// continuation aligned to the value column.
func KV(s Style, key, value string) string {
	head := key + "  "
	pad := utf8.RuneCountInString(head)
	lines := strings.Split(wrapAt(value, pad, s.cols()), "\n")
	return head + strings.Join(lines, "\n"+strings.Repeat(" ", pad)) + "\n"
}

var notePrefix = regexp.MustCompile(`^\s*(?:[a-z]+(?: \([a-z ]+\))?: )?`)

// Note is a line of human text. A leading label ("note: ", "whr: ") is the
// prefix; a continuation is indented by its width.
func Note(s Style, text string) string {
	head := notePrefix.FindString(text)
	pad := utf8.RuneCountInString(head)
	lines := strings.Split(wrapAt(text[len(head):], pad, s.cols()), "\n")
	return head + strings.Join(lines, "\n"+strings.Repeat(" ", pad)) + "\n"
}

// Question is an ACTION line without its newline, for a prompt that waits.
func Question(s Style, text string) string {
	return s.paint(RoleAction, s.bar()+" ACTION") + "  " + indent(wrapAt(text, 10, s.cols()), "          ") + " "
}

// ToolOutput sets what an outside tool printed apart: a label line, then each
// line indented behind a marker. It is empty for empty text.
func ToolOutput(s Style, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	mark := "|"
	if s.Unicode {
		mark = "│"
	}
	var b strings.Builder
	b.WriteString("    " + s.paint(RoleTool, "tool output:") + "\n")
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString("    " + s.paint(RoleTool, mark+" "+strings.TrimRight(l, " \t\r")) + "\n")
	}
	return b.String()
}

var toolText = regexp.MustCompile(`(?:: |^)((?:exit status \d+|signal: \w+)(?:: .*)?)$`)

// SplitTool separates whr's own reason from the raw text of the tool a check
// ran, which the checks append after the tool's exit status ("...: exit status
// 1: <what it said>"). The raw text is shown only with --verbose.
func SplitTool(detail string) (reason, tool string) {
	m := toolText.FindStringSubmatchIndex(detail)
	if m == nil {
		return detail, ""
	}
	return strings.TrimRight(detail[:m[0]], " :"), detail[m[2]:m[3]]
}

// ToolWriter indents the live output of a command behind a marker, under one
// label, without holding anything back: a prompt without a newline appears at
// once. It is for commands that run with the terminal attached.
type ToolWriter struct {
	w       io.Writer
	s       Style
	started bool
	atStart bool
	pend    []byte // incomplete trailing UTF-8 of the last Write
	line    string // text of the current line so far, to spot a split prompt
	skipNL  bool   // the newline that follows a dropped prompt is dropped too
}

// secretPrompt matches a line a tool prints to ask for a secret ("Password:",
// "User password:", "Enter passphrase for key:").
var secretPrompt = regexp.MustCompile(`(?i)\b(pass(word|phrase|code)|secret|pin)\b[^:]*:\s*$`)

// NeutralPromptLine replaces a prompt that asks for a secret. whr never relays
// such a prompt: it would invite typing a secret into a terminal whr does not
// control, and the child is not given the terminal for it.
const NeutralPromptLine = "(a prompt for a secret is not shown: whr asks for passwords itself, without echo)"

// NewToolWriter returns a ToolWriter on w.
func NewToolWriter(w io.Writer, s Style) *ToolWriter { return &ToolWriter{w: w, s: s, atStart: true} }

// Write implements io.Writer. An incomplete UTF-8 character at the end is held
// until the next Write (or End), so one split across writes stays whole.
func (t *ToolWriter) Write(p []byte) (int, error) {
	b := append(t.pend, p...)
	t.pend = nil
	if n := len(b); n > 0 && b[n-1] == '\r' {
		// A CR may be the first half of a CRLF split across writes.
		t.pend = []byte{'\r'}
		b = b[:n-1]
	}
	for i := len(b) - 1; t.pend == nil && i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				t.pend = append([]byte(nil), b[i:]...)
				b = b[:i]
			}
			break
		}
	}
	if err := t.write(b); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *ToolWriter) write(p []byte) error {
	mark := "|"
	if t.s.Unicode {
		mark = "│"
	}
	for _, seg := range strings.SplitAfter(string(p), "\n") {
		if seg == "" {
			continue
		}
		var out string
		nl := strings.HasSuffix(seg, "\n")
		if t.skipNL {
			t.skipNL = false
			if strings.TrimRight(seg, "\r\n") == "" {
				continue
			}
		}
		if t.line += strings.TrimRight(seg, "\r\n"); secretPrompt.MatchString(t.line) {
			if !t.started {
				out += "    " + t.s.paint(RoleTool, "tool output:") + "\n"
				t.started = true
			}
			if !t.atStart {
				out += "\n"
			}
			out += "    " + t.s.paint(RoleTool, mark+" "+NeutralPromptLine) + "\n"
			t.line, t.atStart, t.skipNL = "", true, !nl
			if _, err := io.WriteString(t.w, out); err != nil {
				return err
			}
			continue
		}
		if nl {
			t.line = ""
		}
		if !t.started {
			out += "    " + t.s.paint(RoleTool, "tool output:") + "\n"
			t.started = true
		}
		if t.atStart {
			out += "    " + t.s.paint(RoleTool, mark) + " "
		}
		// Tool output is untrusted: an escape sequence or a bare carriage
		// return would act on the terminal, so every control is shown escaped.
		// The line ending (LF or CRLF) is the one thing kept, as LF.
		body := strings.TrimSuffix(seg, "\n")
		if strings.HasSuffix(seg, "\n") {
			body = strings.TrimSuffix(body, "\r")
		}
		out += textsafe.Escape(body)
		if strings.HasSuffix(seg, "\n") {
			out += "\n"
		}
		t.atStart = strings.HasSuffix(seg, "\n")
		if _, err := io.WriteString(t.w, out); err != nil {
			return err
		}
	}
	return nil
}

// End closes the block: the next write starts a new one with its label.
func (t *ToolWriter) End() {
	if len(t.pend) > 0 {
		b := t.pend
		t.pend = nil
		_ = t.write(b)
	}
	if t.started && !t.atStart {
		_, _ = io.WriteString(t.w, "\n")
	}
	t.started, t.atStart = false, true
}

// Header is the step header: "Step n of N: title" between rules.
func Header(s Style, n, total int, title string) string {
	dash := "-"
	if s.Unicode {
		dash = "─"
	}
	return s.paint(RoleHeader, fmt.Sprintf("%s%s Step %d of %d %s %s %s%s", dash, dash, n, total, dash, title, dash, dash)) + "\n"
}

// Section is the title of a group of reports, such as the host steps.
func Section(s Style, title string) string {
	dash := "="
	if s.Unicode {
		dash = "━"
	}
	return s.paint(RoleHeader, dash+dash+" "+title+" "+dash+dash) + "\n"
}

// Rule separates one part of the output from the next.
func Rule(s Style) string {
	dash := "-"
	if s.Unicode {
		dash = "─"
	}
	return strings.Repeat(dash, 60) + "\n"
}

// Legend says what the symbols, words and bars mean. It is the report and
// action lines themselves, so the legend cannot drift from the output.
func Legend(s Style) string {
	return "Legend (colour is only decoration: the words are always printed)\n" +
		Report(s, LevelOK, "checked and fine") +
		Report(s, LevelFail, "checked and wrong") +
		Report(s, LevelNotVerified, "not verified: nothing measured it") +
		Report(s, LevelWarn, "works, weaker than recommended") +
		Report(s, LevelSkipped, "left out, or not offered") +
		Action(s, "something you must do or answer") +
		Command(s, "a command you can copy")
}

// Counts is how many steps ended in each level.
type Counts struct{ OK, Fail, NotVerified, Warn, Skipped int }

// Summary is the one-line summary at the end.
func Summary(_ Style, c Counts) string {
	return fmt.Sprintf("Summary: %d ok, %d FAIL, %d not verified (?), %d WARN, %d skipped\n", c.OK, c.Fail, c.NotVerified, c.Warn, c.Skipped)
}

// TodoItem is one thing the person must do, with an optional command.
type TodoItem struct {
	Text     string
	Commands []string
}

// Todo is the numbered list "What you need to do now", for the end of a run. It
// is empty for no items.
func Todo(s Style, items []TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(s.paint(RoleTodo, "What you need to do now") + "\n")
	for i, it := range items {
		prefix := fmt.Sprintf("  %d. ", i+1)
		pad := strings.Repeat(" ", len(prefix))
		fmt.Fprintf(&b, "%s%s\n", prefix, indent(wrap(it.Text, len(prefix)), pad))
		for _, c := range it.Commands {
			fmt.Fprintf(&b, "%s%s\n", pad, s.paint(RoleCommand, "$ "+c))
		}
	}
	return b.String()
}

// Writer writes the kinds to a stream with one Style.
type Writer struct {
	W io.Writer
	S Style
}

func (w Writer) put(text string) { _, _ = io.WriteString(w.W, text) }

// Put writes text that is already rendered.
func (w Writer) Put(text string) { w.put(text) }

// Legend writes the legend.
func (w Writer) Legend() { w.put(Legend(w.S)) }

// Header writes a step header.
func (w Writer) Header(n, total int, title string) { w.put("\n" + Header(w.S, n, total, title)) }

// Report writes a report line.
func (w Writer) Report(l Level, text string) { w.put(Report(w.S, l, text)) }

// Action writes an ACTION line.
func (w Writer) Action(text string) { w.put(Action(w.S, text)) }

// KV writes a key and its value.
func (w Writer) KV(key, value string) { w.put(KV(w.S, key, value)) }

// Note writes a line of human text.
func (w Writer) Note(text string) { w.put(Note(w.S, text)) }

// Command writes a copyable command.
func (w Writer) Command(cmd string) { w.put(Command(w.S, cmd)) }

// Tool writes an outside tool's output apart from whr's own text.
func (w Writer) Tool(text string) { w.put(ToolOutput(w.S, text)) }

// Section writes a group title.
func (w Writer) Section(title string) { w.put("\n" + Section(w.S, title)) }

// Rule writes a separating rule.
func (w Writer) Rule() { w.put(Rule(w.S)) }

// Summary writes the one-line summary.
func (w Writer) Summary(c Counts) { w.put(Summary(w.S, c)) }

// Todo writes the closing numbered list.
func (w Writer) Todo(items []TodoItem) {
	if len(items) > 0 {
		w.put("\n" + Todo(w.S, items))
	}
}

// Question writes a prompt as an ACTION line, without a newline.
func (w Writer) Question(text string, d Default) {
	w.put(Question(w.S, text+" "+d.suffix()))
}
