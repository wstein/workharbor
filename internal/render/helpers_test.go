package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func widest(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		if n := utf8.RuneCountInString(l); n > w {
			w = n
		}
	}
	return w
}

func TestKVAlignsContinuationToTheValueColumn(t *testing.T) {
	val := strings.TrimSpace(strings.Repeat("com.apple.group ", 12))
	got := KV(Style{}, "groups", val)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 || widest(got) > 80 {
		t.Fatalf("not wrapped to 80:\n%s", got)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "        com") {
			t.Errorf("continuation not under the value: %q", l)
		}
	}
	if strings.Join(strings.Fields(got), " ") != "groups "+val {
		t.Errorf("text changed:\n%s", got)
	}
	if got := KV(Style{}, "uid", "502"); got != "uid  502\n" {
		t.Errorf("short row: %q", got)
	}
}

func TestNoteIndentsByThePrefixWidth(t *testing.T) {
	got := Note(Style{}, "note: sysadminctl "+strings.Repeat("word ", 30))
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 || widest(got) > 80 {
		t.Fatalf("not wrapped to 80:\n%s", got)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "      word") {
			t.Errorf("continuation not behind the prefix: %q", l)
		}
	}
}

func TestWidthIsTheTerminalCappedAt80(t *testing.T) {
	text := strings.Repeat("word ", 30)
	if w := widest(Note(DetectEnv(Env{TTY: true, Cols: 200}), text)); w > 80 || w < 70 {
		t.Errorf("wide terminal: %d", w)
	}
	if w := widest(Note(DetectEnv(Env{TTY: true, Cols: 50}), text)); w > 50 {
		t.Errorf("narrow terminal: %d", w)
	}
	if w := widest(Note(DetectEnv(Env{Cols: 50}), text)); w < 70 || w > 80 {
		t.Errorf("not a terminal is 80: %d", w)
	}
	if w := widest(Action(DetectEnv(Env{TTY: true, Cols: 50, Plain: true}), text)); w > 50 {
		t.Errorf("action on a narrow terminal: %d", w)
	}
}

func TestCmdIsNeverWrapped(t *testing.T) {
	short := Cmd(Style{}, "ls -l")
	if short != "\n    ls -l\n\n" {
		t.Errorf("short: %q", short)
	}
	long := "sudo dseditgroup -o edit -d workharbor -t user " + strings.Repeat("com.apple.access_ssh ", 5)
	got := Cmd(Style{}, long)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 || lines[0] != "" || lines[1] != "    "+long || lines[2] != "" {
		t.Errorf("long command:\n%s", got)
	}
}
