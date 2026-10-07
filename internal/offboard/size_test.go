package offboard

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
)

const duKey = "/usr/bin/du -skx /Users/workharbor"

func TestADUAnswerWithTrailingSpaceOrCRLFIsUnknown(t *testing.T) {
	for _, out := range []string{"7\t/Users/workharbor \n", "7\t/Users/workharbor\r\n", "7\t/Users/workharbor\n\n"} {
		if _, ok := parseDU(out, "/Users/workharbor"); ok {
			t.Errorf("%q taken", out)
		}
	}
}

func TestADUErrorIsUnknownEvenWithOutput(t *testing.T) {
	h := newHost()
	h.before[duKey] = "7\t/Users/workharbor\n"
	h.errs[duKey] = errors.New("exit status 1")
	f := Inspect(context.Background(), h.deps(), inv())
	if f.HomeSizeKnown {
		t.Errorf("a failed du was trusted")
	}
}

func TestADifferentNoteStartingWithTheStillBlocksTheRecheck(t *testing.T) {
	h := newHost()
	f := Inspect(context.Background(), h.deps(), inv())
	f.Notes = append(f.Notes, "the home folder does not exist: nothing of it is removed")
	var so, se bytes.Buffer
	h.log = &se
	lg := Log{W: &se, Now: time.Now, Whr: "t"}
	if c := Execute(context.Background(), h, h.deps(), f, lg, Out{Out: &so, Err: &se}); c != exitcode.Conflict {
		t.Errorf("note drift did not block: %d", c)
	}
}

func TestASymlinkedHomeIsNotMeasured(t *testing.T) {
	h := newHost()
	d := h.deps()
	d.Stat = func(string) (HomeInfo, error) {
		return HomeInfo{Symlink: true, Dir: true, UID: 502, UIDKnown: true}, nil
	}
	f := Inspect(context.Background(), d, inv())
	for _, r := range h.reads {
		if strings.Contains(r, "du ") || strings.Contains(r, "mount") {
			t.Errorf("read through a symlinked home: %s", r)
		}
	}
	if f.HomeSizeKnown {
		t.Errorf("size known")
	}
}
