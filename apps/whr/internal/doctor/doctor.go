// SPDX-License-Identifier: EUPL-1.2

// Package doctor implements the local checks of `whr doctor` (FR-010, FR-060)
// mirrored from contracts/cli/doctor-checks.json. Every environmental fact
// (filesystem, socket dial, git --version, free disk, clock) arrives through
// Ports, so the checks are hermetic in tests and read-only in production: they
// create no file, send nothing over a socket and run no process but the fixed
// git --version.
//
// Redaction (FR-060) decision note: user and host names are replaced only in
// path-valued evidence, as whole path words (case-insensitive, any non-letter
// non-digit delimiter), never inside other words, never in messages or
// remediations, and never when the name is a standard system word such as run,
// git or docker (see SystemWords). The home directory becomes "~". The
// system-word skip is a tradeoff against "wherever they occur in a path": it
// keeps /var/run/docker.sock and /usr/bin/git intact at the price of not
// hiding a user who is literally called one of those words. The spec wording
// needs an amendment note from the owner. Names are also not Unicode-normalised:
// composed and decomposed spellings of the same name (HFS+) do not match each other.
package doctor

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"time"
)

// Check statuses (doctor-checks.json "statuses").
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusSkipped = "skipped"
	StatusUnknown = "unknown"
)

// Verdicts (output.schema.json doctorResult).
const (
	VerdictOK         = "ok"
	VerdictFailed     = "failed"
	VerdictIncomplete = "incomplete"
)

// Thresholds confirmed by the owner on 2026-10-17. Changing one needs a spec
// amendment and a test change (doctor-checks.json).
const (
	MinGitVersion = "2.39.5"
	MinFreeBytes  = uint64(2147483648)
)

// Budget is the wall-clock budget one run shares with validate (FR-060).
const Budget = 5 * time.Second

// Reasons of skipped and unknown checks (output.schema.json).
const (
	reasonRequiresControlPlane = "requires_control_plane"
	reasonNotInIncrement       = "not_in_increment"
	reasonPrecondition         = "precondition_absent"
	reasonNotMeasurable        = "not_measurable"
	reasonUnsupportedPlatform  = "unsupported_platform"
	reasonRemoteNotChecked     = "remote_not_checked"
)

const runtimeDoc = "docs/container-runtime.md"

// Statuses lists the statuses in contract order.
func Statuses() []string { return []string{StatusPass, StatusFail, StatusSkipped, StatusUnknown} }

