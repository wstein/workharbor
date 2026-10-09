package main

import (
	"context"
	"os"
	"os/user"
	"regexp"
	goruntime "runtime"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/redact"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/serve"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/setup"
	"github.com/wstein/workharbor/internal/version"
	"github.com/wstein/workharbor/internal/web"
)

// serveSetup exposes only doctor.Run, which reads checks and formats fix text.
// No wizard, fix execution, Host.Run or sudo operation is reachable here.
type serveSetup struct {
	deps     doctor.Deps
	redactor *redact.Redactor
	hostname string
	now      func() time.Time
	checks   func(doctor.Deps) []doctor.Check
}

func newServeSetup(cfg *config.Config, path, exe, home string) (web.Setup, error) {
	env, _, err := service.AgentCredentials(cfg)
	if err != nil {
		return nil, err
	}
	rd, err := serve.Redactor(cfg, env)
	if err != nil {
		return nil, err
	}
	account, err := user.Current()
	if err != nil {
		return nil, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	return &serveSetup{
		deps: doctor.Deps{
			ConfigPath: path, Home: home, FS: runtime.OSFS{}, LookPath: doctor.DefaultLookPath,
			Runner: setup.Terminal{}, GOOS: goruntime.GOOS, User: account.Username, Account: doctor.WhrUser,
			UID: os.Getuid(), Whr: exe, Prefix: doctor.DefaultPrefix,
		},
		redactor: rd, hostname: hostname, now: time.Now, checks: doctor.Checks,
	}, nil
}

var setupHomePath = regexp.MustCompile(`/(?:Users|home)/[^/\s"'<>]+`)

func (s *serveSetup) Check(ctx context.Context) (doctor.Artifact, error) {
	// Check closures share their lazy configuration; build fresh ones per flight.
	checks := s.checks(s.deps)
	if s.deps.User != s.deps.Account {
		for i := range checks {
			if checks[i].Phase == doctor.PhaseUser {
				checks[i].Run = func(context.Context) (doctor.Status, string) {
					return doctor.NotVerified, "run whr doctor as the workharbor account to verify user setup"
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	results := doctor.Run(ctx, checks, nil)
	p := doctor.PresentResults(results)
	report := p.Artifact(s.now(), version.Get().Version, "web", "", s.deps.Account, s.deps.Home, s.hostname)
	clean := func(value string) string { return setupHomePath.ReplaceAllString(s.redactor.String(value), "~") }
	for i := range report.Checks {
		report.Checks[i].Check = clean(report.Checks[i].Check)
		report.Checks[i].Detail = clean(report.Checks[i].Detail)
		report.Checks[i].Fix = clean(report.Checks[i].Fix)
	}
	return report, nil
}
