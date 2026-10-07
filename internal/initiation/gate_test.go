package initiation_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/initiation"
	"github.com/wstein/workharbor/internal/store"
)

type adapter struct {
	calls     int
	err       error
	sessOnErr bool
	st        *store.Store
	t         *testing.T
}

func (*adapter) Name() string                     { return "test" }
func (*adapter) Capabilities() agent.Capabilities { return agent.Capabilities{} }
func (a *adapter) send(ctx context.Context) (agent.Session, error) {
	a.calls++
	events, err := a.st.EventsOfKind(ctx, "task", domain.EventRunInitiated)
	if err != nil || len(events) != a.calls {
		a.t.Fatalf("send preceded its audit record: %v, %d events for %d sends", err, len(events), a.calls)
	}
	if a.err != nil {
		if a.sessOnErr {
			return &session{a: a}, a.err
		}
		return nil, a.err
	}
	return &session{a: a}, nil
}

func (a *adapter) Start(ctx context.Context, _ agent.StartSpec) (agent.Session, error) {
	return a.send(ctx)
}

func (a *adapter) Resume(ctx context.Context, _ agent.StartSpec, _ string) (agent.Session, error) {
	return a.send(ctx)
}

type session struct{ a *adapter }

func (*session) ID() string                 { return "session" }
func (*session) Events() <-chan agent.Event { return nil }
func (s *session) Instruct(ctx context.Context, _ string) (agent.Delivery, error) {
	_, err := s.a.send(ctx)
	return agent.DeliveryInjected, err
}
func (*session) Stop(context.Context) error           { return nil }
func (*session) Wait() (agent.Result, error)          { return agent.Result{}, nil }
func (*session) Pause(context.Context) error          { return nil }
func (s *session) Continue(ctx context.Context) error { _, err := s.a.send(ctx); return err }

func fixture(t *testing.T) (*store.Store, *adapter, *initiation.Gate) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	agg := domain.NewTaskAggregate(domain.Task{ID: "task", State: domain.TaskRunning})
	agg.AddEnvironment(domain.Environment{ID: "env", Backend: "test", State: domain.EnvRunning})
	if err := agg.StartRun(domain.Run{ID: "run", EnvID: "env"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTask(t.Context(), agg); err != nil {
		t.Fatal(err)
	}
	a := &adapter{st: st, t: t}
	return st, a, initiation.New(a, st)
}

func human() context.Context {
	return initiation.ForRun(initiation.With(context.Background(), initiation.UserAction("user", "api")), "task", "run")
}

func TestGateMatrix(t *testing.T) {
	for _, action := range []string{"start", "resume", "say", "continue"} {
		for _, marker := range []string{"absent", "zero", "invalid_actor", "invalid_channel", "user_action", "decision_answer", "without_cancel", "spent"} {
			t.Run(action+"/"+marker, func(t *testing.T) {
				st, a, g := fixture(t)
				var sess agent.Session
				if action == "say" || action == "continue" {
					var err error
					sess, err = g.Start(human(), agent.StartSpec{})
					if err != nil {
						t.Fatal(err)
					}
				}
				ctx := human()
				allowed := true
				switch marker {
				case "absent":
					ctx = initiation.ForRun(context.Background(), "task", "run")
					allowed = false
				case "zero":
					ctx = initiation.With(ctx, initiation.Marker{})
					allowed = false
				case "invalid_actor":
					ctx = initiation.With(ctx, initiation.UserAction("", "api"))
					allowed = false
				case "invalid_channel":
					ctx = initiation.With(ctx, initiation.UserAction("user", "reconcile"))
					allowed = false
				case "decision_answer":
					ctx = initiation.DecisionAnswer(ctx, "decision")
				case "without_cancel":
					ctx = context.WithoutCancel(ctx)
				case "spent":
					if _, err := g.Start(ctx, agent.StartSpec{}); err != nil {
						t.Fatal(err)
					}
					allowed = false
				}
				before := a.calls
				var err error
				switch action {
				case "start":
					_, err = g.Start(ctx, agent.StartSpec{})
				case "resume":
					_, err = g.Resume(ctx, agent.StartSpec{}, "session")
				case "say":
					_, err = sess.Instruct(ctx, "never record this prompt")
				case "continue":
					err = sess.(agent.Pauser).Continue(ctx)
				}
				if allowed {
					if err != nil || a.calls != before+1 {
						t.Fatalf("allow: err=%v calls=%d", err, a.calls)
					}
				} else if !errors.Is(err, initiation.ErrNotInitiated) || a.calls != before {
					t.Fatalf("deny: err=%v calls=%d", err, a.calls)
				}
				if allowed { // one capability never sends twice, even through another gate.
					_, err = initiation.New(a, st).Resume(ctx, agent.StartSpec{}, "session")
					if !errors.Is(err, initiation.ErrNotInitiated) || a.calls != before+1 {
						t.Fatalf("replay allowed: %v", err)
					}
				}
			})
		}
	}
}

func TestFailedAdapterStillSpendsMarker(t *testing.T) {
	_, a, g := fixture(t)
	boom := errors.New("adapter failed")
	a.err = boom
	ctx := human()
	if _, err := g.Start(ctx, agent.StartSpec{}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := g.Resume(ctx, agent.StartSpec{}, "session"); !errors.Is(err, initiation.ErrNotInitiated) || a.calls != 1 {
		t.Fatalf("failure retried: %v calls=%d", err, a.calls)
	}
}

func TestConcurrentUseSendsOnce(t *testing.T) {
	_, a, g := fixture(t)
	ctx := human()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := g.Start(ctx, agent.StartSpec{}); errs <- err })
	}
	wg.Wait()
	close(errs)
	allowed, denied := 0, 0
	for err := range errs {
		if err == nil {
			allowed++
		} else if errors.Is(err, initiation.ErrNotInitiated) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if allowed != 1 || denied != 1 || a.calls != 1 {
		t.Fatalf("allowed %d denied %d sends %d", allowed, denied, a.calls)
	}
}

func TestMissingTargetOrFailedStoreSendsNothing(t *testing.T) {
	st, a, g := fixture(t)
	if _, err := g.Start(initiation.With(context.Background(), initiation.UserAction("user", "web")), agent.StartSpec{}); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Start(human(), agent.StartSpec{}); err == nil || a.calls != 0 {
		t.Fatalf("store failed open: %v", err)
	}
}

func TestDecisionAnswerCannotMint(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), initiation.With(context.Background(), initiation.Marker{})} {
		if initiation.Valid(initiation.DecisionAnswer(ctx, "decision")) {
			t.Fatal("answer invented a capability")
		}
	}
}

// A failed send returns no session, even when the adapter hands one back (#356).
func TestFailedSendReturnsNoSession(t *testing.T) {
	_, a, g := fixture(t)
	a.err, a.sessOnErr = errors.New("boom"), true
	if s, err := g.Start(human(), agent.StartSpec{}); err == nil || s != nil {
		t.Errorf("Start = %v, %v", s, err)
	}
	if s, err := g.Resume(human(), agent.StartSpec{}, "id"); err == nil || s != nil {
		t.Errorf("Resume = %v, %v", s, err)
	}
}
