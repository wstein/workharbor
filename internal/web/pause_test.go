package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
)

func (r *rig) showRun(state domain.RunState) {
	r.be.show = func(id domain.ID) (service.TaskView, error) {
		return service.TaskView{Task: domain.Task{ID: id, Repo: "wstein/workharbor", Issue: "#7", State: domain.TaskRunning}, Runs: []domain.Run{{ID: "r1", State: state}}, Agent: "docs/runtime"}, nil
	}
}

// A running run offers Pause, a paused one Resume, and each calls the service
// once, with the form's idempotency key.
func TestThePageOffersPauseWhileRunningAndResumeWhilePaused(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()

	r.showRun(domain.RunRunning)
	csrf, key := b.form("/tasks/t1")
	_, page := b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(page, `action="/tasks/t1/pause"`) || strings.Contains(page, "/resume") {
		t.Fatalf("a running run:\n%s", page)
	}
	if resp, _ := b.do("POST", "/tasks/t1/pause", url.Values{"csrf": {csrf}}); resp.StatusCode == http.StatusSeeOther {
		t.Error("a pause without an idempotency key was accepted")
	}
	resp, _ := b.do("POST", "/tasks/t1/pause", url.Values{"csrf": {csrf}, "key": {key + "p"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=paused") {
		t.Fatalf("pause: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// the same form sent twice pauses once
	b.do("POST", "/tasks/t1/pause", url.Values{"csrf": {csrf}, "key": {key + "p"}})
	if len(r.be.pauses) != 1 || r.be.pauses[0] != "t1" {
		t.Errorf("pauses = %v", r.be.pauses)
	}
	// without the CSRF token nothing happens
	b.do("POST", "/tasks/t1/pause", url.Values{"key": {key + "q"}})
	if len(r.be.pauses) != 1 {
		t.Errorf("a pause without CSRF reached the service: %v", r.be.pauses)
	}

	r.showRun(domain.RunPaused)
	_, page = b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(page, `action="/tasks/t1/resume"`) || strings.Contains(page, "/pause") {
		t.Fatalf("a paused run:\n%s", page)
	}
	resp, _ = b.do("POST", "/tasks/t1/resume", url.Values{"csrf": {csrf}, "key": {key + "r"}})
	if resp.StatusCode != http.StatusSeeOther || len(r.be.resumes) != 1 {
		t.Errorf("resume: %d, %v", resp.StatusCode, r.be.resumes)
	}
	r.showRun(domain.RunStopped)
	if _, page = b.do("GET", "/tasks/t1", nil); strings.Contains(page, "/pause") || strings.Contains(page, "/resume") {
		t.Error("a stopped run offers pause or resume")
	}
}

// The purge page says what goes and what stays before anything is deleted.
func TestPurgeAsksFirstAndSaysWhatGoesAndWhatStays(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	resp, page := b.do("GET", "/tasks/t1/purge", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d", resp.StatusCode)
	}
	for _, want := range []string{"12 transcript event(s)", "3400 byte(s)", "audit entries, the usage records and the Decisions stay", "recorded as an audit entry", `action="/tasks/t1/purge"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if len(r.be.purges) != 0 {
		t.Fatal("looking at the page purged")
	}
	csrf, key := b.form("/tasks/t1/purge")
	if resp, _ := b.do("POST", "/tasks/t1/purge", url.Values{"key": {key}}); resp.StatusCode == http.StatusSeeOther || len(r.be.purges) != 0 {
		t.Error("a purge without CSRF went through")
	}
	resp, _ = b.do("POST", "/tasks/t1/purge", url.Values{"csrf": {csrf}, "key": {key}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=purged") {
		t.Fatalf("purge: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	b.do("POST", "/tasks/t1/purge", url.Values{"csrf": {csrf}, "key": {key}})
	if len(r.be.purges) != 1 || r.be.purges[0] != "t1 by web" {
		t.Errorf("purges = %v", r.be.purges)
	}
}
