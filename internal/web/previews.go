package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/wstein/workharbor/internal/domain"
)

// previewRows fills the task page's previews: each port the task's environment
// declares, with its open preview if there is one (D33). A task whose ports cannot
// be read shows none.
func (s *Server) previewRows(ctx context.Context, task domain.ID, p *taskPage) {
	if s.opt.Previews == nil {
		return
	}
	ports, err := s.opt.Previews.Ports(ctx, task)
	if err != nil {
		return
	}
	open := map[int]string{}
	for _, v := range s.opt.Previews.OfTask(task) {
		open[v.Port] = v.ID
	}
	p.PreviewsOn = true
	for _, port := range ports {
		p.Previews = append(p.Previews, previewRow{ID: open[port], TaskID: string(task), Port: port, Key: newKey()})
	}
}

// actorOf names who did something from the web: a device, never a secret.
func actorOf(sess Session) string { return "web:" + sess.ID }

// startPreview opens a preview of one declared port. The session is enough: it
// shows the human a page, answers no Decision and holds no secret, and what it
// opens is agent-written code that runs on an origin of its own.
func (s *Server) startPreview(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	port, perr := strconv.Atoi(r.PostForm.Get("port"))
	if s.opt.Previews == nil || perr != nil {
		s.fail(w, r, sess, &httpError{status: http.StatusNotFound, msg: "previews are not available"})
		return
	}
	loc, err := s.once(r, func() (string, error) {
		if _, err := s.opt.Previews.Open(r.Context(), id, port, actorOf(sess)); err != nil {
			return "", err
		}
		return "/tasks/" + url.PathEscape(string(id)) + "?flash=preview_started", nil
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

// goPreview sends the browser to a preview with a single-use grant in the link,
// which the preview trades for its own cookie. It is a link and not a form
// because the preview has another origin, which the page's form-action forbids.
// The session cookie is SameSite=Strict, so another site cannot make this request
// with the human's session.
func (s *Server) goPreview(w http.ResponseWriter, r *http.Request, sess Session) {
	if s.opt.Previews == nil {
		s.fail(w, r, sess, &httpError{status: http.StatusNotFound, msg: "previews are not available"})
		return
	}
	link, err := s.opt.Previews.Link(r.Context(), r.PathValue("preview"))
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, link, http.StatusSeeOther) //nolint:gosec // the link is made by the supervisor for a preview it holds, never from the request
}

// closePreview closes a preview. Closing one that is gone says so.
func (s *Server) closePreview(w http.ResponseWriter, r *http.Request, sess Session) {
	if s.opt.Previews == nil {
		s.fail(w, r, sess, &httpError{status: http.StatusNotFound, msg: "previews are not available"})
		return
	}
	if err := s.opt.Previews.Close(r.Context(), r.PathValue("preview"), actorOf(sess)); err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, "/inbox?flash=preview_closed")
}
