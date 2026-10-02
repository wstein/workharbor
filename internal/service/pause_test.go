package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// raiseApproval opens an approval of the run, as an agent that asks leaves it.
func (r *rig) raiseApproval(id domain.ID) {
	r.t.Helper()
	a := r.load()
	_, err := a.RaiseDecision(domain.NewDecision{ID: id, RunID: "r1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Input: "make deploy", Now: r.clock.now})
	must(r.t, err)
	_, err = r.store.SaveTask(bg, a)
	must(r.t, err)
}

// Pause is a hard interrupt: the run is paused, what it asked is superseded, its
// agent is stopped and the environment keeps running.
func TestPauseStopsTheAgentSupersedesWhatItAskedAndKeepsTheEnvironment(t *testing.T) {
	r := newRig(t)
	r.live()
	r.raiseApproval("ap1")
	if r.load().Task().State != domain.TaskAwaitingGuidance {
		t.Fatal("the approval did not block the task")
	}

	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait() // the stopped session ends

	got := r.load()
	run, _ := got.Run("r1")
	d, _ := got.Decision("ap1")
	if run.State != domain.RunPaused || d.Status != domain.DecisionSuperseded {
		t.Errorf("run %s, approval %s", run.State, d.Status)
	}
	if got.Task().State != domain.TaskRunning {
		t.Errorf("a paused run leaves its task %s, want running (pause is a run state)", got.Task().State)
	}
	if r.envState() != domain.EnvRunning {
		t.Errorf("environment = %s: pause must not stop it", r.envState())
	}
	if r.svc.attached("r1") {
		t.Error("the agent session is still attached")
	}
	// the stopped session did not turn the pause into a loss or a failure
	if r.runState() != domain.RunPaused {
		t.Errorf("after the session ended: %s", r.runState())
	}
	// an answer to the superseded approval is refused (D23)
	if err := r.svc.AnswerDecision(bg, "ap1", domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now}); err == nil {
		t.Error("an answer to a superseded approval was accepted")
	}
	// a second pause, or a pause of a task without a run, is a conflict
	var c *domain.ConflictError
	if err := r.svc.Pause(bg, "t1"); !errors.As(err, &c) {
		t.Errorf("pausing a paused run: %v", err)
	}
	if err := r.svc.Pause(bg, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("pausing an unknown task: %v", err)
	}
	if len(r.errs) != 0 {
		t.Errorf("errors: %v", r.errs)
	}
}

// Resume relaunches the agent from its session, with the briefing that names the
// superseded approval.
func TestResumeRelaunchesFromTheSessionWithTheBriefing(t *testing.T) {
	r := newRig(t)
	r.live()
	r.raiseApproval("ap1")
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	r.agent.Block()

	run, err := r.svc.Resume(bg, "t1")
	if err != nil || run != "r1" {
		t.Fatalf("resume = %q, %v", run, err)
	}
	if r.runState() != domain.RunRunning || !r.svc.attached("r1") {
		t.Errorf("run %s, attached %v", r.runState(), r.svc.attached("r1"))
	}
	last := r.agent.Specs[len(r.agent.Specs)-1]
	for _, want := range []string{"Supervisor briefing", "superseded", "Bash", "make deploy", "Check the workspace first"} {
		if !strings.Contains(last.Prompt, want) {
			t.Errorf("the briefing lacks %q:\n%s", want, last.Prompt)
		}
	}
	// a running run is not resumed twice
	var c *domain.ConflictError
	if _, err := r.svc.Resume(bg, "t1"); !errors.As(err, &c) {
		t.Errorf("resuming a running run: %v", err)
	}
	must(t, r.svc.Cancel(bg, "t1"))
	r.svc.Wait()
}

// A run waiting on a login or quota question is not resumed behind its back.
func TestResumeIsRefusedWhileALoginQuestionIsOpen(t *testing.T) {
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	before := len(r.agent.Specs)

	_, err = r.svc.Resume(bg, "t1")
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != domain.RuleDecisionOpen {
		t.Fatalf("resume = %v", err)
	}
	if len(r.agent.Specs) != before || r.svc.attached("r1") || r.runState() != domain.RunPaused {
		t.Errorf("a refused resume started something: specs %d, run %s", len(r.agent.Specs)-before, r.runState())
	}
}

// A session the agent forgot ends the run failed, not stuck starting.
func TestResumeOfAForgottenSessionFailsTheRun(t *testing.T) {
	r := newRig(t, withSession("gone"))
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	if _, err := r.svc.Resume(bg, "t1"); !errors.Is(err, agent.ErrNoSession) {
		t.Fatalf("resume = %v", err)
	}
	if r.runState() != domain.RunFailed {
		t.Errorf("run = %s", r.runState())
	}
}

func (r *rig) transcript(n int) {
	r.t.Helper()
	for range n {
		_, err := r.store.Append(bg, domain.NewTranscriptEvent("t1", []byte(`{"text":"hello there"}`), r.clock.now))
		must(r.t, err)
	}
}

// A purge deletes the transcript and nothing else: the audit entries, the usage
// rows and the Decisions stay, and the purge is itself an audit entry.
func TestPurgeDeletesTheTranscriptAndKeepsTheRest(t *testing.T) {
	r := newRig(t)
	r.live()
	r.raiseApproval("ap1")
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	base, _ := r.svc.TranscriptSize(bg, "t1") // what the fake agent already wrote
	r.transcript(3)

	audit := func() int {
		evs, err := r.store.EventsSince(bg, "t1", 0, 1000)
		must(t, err)
		n := 0
		for _, e := range evs {
			if e.Tier == domain.TierAudit {
				n++
			}
		}
		return n
	}
	before := audit()
	size, err := r.svc.TranscriptSize(bg, "t1")
	if err != nil || size.Events != base.Events+3 || size.Bytes != base.Bytes+3*int64(len(`{"text":"hello there"}`)) {
		t.Fatalf("size = %+v, %v", size, err)
	}

	res, err := r.svc.PurgeTranscript(bg, "t1", "cli")
	if err != nil || res.Events != size.Events || res.Bytes != size.Bytes || res.Digest == "" {
		t.Fatalf("purge = %+v, %v", res, err)
	}
	if size, _ := r.svc.TranscriptSize(bg, "t1"); size.Events != 0 {
		t.Errorf("transcript left: %+v", size)
	}
	if got := audit(); got != before+1 {
		t.Errorf("audit entries %d, want %d: all kept and one added", got, before+1)
	}
	if res.Audit.Kind != domain.EventPurged || !strings.Contains(string(res.Audit.Payload), `"actor":"cli"`) {
		t.Errorf("the purge's audit entry: %s %s", res.Audit.Kind, res.Audit.Payload)
	}
	if d, ok := r.load().Decision("ap1"); !ok || d.Status != domain.DecisionSuperseded {
		t.Errorf("the Decision was touched: %+v, %v", d, ok)
	}
	if rep, err := r.store.VerifyAudit(bg, nil); err != nil || rep.Checked == 0 {
		t.Errorf("the audit chain after a purge: %+v, %v", rep, err)
	}
	// purging again records another, empty, purge
	if res, err := r.svc.PurgeTranscript(bg, "t1", "cli"); err != nil || res.Events != 0 {
		t.Errorf("second purge = %+v, %v", res, err)
	}
	if _, err := r.svc.PurgeTranscript(bg, "nope", "cli"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown task: %v", err)
	}
}

// The transcript of a running run is still being written, so it is not purged.
func TestPurgeIsRefusedWhileTheRunIsRunning(t *testing.T) {
	r := newRig(t)
	r.live()
	base, _ := r.svc.TranscriptSize(bg, "t1")
	r.transcript(2)
	var c *domain.ConflictError
	if _, err := r.svc.PurgeTranscript(bg, "t1", "cli"); !errors.As(err, &c) {
		t.Fatalf("purge of a running run: %v", err)
	}
	if size, _ := r.svc.TranscriptSize(bg, "t1"); size.Events < base.Events+2 { // the live agent may add more
		t.Errorf("a refused purge deleted: %+v", size)
	}
}

// A pause or cancel that arrives after the run was marked running but before its
// session is attached is remembered and honoured when the session comes up: the
// agent never runs on after it was told to stop.
func TestAStopBeforeTheSessionIsUpStopsItOnceAttached(t *testing.T) {
	for name, stop := range map[string]func(*rig) error{
		"pause":  func(r *rig) error { return r.svc.Pause(bg, "t1") },
		"cancel": func(r *rig) error { return r.svc.Cancel(bg, "t1") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			sl := r.svc.begin("r1") // the launch is in progress: the run is running, no session yet
			if err := stop(r); err != nil {
				t.Fatal(err)
			}
			r.agent.Block()
			sess, err := r.agent.Resume(bg, spec(), r.session)
			must(t, err)
			r.svc.attach("t1", "r1", sl, sess)

			done := make(chan struct{})
			go func() { r.svc.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the agent kept running after it was told to stop")
			}
			if r.svc.attached("r1") {
				t.Error("the stopped session is still attached")
			}
			if got := r.runState(); name == "pause" && got != domain.RunPaused {
				t.Errorf("run = %s, want paused", got)
			}
		})
	}
}

// Two resumes of the same run at once start the agent once: the other is told the
// run is running (design §4.1: one live run).
func TestConcurrentResumesStartTheAgentOnce(t *testing.T) {
	r := newRig(t)
	r.live()
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	r.agent.Block()
	before := len(r.agent.Specs)

	const n = 6
	errs := make(chan error, n)
	for range n {
		go func() { _, err := r.svc.Resume(bg, "t1"); errs <- err }()
	}
	ok := 0
	for range n {
		if err := <-errs; err == nil {
			ok++
		} else {
			var c *domain.ConflictError
			if !errors.As(err, &c) {
				t.Errorf("a refused resume is a conflict: %v", err)
			}
		}
	}
	if ok != 1 {
		t.Errorf("%d resumes succeeded, want exactly one", ok)
	}
	if started := len(r.agent.Specs) - before; started != 1 {
		t.Errorf("the agent was started %d times", started)
	}
	must(t, r.svc.Cancel(bg, "t1"))
	r.svc.Wait()
	if len(r.errs) != 0 {
		t.Errorf("errors: %v", r.errs)
	}
}
