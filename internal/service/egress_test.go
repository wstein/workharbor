package service

import (
	"reflect"
	"strings"
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
	pending, err := r.svc.PendingEgress(bg, "wstein/workharbor", env, false)
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
	if again, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env, false); len(again) != 0 {
		t.Errorf("asked again for %+v (denied: %v)", again, denied)
	}
	if other, _ := r.svc.PendingEgress(bg, "wstein/other", env, false); len(other) != 3 {
		t.Errorf("another repository must be asked for all three: %+v", other)
	}
	// A host the file adds later is asked at the next start.
	env.Config.EgressRequests = append(env.Config.EgressRequests, "example.org")
	if later, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env, false); len(later) != 1 || later[0].Host != "example.org" {
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
	if again, _ := r.svc.PendingEgress(bg, "wstein/workharbor", env, false); len(again) != 1 {
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

// publishedEnv is a repository whose devcontainer.json (at the digest) requests
// one host and whose lockfile suggests another.
func publishedEnv(digest string) devcontainer.Environment {
	return devcontainer.Environment{
		Config:         devcontainer.Config{EgressRequests: []string{"proxy.golang.org"}, SourceDigest: digest},
		SuggestedHosts: []string{"registry.npmjs.org"},
	}
}

// answerAll answers every pending request allow.
func (r *rig) answerAll(repo string, env devcontainer.Environment, published bool) []devcontainer.HostRequest {
	r.t.Helper()
	pending, err := r.svc.PendingEgress(bg, repo, env, published)
	must(r.t, err)
	ids, err := r.svc.RequestEgress(bg, "t1", "r1", pending)
	must(r.t, err)
	for _, id := range ids {
		must(r.t, r.svc.AnswerDecision(bg, id, domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0}))
	}
	return pending
}

// Under the published preset an answer holds for the file it was given for: a
// changed file asks again, and the old allow counts as unanswered, not as a deny.
func TestPublishedAsksAgainWhenTheSourceChanged(t *testing.T) {
	r := newRig(t)
	const repo = "wstein/workharbor"
	first := r.answerAll(repo, publishedEnv("digest-1"), true)
	if len(first) != 1 || first[0].Host != "proxy.golang.org" || first[0].Digest != "digest-1" {
		t.Fatalf("first = %+v: lockfile suggestions must be ignored and the digest carried", first)
	}
	if got, _ := r.store.EgressHosts(bg, repo); got.Sources["proxy.golang.org"] != "digest-1" || !reflect.DeepEqual(got.Allowed, []string{"proxy.golang.org"}) {
		t.Fatalf("the answer did not record its source: %+v", got)
	}
	// the same file: nothing to ask, and the allow stands
	if again, _ := r.svc.PendingEgress(bg, repo, publishedEnv("digest-1"), true); len(again) != 0 {
		t.Errorf("asked again for an unchanged file: %+v", again)
	}
	// the file changed: the host is asked again and its old allow no longer opens it
	again, err := r.svc.PendingEgress(bg, repo, publishedEnv("digest-2"), true)
	if err != nil || len(again) != 1 || again[0].Host != "proxy.golang.org" || again[0].Digest != "digest-2" {
		t.Fatalf("after the change: %+v, %v", again, err)
	}
	got, _ := r.store.EgressHosts(bg, repo)
	if len(got.Allowed) != 0 || got.Answered["proxy.golang.org"] {
		t.Errorf("a stale allow must count as unanswered, not allowed and not denied: %+v", got)
	}
	if allow, _ := r.svc.EgressAllow(bg, repo); len(allow) != 0 {
		t.Errorf("the stale allow still opens the host: %v", allow)
	}
	// the new answer carries the new digest
	r.answerAll(repo, publishedEnv("digest-2"), true)
	if got, _ := r.store.EgressHosts(bg, repo); got.Sources["proxy.golang.org"] != "digest-2" {
		t.Errorf("sources = %v", got.Sources)
	}
}

// A lockfile answer from another preset, and one from before sources were
// recorded, do not carry over to the published preset.
func TestPublishedForgetsLockfileAndUnrecordedAnswers(t *testing.T) {
	r := newRig(t)
	const repo = "wstein/workharbor"
	must(t, r.store.SetEgressHost(bg, repo, "registry.npmjs.org", true, "d1", devcontainer.LockfileDigest, t0))
	must(t, r.store.SetEgressHost(bg, repo, "old.example", true, "d2", "", t0))
	if _, err := r.svc.PendingEgress(bg, repo, publishedEnv("digest-1"), true); err != nil {
		t.Fatal(err)
	}
	if allow, _ := r.svc.EgressAllow(bg, repo); len(allow) != 0 {
		t.Errorf("allowed after the published preset forgot them: %v", allow)
	}
	// and a repository with no devcontainer.json has nothing a published allow could rest on
	must(t, r.store.SetEgressHost(bg, repo, "proxy.golang.org", true, "d3", "digest-1", t0))
	if _, err := r.svc.PendingEgress(bg, repo, devcontainer.Environment{}, true); err != nil {
		t.Fatal(err)
	}
	if allow, _ := r.svc.EgressAllow(bg, repo); len(allow) != 0 {
		t.Errorf("allowed without a devcontainer.json: %v", allow)
	}
}

// The other presets ask once per repository, whatever changes, and still take
// lockfile suggestions.
func TestOtherPresetsAskOncePerRepository(t *testing.T) {
	r := newRig(t)
	const repo = "wstein/workharbor"
	first := r.answerAll(repo, publishedEnv("digest-1"), false)
	if len(first) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if again, _ := r.svc.PendingEgress(bg, repo, publishedEnv("digest-2"), false); len(again) != 0 {
		t.Errorf("a changed file asked again outside published: %+v", again)
	}
	if allow, _ := r.svc.EgressAllow(bg, repo); len(allow) != 2 {
		t.Errorf("allowed = %v", allow)
	}
}

// A feature source outside the allowed one is asked once per repository and reference,
// and the answer is kept like an egress host's (D38, issue #127).
func TestFeatureSourcesAreAskedOnceAndKeptPerRepository(t *testing.T) {
	r := newRig(t)
	const repo = "wstein/workharbor"
	const d1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const d2 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	env := devcontainer.Environment{ForeignFeatures: []devcontainer.ForeignFeature{{Ref: "ghcr.io/someone/else/thing:1", Digest: d1}, {Ref: "ghcr.io/other/one:2", Digest: d2}}}
	pending, err := r.svc.PendingFeatureSources(bg, repo, env)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if err := r.svc.RequestFeatureSources(bg, "t1", "r1", pending); err != nil {
		t.Fatal(err)
	}
	a := r.load()
	if a.Task().State != domain.TaskAwaitingGuidance {
		t.Errorf("task = %s, want awaiting_guidance", a.Task().State)
	}
	var ids []domain.ID
	for _, d := range a.Decisions() {
		if d.Cause != domain.CauseFeatureSource || !d.Blocking || d.Status != domain.DecisionOpen || d.Feature == "" || !domain.ValidDigest(d.FeatureDigest) {
			t.Errorf("decision = %+v", d)
		}
		ids = append(ids, d.ID)
	}
	if ok, _ := r.svc.ApprovedFeatureSources(bg, repo); len(ok) != 0 {
		t.Fatalf("approved before any answer: %v", ok)
	}
	for _, id := range ids {
		d, _ := a.Decision(id)
		opt := domain.AnswerDeny
		if d.Feature == "ghcr.io/someone/else/thing:1" {
			opt = domain.AnswerAllow
		}
		if err := r.svc.AnswerDecision(bg, id, domain.Response{Option: opt, By: "werner", At: t0}); err != nil {
			t.Fatalf("answer %s: %v", d.Feature, err)
		}
	}
	ok, err := r.svc.ApprovedFeatureSources(bg, repo)
	if err != nil || len(ok) != 1 || ok["ghcr.io/someone/else/thing:1"] != d1 {
		t.Errorf("approved = %v, %v", ok, err)
	}
	if st := r.load().Task().State; st != domain.TaskRunning {
		t.Errorf("task = %s after the last answer, want running again", st)
	}
	if again, _ := r.svc.PendingFeatureSources(bg, repo, env); len(again) != 0 {
		t.Errorf("asked again for %v", again)
	}
	if other, _ := r.svc.PendingFeatureSources(bg, "wstein/other", env); len(other) != 2 {
		t.Errorf("another repository must be asked for both: %v", other)
	}
	if err := r.svc.RequestFeatureSources(bg, "t1", "r1", []devcontainer.ForeignFeature{{Ref: "bad ref", Digest: d1}}); err == nil {
		t.Error("a malformed reference was asked about")
	}
	if err := r.svc.RequestFeatureSources(bg, "t1", "r1", []devcontainer.ForeignFeature{{Ref: "ghcr.io/x/y:1"}}); err == nil {
		t.Error("a feature with no digest was asked about")
	}
	// the allowed tag is moved: it resolves to another digest, so it is asked again; the
	// deny stays per reference, whatever the reference resolves to
	d3 := "sha256:" + strings.Repeat("3", 64)
	moved := devcontainer.Environment{ForeignFeatures: []devcontainer.ForeignFeature{{Ref: "ghcr.io/someone/else/thing:1", Digest: d3}, {Ref: "ghcr.io/other/one:2", Digest: d3}}}
	again, _ := r.svc.PendingFeatureSources(bg, repo, moved)
	if len(again) != 1 || again[0].Ref != "ghcr.io/someone/else/thing:1" || again[0].Digest != d3 {
		t.Errorf("a moved tag must be asked again, and a deny must stay: %v", again)
	}
	if len(r.errs) != 0 {
		t.Errorf("errors: %v", r.errs)
	}
}