// Check is one entry of the result (output.schema.json "check").
type Check struct {
	ID          string         `json:"id"`
	Status      string         `json:"status"`
	Message     string         `json:"message"`
	Remediation string         `json:"remediation,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

// Platform is the host the run measured.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Counts tallies the statuses.
type Counts struct {
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Skipped int `json:"skipped"`
	Unknown int `json:"unknown"`
}

// Report is the doctorResult of the --json envelope.
type Report struct {
	Platform Platform `json:"platform"`
	Verdict  string   `json:"verdict"`
	Counts   Counts   `json:"counts"`
	Checks   []Check  `json:"checks"`
}

// Verdict derives the verdict: failed when any check failed, ok only when
// all passed, incomplete otherwise (FR-060).
func Verdict(c Counts) string {
	switch {
	case c.Fail > 0:
		return VerdictFailed
	case c.Skipped > 0 || c.Unknown > 0:
		return VerdictIncomplete
	}
	return VerdictOK
}

// catalogue is the check order of doctor-checks.json.
var catalogue = []string{
	"platform.host", "container.socket", "git.version", "disk.working_directory", "disk.container_storage",
	"config.valid", "control_plane.reachable", "host.identity", "host.credentials_permissions", "service.agent",
}

// CatalogueIDs returns the check ids in report order.
func CatalogueIDs() []string { return append([]string(nil), catalogue...) }

// budget is the shared wall-clock budget on the injected clock.
type budget struct {
	now   func() time.Time
	start time.Time
	total time.Duration
}

func (b *budget) remaining() time.Duration {
	if r := b.total - b.now().Sub(b.start); r > 0 {
		return r
	}
	return 0
}

// limit returns the smaller of cap and the remainder.
func (b *budget) limit(cap time.Duration) time.Duration {
	if r := b.remaining(); r < cap {
		return r
	}
	return cap
}

// Run executes every check once, in contract order, and never writes.
func Run(ctx context.Context, p Ports) Report {
	p = p.withDefaults()
	r := newRun(ctx, p)
	checks := []Check{
		r.platform(), r.socket(), r.git(), r.diskWorkdir(), r.diskStorage(), r.config(),
		later("control_plane.reachable", reasonRequiresControlPlane, "Control-plane reachability needs the control plane, which the first increment does not use.",
			"Nothing to do now; this check arrives with the control-plane client."),
		later("host.identity", reasonRequiresControlPlane, "Host identity needs the control plane, which the first increment does not use.",
			"Nothing to do now; this check arrives with host setup."),
		later("host.credentials_permissions", reasonNotInIncrement, "No host credential exists before setup (FR-021).",
			"Nothing to do now; this check arrives with host setup."),
		later("service.agent", reasonNotInIncrement, "No agent service unit exists before host setup.",
			"Nothing to do now; this check arrives with host setup."),
	}
	// Only dynamic evidence is redacted; messages and remediations are static
	// text and must not be altered by a user or host name such as "doc".
	rd := NewRedactor(r.home, p.User(), p.Host())
	for i := range checks {
		checks[i] = rd.check(checks[i])
	}
	rep := Report{Platform: Platform{OS: p.OS, Arch: p.Arch}, Checks: checks, Counts: Count(checks)}
	rep.Verdict = Verdict(rep.Counts)
	return rep
}

// Count tallies statuses; anything unexpected counts as unknown, never pass.
func Count(checks []Check) Counts {
	var c Counts
	for _, k := range checks {
		switch k.Status {
		case StatusPass:
			c.Pass++
		case StatusFail:
			c.Fail++
		case StatusSkipped:
			c.Skipped++
		default:
			c.Unknown++
		}
	}
	return c
}

func later(id, reason, msg, fix string) Check {
	return Check{ID: id, Status: StatusSkipped, Reason: reason, Message: msg, Remediation: fix}
}

func unknown(id, reason, msg, fix string, ev map[string]any) Check {
	return Check{ID: id, Status: StatusUnknown, Reason: reason, Message: msg, Remediation: fix, Evidence: ev}
}

func failed(id, msg, fix string, ev map[string]any) Check {
	return Check{ID: id, Status: StatusFail, Message: msg, Remediation: fix, Evidence: ev}
}

func passed(id, msg string, ev map[string]any) Check {
	return Check{ID: id, Status: StatusPass, Message: msg, Evidence: ev}
}

const exhaustedMsg = "The 5 s doctor budget was spent before this check could run."
const exhaustedFix = "Rerun whr doctor when the machine is less busy."

func exhausted(id string) Check {
	return unknown(id, reasonNotMeasurable, exhaustedMsg, exhaustedFix, nil)
}

// run holds one run's state.
type run struct {
	ctx  context.Context
	p    Ports
	b    *budget
	home string
	cwd  string
	// dockerHost is the value of DOCKER_HOST (never reported).
	dockerHost string
}

func newRun(ctx context.Context, p Ports) *run {
	total := Budget
	if p.Budget > 0 {
		total = p.Budget
	}
	r := &run{ctx: ctx, p: p, b: &budget{now: p.Now, start: p.Now(), total: total}}
	r.home, _ = p.Home()
	r.cwd, _ = p.Getwd()
	r.dockerHost = strings.TrimSpace(p.Getenv("DOCKER_HOST"))
	return r
}

func (r *run) platform() Check {
	ev := map[string]any{"os": r.p.OS, "arch": r.p.Arch}
	if r.p.OS == "linux" && (r.p.Arch == "amd64" || r.p.Arch == "arm64") {
		return passed("platform.host", "Linux on "+r.p.Arch+".", ev)
	}
	return unknown("platform.host", reasonUnsupportedPlatform,
		"This platform is not in the host-platform matrix; the remaining read-only checks still ran.",
		"Use Linux on amd64 or arm64 for a supported host; the other results are still valid measurements.", ev)
}

// lstatGrace lets a metadata lookup finish after the budget is spent, so
// that a precondition (is there a workharbor.json?) is still answered.
const lstatGrace = 100 * time.Millisecond

var errBudget = errors.New("the doctor budget ran out")

// bound runs a call that cannot take a context (statfs, readdir, stat on a
// possibly hung mount) and gives up when the shared budget is spent. The call
// may keep running in its goroutine; doctor reports unknown and moves on.
func bound[T any](r *run, floor time.Duration, fn func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() { v, err := fn(); ch <- res{v, err} }()
	t := time.NewTimer(max(r.b.remaining(), floor))
	defer t.Stop()
	select {
	case x := <-ch:
		return x.v, x.err
	case <-t.C:
		var zero T
		return zero, errBudget
	}
}

func (r *run) lstat(path string) (fs.FileInfo, error) {
	return bound(r, lstatGrace, func() (fs.FileInfo, error) { return r.p.Lstat(path) })
}

func (r *run) readFile(path string, max int64) ([]byte, error) {
	return bound(r, 0, func() ([]byte, error) { return r.p.ReadFile(path, max) })
}

func (r *run) canRead(dir string) error {
	_, err := bound(r, 0, func() (struct{}, error) { return struct{}{}, r.p.CanRead(dir) })
	return err
}

func (r *run) readlink(path string) (string, error) {
	return bound(r, 0, func() (string, error) { return r.p.Readlink(path) })
}

func (r *run) freeBytes(path string) (uint64, error) {
	return bound(r, 0, func() (uint64, error) { return r.p.FreeBytes(path) })
}
