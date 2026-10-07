package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/config"
)

// Counts includes every status, including those with no checks.
type Counts struct {
	OK          int `json:"ok"`
	Warn        int `json:"warn"`
	Fail        int `json:"fail"`
	NotVerified int `json:"not_verified"`
	Skipped     int `json:"skipped"`
}

// ReportCheck retains the existing JSON result and optional setup outcomes.
type ReportCheck struct {
	Result
	Fixed *bool `json:"fixed,omitempty"`
	Asked *bool `json:"asked,omitempty"`
}

// PhaseGroup is a renderer's ordered phase and its check/fix blocks.
// Result.Fix is the diagnostic fix block; no commands are executed here.
type PhaseGroup struct {
	Phase  Phase
	Checks []ReportCheck
	Counts Counts
}

// Presentation is the shared value for JSON, terminal, web and artifacts.
// Checks preserve execution order; Groups present host, user, then shared.
type Presentation struct {
	Checks []ReportCheck
	Groups []PhaseGroup
	Counts Counts
	OK     bool
}

func count(c *Counts, status Status) {
	switch status {
	case OK:
		c.OK++
	case Warn:
		c.Warn++
	case Fail:
		c.Fail++
	case NotVerified:
		c.NotVerified++
	case Skipped:
		c.Skipped++
	}
}

// Present copies checks so renderer-specific transformations cannot change callers.
func Present(checks []ReportCheck) Presentation {
	p := Presentation{Checks: append([]ReportCheck{}, checks...), OK: true}
	for _, phase := range []Phase{PhaseHost, PhaseUser, ""} {
		g := PhaseGroup{Phase: phase, Checks: []ReportCheck{}}
		for _, r := range p.Checks {
			if r.Phase == phase {
				g.Checks = append(g.Checks, r)
				count(&g.Counts, r.Status)
			}
		}
		p.Groups = append(p.Groups, g)
	}
	for _, r := range p.Checks {
		count(&p.Counts, r.Status)
	}
	p.OK = p.Counts.Fail == 0
	return p
}

// PresentResults adapts read-only doctor results to the shared presentation.
func PresentResults(results []Result) Presentation {
	checks := make([]ReportCheck, 0, len(results))
	for _, r := range results {
		checks = append(checks, ReportCheck{Result: r})
	}
	return Present(checks)
}

// Results is the unchanged legacy --json checks contract.
func (p Presentation) Results() []Result {
	results := make([]Result, 0, len(p.Checks))
	for _, r := range p.Checks {
		results = append(results, r.Result)
	}
	return results
}

// Artifact is versioned separately from the CLI's JSON envelope.
type Artifact struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	GeneratedAt   time.Time     `json:"generated_at"`
	WhrVersion    string        `json:"whr_version"`
	Source        string        `json:"source"`
	Phase         Phase         `json:"phase"`
	Account       string        `json:"account"`
	Dev           bool          `json:"dev"`
	OK            bool          `json:"ok"`
	Counts        Counts        `json:"counts"`
	Checks        []ReportCheck `json:"checks"`
}

// Artifact returns an export-safe copy. Only existing diagnostic text is copied;
// no environment, host metadata, credentials or additional paths are collected.
func (p Presentation) Artifact(now time.Time, version, source string, phase Phase, account string, dev bool, home, hostname string) Artifact {
	redact := func(s string) string {
		if home != "" && home != "/" {
			s = strings.ReplaceAll(s, strings.TrimRight(home, "/"), "~")
		}
		if hostname != "" {
			s = strings.ReplaceAll(s, hostname, "[host]")
		}
		return s
	}
	checks := append([]ReportCheck{}, p.Checks...)
	for i := range checks {
		checks[i].Check = redact(checks[i].Check)
		checks[i].Detail = redact(checks[i].Detail)
		checks[i].Fix = redact(checks[i].Fix)
	}
	return Artifact{1, "whr.setup-report", now.UTC(), redact(version), source, phase, redact(account), dev, p.OK, p.Counts, checks}
}

// WriteArtifact publishes a complete 0600 file atomically in private directories.
// Retention affects only report-shaped regular files, never links or unrelated files.
func WriteArtifact(stateDir string, report Artifact) (string, error) {
	if report.Source != "doctor" && report.Source != "setup" && report.Source != "web" {
		return "", fmt.Errorf("invalid report source %q", report.Source)
	}
	if !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("report state directory must be absolute")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	if err := config.CheckStateDir(stateDir); err != nil {
		return "", err
	}
	dir := filepath.Join(stateDir, "setup-reports")
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := config.CheckStateDir(dir); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	f, err := os.CreateTemp(dir, ".report-*") //nolint:gosec // both directories are checked private, owned and not links
	if err != nil {
		return "", err
	}
	temporary := f.Name()
	defer func() { _ = os.Remove(temporary) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	// Link publishes the finished inode atomically without overwriting an existing
	// report. Nanosecond suffixes resolve simultaneous runs at the same time.
	var path string
	for attempt := 0; attempt < 10000; attempt++ {
		stamp := report.GeneratedAt.UTC().Add(time.Duration(attempt)).Format("20060102T150405.000000000Z")
		path = filepath.Join(dir, stamp+"-"+report.Source+".json")
		err = os.Link(temporary, path) //nolint:gosec // private report directory, validated source and generated basename
		if !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && reportName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > 10 {
		if err = os.Remove(filepath.Join(dir, names[0])); err != nil && !os.IsNotExist(err) {
			return path, err
		}
		names = names[1:]
	}
	return path, nil
}

func reportName(name string) bool {
	if len(name) < 28 || name[26] != '-' {
		return false
	}
	if _, err := time.Parse("20060102T150405.000000000Z", name[:26]); err != nil {
		return false
	}
	for _, source := range []string{"doctor", "setup", "web"} {
		if strings.HasSuffix(name, "-"+source+".json") {
			return true
		}
	}
	return false
}
