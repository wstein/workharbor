package domain

import (
	"fmt"
	"sort"
)

// DecisionState is one Decision in a Snapshot.
type DecisionState struct {
	Decision Decision
	// Changed is set when the Decision changed since it was last saved, so the
	// store writes only those.
	Changed bool
}

// Snapshot is a task aggregate as plain data, for the store to write. It is a
// copy: changing it changes nothing in the aggregate.
type Snapshot struct {
	Task       Task
	Runs       []Run // oldest first
	Envs       []Environment
	Candidates []ReviewCandidate // oldest first
	Decisions  []DecisionState   // oldest first
}

// Snapshot returns a copy of the aggregate's state.
func (a *TaskAggregate) Snapshot() Snapshot {
	snap := Snapshot{Task: a.task, Envs: a.Environments()}
	for _, r := range a.runs {
		snap.Runs = append(snap.Runs, *r)
	}
	for _, c := range a.candidates {
		snap.Candidates = append(snap.Candidates, *c)
	}
	for _, d := range a.decisions {
		snap.Decisions = append(snap.Decisions, DecisionState{Decision: *d, Changed: d.changed})
	}
	return snap
}

// Restore rebuilds an aggregate from stored state, as a store does after a
// load. It checks the pieces against each other, because a damaged store must
// not produce an aggregate whose guards no longer hold: unique IDs, runs in
// known environments, at most one live run, candidates and Decisions that
// name runs of the task. The result has no recorded events.
func Restore(snap Snapshot) (*TaskAggregate, error) {
	a := NewTaskAggregate(snap.Task)
	bad := func(format string, args ...any) error {
		return fmt.Errorf("restore task %s: %s", snap.Task.ID, fmt.Sprintf(format, args...))
	}
	for _, e := range snap.Envs {
		if _, dup := a.envs[e.ID]; dup || e.ID == "" {
			return nil, bad("environment %q is empty or repeated", e.ID)
		}
		a.AddEnvironment(e)
	}
	seenRun := map[ID]bool{}
	for _, r := range snap.Runs {
		if r.ID == "" || seenRun[r.ID] {
			return nil, bad("run %q is empty or repeated", r.ID)
		}
		seenRun[r.ID] = true
		if _, ok := a.envs[r.EnvID]; !ok {
			return nil, bad("run %s is in an unknown environment %q", r.ID, r.EnvID)
		}
		run := r
		a.runs = append(a.runs, &run)
	}
	live := 0
	for _, r := range a.runs {
		if !r.State.Terminal() {
			live++
		}
	}
	if live > 1 {
		return nil, bad("%d live runs", live)
	}
	seenSHA := map[string]bool{}
	for _, c := range snap.Candidates {
		if !seenRun[c.RunID] || seenSHA[c.SHA] {
			return nil, bad("candidate %q is for an unknown run or repeated", c.SHA)
		}
		seenSHA[c.SHA] = true
		cand := c
		a.candidates = append(a.candidates, &cand)
	}
	seenDec := map[ID]bool{}
	for _, ds := range snap.Decisions {
		d := ds.Decision
		if d.ID == "" || seenDec[d.ID] || d.TaskID != snap.Task.ID {
			return nil, bad("decision %q is empty, repeated or for another task", d.ID)
		}
		if d.RunID != "" && !seenRun[d.RunID] {
			return nil, bad("decision %s is for an unknown run %q", d.ID, d.RunID)
		}
		seenDec[d.ID] = true
		d.events, d.changed = nil, ds.Changed
		a.decisions = append(a.decisions, &d)
	}
	return a, nil
}

// MarkSaved tells the aggregate the store has written it: the task has the new
// version, the saved Decisions have theirs and are no longer changed, and the
// recorded events are forgotten, since the store wrote them with the state.
func (a *TaskAggregate) MarkSaved(taskVersion int64, decisions map[ID]int64) {
	a.task.Version = taskVersion
	a.events = nil
	for _, d := range a.decisions {
		if v, ok := decisions[d.ID]; ok {
			d.Version, d.changed = v, false
		}
	}
}

// Task returns a copy of the task.
func (a *TaskAggregate) Task() Task { return a.task }

// Runs returns copies of the runs, oldest first.
func (a *TaskAggregate) Runs() []Run {
	out := make([]Run, len(a.runs))
	for i, r := range a.runs {
		out[i] = *r
	}
	return out
}

// Run returns a copy of a run.
func (a *TaskAggregate) Run(id ID) (Run, bool) {
	if r, err := a.run(id); err == nil {
		return *r, true
	}
	return Run{}, false
}

// Environments returns copies of the environments, ordered by ID.
func (a *TaskAggregate) Environments() []Environment {
	out := make([]Environment, 0, len(a.envs))
	for _, e := range a.envs {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Environment returns a copy of an environment.
func (a *TaskAggregate) Environment(id ID) (Environment, bool) {
	if e, ok := a.envs[id]; ok {
		return *e, true
	}
	return Environment{}, false
}

// Candidates returns copies of the review candidates, oldest first.
func (a *TaskAggregate) Candidates() []ReviewCandidate {
	out := make([]ReviewCandidate, len(a.candidates))
	for i, c := range a.candidates {
		out[i] = *c
	}
	return out
}

// Decisions returns copies of the Decisions, oldest first.
func (a *TaskAggregate) Decisions() []Decision {
	out := make([]Decision, len(a.decisions))
	for i, d := range a.decisions {
		out[i] = *d
	}
	return out
}

// Decision returns a copy of a Decision.
func (a *TaskAggregate) Decision(id ID) (Decision, bool) {
	if d, err := a.decision(id); err == nil {
		return *d, true
	}
	return Decision{}, false
}
