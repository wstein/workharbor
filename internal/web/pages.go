package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
)

func newKey() string { return randomHex(16) }

// flashes are the only messages a redirect may carry back to a page: a fixed set,
// never text from the query string.
var flashes = map[string]string{
	"started":   "The task was started.",
	"held":      "That issue is from an author you do not trust, so nothing started: its text is in the inbox for you to read first.",
	"answered":  "Your answer was recorded.",
	"cancelled": "The task was cancelled.",
	"injected":  "Sent: the agent has it now.",
	"next_turn": "Sent: the agent gets it at its next step.",
	"resumed":   "Sent: it starts a resumed turn.",
}

func flash(r *http.Request) string { return flashes[r.URL.Query().Get("flash")] }

// navOf builds what every page shows. A failure to count the inbox is not worth
// failing a page for.
func (s *Server) navOf(r *http.Request, sess Session, active string) nav {
	n := nav{CSRF: sess.CSRF, Active: active}
	if in, err := s.be.Inbox(r.Context()); err == nil {
		n.Inbox = len(in)
	}
	return n
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.opt.Auth.Session(r); ok {
		seeOther(w, r, "/")
		return
	}
	s.render(w, r, http.StatusOK, loginView("", s.passkeyMode(r.Context())))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// A sign-in form is a POST like the others: another site must not be able
	// to sign the browser in to its own session.
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || !sameHost(u.Host, r.Host) {
			s.render(w, r, http.StatusForbidden, loginView(errForbidden.msg, s.passkeyMode(r.Context())))
			return
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		s.render(w, r, http.StatusForbidden, loginView(errForbidden.msg, s.passkeyMode(r.Context())))
		return
	}
	if s.passkeyMode(r.Context()) { // no password fallback once a passkey is enrolled
		s.render(w, r, http.StatusForbidden, loginView("Sign in with a passkey. The token no longer signs in to the web UI.", true))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	err := s.opt.Auth.SignIn(w, r)
	switch {
	case err == nil:
		seeOther(w, r, "/")
	case errors.Is(err, ErrTooManyTries):
		s.render(w, r, http.StatusTooManyRequests, loginView(err.Error(), false))
	default:
		s.render(w, r, http.StatusUnauthorized, loginView(ErrBadCredentials.Error(), false))
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, _ Session) {
	s.opt.Auth.SignOut(w, r)
	seeOther(w, r, "/login")
}

func (s *Server) harbor(w http.ResponseWriter, r *http.Request, sess Session) {
	tasks, err := s.be.List(r.Context(), false)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	ws, err := s.be.WorkspaceList(r.Context())
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	p := harborPage{nav: s.navOf(r, sess, "harbor"), Key: newKey(), Flash: flash(r)}
	p.NeedsYou, p.Others = taskRows(tasks)
	for _, w := range ws {
		for _, a := range w.Agents {
			p.Agents = append(p.Agents, agentChoice{Ref: w.Workspace.Name + "/" + a.Role, Repo: w.Workspace.Repo})
		}
	}
	s.render(w, r, http.StatusOK, harborView(p))
}

// start starts a task from an issue on an agent: the service's Run, as
// `whr run` does. An issue by an untrusted author is held, and the page says so.
func (s *Server) start(w http.ResponseWriter, r *http.Request, sess Session) {
	issue, agentRef := strings.TrimSpace(r.PostForm.Get("issue")), strings.TrimSpace(r.PostForm.Get("agent"))
	if issue == "" || agentRef == "" {
		s.fail(w, r, sess, &domain.InvalidError{Msg: "an issue address and an agent are needed"})
		return
	}
	loc, err := s.once(r, func() (string, error) {
		res, err := s.be.Run(r.Context(), service.RunRequest{IssueURL: issue, Agent: agentRef})
		if err != nil {
			return "", err
		}
		if res.Held {
			return "/inbox?flash=held", nil
		}
		return "/tasks/" + url.PathEscape(string(res.Task)) + "?flash=started", nil
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request, sess Session) {
	in, err := s.be.Inbox(r.Context())
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	p := inboxPage{nav: s.navOf(r, sess, "inbox"), Decisions: decisionRows(in, newKey), Flash: flash(r)}
	p.StepUp = s.stepUpAvailable(r.Context())
	s.render(w, r, http.StatusOK, inboxView(p))
}

// answer records the human's answer to a Decision. It refuses a "Ready to push?"
// review whatever the form says: that answer needs the passkey step-up (D45),
// which this UI does not have, so there is no weaker path to it. An option the
// Decision does not offer is refused too.
func (s *Server) answer(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("decision"))
	option, reason := r.PostForm.Get("option"), strings.TrimSpace(r.PostForm.Get("reason"))
	open, err := s.be.Inbox(r.Context())
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	var d *domain.Decision
	for i := range open {
		if open[i].ID == id {
			d = &open[i]
		}
	}
	switch {
	case d == nil:
		s.fail(w, r, sess, &domain.NotFoundError{Kind: "open decision", ID: string(id)})
		return
	case sensitive(d):
		s.fail(w, r, sess, &httpError{status: http.StatusForbidden, msg: "this answer needs a fresh passkey assertion: use the passkey buttons in the inbox (they need JavaScript), or answer it with `whr approve` or `whr reject` on the host"})
		return
	case !contains(d.Options, option):
		s.fail(w, r, sess, &domain.InvalidError{Msg: "that is not one of the answers to this decision"})
		return
	}
	loc, err := s.once(r, func() (string, error) {
		if _, err := s.be.Answer(r.Context(), id, domain.Response{By: "web", Option: option, Reason: reason, At: s.opt.Now()}); err != nil {
			return "", err
		}
		return "/inbox?flash=answered", nil
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// maxShown is how many of the latest events the task page shows; the live stream
// adds the rest.
const maxShown = 100

func (s *Server) task(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	v, err := s.be.Show(r.Context(), id)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	evs, err := s.be.Log(r.Context(), id, 0, 1000)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	p := taskPageOf(v)
	p.nav, p.SayKey, p.Flash = s.navOf(r, sess, "harbor"), newKey(), flash(r)
	p.Decisions = decisionRows(v.Open, newKey)
	p.StepUp = s.stepUpAvailable(r.Context())
	if len(evs) > maxShown {
		p.Earlier = len(evs) - maxShown
		evs = evs[len(evs)-maxShown:]
	}
	for _, e := range evs {
		p.Events = append(p.Events, eventRowOf(e))
		if e.Seq > p.Since {
			p.Since = e.Seq
		}
	}
	s.render(w, r, http.StatusOK, taskView(p))
}

// say sends the running agent a message and reports how it was delivered.
func (s *Server) say(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	msg := strings.TrimSpace(r.PostForm.Get("message"))
	if msg == "" {
		s.fail(w, r, sess, &domain.InvalidError{Msg: "a message must not be empty"})
		return
	}
	loc, err := s.once(r, func() (string, error) {
		d, err := s.be.Say(r.Context(), id, msg)
		if err != nil {
			return "", err
		}
		return "/tasks/" + url.PathEscape(string(id)) + "?flash=" + flashOfDelivery(string(d)), nil
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

func flashOfDelivery(d string) string {
	switch d {
	case "injected":
		return "injected"
	case "resumed_turn":
		return "resumed"
	}
	return "next_turn"
}

// cancelPage asks before a cancel: it ends the run and cannot be undone.
func (s *Server) cancelPage(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	v, err := s.be.Show(r.Context(), id)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	s.render(w, r, http.StatusOK, cancelView(cancelPage{nav: s.navOf(r, sess, "harbor"), ID: string(id), Repo: v.Task.Repo, Issue: v.Task.Issue, Key: newKey()}))
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	loc, err := s.once(r, func() (string, error) {
		if err := s.be.Cancel(r.Context(), id); err != nil {
			return "", err
		}
		return "/tasks/" + url.PathEscape(string(id)) + "?flash=cancelled", nil
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

// open makes the supervisor-owned copy of the agent's branch for an editor (design
// §4.5, issue #59) and shows where it is, with the files in it that an editor may
// run by itself. It is never the agent's checkout. Making the copy twice is
// harmless, so it needs no idempotency key.
func (s *Server) open(w http.ResponseWriter, r *http.Request, sess Session) {
	id := domain.ID(r.PathValue("task"))
	v, err := s.be.Show(r.Context(), id)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	ws, role, ok := strings.Cut(v.Agent, "/")
	if !ok {
		s.fail(w, r, sess, &domain.InvalidError{Msg: "this task has no agent, so there is no branch to open"})
		return
	}
	cp, err := s.be.OpenCopy(r.Context(), ws, role)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	warnings := make([]string, 0, len(cp.Warnings))
	for _, w := range cp.Warnings {
		warnings = append(warnings, clip(w))
	}
	s.render(w, r, http.StatusOK, openView(openPage{nav: s.navOf(r, sess, "harbor"), ID: string(id), Repo: v.Task.Repo, Issue: v.Task.Issue, Path: cp.Path, Warnings: warnings}))
}
