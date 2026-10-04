package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/hostgit"
)

// restartedRetry reports what a restarted pipeline restores for the SHA: its
// memory is empty, only the recorded attempts are left.
func restartedRetry(t *testing.T, f *flowRig, sha string) *publishRetry {
	t.Helper()
	f.pipe.mu.Lock()
	f.pipe.retry = map[domain.ID]*publishRetry{}
	f.pipe.mu.Unlock()
	must(t, f.pipe.seedRetry(bg, "t1", sha))
	f.pipe.mu.Lock()
	defer f.pipe.mu.Unlock()
	return f.pipe.retry["t1"]
}

// seedRetry takes only the attempts of the SHA in question: a newly approved
// SHA must not inherit the backoff of an earlier one.
func TestSeedRetryIgnoresTheAttemptsOfAnotherSHA(t *testing.T) {
	t.Parallel()
	f := newFlowRig(t)
	fp := &failingPusher{err: fmt.Errorf("push: %w", hostgit.ErrTransport), next: localPusher{repo: f.repo, remote: f.remote}}
	f.pusher(fp)
	f.reconcileNow()
	d, _ := f.review()
	must(t, f.approve(d))
	f.svc.Wait()

	if r := restartedRetry(t, f, "0000000000000000000000000000000000000000"); r != nil {
		t.Fatalf("another SHA inherited the backoff: %+v", r)
	}
	// The same SHA does get it.
	if r := restartedRetry(t, f, d.SHA); r == nil || r.attempts != 1 {
		t.Fatalf("the SHA's own attempt did not seed: %+v", r)
	}
}

// A refusal ends the publish: when it is the last event for the SHA, a restart
// does not seed from the transport faults before it, so a Retry starts at 1.
func TestSeedRetryIgnoresARefusalAsTheLastAttempt(t *testing.T) {
	t.Parallel()
	f := newFlowRig(t)
	fp := &failingPusher{err: fmt.Errorf("push: %w", hostgit.ErrTransport), next: localPusher{repo: f.repo, remote: f.remote}}
	f.pusher(fp)
	f.reconcileNow()
	d, _ := f.review()
	must(t, f.approve(d))
	f.svc.Wait()

	fp.set(fmt.Errorf("push: %w", hostgit.ErrNotFastForward))
	f.clock.now = f.clock.now.Add(time.Minute)
	f.reconcileNow()
	v, err := f.svc.Show(bg, "t1")
	must(t, err)
	n := len(v.PublishAttempts)
	if n != 2 || v.PublishAttempts[n-1].Transient {
		t.Fatalf("attempts %+v, want a transport fault then a refusal", v.PublishAttempts)
	}
	if r := restartedRetry(t, f, d.SHA); r != nil {
		t.Fatalf("a refusal as the last event seeded: %+v", r)
	}
}

// Transport faults keep a publish outstanding for DefaultPublishMaxAge from its
// first failed attempt, not for ever: the next failure after that ends it with
// publish_failed. A restart does not reset the clock (the first attempt's time
// comes from the recorded events).
func TestAFaultThatNeverClearsEndsThePublishAfterTheCap(t *testing.T) {
	t.Parallel()
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%v", restart), func(t *testing.T) {
			f := newFlowRig(t)
			fp := &failingPusher{err: fmt.Errorf("push: %w", hostgit.ErrTransport), next: localPusher{repo: f.repo, remote: f.remote}}
			f.pusher(fp)
			f.reconcileNow()
			d, _ := f.review()
			must(t, f.approve(d))
			f.svc.Wait()
			start := f.clock.now

			// Inside the cap the publish stays outstanding.
			f.clock.now = start.Add(DefaultPublishMaxAge - time.Minute)
			if restart {
				restartedRetry(t, f, d.SHA)
			}
			f.reconcileNow()
			if _, ok := f.question(domain.CausePublishFailed); ok {
				t.Fatal("publish_failed before the cap")
			}
			calls := fp.calls
			if calls < 2 {
				t.Fatalf("no retry inside the cap: %d calls", calls)
			}

			// At the cap the next failure ends it.
			f.clock.now = start.Add(DefaultPublishMaxAge + 10*time.Minute) // past the last backoff
			if restart {
				restartedRetry(t, f, d.SHA)
			}
			f.reconcileNow()
			f.svc.Wait()
			q, ok := f.question(domain.CausePublishFailed)
			if !ok {
				t.Fatalf("no publish_failed at the cap; errors %v", f.reported())
			}
			if q.SHA != d.SHA {
				t.Errorf("question %+v", q)
			}
			// It stays ended.
			n := fp.calls
			f.clock.now = f.clock.now.Add(time.Hour)
			f.reconcileNow()
			if fp.calls != n {
				t.Errorf("a pass under an open publish_failed pushed again")
			}
		})
	}
}
