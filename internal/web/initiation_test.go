package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/initiation"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

type initiationBackend struct {
	*fake
	t     *testing.T
	calls int
}

func (b *initiationBackend) check(ctx context.Context) {
	b.t.Helper()
	if !initiation.Valid(ctx) {
		b.t.Fatal("web action missing initiation")
	}
	b.calls++
}

func (b *initiationBackend) Run(ctx context.Context, _ service.RunRequest) (service.RunResult, error) {
	b.check(ctx)
	return service.RunResult{Task: "t1", Run: "r1"}, nil
}

func (b *initiationBackend) Say(ctx context.Context, _ domain.ID, _ string) (agent.Delivery, error) {
	b.check(ctx)
	return agent.DeliveryNextTurn, nil
}

func (b *initiationBackend) Resume(ctx context.Context, _ domain.ID) (domain.ID, error) {
	b.check(ctx)
	return "r1", nil
}

func (b *initiationBackend) Answer(ctx context.Context, _ domain.ID, _ domain.Response) (domain.ID, error) {
	b.check(ctx)
	return "", nil
}

func TestHumanFormsMintOnce(t *testing.T) {
	for _, action := range []string{"start", "say", "resume", "answer"} {
		t.Run(action, func(t *testing.T) {
			st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			be := &initiationBackend{fake: &fake{inbox: []domain.Decision{{ID: "d1", Kind: domain.DecisionQuestion, Cause: domain.CauseAuthExpired, Options: []string{domain.AnswerResume}}}}, t: t}
			s := &Server{be: be, opt: Options{Store: st, Now: time.Now}}
			for range 2 {
				req := httptest.NewRequestWithContext(context.Background(), "POST", "/human-action", nil)
				req.SetPathValue("task", "t1")
				req.SetPathValue("decision", "d1")
				req.PostForm = url.Values{"key": {"one-action"}, "issue": {"https://github.com/a/b/issues/1"}, "agent": {"workspace/agent"}, "message": {"human"}, "option": {domain.AnswerResume}}
				rec := httptest.NewRecorder()
				sess := Session{ID: "device"}
				switch action {
				case "start":
					s.start(rec, req, sess)
				case "say":
					s.say(rec, req, sess)
				case "resume":
					s.resume(rec, req, sess)
				case "answer":
					s.answer(rec, req, sess)
				}
				if rec.Code != 303 {
					t.Fatalf("response %d: %s", rec.Code, rec.Body.String())
				}
			}
			if be.calls != 1 {
				t.Fatalf("replay called backend %d times", be.calls)
			}
		})
	}
}
