package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/preview"
	"github.com/wstein/workharbor/internal/runtime"
)

// PreviewConfig is what the preview operations need besides the service. Without a
// range of ports there is no Previews at all.
type PreviewConfig struct {
	// Upstream reaches a port of an environment: the runtime, if its adapter
	// implements runtime.Previewer. Nil makes every open fail as unavailable.
	Upstream runtime.Previewer
	// FirstPort and LastPort are the loopback ports previews listen on.
	FirstPort, LastPort int
	// Scheme and Host make the link the human opens: the forwarder's name.
	Scheme, Host string
}

// Previews opens a preview of a dev server the agent runs (design D33, issue #72).
// It decides what may be previewed and by whom and keeps the audit trail; the
// proxy itself is package preview.
type Previews struct {
	svc *Service
	ws  *Workspaces
	mgr *preview.Manager
	// reachable says the runtime can dial an environment's ports.
	reachable bool
}

// ErrNoPreviewPort means the port is not one the environment declares.
var ErrNoPreviewPort = domain.NewConflict(domain.RuleEnvRunning, "that port is not one this repository's environment declares for previews (forwardPorts, or customizations.workharbor.previewPorts)")

// NewPreviews returns the preview operations. The manager closes a preview when
// its environment stops, and every close is audited.
func NewPreviews(s *Service, w *Workspaces, cfg PreviewConfig) (*Previews, error) {
	p := &Previews{svc: s, ws: w, reachable: cfg.Upstream != nil}
	up := cfg.Upstream
	if up == nil {
		up = unavailable{}
	}
	m, err := preview.New(preview.Config{
		Upstream: up, FirstPort: cfg.FirstPort, LastPort: cfg.LastPort, Scheme: cfg.Scheme, Host: cfg.Host,
		Live: func(ctx context.Context, env string) bool {
			info, err := s.rt.Inspect(ctx, env)
			return err == nil && info.State == domain.EnvRunning
		},
		Now: s.clock.Now,
		OnClosed: func(pv preview.Preview, reason string) {
			p.audit(context.Background(), domain.EventPreviewClosed, pv, "", reason)
		},
		OnError: s.report,
	})
	if err != nil {
		return nil, err
	}
	p.mgr = m
	return p, nil
}

type unavailable struct{}

func (unavailable) DialPreview(context.Context, string, int) (net.Conn, error) {
	return nil, preview.ErrUnavailable
}

// Manager returns the proxy, for the supervisor to run its sweep.
func (p *Previews) Manager() *preview.Manager { return p.mgr }

// environmentOf finds the environment and the declared preview ports of a task.
func (p *Previews) environmentOf(ctx context.Context, task domain.ID) (env string, ports []int, err error) {
	agg, err := p.svc.store.LoadTask(ctx, task)
	if err != nil {
		return "", nil, err
	}
	t := agg.Task()
	if t.AgentID == "" {
		return "", nil, domain.NewConflict(domain.RuleEnvRunning, "task %s has no environment", task)
	}
	_, ws, err := p.ws.agentAndWorkspace(ctx, t.AgentID)
	if err != nil {
		return "", nil, err
	}
	re, ok := p.ws.repoEnvironment(ctx, ws)
	if !ok {
		return string(ws.EnvID), nil, nil
	}
	return string(ws.EnvID), re.PreviewPorts(), nil
}

// Ports returns the ports a task's environment declares for previews, read from
// the repository's default branch (D38): the ones the human may open.
func (p *Previews) Ports(ctx context.Context, task domain.ID) ([]int, error) {
	_, ports, err := p.environmentOf(ctx, task)
	return ports, err
}

// Open opens a preview of one declared port of a task's environment, or returns
// the one already open. Only the human asks for one (the agent has no way to), and
// the environment must be running.
func (p *Previews) Open(ctx context.Context, task domain.ID, port int, actor string) (preview.Preview, error) {
	if !p.reachable {
		return preview.Preview{}, domain.NewConflict("previews_unavailable", "this runtime cannot reach an environment's ports yet, so there is nothing to preview through (issues #72, #69)")
	}
	env, ports, err := p.environmentOf(ctx, task)
	if err != nil {
		return preview.Preview{}, err
	}
	if !slices.Contains(ports, port) {
		return preview.Preview{}, ErrNoPreviewPort
	}
	pv, created, err := p.mgr.Open(ctx, string(task), env, port, sessionOf(actor))
	if errors.Is(err, preview.ErrNotRunning) {
		return preview.Preview{}, domain.NewConflict(domain.RuleEnvRunning, "the environment is not running, so there is nothing to preview")
	}
	if err != nil {
		return preview.Preview{}, err
	}
	if created {
		p.audit(ctx, domain.EventPreviewOpened, pv, actor, "")
	}
	return pv, nil
}

// sessionOf is the web session an actor names ("web:<session>"), or empty for the
// host's CLI and API, which no session ends.
func sessionOf(actor string) string {
	if id, ok := strings.CutPrefix(actor, "web:"); ok {
		return id
	}
	return ""
}

// CloseSession closes the previews that only this web session opened: no preview
// outlives the session that opened it.
func (p *Previews) CloseSession(sessionID string) {
	p.mgr.CloseOwner(sessionID, "its session ended")
}

// Link returns a link that opens a preview in a browser once, for five minutes.
func (p *Previews) Link(_ context.Context, id string) (string, error) {
	link, err := p.mgr.Grant(id)
	if errors.Is(err, preview.ErrNotFound) {
		return "", &domain.NotFoundError{Kind: "preview", ID: id}
	}
	return link, err
}

// List returns the open previews.
func (p *Previews) List(context.Context) []preview.Preview { return p.mgr.List() }

// OfTask returns the open previews of a task.
func (p *Previews) OfTask(task domain.ID) []preview.Preview {
	var out []preview.Preview
	for _, pv := range p.mgr.List() {
		if pv.Task == string(task) {
			out = append(out, pv)
		}
	}
	return out
}

// Close closes a preview. One that is already closed is not an error.
func (p *Previews) Close(_ context.Context, id, actor string) error {
	for _, pv := range p.mgr.List() {
		if pv.ID == id {
			p.mgr.Close(id, "closed by "+actor)
			return nil
		}
	}
	return &domain.NotFoundError{Kind: "preview", ID: id}
}

func (p *Previews) audit(ctx context.Context, kind domain.EventKind, pv preview.Preview, actor, reason string) {
	saved, err := p.svc.store.Append(ctx, domain.NewPreviewEvent(kind, domain.PreviewChange{Preview: pv.ID, Task: pv.Task, Port: pv.Port, Actor: actor, Reason: reason}, p.svc.clock.Now()))
	if err != nil {
		p.svc.report(fmt.Errorf("preview audit entry: %w", err))
		return
	}
	p.svc.publish(saved)
}
