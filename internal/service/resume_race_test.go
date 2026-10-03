package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// gateRuntime holds the first Exec until the test releases it, so a test can
// act while recovery waits for the environment to answer.
type gateRuntime struct {
	runtime.Adapter
	entered, release chan struct{}
}

func (g *gateRuntime) Exec(ctx context.Context, id string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	close(g.entered)
	<-g.release
	return g.Adapter.Exec(ctx, id, req)
}

// F1 of the 2026-10-03 advisory review (#202, design 4.1 "one live agent per
// run"): recovery waits for the environment while the human resumes the run.
// Recovery loses the race and changes nothing: the live session stays attached,
// no error is reported, and Cancel stops the agent.
func TestRecoveryThatLosesToAManualResumeLeavesTheLiveAgentAttached(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	gate := &gateRuntime{Adapter: r.rt.Adapter, entered: make(chan struct{}), release: make(chan struct{})}
	r.svc.rt = gate

	type result struct {
		rep Report
		err error
	}
	done := make(chan result, 1)
	go func() {
		var rep Report
		err := r.svc.recover(bg, "t1", "r1", &rep)
		done <- result{rep, err}
	}()
	<-gate.entered // recovery has read the run and waits for exec

	r.agent.Block()
	_, err := r.svc.Resume(bg, "t1")
	must(t, err)
	close(gate.release)
	res := <-done

	if res.err != nil || len(res.rep.Resumed) != 0 || len(res.rep.Failed) != 0 {
		t.Errorf("the losing recovery changed something: %+v, error %v", res.rep, res.err)
	}
	if !r.svc.attached("r1") {
		t.Fatal("the live session is no longer attached")
	}
	before := r.agent.Stops()
	must(t, r.svc.Cancel(bg, "t1"))
	if n := r.agent.Stops(); n != before+1 {
		t.Errorf("Cancel did not stop the live agent: %d stops, want %d", n, before+1)
	}
	r.svc.Wait()
}

// A session slot is never replaced: taking an occupied one is refused, and the
// refused caller's end leaves the holder's slot alone.
func TestASessionSlotIsNeverReplaced(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	holder, err := r.svc.begin("r1")
	must(t, err)
	if _, err := r.svc.begin("r1"); err == nil {
		t.Fatal("begin replaced an occupied slot")
	} else if !errors.Is(err, errRunAttached) {
		t.Errorf("begin refused with %v, want errRunAttached", err)
	}
	if !r.svc.attached("r1") {
		t.Fatal("the refused begin removed the holder's slot")
	}
	r.svc.end("r1", holder)
	if r.svc.attached("r1") {
		t.Error("the slot was not removed by its own holder")
	}
}

// mustBegin takes the session slot of run r1.
func mustBegin(t *testing.T, s *Service) *slot {
	t.Helper()
	sl, err := s.begin("r1")
	if err != nil {
		t.Fatal(err)
	}
	return sl
}

// The supervisor dies after a resume wrote starting and before the session was
// attached: the slot died with the process, so the next start takes the run for
// lost and resumes it, once.
func TestACrashBetweenTheStartingWriteAndTheAttachIsRecovered(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		if err := a.Pause("r1"); err != nil {
			return err
		}
		return a.Resume("r1") // starting, with no slot and no session: the crash
	}))
	if got := r.runState(); got != domain.RunStarting {
		t.Fatalf("run %s, want starting", got)
	}
	r.agent.Block()
	rep := r.reconcile()
	if len(rep.Interrupted) != 1 || len(rep.Resumed) != 1 || r.runState() != domain.RunRunning || !r.svc.attached("r1") {
		t.Errorf("report %+v, run %s, attached %v", rep, r.runState(), r.svc.attached("r1"))
	}
	r.svc.Shutdown()
}

// The third path to starting, an answer that resumes (design 4.1): while a
// launch or session holds the run's slot the answer is refused before it
// changes anything, and the holder keeps its slot.
func TestAnAnswerThatResumesNeverReplacesAnAttachedSession(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	holder := mustBegin(t, r.svc) // a launch of the run is in progress

	err = r.svc.AnswerDecision(bg, "auth1", domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now})
	var c *domain.ConflictError
	if !errors.As(err, &c) {
		t.Fatalf("answer = %v, want a conflict", err)
	}
	if !r.svc.attached("r1") {
		t.Error("the answer replaced or removed the holder's slot")
	}
	if got := r.runState(); got == domain.RunStarting || got == domain.RunRunning {
		t.Errorf("the refused answer moved the run to %s", got)
	}
	if d, lerr := r.store.LoadDecision(bg, "auth1"); lerr != nil || d.Status != domain.DecisionOpen {
		t.Errorf("the decision was changed: %v %v", d, lerr)
	}
	r.svc.end("r1", holder)
}
