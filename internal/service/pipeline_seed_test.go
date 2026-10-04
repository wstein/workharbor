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
