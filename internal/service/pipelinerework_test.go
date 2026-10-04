package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

// reworkRig is a task with its first run stopped and an open prepare_failed or
// publish_failed question, a Pipeline over the workspace rig whose repository
// lookup counts every prepare or publish it is asked for, and a hook that calls
// Kick (as the reconciler's pass does) while the answer's new run is starting:
// after the answer is recorded and before the run is saved.
type reworkRig struct {
	*wsRig
	pipe     *Pipeline
	question domain.Decision
	work     atomic.Int32 // prepares and publishes the pipeline started

	mu     sync.Mutex
	probed bool
	kicked string
	kerr   error
}

func newReworkRig(t *testing.T, cause domain.DecisionCause) *reworkRig {
	t.Helper()
	r := &reworkRig{wsRig: newWsRigBlocking(t, false)}
	r.agent.Finish("done")
	_, a := r.create("rw")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	must(t, err)
	r.svc.Wait()
	agg, _ := r.store.LoadTask(bg, task)
	switch cause {
	case domain.CausePrepareFailed:
		r.question, err = agg.RaisePrepareFailed(run, "d-prep", "the check exited with status 1", r.svc.clock.Now())
		must(t, err)
	case domain.CausePublishFailed:
		_, err = agg.PinRevision(run, a.Branch, "aaa111")
		must(t, err)
		must(t, agg.MarkReady(false))
		_, err = agg.RaiseDecision(domain.NewDecision{ID: "rev", Kind: domain.DecisionReview, Blocking: true, SHA: "aaa111", Subject: "Ready to push?", Now: r.svc.clock.Now()})
		must(t, err)
		must(t, agg.Answer("rev", domain.Response{By: "w", Option: domain.AnswerAllow, SHA: "aaa111", At: r.svc.clock.Now()}))
		r.question, err = agg.RaisePublishFailed("d-pub", "the branch is not a fast-forward", r.svc.clock.Now())
		must(t, err)
	}
	_, err = r.store.SaveTask(bg, agg)
	must(t, err)

	r.pipe = NewPipeline(r.svc, r.ws, PipelineConfig{
		Publish: func(context.Context, string) (PublishConfig, error) {
			r.work.Add(1)
			return PublishConfig{}, errors.New("not set up")
		},
	})
	r.agent.Block() // the new run keeps running, so nothing prepares it
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) > 0 && cmd[0] == "echo" { // the readiness probe of the starting environment
			r.mu.Lock()
			if !r.probed {
				r.probed = true
				r.kicked, r.kerr = r.pipe.Kick(bg, task)
			}
			r.mu.Unlock()
		}
		return nil, "", 0, false
	}
	return r
}

// Answering rework keeps Kick, and so the reconciler, off the task from the
// answer until the new run is saved: the old approval must not be published, the
// old run must not be prepared again, and the new run must not be refused as busy
// (design §4.5).
func TestReworkKeepsThePipelineOffTheTaskUntilTheNewRunIsSaved(t *testing.T) {
	t.Parallel()
	for name, cause := range map[string]domain.DecisionCause{
		"publish_failed": domain.CausePublishFailed,
		"prepare_failed": domain.CausePrepareFailed,
	} {
		t.Run(name, func(t *testing.T) {
			r := newReworkRig(t, cause)
			// the reconciler's tick does not mark the environment started: the new
			// run's start goes through the restart and readiness path
			run, err := r.ws.Answer(bg, r.question.ID, domainRework(r))
			if err != nil || run == "" {
				t.Fatalf("rework = %q, %v (errors %v)", run, err, r.reported())
			}
			r.mu.Lock()
			probed, kicked, kerr := r.probed, r.kicked, r.kerr
			r.mu.Unlock()
			if !probed {
				t.Fatal("the new run's start never probed the environment: the test did not reach the window")
			}
			if kicked != "" || kerr != nil || r.work.Load() != 0 {
				t.Errorf("Kick between the answer and the saved run started %q, %v (%d prepares or publishes)", kicked, kerr, r.work.Load())
			}
			if v, _ := r.svc.Show(bg, r.question.TaskID); len(v.Runs) != 2 || v.Task.State != domain.TaskRunning {
				t.Errorf("runs %d, task %s: the rework was lost", len(v.Runs), v.Task.State)
			}
			if r.pipe.reworkHeld(r.question.TaskID) {
				t.Error("the guard outlived the answer")
			}
		})
	}
}

func domainRework(r *reworkRig) domain.Response {
	return domain.Response{By: "w", Option: domain.AnswerRework, At: r.svc.clock.Now()}
}

// The guard is released on every path: a rework that fails to start, a decision
// that does not exist, and a panic.
func TestTheReworkGuardIsReleasedOnEveryPath(t *testing.T) {
	t.Parallel()
	r := newReworkRig(t, domain.CausePrepareFailed)
	task := r.question.TaskID

	release := r.pipe.holdRework(task)
	second := r.pipe.holdRework(task)
	release()
	release() // releasing twice must not drop the other hold
	if !r.pipe.reworkHeld(task) {
		t.Error("one release dropped both holds")
	}
	second()
	if r.pipe.reworkHeld(task) {
		t.Error("the guard outlived its releases")
	}

	if _, err := r.ws.Answer(bg, "no-such-decision", domainRework(r)); err == nil {
		t.Error("an unknown decision was answered")
	}
	if r.pipe.reworkHeld(task) {
		t.Error("a failed answer left the guard")
	}

	// the new run cannot start (a cancelled context): the answer is recorded, the
	// question raised again, and nothing is left held
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, _ = r.ws.Answer(ctx, r.question.ID, domainRework(r))
	if r.pipe.reworkHeld(task) {
		t.Error("a rework that could not start left the guard")
	}

	func() {
		defer func() { _ = recover() }()
		defer r.pipe.holdRework(task)()
		panic("boom")
	}()
	if r.pipe.reworkHeld(task) {
		t.Error("a panic left the guard")
	}
}
