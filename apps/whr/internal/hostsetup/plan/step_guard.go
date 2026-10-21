//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import "github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"

// StepGuard provides a real writable-journal checkpoint and immutable evidence
// for this dispatched step's already-completed dependencies. It grants no lock,
// profile, journal append, finish or close capability to action adapters.
type StepGuard interface {
	Checkpoint() error
	Completed(string) bool
}
type runGuard struct {
	run       *profile.Run
	completed map[string]bool
}

func (g runGuard) Checkpoint() error        { return g.run.Checkpoint() }
func (g runGuard) Completed(id string) bool { return g.completed[id] }
func guardFor(run *profile.Run, step Step, completed map[string]bool) StepGuard {
	evidence := map[string]bool{}
	for _, id := range step.DependsOn {
		evidence[id] = completed[id]
	}
	return runGuard{run: run, completed: evidence}
}
