package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

func TestTheCheckLeavesAReceiptBoundToThePreparedCommit(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.onCheck = func(runtime.ExecRequest) (string, int) { return "all good\n", 0 }
	c.clock.now = t0
	prepared, err := c.pub.Prepare(bg, c.req)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.svc.checkReceipt(bg, "t1", prepared.SHA)
	if err != nil || r == nil {
		t.Fatalf("receipt = %+v, %v", r, err)
	}
	if r.SHA != prepared.SHA || r.Command != "make check" || r.Source != CheckFromConfig || !r.Passed() || r.Output != "all good\n" {
		t.Errorf("receipt = %+v", r)
	}
	// "Ready to push?" shows it.
	var review domain.Decision
	for _, d := range c.load().Decisions() {
		if d.Kind == domain.DecisionReview {
			review = d
		}
	}
	if !strings.HasPrefix(review.Input, "check make check (from config): passed in ") || !strings.Contains(review.Input, "all good") {
		t.Errorf("the question's input = %q", review.Input)
	}
	// A receipt for another commit is not this one's.
	if other, _ := c.svc.checkReceipt(bg, "t1", strings.Repeat("e", 40)); other != nil {
		t.Errorf("a receipt for another commit: %+v", other)
	}
}

func TestAFailedCheckLeavesAReceiptWithItsCappedUntrustedOutput(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.onCheck = func(runtime.ExecRequest) (string, int) {
		return strings.Repeat("x", 3*ReceiptOutputMax) + "tail \x1b[31mred\n", 4
	}
	if _, err := c.pub.Prepare(bg, c.req); err == nil {
		t.Fatal("Prepare passed")
	}
	evs, err := c.store.EventsOfKind(bg, "t1", domain.EventCheckReceipt)
	if err != nil || len(evs) != 1 {
		t.Fatalf("receipts = %v, %v", evs, err)
	}
	var r domain.CheckReceipt
	if err := json.Unmarshal(evs[0].Payload, &r); err != nil {
		t.Fatal(err)
	}
	if r.Code != 4 || r.Passed() || len(r.Output) > ReceiptOutputMax || !strings.HasSuffix(r.Output, "tail [31mred\n") || strings.ContainsRune(r.Output, 0x1b) {
		t.Errorf("receipt = code %d, %d bytes of output, tail %q", r.Code, len(r.Output), r.Output[len(r.Output)-20:])
	}
	// No decision for a commit whose check failed.
	if len(c.load().Decisions()) != 0 {
		t.Error("a question was raised")
	}
}

func TestTheReceiptNamesWhereTheCommandCameFrom(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.chk.cfg.Command = func(string) string { return "" }
	c.chk.cfg.Source = nil
	if _, err := c.pub.Prepare(bg, c.req); err == nil {
		t.Fatal("Prepare passed without a check")
	}
	if evs, _ := c.store.EventsOfKind(bg, "t1", domain.EventCheckReceipt); len(evs) != 0 {
		t.Errorf("a check that never ran left a receipt: %v", evs)
	}
}

func TestTailOfKeepsWholeCharacters(t *testing.T) {
	t.Parallel()
	if got := tailOf("aäb", 2); got != "b" {
		t.Errorf("tailOf = %q, want a cut that starts at a character", got)
	}
	if got := tailOf("abc", 10); got != "abc" {
		t.Errorf("tailOf = %q", got)
	}
	if got := tailRunes("aäb", 2); got != "äb" {
		t.Errorf("tailRunes = %q", got)
	}
}
