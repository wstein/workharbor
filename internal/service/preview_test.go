package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

// stubUpstream is a runtime that can be dialled and listens nowhere.
type stubUpstream struct{}

func (stubUpstream) DialPreview(context.Context, string, int) (net.Conn, error) {
	return nil, errors.New("stub: nothing listens")
}

// withPreviewPorts gives the rig a repository that declares ports 3000 and 8080.
func (r *wsRig) withPreviewPorts() *Previews {
	r.ws.cfg.Environment = func(context.Context, string, string) (RepoEnvironment, error) {
		return RepoEnvironment{Environment: devcontainer.Environment{
			Origin: devcontainer.OriginDefault, Commit: strings.Repeat("a", 40),
			Config: devcontainer.Config{ForwardPorts: []int{8080}, Hints: devcontainer.Hints{PreviewPorts: []int{3000}}},
		}}, nil
	}
	p, err := NewPreviews(r.svc, r.ws, PreviewConfig{Upstream: stubUpstream{}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() {
		for _, pv := range p.List(bg) {
			p.Manager().Close(pv.ID, "test over")
		}
	})
	return p
}

func (r *wsRig) supervisorEvents(kind domain.EventKind) []domain.Event {
	r.t.Helper()
	evs, err := r.store.EventsSince(bg, domain.SupervisorStream, 0, 50)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []domain.Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Only a port the repository's environment declares may be previewed, only while
// the environment runs, and opening and closing are audited with who did it.
func TestAPreviewOpensOnlyADeclaredPortOfARunningEnvironment(t *testing.T) {
	r := newWsRig(t)
	p := r.withPreviewPorts()
	ws, a := r.create("run")
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	ports, err := p.Ports(bg, task)
	if err != nil || len(ports) != 2 || ports[0] != 3000 || ports[1] != 8080 {
		t.Fatalf("declared ports = %v, %v", ports, err)
	}
	if _, err := p.Open(bg, task, 22, "web:d1"); !errors.Is(err, ErrNoPreviewPort) {
		t.Errorf("an undeclared port: %v", err)
	}
	pv, err := p.Open(bg, task, 3000, "web:d1")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Task != string(task) || pv.Env != string(ws.EnvID) || pv.Port != 3000 {
		t.Errorf("preview = %+v", pv)
	}
	if again, _ := p.Open(bg, task, 3000, "web:d1"); again.ID != pv.ID {
		t.Error("the same port opened a second preview")
	}
	if got := p.OfTask(task); len(got) != 1 {
		t.Errorf("OfTask = %v", got)
	}
	if link, err := p.Link(bg, pv.ID); err != nil || !strings.Contains(link, "whr_preview=") {
		t.Errorf("link = %q, %v", link, err)
	}
	opened := r.supervisorEvents(domain.EventPreviewOpened)
	if len(opened) != 1 || !strings.Contains(string(opened[0].Payload), `"actor":"web:d1"`) || strings.Contains(string(opened[0].Payload), "whr_preview") {
		t.Fatalf("opened = %+v", opened)
	}
	if err := p.Close(bg, pv.ID, "web:d1"); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(bg, pv.ID, "web:d1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("closing a closed preview: %v", err)
	}
	if closed := r.supervisorEvents(domain.EventPreviewClosed); len(closed) != 1 || !strings.Contains(string(closed[0].Payload), "closed by web:d1") {
		t.Errorf("closed = %+v", closed)
	}
}

// A preview ends with its environment: nothing is opened on a stopped one, and a
// sweep after a stop closes what was open and says so in the audit log.
func TestAPreviewEndsWhenItsEnvironmentStops(t *testing.T) {
	r := newWsRig(t)
	p := r.withPreviewPorts()
	ws, a := r.create("run")
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open(bg, task, 3000, "api"); err != nil {
		t.Fatal(err)
	}
	r.clock.now = r.clock.now.Add(time.Minute) // the liveness check is cached for a moment
	if err := r.rt.Adapter.Stop(bg, string(ws.EnvID)); err != nil {
		t.Fatal(err)
	}
	p.Manager().Sweep(bg)
	if got := p.List(bg); len(got) != 0 {
		t.Fatalf("a preview of a stopped environment stayed: %v", got)
	}
	if closed := r.supervisorEvents(domain.EventPreviewClosed); len(closed) != 1 || !strings.Contains(string(closed[0].Payload), "environment stopped") {
		t.Errorf("closed = %+v", closed)
	}
	_, err = p.Open(bg, task, 3000, "api")
	var c *domain.ConflictError
	if !errors.As(err, &c) {
		t.Errorf("opening on a stopped environment: %v", err)
	}
	if _, err := p.Open(bg, "no-such-task", 3000, "api"); err == nil {
		t.Error("a preview of an unknown task was opened")
	}
}

// A runtime that cannot reach an environment's ports says so when a preview is
// asked for, instead of opening a listener that can only fail.
func TestPreviewsSayWhenTheRuntimeCannotReachAPort(t *testing.T) {
	r := newWsRig(t)
	r.withPreviewPorts()
	p, err := NewPreviews(r.svc, r.ws, PreviewConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, a := r.create("run")
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Open(bg, task, 3000, "web:d1")
	var c *domain.ConflictError
	if !errors.As(err, &c) || !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("open on a runtime that cannot dial: %v", err)
	}
	if len(p.List(bg)) != 0 {
		t.Error("a listener was opened for nothing")
	}
}
