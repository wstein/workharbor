package domain

import "testing"

var allRunStates = []RunState{
	RunStarting,
	RunRunning,
	RunPaused,
	RunInterrupted,
	RunStopped,
	RunFailed,
}

// legalRun is the run state machine of design §4.1, written out in full.
var legalRun = map[[2]RunState]bool{
	{RunStarting, RunRunning}:     true,
	{RunStarting, RunStopped}:     true,
	{RunStarting, RunFailed}:      true,
	{RunStarting, RunInterrupted}: true,

	{RunRunning, RunPaused}:      true,
	{RunRunning, RunStopped}:     true,
	{RunRunning, RunFailed}:      true,
	{RunRunning, RunInterrupted}: true,

	{RunPaused, RunStarting}:    true, // resume by relaunching the agent (D11)
	{RunPaused, RunRunning}:     true, // resume of an agent with cooperative pause
	{RunPaused, RunStopped}:     true,
	{RunPaused, RunInterrupted}: true,

	{RunInterrupted, RunStarting}: true, // the reconciler resumes from the session
	{RunInterrupted, RunStopped}:  true, // cancelled
	{RunInterrupted, RunFailed}:   true, // cannot or no longer resume
}

var allEnvStates = []EnvState{
	EnvProvisioning,
	EnvRunning,
	EnvStopped,
	EnvDeleted,
}

// legalEnv is the environment state machine of design §4.1, written out in full.
var legalEnv = map[[2]EnvState]bool{
	{EnvProvisioning, EnvStopped}: true, // created, not started
	{EnvProvisioning, EnvDeleted}: true, // provisioning failed or abandoned

	{EnvStopped, EnvRunning}: true,
	{EnvStopped, EnvDeleted}: true,

	{EnvRunning, EnvStopped}: true,
}

