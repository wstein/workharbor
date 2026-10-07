package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
)

// Setup runs diagnostics only, never setup actions. Check must return an
// export-safe artifact with secrets, home paths and host names redacted.
// The server caches the latest completed report; reads never invoke Check.
type Setup interface {
	Check(context.Context) (doctor.Artifact, error)
}

const setupCooldown = time.Minute

type setupState struct {
	mu      sync.Mutex
	running bool
	next    time.Time
	report  *doctor.Artifact
}

type setupPage struct {
	nav
	Key    string
	Report *doctor.Artifact
	Groups []doctor.PhaseGroup
}

func (s *Server) setupSnapshot() *doctor.Artifact {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	// Completed artifacts are immutable after publication.
	return s.setup.report
}

func (s *Server) setupAvailable(w http.ResponseWriter, r *http.Request, sess Session) bool {
	if s.opt.Setup != nil {
		return true
	}
	s.fail(w, r, sess, &httpError{http.StatusNotFound, "setup status is not available"})
	return false
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request, sess Session) {
	if !s.setupAvailable(w, r, sess) {
		return
	}
	report := s.setupSnapshot()
	p := setupPage{nav: s.navOf(r, sess, "setup"), Key: newKey(), Report: report}
	if report != nil {
		p.Groups = doctor.Present(report.Checks).Groups
	}
	s.render(w, r, http.StatusOK, setupView(p))
}

func (s *Server) setupCheck(w http.ResponseWriter, r *http.Request, sess Session) {
	if !s.setupAvailable(w, r, sess) {
		return
	}
	// This route has no execution parameters. Reject attempts to request a phase,
	// action or host step rather than silently accepting an execution-shaped form.
	for name := range r.PostForm {
		if name != "csrf" && name != "key" {
			s.fail(w, r, sess, errBadForm)
			return
		}
	}
	if len(r.URL.Query()) != 0 {
		s.fail(w, r, sess, errBadForm)
		return
	}
	// Require a full same origin, including scheme, for diagnostics that spend
	// the shared forge budget. The common form guard has already checked CSRF.
	origin, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if s.opt.SetupOrigin != "" {
		configured, parseErr := url.Parse(s.opt.SetupOrigin)
		if parseErr != nil {
			s.fail(w, r, sess, errForbidden)
			return
		}
		scheme, host = configured.Scheme, configured.Host
	}
	if err != nil || origin.Scheme != scheme || !sameHost(origin.Host, host) || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		s.fail(w, r, sess, errForbidden)
		return
	}
	loc, err := s.once(r, func() (string, error) {
		s.setup.mu.Lock()
		if s.setup.running {
			s.setup.mu.Unlock()
			return "", &httpError{http.StatusConflict, "a setup check is already running"}
		}
		if s.opt.Now().Before(s.setup.next) {
			s.setup.mu.Unlock()
			return "", &httpError{http.StatusTooManyRequests, "wait one minute between setup checks"}
		}
		s.setup.running = true
		s.setup.mu.Unlock()
		// Release the flag even if Check panics (ok stays false and the panic
		// continues). The cooldown starts only after a successful check, so a
		// failed one can be retried at once.
		var report doctor.Artifact
		var checkErr error
		ok := false
		defer func() {
			s.setup.mu.Lock()
			defer s.setup.mu.Unlock()
			s.setup.running = false
			if ok && checkErr == nil {
				report.Checks = append([]doctor.ReportCheck{}, report.Checks...)
				s.setup.report = &report
				s.setup.next = s.opt.Now().Add(setupCooldown)
			}
		}()
		report, checkErr = s.opt.Setup.Check(r.Context())
		ok = true
		return "/setup", checkErr
	})
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	seeOther(w, r, loc)
}

func (s *Server) setupReport(w http.ResponseWriter, r *http.Request, sess Session) {
	if !s.setupAvailable(w, r, sess) {
		return
	}
	report := s.setupSnapshot()
	if report == nil {
		s.fail(w, r, sess, &httpError{http.StatusNotFound, "run a setup check first"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="whr-setup-report.json"`)
	_ = json.NewEncoder(w).Encode(report)
}

func setupStatusLabel(status doctor.Status) string {
	switch status {
	case doctor.OK:
		return "OK"
	case doctor.Warn:
		return "Warning"
	case doctor.Fail:
		return "Failed"
	case doctor.NotVerified:
		return "Not verified"
	case doctor.Skipped:
		return "Skipped"
	default:
		return "Not verified"
	}
}

func setupHasFix(c doctor.ReportCheck) bool {
	return c.Fix != "" && (c.Status == doctor.Fail || c.Status == doctor.NotVerified || c.Status == doctor.Warn)
}
