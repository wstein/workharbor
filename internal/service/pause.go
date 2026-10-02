package service

import (
	"context"
	"errors"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

// Pause pauses a task's running run (design §4.1, D11). None of the agents has a
// cooperative pause, so it is a hard interrupt: the run becomes paused, the open
// questions and approvals it raised are superseded (D23), and the agent process
// is stopped through the runtime (whr-shim cancels its process group, D25). The
// environment keeps running, so the human can look at or edit the workspace. The
// agent's own session stays, and Resume continues from it.
func (s *Service) Pause(ctx context.Context, task domain.ID) error {
	var run domain.ID
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		r, ok := a.LiveRun()
		if !ok {
			return domain.NewConflict(domain.RuleTransition, "task %s has no run to pause", task)
		}
		run = r.ID
		return a.Pause(r.ID)
	})
	if err != nil {
		return err
	}
	// The run is already paused, so the session's end is not taken for a loss
	// (finish leaves a paused run alone).
	s.stopSession(run)
	return nil
}

// Resume starts a task's paused or interrupted run again from the agent's
// session, with the supervisor's briefing (D27) that says what was superseded.
// It is refused while a login or quota question of the run is open: answer it, or
// cancel. A session the agent has forgotten, or attempts that are used up, fail
// the run and open the retry-or-cancel question, as the answer to a question
// that resumes does. It returns the run.
func (s *Service) Resume(ctx context.Context, task domain.ID) (domain.ID, error) {
	var run domain.ID
	var sl *slot
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		r, ok := a.LiveRun()
		if !ok {
			return domain.NewConflict(domain.RuleTransition, "task %s has no run to resume", task)
		}
		if s.attached(r.ID) {
			return domain.NewConflict(domain.RuleTransition, "run %s is already running", r.ID)
		}
		run = r.ID
		if err := a.Resume(r.ID); err != nil {
			return err
		}
		// Before the change is saved, so the reconciler does not see a starting run
		// with no session and take it for lost.
		if sl != nil { // the change is tried again after a stale write
			s.end(r.ID, sl)
		}
		sl = s.begin(r.ID)
		return nil
	})
	if err != nil {
		if sl != nil {
			s.end(run, sl)
		}
		return "", err
	}
	if lerr := s.launch(ctx, task, run, sl); lerr != nil {
		if errors.Is(lerr, agent.ErrNoSession) || errors.Is(lerr, errAttemptsUsedUp) {
			var rep Report
			return run, errors.Join(lerr, s.failRun(ctx, task, run, &rep))
		}
		return run, lerr
	}
	return run, nil
}

// TranscriptSize is what a purge of a task's transcript would delete.
type TranscriptSize struct {
	Events int
	Bytes  int64
}

// TranscriptSize counts the transcript content of a task, for the confirmation
// that says what a purge deletes.
func (s *Service) TranscriptSize(ctx context.Context, task domain.ID) (TranscriptSize, error) {
	if _, err := s.store.LoadTask(ctx, task); err != nil {
		return TranscriptSize{}, err
	}
	n, b, err := s.store.TranscriptSize(ctx, task)
	return TranscriptSize{Events: n, Bytes: b}, err
}

// PurgeTranscript deletes the stored transcript content of a task (design §5.4):
// the assistant's text, tool inputs and results, diffs and thinking. It keeps the
// audit entries, the usage rows and the Decisions, and the agent's own session,
// and it records itself with one audit entry: who, when, how many events and
// bytes, and a digest of what went. It is refused while the task's run is
// running, because the transcript is still being written: pause or stop it first.
func (s *Service) PurgeTranscript(ctx context.Context, task domain.ID, actor string) (store.PurgeResult, error) {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return store.PurgeResult{}, err
	}
	for _, r := range agg.Runs() {
		if r.State == domain.RunRunning || r.State == domain.RunStarting {
			return store.PurgeResult{}, domain.NewConflict(domain.RuleTransition, "run %s is %s and still writing its transcript: pause or stop it first", r.ID, r.State)
		}
	}
	res, err := s.store.Purge(ctx, store.PurgeSpec{TaskID: task, Actor: actor, All: true})
	if err != nil {
		return store.PurgeResult{}, err
	}
	s.publish([]domain.Event{res.Audit})
	return res, nil
}
