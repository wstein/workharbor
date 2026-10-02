package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/preview"
	"github.com/wstein/workharbor/internal/service"
)

// fakePreviews is the preview service with its calls counted.
type fakePreviews struct {
	mu     sync.Mutex
	ports  []int
	open   []preview.Preview
	opens  int
	actors []string
}

func (f *fakePreviews) Ports(context.Context, domain.ID) ([]int, error) { return f.ports, nil }

func (f *fakePreviews) Open(_ context.Context, task domain.ID, port int, actor string) (preview.Preview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	f.actors = append(f.actors, actor)
	for _, p := range f.open {
		if p.Port == port {
			return p, nil
		}
	}
	p := preview.Preview{ID: "pv-1", Task: string(task), Env: "env-secret", Port: port, Listen: 9401, Opened: t0, Expires: t0.Add(time.Hour)}
	f.open = append(f.open, p)
	return p, nil
}

func (f *fakePreviews) Link(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.open {
		if p.ID == id {
			return "https://whr.example.test:9401/?whr_preview=grant1", nil
		}
	}
	return "", &domain.NotFoundError{Kind: "preview", ID: id}
}

func (f *fakePreviews) List(context.Context) []preview.Preview {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]preview.Preview(nil), f.open...)
}

func (f *fakePreviews) OfTask(task domain.ID) []preview.Preview {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []preview.Preview
	for _, p := range f.open {
		if p.Task == string(task) {
			out = append(out, p)
		}
	}
	return out
}

func (f *fakePreviews) Close(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.open {
		if p.ID == id {
			f.open = append(f.open[:i], f.open[i+1:]...)
			return nil
		}
	}
	return &domain.NotFoundError{Kind: "preview", ID: id}
}

func TestAPreviewIsStartedLinkedAndClosedFromTheTaskPage(t *testing.T) {
	f := &fakePreviews{ports: []int{3000, 8080}}
	r := newRigWith(t, func(o *Options) { o.Previews = f })
	r.be.show = func(id domain.ID) (service.TaskView, error) {
		return service.TaskView{Task: domain.Task{ID: id, Repo: "o/r", Issue: "#1", State: domain.TaskRunning}, Agent: "w/a"}, nil
	}
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(page, "port 3000") || !strings.Contains(page, "port 8080") || !strings.Contains(page, "Start a preview") || strings.Contains(page, "Open preview") {
		t.Fatalf("declared ports without a preview:\n%s", page)
	}
	if strings.Contains(page, "env-secret") || strings.Contains(page, "whr_preview") {
		t.Error("the page carries the environment or a grant")
	}
	csrf, key := b.form("/tasks/t1")
	startForm := url.Values{"csrf": {csrf}, "key": {key}, "port": {"3000"}}
	resp, _ := b.do("POST", "/tasks/t1/previews", startForm)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=preview_started") {
		t.Fatalf("start: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	b.do("POST", "/tasks/t1/previews", startForm) // a double tap with the same key
	if f.opens != 1 || !strings.HasPrefix(f.actors[0], "web:") {
		t.Errorf("opens = %d, actors %v: the same form key must open once, as the device", f.opens, f.actors)
	}

	_, page = b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(page, `href="/previews/pv-1/go"`) || !strings.Contains(page, "Close") {
		t.Fatalf("an open preview is not offered:\n%s", page)
	}
	// the inbox lists it too
	if _, inbox := b.do("GET", "/inbox", nil); !strings.Contains(inbox, "Open previews") || !strings.Contains(inbox, "port 3000") {
		t.Errorf("the inbox does not show the open preview:\n%s", inbox)
	}

	// the link redirects to the preview's own origin, with no referrer
	resp, _ = b.do("GET", "/previews/pv-1/go", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "https://whr.example.test:9401/?whr_preview=grant1" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("go: %d %q %q", resp.StatusCode, resp.Header.Get("Location"), resp.Header.Get("Referrer-Policy"))
	}
	if resp, _ = b.do("GET", "/previews/nope/go", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("go for an unknown preview: %d", resp.StatusCode)
	}
	// closing needs the CSRF token
	if resp, _ = b.do("POST", "/previews/pv-1/close", url.Values{}); resp.StatusCode != http.StatusForbidden || len(f.open) != 1 {
		t.Errorf("close without a token: %d", resp.StatusCode)
	}
	resp, _ = b.do("POST", "/previews/pv-1/close", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther || len(f.open) != 0 {
		t.Errorf("close: %d, %d open", resp.StatusCode, len(f.open))
	}
}

// Without a session nothing about previews is reachable, and without the proxy the
// UI shows none and refuses the routes.
func TestPreviewRoutesNeedASessionAndTheProxy(t *testing.T) {
	f := &fakePreviews{ports: []int{3000}, open: []preview.Preview{{ID: "pv-1", Task: "t1", Port: 3000}}}
	r := newRigWith(t, func(o *Options) { o.Previews = f })
	anon := r.browser()
	if resp, _ := anon.do("GET", "/previews/pv-1/go", nil); resp.StatusCode != http.StatusSeeOther || !strings.HasSuffix(resp.Header.Get("Location"), "/login") {
		t.Errorf("go without a session: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := anon.do("POST", "/previews/pv-1/close", url.Values{}); resp.StatusCode == http.StatusSeeOther && len(f.open) == 0 {
		t.Error("an anonymous close worked")
	}

	off := newRig(t)
	b := off.browser()
	b.signIn()
	csrf, key := b.form("/inbox")
	if resp, _ := b.do("POST", "/tasks/t1/previews", url.Values{"csrf": {csrf}, "key": {key}, "port": {"3000"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("start without the proxy: %d", resp.StatusCode)
	}
	if resp, _ := b.do("GET", "/previews/pv-1/go", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("go without the proxy: %d", resp.StatusCode)
	}
	if _, page := b.do("GET", "/tasks/t1", nil); strings.Contains(page, "Start a preview") {
		t.Error("the task page offers previews without the proxy")
	}
}
