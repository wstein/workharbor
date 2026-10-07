package offboard

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
)

func TestFinalOfNamesChangeLogAndNextStep(t *testing.T) {
	f := Facts{Account: "workharbor"}
	s := render.Detect(false, "", false)
	ok := render.FinalBlock(s, finalOf(f, exitcode.OK))
	for _, w := range []string{"Deleted the account workharbor"} {
		if !strings.Contains(ok, w) {
			t.Errorf("missing %q in\n%s", w, ok)
		}
	}
	bad := render.FinalBlock(s, finalOf(f, exitcode.Error))
	if !strings.Contains(bad, "ACTION") {
		t.Errorf("failed run:\n%s", bad)
	}
}

func TestNewOffboardLinesFitIn80Columns(t *testing.T) {
	f := Facts{Account: "workharbor", HomeDir: "/Users/workharbor"}
	var so, se strings.Builder
	o := Out{Out: &so, Err: &se}
	Plan(o, f)
	se.WriteString(render.FinalBlock(render.Detect(false, "", false),
		finalOf(f, exitcode.Error)))
	for _, l := range strings.Split(se.String(), "\n") {
		if len([]rune(l)) > 80 {
			t.Errorf("over 80 columns (%d): %q", len([]rune(l)), l)
		}
	}
	if !strings.Contains(se.String(), "preflight:") {
		t.Errorf("no preflight line:\n%s", se.String())
	}
}
