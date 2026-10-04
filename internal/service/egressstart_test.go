package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

// withEgressRequests gives the rig a repository whose environment requests one
// host and whose lockfiles suggest another, and environments with a proxy sidecar.
func (r *wsRig) withEgressRequests() {
	r.egress = true
	r.ws.cfg.Environment = func(_ context.Context, repo, branch string) (RepoEnvironment, error) {
		if repo != "wstein/workharbor" || branch != "main" {
			r.t.Errorf("the environment was read for %s@%s", repo, branch)
		}
		return RepoEnvironment{Environment: devcontainer.Environment{
			Origin: devcontainer.OriginDefault, Commit: strings.Repeat("a", 40),
			Config:         devcontainer.Config{EgressRequests: []string{"proxy.golang.org"}},
			SuggestedHosts: []string{"sum.golang.org"},
		}}, nil
	}
}

func (r *wsRig) allowOf(env domain.ID) []string {
	r.t.Helper()
	info, err := r.rt.Adapter.Inspect(bg, string(env))
	if err != nil {
		r.t.Fatal(err)
	}
	return info.EgressAllow
}

func (r *wsRig) openEgress(task domain.ID) []domain.Decision {
	r.t.Helper()
	a, err := r.store.LoadTask(bg, task)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []domain.Decision
	for _, d := range a.Decisions() {
		if d.Cause == domain.CauseEgressRequest && d.Status == domain.DecisionOpen {
			out = append(out, d)
		}
	}
	return out
}

// The run stays starting and the agent does not start until every request is
// answered; an allowed host is then in the sidecar before the agent starts, a
// denied one is not, and the agent starts without it (design §4.2).
func TestARunWaitsForItsEgressRequestsThenStartsWithTheAllowedHosts(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withEgressRequests()
	w, a := r.create("docs-ws")
	base := r.allowOf(w.EnvID)
	if !reflect.DeepEqual(base, []string{"api.anthropic.com"}) {
		t.Fatalf("the environment starts with the supervisor's hosts only: %v", base)
	}

	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	agg, _ := r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunStarting || agg.Task().State != domain.TaskAwaitingGuidance {
		t.Fatalf("run %s, task %s: the run stays starting and the task waits", rn.State, agg.Task().State)
	}
	if r.agent.Started() != 0 {
		t.Fatal("the agent started before the requests were answered")
	}
	open := r.openEgress(task)
	if len(open) != 2 || open[0].Host != "proxy.golang.org" || open[1].Host != "sum.golang.org" {
		t.Fatalf("open requests = %+v", open)
	}
	if !reflect.DeepEqual(r.allowOf(w.EnvID), base) {
		t.Error("the allowlist changed before any answer")
	}

	// Reconcile does not take the waiting run for lost.
	if rep, err := r.svc.Reconcile(bg); err != nil || len(rep.Interrupted) != 0 {
		t.Fatalf("reconcile: %+v, %v", rep, err)
	}

	// The first answer is not enough.
	if err := r.svc.AnswerDecision(bg, open[0].ID, domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0}); err != nil {
		t.Fatal(err)
	}
	if r.agent.Started() != 0 {
		t.Fatal("the agent started with a request still open")
	}
	// The last one starts it, with the allowed host in the sidecar and the denied one out.
	if err := r.svc.AnswerDecision(bg, open[1].ID, domain.Response{Option: domain.AnswerDeny, By: "werner", At: t0}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.agent.Started() == 1 })
	if r.agent.Started() != 1 {
		t.Fatalf("agent starts = %d, want 1", r.agent.Started())
	}
	if got := r.allowOf(w.EnvID); !reflect.DeepEqual(got, []string{"api.anthropic.com", "proxy.golang.org"}) {
		t.Errorf("allowlist = %v, want the base and the allowed host, not the denied one", got)
	}
	eventually(t, func() bool {
		agg, _ = r.store.LoadTask(bg, task)
		rn, _ := agg.Run(run)
		return rn.State == domain.RunRunning
	})
	if rn, _ := agg.Run(run); rn.State != domain.RunRunning || agg.Task().State != domain.TaskRunning {
		t.Errorf("run %s, task %s after the answers", rn.State, agg.Task().State)
	}
	if len(r.svc.egressWaits) != 0 {
		t.Error("the wait was not cleared")
	}
	if len(r.reported()) != 0 {
		t.Errorf("errors: %v", r.reported())
	}

	// Another workspace of the repository is not asked again and starts with the allowed host.
	w2, a2 := r.create("docs-two")
	if got := r.allowOf(w2.EnvID); !reflect.DeepEqual(got, []string{"api.anthropic.com", "proxy.golang.org"}) {
		t.Errorf("a new workspace of the repository starts with %v", got)
	}
	task2, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a2.ID, Issue: "#8"})
	if err != nil {
		t.Fatal(err)
	}
	if open := r.openEgress(task2); len(open) != 0 {
		t.Errorf("answered hosts were asked again: %+v", open)
	}
	if r.agent.Started() != 2 {
		t.Errorf("the second run did not start at once: %d agent starts", r.agent.Started())
	}
}

