package doctor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exampleReport() Artifact {
	fixed, asked := true, false
	return Present([]ReportCheck{
		{Result: Result{Check: "power", Step: 1, Status: Warn, Detail: "check /home/operator/power on example-host", Phase: PhaseHost, Fix: "whr setup host --only power"}},
		{Result: Result{Check: "config", Step: 2, Status: OK, Detail: "ready", Phase: PhaseUser}, Fixed: &fixed, Asked: &asked},
		{Result: Result{Check: "capacity", Status: NotVerified, Detail: "not measured"}},
	}).Artifact(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), "v0.1.0", "setup", PhaseUser, "operator", true, "/home/operator", "example-host")
}

func TestReportV1GoldenAndRedaction(t *testing.T) {
	report := exampleReport()
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/setup-report-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("schema differs:\n%s", got)
	}
	for _, forbidden := range []string{"/home/operator", "example-host", "hostname", "environment"} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Errorf("report includes %q", forbidden)
		}
	}
}

func TestReportPresentation(t *testing.T) {
	results := []Result{{Check: "shared", Status: Fail}, {Check: "user", Phase: PhaseUser, Status: Skipped}, {Check: "host", Phase: PhaseHost, Status: OK}}
	p := PresentResults(results)
	if p.OK || p.Counts.Fail != 1 || p.Counts.Skipped != 1 || p.Counts.OK != 1 {
		t.Fatalf("counts: %+v", p)
	}
	if p.Groups[0].Checks[0].Check != "host" || p.Groups[1].Checks[0].Check != "user" || p.Groups[2].Checks[0].Check != "shared" {
		t.Fatal("phase order")
	}
	if p.Results()[0] != results[0] {
		t.Fatal("legacy order changed")
	}
	p.Checks[0].Detail = "changed"
	if results[0].Detail != "" {
		t.Fatal("mutated caller")
	}
}

func TestReportArtifactPrivateAtomicRetention(t *testing.T) {
	dir := privateReportDir(t)
	report := exampleReport()
	for i := range 12 {
		report.GeneratedAt = report.GeneratedAt.Add(time.Second)
		path, err := WriteArtifact(dir, report)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v: %v", fi, err)
		}
		data, err := os.ReadFile(path) //nolint:gosec // generated report in the test-owned directory
		if err != nil || !json.Valid(data) {
			t.Fatalf("partial report %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "setup-reports"))
	if err != nil || len(entries) != 10 {
		t.Fatalf("retention %d: %v", len(entries), err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".report-") {
			t.Fatal("temporary file remained")
		}
	}
	// Identical timestamps must not overwrite one another.
	a, err := WriteArtifact(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	b, err := WriteArtifact(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("same timestamp overwrote a report")
	}
}

func TestReportArtifactRejectsUnsafeDirectoriesAndSources(t *testing.T) {
	for _, kind := range []string{"link", "public", "file"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateReportDir(t)
			path := filepath.Join(dir, "setup-reports")
			var err error
			switch kind {
			case "link":
				err = os.Symlink(t.TempDir(), path)
			case "public":
				err = os.Mkdir(path, 0o755) //nolint:gosec // deliberately public fixture tests rejection
			case "file":
				err = os.WriteFile(path, nil, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := WriteArtifact(dir, exampleReport()); err == nil {
				t.Fatal("unsafe directory accepted")
			}
		})
	}
	r := exampleReport()
	r.Source = "../../outside"
	if _, err := WriteArtifact(privateReportDir(t), r); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func privateReportDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // private directory requires owner traversal
		t.Fatal(err)
	}
	return dir
}
