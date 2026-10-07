package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/redact"
)

func TestNewServeSetupUsesConfiguredInstallationPrefix(t *testing.T) {
	for _, development := range []bool{false, true} {
		t.Run(map[bool]string{false: "managed", true: "development"}[development], func(t *testing.T) {
			dir := t.TempDir()
			tokenFile := filepath.Join(dir, "api-token")
			if err := os.WriteFile(tokenFile, []byte("test-supervisor-token-long-enough"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{APITokenFile: tokenFile}
			want := doctor.DefaultPrefix
			if development {
				want = filepath.Join(dir, "development")
				cfg.DevelopmentPrefix = want
			}
			adapter, err := newServeSetup(cfg, filepath.Join(dir, "config.json"), filepath.Join(want, "bin", "whr"), dir)
			if err != nil {
				t.Fatal(err)
			}
			deps := adapter.(*serveSetup).deps
			if deps.Prefix != want || deps.Dev != development {
				t.Fatalf("prefix %q, dev %v; want %q, %v", deps.Prefix, deps.Dev, want, development)
			}
		})
	}
}

func TestServeSetupOnlyChecksAndRedactsEveryDiagnosticField(t *testing.T) {
	secret := "registered-supervisor-secret"
	rd := redact.New()
	rd.Add(secret)
	ran := 0
	s := serveSetup{deps: doctor.Deps{Home: "/Users/tester", User: "workharbor", Account: "workharbor"}, hostname: "private-host", redactor: rd, now: func() time.Time { return time.Unix(1, 0) }, checks: func(doctor.Deps) []doctor.Check {
		return []doctor.Check{{Name: secret, Phase: doctor.PhaseHost, Run: func(context.Context) (doctor.Status, string) {
			ran++
			return doctor.Fail, secret + " /Users/tester/private /Users/other/private private-host"
		}, Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"never-execute"}}}}}}
	}}
	report, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{secret, "/Users/tester", "/Users/other", "private-host"} {
		if strings.Contains(string(data), private) {
			t.Errorf("report contains %q", private)
		}
	}
	if ran != 1 || report.Source != "web" || report.Checks[0].Fix == "" {
		t.Fatal(report)
	}
}

func TestServeSetupDoesNotVerifyAnotherAccountsUserPhase(t *testing.T) {
	s := serveSetup{deps: doctor.Deps{User: "administrator", Account: "workharbor"}, redactor: redact.New(), now: time.Now, checks: func(doctor.Deps) []doctor.Check {
		return []doctor.Check{{Name: "user-check", Phase: doctor.PhaseUser, Run: func(context.Context) (doctor.Status, string) {
			t.Fatal("wrong account check ran")
			return doctor.OK, ""
		}}}
	}}
	report, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[0].Status != doctor.NotVerified {
		t.Fatal(report)
	}
}
