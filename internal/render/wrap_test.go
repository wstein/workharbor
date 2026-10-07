package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWrap(t *testing.T) {
	short := "a short line"
	if got := Wrap(short, 10); got != short {
		t.Errorf("short line changed: %q", got)
	}
	long := strings.Repeat("word ", 40)
	got := Wrap(long, 10)
	for _, l := range strings.Split(got, "\n") {
		if utf8.RuneCountInString(l)+10 > 80 {
			t.Errorf("line too wide: %q", l)
		}
	}
	if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(long), " ") {
		t.Error("words lost")
	}
	if again := Wrap(got, 10); again != got {
		t.Errorf("not idempotent:\n%s\n%s", got, again)
	}
	huge := strings.Repeat("x", 100)
	if got := Wrap("see "+huge+" now and more words to push this line past the limit of eighty", 5); !strings.Contains(got, huge) {
		t.Errorf("long word split: %q", got)
	}
	cmd := strings.Repeat("sudo thing ", 12)
	if got := Command(Style{}, cmd); !strings.Contains(got, "$ "+cmd+"\n") {
		t.Errorf("command wrapped: %q", got)
	}
}
