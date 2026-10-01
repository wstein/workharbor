package domain

import (
	"errors"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

func TestIllegalTransitionsAreConflicts(t *testing.T) {
	errs := map[string]error{
		"task":        (&Task{ID: "t1", State: TaskCompleted}).transition(TaskRunning),
		"run":         (&Run{ID: "r1", State: RunStopped}).transition(RunRunning),
		"environment": (&Environment{ID: "e1", State: EnvDeleted}).transition(EnvStopped),
	}
	for name, err := range errs {
		if err == nil {
			t.Fatalf("%s: illegal transition was allowed", name)
		}
		if got := exitcode.From(err); got != exitcode.Conflict {
			t.Errorf("%s: exit code = %d, want Conflict (%d)", name, got, exitcode.Conflict)
		}
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.Rule != RuleTransition {
			t.Errorf("%s: error = %v, want a transition conflict", name, err)
		}
	}
}

func TestNotFoundMapsToNotFound(t *testing.T) {
	err := error(&NotFoundError{Kind: "run", ID: "r9"})
	if got := exitcode.From(err); got != exitcode.NotFound {
		t.Errorf("exit code = %d, want NotFound (%d)", got, exitcode.NotFound)
	}
	if err.Error() != "run r9 not found" {
		t.Errorf("message = %q", err)
	}
}

func TestNewConflictAndErrNotFound(t *testing.T) {
	err := error(NewConflict("stale", "task %s changed", "t1"))
	if got := exitcode.From(err); got != exitcode.Conflict || err.Error() != "task t1 changed" {
		t.Errorf("NewConflict = %q, exit %d", err, got)
	}
	if !errors.Is(&NotFoundError{Kind: "task", ID: "t9"}, ErrNotFound) {
		t.Error("a *NotFoundError must match ErrNotFound")
	}
	if errors.Is(errors.New("something else"), ErrNotFound) {
		t.Error("an unrelated error must not match ErrNotFound")
	}
}
