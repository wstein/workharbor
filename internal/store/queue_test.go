package store

import (
	"errors"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

// The check for an unfinished task of the issue and the save are one transaction: of two
// tasks for one issue only the first is saved, and a finished task does not block.
func TestSaveNewTaskOnceRefusesAnIssueWithAnUnfinishedTask(t *testing.T) {
	s := openTemp(t)
	mk := func(id domain.ID, repo string) *domain.TaskAggregate {
		return domain.NewTaskAggregate(domain.Task{ID: id, Repo: repo, Issue: "#7", State: domain.TaskQueued, CreatedAt: t0})
	}
	if _, err := s.SaveNewTaskOnce(bg, mk("t1", "wstein/workharbor")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNewTaskOnce(bg, mk("t2", "WStein/Workharbor")); !errors.Is(err, ErrIssueActive) {
		t.Fatalf("a second task for the issue: %v", err)
	}
	if _, err := s.SaveNewTaskOnce(bg, mk("t3", "wstein/other")); err != nil {
		t.Errorf("another repository: %v", err)
	}
}