// An expired request is a denial for this run only: the run starts without the
// host, and the host is asked again at the next start.
func TestAnExpiredEgressRequestStartsTheRunWithoutTheHost(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withEgressRequests()
	_, a := r.create("docs-ws")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	if r.agent.Started() != 0 || len(r.openEgress(task)) != 2 {
		t.Fatal("the run did not wait")
	}
	r.clock.now = t0.Add(domain.DefaultApprovalTimeout + time.Minute)
	if _, err := r.svc.Reconcile(bg); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.agent.Started() == 1 })
	if r.agent.Started() != 1 {
		t.Fatalf("agent starts = %d: an expiry opens the way", r.agent.Started())
	}
	eventually(t, func() bool {
		agg, _ := r.store.LoadTask(bg, task)
		rn, _ := agg.Run(run)
		return rn.State == domain.RunRunning
	})
	agg, _ := r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunRunning {
		t.Errorf("run = %s", rn.State)
	}
	if allow, _ := r.svc.EgressAllow(bg, "wstein/workharbor"); len(allow) != 0 {
		t.Errorf("an expiry allowed %v", allow)
	}
	env := devcontainer.Environment{Config: devcontainer.Config{EgressRequests: []string{"proxy.golang.org"}}}
	if again, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env, false); len(again) != 1 {
		t.Errorf("the host is asked again at the next start: %+v", again)
	}
}

func TestCancellingARunThatWaitsForEgressFreesItAndStartsNoAgent(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withEgressRequests()
	_, a := r.create("docs-ws")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	open := r.openEgress(task)
	if err := r.svc.Cancel(bg, task); err != nil {
		t.Fatal(err)
	}
	if len(r.svc.egressWaits) != 0 || r.svc.attached(run) {
		t.Error("the cancelled run still holds its wait or its slot")
	}
	// An answer to a request of a cancelled task is refused and starts nothing.
	if err := r.svc.AnswerDecision(bg, open[0].ID, domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0}); err == nil {
		t.Error("an answer to a superseded request was accepted")
	}
	if r.agent.Started() != 0 {
		t.Error("an agent started for a cancelled run")
	}
	if allow, _ := r.svc.EgressAllow(bg, "wstein/workharbor"); len(allow) != 0 {
		t.Errorf("a refused answer allowed %v", allow)
	}
}

