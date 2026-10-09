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

func TestNewServeSetupUsesTheDefaultInstallationPrefix(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api-token")
	if err := os.WriteFile(tokenFile, []byte("test-supervisor-token-long-enough"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter, err := newServeSetup(&config.Config{APITokenFile: tokenFile}, filepath.Join(dir, "config.json"), filepath.Join(dir, "whr"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := adapter.(*serveSetup).deps.Prefix; got != doctor.DefaultPrefix {
		t.Fatalf("prefix %q; want %q", got, doctor.DefaultPrefix)
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
