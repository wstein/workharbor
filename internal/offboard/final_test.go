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
	ok := render.FinalBlock(s, finalOf(f, exitcode.OK, "/tmp/off.log"))
	for _, w := range []string{"Deleted the account workharbor", "/tmp/off.log"} {
		if !strings.Contains(ok, w) {
			t.Errorf("missing %q in\n%s", w, ok)
		}
	}
	bad := render.FinalBlock(s, finalOf(f, exitcode.Error, ""))
	if !strings.Contains(bad, "ACTION") || strings.Contains(bad, "log  ") {
		t.Errorf("failed run:\n%s", bad)
	}
}
