package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/preview"
)

type fakePreviews struct {
	open    []preview.Preview
	actors  []string
	opens   int
	closeds []string
}

func (f *fakePreviews) Ports(context.Context, domain.ID) ([]int, error) { return []int{3000}, nil }

func (f *fakePreviews) Open(_ context.Context, task domain.ID, port int, actor string) (preview.Preview, error) {
	f.opens++
	f.actors = append(f.actors, actor)
	if task == "nope" {
		return preview.Preview{}, &domain.NotFoundError{Kind: "task", ID: "nope"}
	}
	if port != 3000 {
		return preview.Preview{}, domain.NewConflict(domain.RuleEnvRunning, "that port is not declared")
	}
	p := preview.Preview{ID: "pv-1", Task: string(task), Env: "env-1", Port: port, Listen: 9401, Opened: t0, Expires: t0.Add(12 * time.Hour)}
	f.open = []preview.Preview{p}
	return p, nil
}

func (f *fakePreviews) Link(_ context.Context, id string) (string, error) {
	if id != "pv-1" || len(f.open) == 0 {
		return "", &domain.NotFoundError{Kind: "preview", ID: id}
	}
	return "https://whr.example.test:9401/?whr_preview=grant", nil
}

func (f *fakePreviews) List(context.Context) []preview.Preview { return f.open }
func (f *fakePreviews) OfTask(domain.ID) []preview.Preview     { return f.open }

func (f *fakePreviews) Close(_ context.Context, id, _ string) error {
	if id != "pv-1" || len(f.open) == 0 {
		return &domain.NotFoundError{Kind: "preview", ID: id}
	}
	f.open = nil
	f.closeds = append(f.closeds, id)
	return nil
}

func withPreviews(t *testing.T, r *rig, f *fakePreviews) {
	t.Helper()
	r.ts.Close()
	var err error
	r.srv, err = New(r.be, Options{Token: []byte(token), Store: r.st, Previews: f, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
}

func TestPreviewRoutesAreOffUntilConfigured(t *testing.T) {
	r := newRig(t)
	for _, c := range [][3]string{{"GET", "/v1/previews", ""}, {"POST", "/v1/tasks/t1/previews", `{"port":3000}`}, {"POST", "/v1/previews/x/link", ""}, {"DELETE", "/v1/previews/x", ""}} {
		status, _, body := r.do(c[0], c[1], c[2])
		if status != 409 || !strings.Contains(body, "previews are off") {
			t.Errorf("%s %s = %d %s", c[0], c[1], status, body)
		}
	}
}

func TestAPreviewIsOpenedListedLinkedAndClosed(t *testing.T) {
	r := newRig(t)
	f := &fakePreviews{}
	withPreviews(t, r, f)

	status, _, body := r.do("POST", "/v1/tasks/t1/previews", `{"port":3000}`)
	if status != 201 || !strings.Contains(body, `"url":"https://whr.example.test:9401/?whr_preview=grant"`) || !strings.Contains(body, `"listen_port":9401`) {
		t.Fatalf("open = %d %s", status, body)
	}
	golden(t, "open-preview", status, body)
	if f.actors[0] != "api" {
		t.Errorf("actor = %q", f.actors[0])
	}
	status, _, body = r.do("GET", "/v1/previews", "")
	if status != 200 || !strings.Contains(body, `"id":"pv-1"`) || strings.Contains(body, "grant") || strings.Contains(body, "env-1") {
		t.Errorf("list = %d %s: no link and no environment may be in it", status, body)
	}
	golden(t, "previews", status, body)
	if status, _, body = r.do("POST", "/v1/previews/pv-1/link", ""); status != 200 || !strings.Contains(body, "whr_preview=") {
		t.Errorf("link = %d %s", status, body)
	}
	if status, _, _ = r.do("POST", "/v1/previews/nope/link", ""); status != 404 {
		t.Errorf("a link for an unknown preview: %d", status)
	}
	// bad bodies
	for _, b := range []string{`{"port":0}`, `{"port":70000}`, `{}`, `{"port":3000,"extra":1}`, ``} {
		if status, _, _ = r.do("POST", "/v1/tasks/t1/previews", b); status != 400 {
			t.Errorf("body %q = %d, want 400", b, status)
		}
	}
	if status, _, body = r.do("POST", "/v1/tasks/t1/previews", `{"port":22}`); status != 409 {
		t.Errorf("an undeclared port: %d %s", status, body)
	}
	if status, _, _ = r.do("POST", "/v1/tasks/nope/previews", `{"port":3000}`); status != 404 {
		t.Errorf("an unknown task: %d", status)
	}
	if status, _, _ = r.do("POST", "/v1/tasks/t1/previews", `{"port":3000}`, "Authorization", ""); status != 401 {
		t.Errorf("no token: %d", status)
	}
	if status, _, _ = r.do("DELETE", "/v1/previews/pv-1", ""); status != 200 || len(f.closeds) != 1 {
		t.Errorf("close = %d, closed %v", status, f.closeds)
	}
	if status, _, _ = r.do("DELETE", "/v1/previews/pv-1", ""); status != 404 {
		t.Errorf("closing a closed preview: %d", status)
	}
}
