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
	}).Artifact(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), "v0.1.0", "setup", PhaseUser, "operator", "/home/operator", "example-host")
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

func TestReportRedactionBoundaries(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://h/home/op/x?y", "https://h~/x?y"},
		{"-L/home/op/lib", "-L~/lib"},
		{"-v/home/op:/data", "-v~:/data"},
		{"/srv/home/alice/x", "/srv~/x"},
		{"./home/op", ".~"},
		{"/home/op-x", "/home/op-x"},
		{"/home/op_x", "/home/op_x"},
		{"/home/op1", "/home/op1"},
		{"/home/opX", "/home/opX"},
		{"/home/op./x", "~./x"},
		{"/home/op.:", "~.:"},
		{"mac_x", "mac_x"},
		{"/home/alice.", "~."},
		{"/home/alice.bak", "/home/alice.bak"},
		{"/home/alice", "~"},
		{"/home/op/a", "~/a"},
		{"/home/op.", "~."},
		{"/home/op.bak", "/home/op.bak"},
		{"/home/operator/b", "/home/operator/b"},
		{"unix:///home/op/x.sock", "unix://~/x.sock"},
		{"file:///Users/alice/x", "file://~/x"},
		{"/Users/alice/x", "~/x"},
		{"/System/Volumes/Data/Users/alice/x", "/System/Volumes/Data~/x"},
		{"/Users/alicia/x", "/Users/alicia/x"},
		{"ssh mac.local", "ssh [host].local"},
		{"x.mac", "x.mac"},
		{"xmac machine mac-1", "xmac machine mac-1"},
		{"on mac", "on [host]"},
		{"/srv/mac/data", "/srv/[host]/data"},
		{"token " + "ghp" + "_" + strings.Repeat("a", 36) + " end", "token [REDACTED] end"},
		{"github" + "_pat_" + strings.Repeat("b", 24), "[REDACTED]"},
		{"glpat" + "-" + strings.Repeat("c", 22), "[REDACTED]"},
		{"sk" + "-" + strings.Repeat("d", 26), "[REDACTED]"},
		{"xox" + "b-" + strings.Repeat("1", 12), "[REDACTED]"},
		{"Bearer " + strings.Repeat("e", 24), "Bearer [REDACTED]"},
		{"https://u:" + strings.Repeat("f", 10) + "@example.com/x", "https://u:[REDACTED]@example.com/x"},
		{"sk-short", "sk-short"},
	} {
		p := PresentResults([]Result{{Check: "c", Detail: tc.in}})
		got := p.Artifact(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), "v0", "doctor", "", "alice", "/home/op", "mac").Checks[0].Detail
		if got != tc.want {
			t.Errorf("%q -> %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestReportRedactionDegenerateHome(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, home := range []string{"", "/", "//", "///"} {
			p := PresentResults([]Result{{Check: "c", Detail: "/a//b /home/x"}})
			got := p.Artifact(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), "v0", "doctor", "", "", home, "").Checks[0].Detail
			if got != "/a//b /home/x" {
				t.Errorf("home %q: %q", home, got)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("redaction does not finish")
	}
}

func TestReplaceComponentOverlapAndEmpty(t *testing.T) {
	done := make(chan string, 1)
	go func() { done <- replaceComponent("abc", "", "~", true) }()
	select {
	case got := <-done:
		if got != "abc" {
			t.Errorf("empty name: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("empty name does not finish")
	}
	for _, tc := range []struct{ in, old, want string }{
		{"a/xa/x/xa/xa/x/x", "/xa/x", "a~/xa~/x"},
		{"/var/var/v", "/var/v", "/var~"},
		{"/opt/op/opt/op", "/opt/op", "~~"},
		{"/Users/U/Users/U", "/Users/U", "~~"},
	} {
		if got := replaceComponent(tc.in, tc.old, "~", true); got != tc.want {
			t.Errorf("%q: %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestReplaceComponentStartIsTheInputStart(t *testing.T) {
	for _, tc := range []struct {
		in, old string
		path    bool
		want    string
	}{
		{"aa", "a", false, "aa"},
		{"baa", "a", false, "baa"},
		{"-aa:", "a", false, "-aa:"},
		{"xx xxx", "xx", false, "~ xxx"},
		{"/x/x", "/x", false, "~/x"},
	} {
		if got := replaceComponent(tc.in, tc.old, "~", tc.path); got != tc.want {
			t.Errorf("%q: %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestReportEmptyAccountLeavesRootsAlone(t *testing.T) {
	p := PresentResults([]Result{{Check: "c", Detail: "/home/ x /Users/ x"}})
	got := p.Artifact(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), "v0", "doctor", "", "", "/srv/h", "").Checks[0].Detail
	if got != "/home/ x /Users/ x" {
		t.Fatalf("%q", got)
	}
}

func TestReportRetentionKeepsNewReportWhenClockStepsBack(t *testing.T) {
	dir := privateReportDir(t)
	report := exampleReport()
	for range 10 {
		report.GeneratedAt = report.GeneratedAt.Add(time.Second)
		if _, err := WriteArtifact(dir, report); err != nil {
			t.Fatal(err)
		}
	}
	report.GeneratedAt = report.GeneratedAt.Add(-time.Hour)
	path, err := WriteArtifact(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("new report deleted: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "setup-reports"))
	if err != nil || len(entries) != 10 {
		t.Fatalf("retention %d: %v", len(entries), err)
	}
}
