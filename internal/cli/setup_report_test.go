package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/setup"
)

func TestSetupReportOutcomes(t *testing.T) {
	p := setupPresentation([]doctor.Check{{Name: "power", Step: 3, Phase: doctor.PhaseHost, Fix: &doctor.Fix{}}}, []setup.Outcome{{Step: "power", Status: doctor.Fail, Detail: "off", Asked: true}}, doctor.PhaseHost, repairContext{})
	r := p.Checks[0]
	if r.Step != 3 || r.Phase != doctor.PhaseHost || r.Fix != "whr setup --only power" || r.Asked == nil || !*r.Asked || r.Fixed == nil || *r.Fixed || p.OK {
		t.Fatalf("outcome lost: %+v", p)
	}
}

func TestDoctorReportFlagAndSetupArtifact(t *testing.T) {
	for _, command := range []string{"doctor", "setup-guard"} {
		t.Run(command, func(t *testing.T) {
			rig := newSetupRig(t)
			home := t.TempDir()
			stateDir := filepath.Join(home, "state")
			path := filepath.Join(home, "config.json")
			data, err := json.Marshal(map[string]string{"state_dir": stateDir})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr, Getenv: func(k string) string {
				if k == "HOME" {
					return home
				}
				return ""
			}, Setup: rig.env}
			source := command
			if source == "setup-guard" {
				source = "setup"
			}
			args := []string{source, "--config", path, "--prefix", filepath.Dir(filepath.Dir(rig.exe))}
			switch command {
			case "doctor":
				args = append(args, "--report", "--json")
			case "setup":
				args = append(args, "--dry-run")
			}
			Execute(context.Background(), env, args)
			entries, err := os.ReadDir(filepath.Join(config.StateDirOf(stateDir, home), "setup-reports"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("artifact missing: %v (%s)", err, stderr.String())
			}
			reportData, err := os.ReadFile(filepath.Join(stateDir, "setup-reports", entries[0].Name())) //nolint:gosec // generated report in the test-owned directory
			if err != nil {
				t.Fatal(err)
			}
			var report doctor.Artifact
			if err := json.Unmarshal(reportData, &report); err != nil {
				t.Fatal(err)
			}
			if report.Source != source || len(report.Checks) == 0 || bytes.Contains(reportData, []byte(home)) {
				t.Fatalf("invalid artifact: %s", reportData)
			}
			if command == "doctor" && !json.Valid(out.Bytes()) {
				t.Fatal("JSON output polluted")
			}
		})
	}
}

func TestSetupReportRepairContext(t *testing.T) {
	steps := []doctor.Check{{Name: "power", Phase: doctor.PhaseHost, Fix: &doctor.Fix{}}}
	for _, tc := range []struct {
		name    string
		context repairContext
		useUser string
		want    string
	}{
		{"prefix", repairContext{Prefix: "/opt/custom install", Account: "operator"}, "", "whr setup --only power --prefix '/opt/custom install' --user 'operator'"},
		{"step account", repairContext{Prefix: "/opt/custom", Account: "operator"}, "legacy", "whr setup --only power --prefix '/opt/custom' --user 'legacy'"},
		{"plain", repairContext{Account: "operator"}, "", "whr setup --only power --user 'operator'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := setupPresentation(steps, []setup.Outcome{{Step: "power", Status: doctor.Fail, UseUser: tc.useUser}}, doctor.PhaseHost, tc.context)
			if got := p.Checks[0].Fix; got != tc.want {
				t.Fatalf("fix = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestSetupDryRunDoesNotWriteOrPruneReports(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "existing"}[existing], func(t *testing.T) {
			rig := newSetupRig(t)
			home := t.TempDir()
			stateDir := filepath.Join(home, "state")
			configPath := filepath.Join(home, "config.json")
			data, err := json.Marshal(map[string]string{"state_dir": stateDir})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			reportDir := filepath.Join(stateDir, "setup-reports")
			if existing {
				if err := os.MkdirAll(reportDir, 0o700); err != nil {
					t.Fatal(err)
				}
				for i := range 11 {
					name := fmt.Sprintf("20261006T1200%02d.000000000Z-setup.json", i)
					if err := os.WriteFile(filepath.Join(reportDir, name), []byte("unchanged"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var out, stderr bytes.Buffer
			env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr, Getenv: func(k string) string {
				if k == "HOME" {
					return home
				}
				return ""
			}, Setup: rig.env}
			Execute(context.Background(), env, []string{"setup", "host", "--dry-run", "--only", "power", "--config", configPath, "--prefix", filepath.Dir(filepath.Dir(rig.exe))})
			if !existing {
				if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
					t.Fatalf("dry run created state: %v", err)
				}
				return
			}
			entries, err := os.ReadDir(reportDir)
			if err != nil || len(entries) != 11 {
				t.Fatalf("reports changed: %d, %v", len(entries), err)
			}
			for _, entry := range entries {
				got, err := os.ReadFile(filepath.Join(reportDir, entry.Name())) //nolint:gosec // fixture in the test-owned directory
				if err != nil || string(got) != "unchanged" {
					t.Fatalf("report changed: %s (%v)", got, err)
				}
			}
		})
	}
}

func TestDoctorReportWriteFailureKeepsResults(t *testing.T) {
	t.Run("checks fail", func(t *testing.T) { reportWriteFailure(t, false) })
	t.Run("checks pass", func(t *testing.T) { reportWriteFailure(t, true) })
}

func reportWriteFailure(t *testing.T, pass bool) {
	rig := newSetupRig(t)
	home := t.TempDir()
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	data, err := json.Marshal(map[string]string{"state_dir": filepath.Join(blocker, "state")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr, Getenv: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, Setup: rig.env}
	args := []string{"doctor", "--config", path, "--prefix", filepath.Dir(filepath.Dir(rig.exe)), "--report", "--json"}
	if pass { // skipped checks do not fail the run
		for _, c := range doctor.Checks(doctor.Deps{}) {
			if c.Name != "whr-user" {
				args = append(args, "--skip", c.Name)
			}
		}
	}
	code := Execute(context.Background(), env, args)
	if pass && strings.Contains(stderr.String(), "some checks failed") {
		t.Fatalf("checks did not pass: %s", stderr.String())
	}
	if pass && strings.Contains(stderr.String(), "ready, with") {
		t.Fatalf("ready line printed although the report was not written: %s", stderr.String())
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"checks"`) {
		t.Fatalf("results hidden: %q", out.String())
	}
	if !strings.Contains(stderr.String(), "setup report") || code == 0 {
		t.Fatalf("write failure not reported: code %d, %q", code, stderr.String())
	}
}

// An unreachable step shows the step that unblocks it in the report, not a
// setup command for itself (#394).
func TestSetupPresentationNamesTheRemedyOfAnUnreachableStep(t *testing.T) {
	steps := doctor.Steps(doctor.Checks(doctor.Deps{ConfigPath: "x/config.json", User: "werner", Account: "werner"}), doctor.PhaseHost)
	outs := []setup.Outcome{{Step: "workspace-volume", Status: doctor.NotVerified, Detail: "not reachable: x", Remedy: "whr setup --only config-first"}}
	p := setupPresentation(steps, outs, doctor.PhaseHost, repairContext{Account: "werner"})
	got := p.Checks[0].Fix
	if got != "whr setup --only config-first --user 'werner'" {
		t.Errorf("fix %q", got)
	}
}
