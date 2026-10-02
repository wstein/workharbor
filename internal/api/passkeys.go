package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/passkey"
)

// Passkeys is what the host's passkey commands need (design D45); the real one is
// *passkey.Service. These routes are for the host's CLI and its API token only:
// the web UI has no way to enrol or revoke a passkey.
type Passkeys interface {
	NewEnrolment(name string) (token string, expires time.Time, err error)
	List(ctx context.Context) ([]passkey.Info, error)
	Revoke(ctx context.Context, idOrPrefix string) error
	Origin() string
}

type enrolBody struct {
	Name string `json:"name"`
}

// passkeys returns the configured passkeys, or says how to turn them on.
func (s *Server) passkeys() (Passkeys, error) {
	if s.opt.Passkeys == nil {
		return nil, domain.NewConflict("passkeys_off", "passkeys are off: set public_url in the configuration to whr's https name (design D45)")
	}
	return s.opt.Passkeys, nil
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	p, err := s.passkeys()
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := p.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if list == nil {
		list = []passkey.Info{}
	}
	writeOK(w, http.StatusOK, list)
}

// newEnrolment makes a one-time enrolment link. It is not idempotent on purpose: a
// retry makes another link, and a stored response would keep the token.
func (s *Server) newEnrolment(w http.ResponseWriter, r *http.Request) {
	p, err := s.passkeys()
	if err != nil {
		writeError(w, err)
		return
	}
	var b enrolBody
	if _, err := readBody(w, r, &b); err != nil {
		writeError(w, err)
		return
	}
	if b.Name == "" {
		b.Name = "passkey"
	}
	if len(b.Name) > 64 {
		writeError(w, usageError{"the passkey name is at most 64 characters"})
		return
	}
	token, expires, err := p.NewEnrolment(b.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusCreated, map[string]any{
		"url":        p.Origin() + "/enrol?token=" + url.QueryEscape(token),
		"expires_at": expires.UTC(),
	})
}

func (s *Server) revokePasskey(w http.ResponseWriter, r *http.Request) {
	p, err := s.passkeys()
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := idParam(r, "passkey")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := p.Revoke(r.Context(), string(id)); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}
