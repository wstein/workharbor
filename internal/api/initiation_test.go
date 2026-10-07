package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
		b.t.Fatal("API action missing initiation")
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

func TestHumanEndpointsMintOnceAfterAuthentication(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/v1/tasks", `{"issue_url":"https://github.com/a/b/issues/1","agent":"workspace/agent"}`},
		{"/v1/tasks/t1/say", `{"message":"human"}`},
		{"/v1/tasks/t1/resume", ``},
		{"/v1/decisions/d1/answer", `{"option":"resume"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			be := &initiationBackend{fake: &fake{}, t: t}
			s, err := New(be, Options{Token: []byte("fixture-token"), Store: st})
			if err != nil {
				t.Fatal(err)
			}
			for _, authorized := range []bool{false, true, true} {
				req := httptest.NewRequestWithContext(context.Background(), "POST", tc.path, strings.NewReader(tc.body))
				req.Header.Set("Idempotency-Key", "one-human-action")
				if authorized {
					req.Header.Set("Authorization", "Bearer fixture-token")
				}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if authorized && rec.Code >= 300 {
					t.Fatalf("response %d: %s", rec.Code, rec.Body.String())
				}
				if !authorized && be.calls != 0 {
					t.Fatal("unauthenticated request minted")
				}
			}
			if be.calls != 1 {
				t.Fatalf("replay called backend %d times", be.calls)
			}
		})
	}
}
