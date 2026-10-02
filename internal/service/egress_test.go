package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

func TestEgressRequestsAreAskedOnceAndKeptPerRepository(t *testing.T) {
	r := newRig(t)
	env := devcontainer.Environment{
		Config:         devcontainer.Config{EgressRequests: []string{"proxy.golang.org"}},
		SuggestedHosts: []string{"sum.golang.org", "registry.npmjs.org"},
	}
	pending, err := r.svc.PendingEgress(bg, "wstein/workharbor", env)
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	ids, err := r.svc.RequestEgress(bg, "t1", "r1", pending)
	if err != nil || len(ids) != 3 {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	a := r.load()
	if a.Task().State != domain.TaskAwaitingGuidance {
		t.Errorf("task = %s, want awaiting_guidance", a.Task().State)
	}
	for i, id := range ids {
		d, ok := a.Decision(id)
		if !ok || d.Kind != domain.DecisionApproval || !d.Blocking || d.Cause != domain.CauseEgressRequest || d.Host != pending[i].Host || d.Status != domain.DecisionOpen {
			t.Errorf("decision %d = %+v", i, d)
		}
	}

	// Nothing is allowed by being asked: no host is allowed yet.
	if allow, _ := r.svc.EgressAllow(bg, "wstein/workharbor"); len(allow) != 0 {
		t.Fatalf("allowed before any answer: %v", allow)
	}
	// No agent waits for an egress request, and the answer still goes through.
	var allowed, denied []string
	for i, id := range ids {
		opt := domain.AnswerAllow
		if pending[i].Host == "registry.npmjs.org" {
			opt = domain.AnswerDeny
			denied = append(denied, pending[i].Host)
		} else {
			allowed = append(allowed, pending[i].Host)
		}
		if err := r.svc.AnswerDecision(bg, id, domain.Response{Option: opt, By: "werner", At: t0}); err != nil {
			t.Fatalf("answer %s: %v", pending[i].Host, err)
		}
	}
	got, err := r.svc.EgressAllow(bg, "wstein/workharbor")
	if err != nil || !reflect.DeepEqual(got, []string{"proxy.golang.org", "sum.golang.org"}) {
		t.Errorf("allowed = %v, %v (want %v)", got, err, allowed)
	}
	if st := r.load().Task().State; st != domain.TaskRunning {
		t.Errorf("task = %s after the last answer, want running again", st)
	}

	// Every host is answered, so none is asked again, in this task or in another
	// workspace of the repository; a repository that was never asked is asked.
	if again, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env); len(again) != 0 {
		t.Errorf("asked again for %+v (denied: %v)", again, denied)
	}
	if other, _ := r.svc.PendingEgress(bg, "wstein/other", env); len(other) != 3 {
		t.Errorf("another repository must be asked for all three: %+v", other)
	}
	// A host the file adds later is asked at the next start.
	env.Config.EgressRequests = append(env.Config.EgressRequests, "example.org")
	if later, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env); len(later) != 1 || later[0].Host != "example.org" {
		t.Errorf("later = %+v", later)
	}
	if len(r.errs) != 0 {
		t.Errorf("errors: %v", r.errs)
	}
}

// An expired request is a denial for this run only: nothing is kept, so the host
// is asked again at the next start.
func TestAnExpiredEgressRequestIsDeniedForThisRunOnly(t *testing.T) {
	r := newRig(t)
	pending := []devcontainer.HostRequest{{Host: "proxy.golang.org", Source: devcontainer.FromLockfile}}
	ids, err := r.svc.RequestEgress(bg, "t1", "r1", pending)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		if len(a.ExpireDecisions(t0.Add(domain.DefaultApprovalTimeout+time.Minute))) != 1 {
			t.Error("the request did not expire")
		}
		return nil
	}))
	d, _ := r.load().Decision(ids[0])
	if d.Status != domain.DecisionExpired {
		t.Fatalf("status = %s", d.Status)
	}
	env := devcontainer.Environment{SuggestedHosts: []string{"proxy.golang.org"}}
	if again, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env); len(again) != 1 {
		t.Errorf("an expired request must be asked again: %+v", again)
	}
	if allow, _ := r.svc.EgressAllow(bg, "wstein/workharbor"); len(allow) != 0 {
		t.Errorf("an expired request allowed %v", allow)
	}
	// An answer after the expiry is refused and keeps nothing.
	if err := r.svc.AnswerDecision(bg, ids[0], domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0.Add(time.Hour)}); err == nil {
		t.Error("an answer to an expired request was accepted")
	}
	if allow, _ := r.svc.EgressAllow(bg, "wstein/workharbor"); len(allow) != 0 {
		t.Errorf("a late answer allowed %v", allow)
	}
}

func TestNothingToAskRaisesNothing(t *testing.T) {
	r := newRig(t)
	if ids, err := r.svc.RequestEgress(bg, "t1", "r1", nil); err != nil || len(ids) != 0 {
		t.Errorf("ids = %v, %v", ids, err)
	}
	if r.load().Task().State != domain.TaskRunning {
		t.Error("the task changed")
	}
	// A host the domain refuses stops the whole request.
	bad := []devcontainer.HostRequest{{Host: "ok.example.com", Source: "x"}, {Host: "10.0.0.1", Source: "x"}}
	if _, err := r.svc.RequestEgress(bg, "t1", "r1", bad); err == nil {
		t.Error("an IP address was asked about")
	}
	if len(r.load().Decisions()) != 0 {
		t.Error("a refused request left a Decision behind")
	}
}
