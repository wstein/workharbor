package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// holdLaunchAgent blocks every Resume of the agent until the test releases it,
// as an agent whose launch wedges does (#225).
type holdLaunchAgent struct {
	agent.Adapter
	entered, release         chan struct{}
	enteredOnce, releaseOnce sync.Once
}

func (h *holdLaunchAgent) open() { h.releaseOnce.Do(func() { close(h.release) }) }

func (h *holdLaunchAgent) Resume(ctx context.Context, spec agent.StartSpec, id string) (agent.Session, error) {
	h.enteredOnce.Do(func() { close(h.entered) })
	<-h.release
	return h.Adapter.Resume(ctx, spec, id)
}

// holdLaunch makes the rig's launches block until the returned agent opens.
func (r *rig) holdLaunch() *holdLaunchAgent {
	r.t.Helper()
	h := &holdLaunchAgent{Adapter: r.svc.ag, entered: make(chan struct{}), release: make(chan struct{})}
	r.t.Cleanup(h.open)
	r.svc.ag = h
	return h
}

// async runs f in a goroutine and returns the channel of its error.
func async(f func() error) <-chan error {
	c := make(chan error, 1)
	go func() { c <- f() }()
	return c
}

// A run lock wait ends with its context, and a cancelled wait leaves no lock.
func TestLockRunWaitEndsWithTheContext(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	unlock, err := r.svc.lockRun(bg, "r1")
	must(t, err)
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if _, err := r.svc.lockRun(ctx, "r1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a wait behind a holder = %v, want the context's error", err)
	}
	unlock()
	unlock() // releasing twice is harmless
	u2, err := r.svc.lockRun(bg, "r1")
	must(t, err)
	u2()
	r.svc.mu.Lock()
	n := len(r.svc.runLocks)
	r.svc.mu.Unlock()
	if n != 0 {
		t.Errorf("%d run locks left over", n)
	}
}

// A recovery whose launch blocks does not stall a human's resume (#225): the
// resume is refused at once, because the launch holds the run's slot.
func TestAResumeIsNotStalledByARecoveryThatIsLaunching(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	r.agent.Block() // the launched agent keeps running; an unscripted fake finishes at once and stops the run
	h := r.holdLaunch()
	rec := async(func() error { var rep Report; return r.svc.recover(bg, "t1", "r1", &rep) })
	waitFor(t, h.entered, "the recovery to reach its launch")

	res := async(func() error { _, err := r.svc.Resume(bg, "t1"); return err })
	var c *domain.ConflictError
	if err := waitFor(t, res, "the resume not to wait for the launch"); !errors.As(err, &c) {
		t.Errorf("resume = %v, want a conflict", err)
	}
	h.open()
	must(t, waitFor(t, rec, "the recovery to finish"))
	if r.runState() != domain.RunRunning {
		t.Errorf("run %s, want running", r.runState())
	}
}

// A resume whose launch blocks does not stall the reconciler's recovery of the
// same run (#225): the recovery loses the race and changes nothing.
func TestARecoveryIsNotStalledByAResumeThatIsLaunching(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	gate := newGateRuntime(t, r.rt.Adapter)
	r.svc.rt = gate
	rec := async(func() error { var rep Report; return r.svc.recover(bg, "t1", "r1", &rep) })
	waitFor(t, gate.entered, "the recovery to reach the environment's exec")

	h := r.holdLaunch()
	res := async(func() error { _, err := r.svc.Resume(bg, "t1"); return err })
	waitFor(t, h.entered, "the resume to reach its launch")
	gate.open()
	if err := waitFor(t, rec, "the recovery not to wait for the launch"); err != nil {
		t.Errorf("the losing recovery = %v", err)
	}
	h.open()
	must(t, waitFor(t, res, "the resume to finish"))
	r.svc.Wait()
}

// An answer whose launch blocks stalls neither a human's resume nor a recovery
// of the run, and the run stays refused a second agent (#225).
func TestAnAnswerThatIsLaunchingStallsNeitherResumeNorRecovery(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	r.agent.Block() // the answer's agent keeps running, so the run stays running and attached
	h := r.holdLaunch()
	ans := async(func() error {
		return r.svc.AnswerDecision(bg, "auth1", domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now})
	})
	waitFor(t, h.entered, "the answer to reach its launch")

	res := async(func() error { _, err := r.svc.Resume(bg, "t1"); return err })
	if err := waitFor(t, res, "the resume not to wait for the launch"); err == nil {
		t.Error("a resume during an answer's launch was accepted")
	}
	rec := async(func() error { var rep Report; return r.svc.recover(bg, "t1", "r1", &rep) })
	must(t, waitFor(t, rec, "the recovery not to wait for the launch"))
	h.open()
	must(t, waitFor(t, ans, "the answer to finish"))
	if r.runState() != domain.RunRunning || !r.svc.attached("r1") {
		t.Errorf("run %s, attached %v", r.runState(), r.svc.attached("r1"))
	}
}
