package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent/claude"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// wedgedRunner is a runtime whose Exec never returns and ignores its context,
// the worst case of a wedged guest exec (#279).
type wedgedRunner struct{ release chan struct{} }

func (w wedgedRunner) Exec(context.Context, string, runtime.ExecRequest) (runtime.ExecStream, error) {
	<-w.release
	return nil, errors.New("released")
}

// A wedged instruction-file check fails the launch within its deadline: the
// run is saved interrupted, the slot is free and a later resume is accepted
// (#279).
func TestAWedgedInstructionCheckFailsTheLaunchAndFreesTheSlot(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	orig := r.svc.ag
	w := wedgedRunner{release: make(chan struct{})}
	t.Cleanup(func() { close(w.release) })
	r.svc.ag = claude.New(w, claude.Config{InstructionCheckTimeout: 50 * time.Millisecond})

	res := async(func() error { _, err := r.svc.Resume(bg, "t1"); return err })
	err := waitFor(t, res, "the launch to fail within the deadline")
	if !errors.Is(err, claude.ErrInstructionCheckTimeout) {
		t.Fatalf("resume = %v, want the instruction-check timeout", err)
	}
	if r.runState() != domain.RunInterrupted {
		t.Fatalf("run %s, want interrupted", r.runState())
	}
	if r.svc.attached("r1") {
		t.Fatal("the run's session slot is still taken")
	}

	r.svc.ag = orig
	if _, err := r.svc.Resume(bg, "t1"); err != nil {
		t.Fatalf("a later resume = %v, want accepted", err)
	}
	r.svc.Wait()
}
