package api

import (
	"context"
	"net/http"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/preview"
)

// Previews is what the preview commands and the web UI need of the preview proxy
// (design D33, issue #72); the real one is *service.Previews. It is a separate
// interface so a runtime that cannot reach an environment's ports leaves it off.
type Previews interface {
	// Ports are the ports a task's environment declares for previews.
	Ports(ctx context.Context, task domain.ID) ([]int, error)
	// Open opens a preview of one of them, or returns the one already open.
	Open(ctx context.Context, task domain.ID, port int, actor string) (preview.Preview, error)
	// Link makes a link that opens a preview in a browser once, for five minutes.
	Link(ctx context.Context, id string) (string, error)
	List(ctx context.Context) []preview.Preview
	OfTask(task domain.ID) []preview.Preview
	Close(ctx context.Context, id, actor string) error
}

type previewBody struct {
	Port int `json:"port"`
}

// PreviewView is a preview as the JSON API shows it: no secret, and where it is.
type PreviewView struct {
	ID      string    `json:"id"`
	Task    string    `json:"task"`
	Port    int       `json:"port"`
	Listen  int       `json:"listen_port"`
	Opened  time.Time `json:"opened_at"`
	Expires time.Time `json:"expires_at"`
}

func previewView(p preview.Preview) PreviewView {
	return PreviewView{ID: p.ID, Task: p.Task, Port: p.Port, Listen: p.Listen, Opened: p.Opened.UTC(), Expires: p.Expires.UTC()}
}

func (s *Server) previews() (Previews, error) {
	if s.opt.Previews == nil {
		return nil, domain.NewConflict("previews_off", "previews are off: set preview.first_port and preview.last_port in the configuration (design D33)")
	}
	return s.opt.Previews, nil
}

func (s *Server) listPreviews(w http.ResponseWriter, r *http.Request) {
	p, err := s.previews()
	if err != nil {
		writeError(w, err)
		return
	}
	out := []PreviewView{}
	for _, pv := range p.List(r.Context()) {
		out = append(out, previewView(pv))
	}
	writeOK(w, http.StatusOK, out)
}

// openPreview opens a preview and returns a link for it. It is not idempotent on
// purpose: a retry makes another link, and a stored response would keep a grant.
func (s *Server) openPreview(w http.ResponseWriter, r *http.Request) {
	p, err := s.previews()
	if err != nil {
		writeError(w, err)
		return
	}
	task, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	var b previewBody
	if _, err := readBody(w, r, &b); err != nil {
		writeError(w, err)
		return
	}
	if b.Port < 1 || b.Port > 65535 {
		writeError(w, usageError{"port must be a TCP port from 1 to 65535"})
		return
	}
	pv, err := p.Open(r.Context(), task, b.Port, "api")
	if err != nil {
		s.fail(w, err)
		return
	}
	link, err := p.Link(r.Context(), pv.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusCreated, map[string]any{"preview": previewView(pv), "url": link})
}

// previewLink makes another link for an open preview.
func (s *Server) previewLink(w http.ResponseWriter, r *http.Request) {
	p, err := s.previews()
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := idParam(r, "preview")
	if err != nil {
		writeError(w, err)
		return
	}
	link, err := p.Link(r.Context(), string(id))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, map[string]string{"url": link})
}

func (s *Server) closePreview(w http.ResponseWriter, r *http.Request) {
	p, err := s.previews()
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := idParam(r, "preview")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := p.Close(r.Context(), string(id), "api"); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}