func TestRunTransitionsExhaustive(t *testing.T) {
	for _, from := range allRunStates {
		for _, to := range allRunStates {
			want := legalRun[[2]RunState{from, to}]
			if got := from.CanTransition(to); got != want {
				t.Errorf("%s -> %s: CanTransition = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestEnvTransitionsExhaustive(t *testing.T) {
	for _, from := range allEnvStates {
		for _, to := range allEnvStates {
			want := legalEnv[[2]EnvState{from, to}]
			if got := from.CanTransition(to); got != want {
				t.Errorf("%s -> %s: CanTransition = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalRunAndEnvStates(t *testing.T) {
	for _, s := range allRunStates {
		want := s == RunStopped || s == RunFailed
		if got := s.Terminal(); got != want {
			t.Errorf("run %s: Terminal = %v, want %v", s, got, want)
		}
	}
	for _, s := range allEnvStates {
		want := s == EnvDeleted
		if got := s.Terminal(); got != want {
			t.Errorf("env %s: Terminal = %v, want %v", s, got, want)
		}
	}
}

// A terminal state has no way out, and every other state has one.
func TestTerminalMeansNoExit(t *testing.T) {
	for _, s := range allRunStates {
		if s.Terminal() == (len(runTransitions[s]) > 0) {
			t.Errorf("run %s: Terminal = %v but has %d exits", s, s.Terminal(), len(runTransitions[s]))
		}
	}
	for _, s := range allEnvStates {
		if s.Terminal() == (len(envTransitions[s]) > 0) {
			t.Errorf("env %s: Terminal = %v but has %d exits", s, s.Terminal(), len(envTransitions[s]))
		}
	}
	for _, s := range allTaskStates {
		if s.Terminal() == (len(taskTransitions[s]) > 0) {
			t.Errorf("task %s: Terminal = %v but has %d exits", s, s.Terminal(), len(taskTransitions[s]))
		}
	}
}

// reachable returns the states reachable from start through table.
func reachable[S comparable](table map[S][]S, start S) map[S]bool {
	seen := map[S]bool{start: true}
	queue := []S{start}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, next := range table[s] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

// Every state is reachable from the one an object is created in, so the
// tables contain no dead states.
func TestEveryStateReachable(t *testing.T) {
	task := reachable(taskTransitions, TaskQueued)
	for _, s := range allTaskStates {
		if !task[s] {
			t.Errorf("task state %s is unreachable from %s", s, TaskQueued)
		}
	}
	run := reachable(runTransitions, RunStarting)
	for _, s := range allRunStates {
		if !run[s] {
			t.Errorf("run state %s is unreachable from %s", s, RunStarting)
		}
	}
	env := reachable(envTransitions, EnvProvisioning)
	for _, s := range allEnvStates {
		if !env[s] {
			t.Errorf("env state %s is unreachable from %s", s, EnvProvisioning)
		}
	}
}

// Only the reconciler's observations lead into interrupted, and the
// states a run can leave it for are the three of design §4.1.
func TestInterruptedPaths(t *testing.T) {
	var into []RunState
	for _, from := range allRunStates {
		if from.CanTransition(RunInterrupted) {
			into = append(into, from)
		}
	}
	wantInto := map[RunState]bool{RunStarting: true, RunRunning: true, RunPaused: true}
	if len(into) != len(wantInto) {
		t.Fatalf("states leading into interrupted = %v, want %d of them", into, len(wantInto))
	}
	for _, from := range into {
		if !wantInto[from] {
			t.Errorf("%s -> interrupted must not be legal", from)
		}
	}

	var out []RunState
	for _, to := range allRunStates {
		if RunInterrupted.CanTransition(to) {
			out = append(out, to)
		}
	}
	wantOut := map[RunState]bool{RunStarting: true, RunStopped: true, RunFailed: true}
	if len(out) != len(wantOut) {
		t.Fatalf("states reachable from interrupted = %v, want %d of them", out, len(wantOut))
	}
	for _, to := range out {
		if !wantOut[to] {
			t.Errorf("interrupted -> %s must not be legal", to)
		}
	}
	// A resumed run must pass through starting again, never jump to running or paused.
	for _, to := range []RunState{RunRunning, RunPaused} {
		if RunInterrupted.CanTransition(to) {
			t.Errorf("interrupted -> %s must go through starting", to)
		}
	}
}

func TestRunTransition(t *testing.T) {
	run := &Run{ID: "r1", State: RunStarting}
	steps := []RunState{RunRunning, RunPaused, RunRunning, RunInterrupted, RunStarting, RunRunning, RunStopped}
	for _, to := range steps {
		if err := run.Transition(to); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
	if err := run.Transition(RunRunning); err == nil {
		t.Fatal("a stopped run must not run again")
	}
	if run.State != RunStopped {
		t.Fatalf("state after illegal transition = %s, want stopped", run.State)
	}
}

// Pause is a hard interrupt (D11): resuming relaunches the agent, and a failed
// relaunch ends the run.
func TestPausedRunRelaunches(t *testing.T) {
	run := &Run{ID: "r2", State: RunPaused}
	for _, to := range []RunState{RunStarting, RunFailed} {
		if err := run.Transition(to); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
	if !run.State.Terminal() {
		t.Fatalf("state = %s, want a terminal state", run.State)
	}

	run = &Run{ID: "r3", State: RunPaused}
	for _, to := range []RunState{RunStarting, RunRunning} {
		if err := run.Transition(to); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
}

func TestEnvTransition(t *testing.T) {
	env := &Environment{ID: "e1", State: EnvProvisioning}
	for _, to := range []EnvState{EnvStopped, EnvRunning, EnvStopped, EnvRunning, EnvStopped, EnvDeleted} {
		if err := env.Transition(to); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
	if err := env.Transition(EnvStopped); err == nil {
		t.Fatal("a deleted environment must not come back")
	}
	if env.State != EnvDeleted {
		t.Fatalf("state after illegal transition = %s, want deleted", env.State)
	}

	running := &Environment{ID: "e2", State: EnvRunning}
	if err := running.Transition(EnvDeleted); err == nil {
		t.Fatal("a running environment must be stopped before it is deleted")
	}
}
