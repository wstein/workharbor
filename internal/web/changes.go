package web

import (
	"context"
	"net/http"
	"time"

	"github.com/wstein/workharbor/internal/passkey"
)

// Change is a change of a repository's workflow that waits for the human (D47).
// Text is what a step-up names: the ID alone is not enough, so the assertion
// covers what is changed and not only which row.
type Change struct {
	ID, Repo, From, To, Text string
}

// Changes is what the UI needs for the changes that need a passkey (issue #107);
// the real one is in whr serve, over the store and the service. Confirm and
// RevokeTokens do what a host command does, and the UI only calls them after a
// step-up that named them.
type Changes interface {
	Open(ctx context.Context) ([]Change, error)
	Confirm(ctx context.Context, id, by string, at time.Time) error
	// CanRevokeTokens says whether the supervisor holds forge tokens.
	CanRevokeTokens() bool
	RevokeTokens(ctx context.Context, actor string) (int, error)
}

// revokeBinding is what the step-up of the token revocation names. It is fixed:
// the operation has no parameter.
var revokeBinding = passkey.Binding{Decision: "op:revoke-forge-tokens", SHA: "revoke-forge-tokens"}

func changeBinding(c Change) passkey.Binding {
	return passkey.Binding{Decision: "change:" + c.ID, SHA: c.Text}
}

// changes lists what waits for a passkey. Seeing it needs a session only: the
// page changes nothing.
func (s *Server) changes(w http.ResponseWriter, r *http.Request, sess Session) {
	p := changesPage{nav: s.navOf(r, sess, "changes"), Flash: flash(r), StepUp: s.stepUpAvailable(r.Context())}
	if s.opt.Changes != nil {
		open, err := s.opt.Changes.Open(r.Context())
		if err != nil {
			s.fail(w, r, sess, err)
			return
		}
		for _, c := range open {
			p.Changes = append(p.Changes, changeRow{ID: c.ID, Repo: c.Repo, From: c.From, To: c.To})
		}
		p.Revoke = s.opt.Changes.CanRevokeTokens()
	}
	s.render(w, r, http.StatusOK, changesView(p))
}

// openChange finds the change the path names among those still waiting.
func (s *Server) openChange(r *http.Request) (Change, bool) {
	if s.opt.Changes == nil {
		return Change{}, false
	}
	open, err := s.opt.Changes.Open(r.Context())
	if err != nil {
		return Change{}, false
	}
	for _, c := range open {
		if c.ID == r.PathValue("change") {
			return c, true
		}
	}
	return Change{}, false
}

func (s *Server) changeBegin(w http.ResponseWriter, r *http.Request, sess Session) {
	c, ok := s.openChange(r)
	if s.opt.Passkeys == nil || !sameOrigin(r) || !ok {
		jsonError(w, http.StatusNotFound, "that change is not waiting")
		return
	}
	s.beginStepUp(w, r, sess, changeBinding(c))
}

func (s *Server) changeFinish(w http.ResponseWriter, r *http.Request, sess Session) {
	c, ok := s.openChange(r)
	if s.opt.Passkeys == nil || !sameOrigin(r) || !ok {
		jsonError(w, http.StatusNotFound, "that change is not waiting")
		return
	}
	s.finishStepUp(w, r, sess, changeBinding(c), "confirm", func() (string, error) {
		return "/changes?flash=confirmed", s.opt.Changes.Confirm(r.Context(), c.ID, "web+passkey", s.opt.Now())
	})
}

func (s *Server) revokeBegin(w http.ResponseWriter, r *http.Request, sess Session) {
	if s.opt.Passkeys == nil || !sameOrigin(r) || s.opt.Changes == nil || !s.opt.Changes.CanRevokeTokens() {
		jsonError(w, http.StatusNotFound, "not available")
		return
	}
	s.beginStepUp(w, r, sess, revokeBinding)
}

func (s *Server) revokeFinish(w http.ResponseWriter, r *http.Request, sess Session) {
	if s.opt.Passkeys == nil || !sameOrigin(r) || s.opt.Changes == nil || !s.opt.Changes.CanRevokeTokens() {
		jsonError(w, http.StatusNotFound, "not available")
		return
	}
	s.finishStepUp(w, r, sess, revokeBinding, "revoke", func() (string, error) {
		n, err := s.opt.Changes.RevokeTokens(r.Context(), "web+passkey")
		if err != nil && n > 0 {
			// some went and the entry is written: say so, not that nothing was recorded
			return "/changes?flash=tokens_partial", nil
		}
		return "/changes?flash=tokens", err
	})
}

// beginStepUp starts a fresh assertion whose challenge names want and belongs to
// this session.
func (s *Server) beginStepUp(w http.ResponseWriter, r *http.Request, sess Session, want passkey.Binding) {
	opts, cer, err := s.opt.Passkeys.StepUpBegin(r.Context(), sess.CSRF, want)
	if err != nil {
		jsonError(w, passkeyStatus(err, http.StatusForbidden), passkeyMessage(err))
		return
	}
	jsonReply(w, http.StatusOK, map[string]any{"options": opts, "ceremony": cer})
}