// A repository that cannot be read does not stop the run, and allows nothing.
func TestAnUnreadableRepositoryStartsTheRunAndAllowsNothing(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	r.ws.cfg.Environment = func(context.Context, string, string) (RepoEnvironment, error) {
		return RepoEnvironment{}, errors.New("fetch wstein/workharbor: no route to host")
	}
	w, a := r.create("docs-ws")
	if len(r.reported()) != 1 {
		t.Fatalf("a workspace of an unreadable repository gets the default environment and reports it: %v", r.reported())
	}
	r.forget()
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
	if r.agent.Started() != 1 || len(r.reported()) != 1 {
		t.Errorf("agent starts %d, reported %v", r.agent.Started(), r.reported())
	}
	if !reflect.DeepEqual(r.allowOf(w.EnvID), []string{"api.anthropic.com"}) {
		t.Errorf("an unreadable repository changed the allowlist: %v", r.allowOf(w.EnvID))
	}
}

// eventually waits up to five seconds for what happens on a goroutine of the
// service's own, such as the start of an agent.
func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			return // the caller's own check reports what is wrong
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The environment (devcontainer, Dockerfile, postCreateCommand, egress requests) is
// read at the repository's default branch, never at the workspace's integration
// branch, so an approved agent commit there cannot configure the next environment
// (design §6).
func TestTheEnvironmentIsReadAtTheDefaultBranchNotTheIntegrationBranch(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	r.issues.DefaultBranch = "trunk"
	var read []string
	r.ws.cfg.Environment = func(_ context.Context, _, branch string) (RepoEnvironment, error) {
		read = append(read, branch)
		return RepoEnvironment{}, errors.New("nothing to see")
	}
	_, a := r.create("docs-ws")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
	if len(read) == 0 {
		t.Fatal("the environment was never read")
	}
	for _, b := range read {
		if b != "trunk" {
			t.Errorf("the environment was read at %q: the workspace's integration branch is main, the default is trunk", b)
		}
	}
}

// A forge that cannot name the default branch gets no environment read at all.
func TestNoEnvironmentIsReadWhenTheDefaultBranchIsUnknown(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	r.ws.cfg.Issues = struct{ IssueSource }{r.issues}
	called := false
	r.ws.cfg.Environment = func(context.Context, string, string) (RepoEnvironment, error) {
		called = true
		return RepoEnvironment{}, nil
	}
	_, a := r.create("docs-ws")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("the environment was read although the default branch is unknown")
	}
	if len(r.reported()) == 0 {
		t.Error("the refusal was not reported")
	}
}

// A default branch that changed between two reads is read at its new name, and a
// read that fails gives the zero environment with the failure reported (§6: the
// default is asked of the forge each time, and an unanswered question fails closed).
func TestTheEnvironmentFollowsADefaultBranchChangeAndFailsClosedOnAFailedRead(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	w, _ := r.create("docs-ws")
	var read []string
	r.ws.cfg.Environment = func(_ context.Context, _, branch string) (RepoEnvironment, error) {
		read = append(read, branch)
		return RepoEnvironment{Environment: devcontainer.Environment{Commit: "c-" + branch}}, nil
	}
	r.issues.DefaultBranch = "trunk"
	if env, ok := r.ws.repoEnvironment(bg, w); !ok || env.Commit != "c-trunk" {
		t.Fatalf("first read = %+v, %v", env, ok)
	}
	r.issues.DefaultBranch = "develop" // the human changed it on the forge
	if env, ok := r.ws.repoEnvironment(bg, w); !ok || env.Commit != "c-develop" {
		t.Fatalf("the changed default was not followed: %+v, %v (read %v)", env, ok, read)
	}
	r.ws.cfg.Issues = failingDefault{r.issues}
	r.forget()
	read = nil
	if env, ok := r.ws.repoEnvironment(bg, w); ok || env.Commit != "" || len(read) != 0 {
		t.Errorf("a failed read gave %+v, %v, read %v", env, ok, read)
	}
	if len(r.reported()) != 1 {
		t.Errorf("the failure was not reported: %v", r.reported())
	}
}

type failingDefault struct{ IssueSource }

func (failingDefault) DefaultBranchName(context.Context, string) (string, error) {
	return "", errors.New("no route to host")
}
