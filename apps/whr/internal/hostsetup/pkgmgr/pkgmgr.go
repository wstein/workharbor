// SPDX-License-Identifier: EUPL-1.2

// Package pkgmgr applies only the Homebrew portion of a sealed host setup plan
// through injected ports. It supplies no native process, network or journal.
package pkgmgr

import (
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const LookupUserAgent = "WorkHarbor-host-setup/1"

type Command struct {
	Argv        []string
	Env         map[string]string
	Stdin       string
	OutputLimit int
}
type Completion struct {
	Exit int
	Err  error
}

// Process retains child ownership through cleanup. Nil Kill error means final
// group cleanup and reap completed. A cleanup failure must retain the process
// through CleanupError; callers stop mutation and reconcile that handle.
type Process interface {
	Done() <-chan Completion
	Interrupt() error
	Kill() error
}

// Exec may return a nonnil process with an error when startup occurred but its
// cleanup failed. That outcome is attempted mutation, never a failed-before-start
// result. Ordinary canceled startup leaves no child and returns nil/error.
type Exec interface {
	Start(context.Context, Command) (Process, error)
}
type FS interface{ BrewExecutable(string) (bool, error) }
type Clock interface {
	After(time.Duration) <-chan time.Time
}
type Detector interface {
	Detect(context.Context, catalogue.Item) detect.Result
}
type LookupRequest struct {
	URL, Method, UserAgent string
	Timeout                time.Duration
	BodyLimit              int
	FollowRedirects        bool
}
type Network interface {
	Lookup(context.Context, LookupRequest) (bool, error)
}
type Event struct {
	Stage, StepID, Item, Kind, Class, Status, Reason string
	Argv                                             []string
	Exit                                             *int
}
type Journal interface {
	Record(context.Context, Event) error
	Close(context.Context) error
}
type Bookkeeper interface {
	Begin(context.Context, string) (Journal, error)
}
type Policy struct {
	Mode                                        string
	DryRun, Yes, Terminal, JSON, NonInteractive bool
	UID                                         int
	SUDOUserSet                                 bool
	Confirmed                                   map[string]bool
	Shown                                       map[string][]string
}
type Request struct {
	Input        plan.Input
	ExpectedHash string
	Policy       Policy
	Environment  map[string]string
	Completed    map[string]bool
}
type StepResult struct {
	ID, Status, Reason         string
	Exit                       *int
	MutationAttempted, Changed bool
}
type Result struct{ Steps []StepResult }
type Engine struct {
	Exec       Exec
	FS         FS
	Clock      Clock
	Detector   Detector
	Network    Network
	Bookkeeper Bookkeeper
}

// Apply consumes immutable preview inputs. It rebuilds the sealed plan rather
// than trusting caller-supplied steps or a separately mutable catalogue. Only
// brew steps are executed; Completed is T006's evidence for external steps.
// Bookkeeper.Begin must acquire the run lock and durable writable protocol;
// this interface is a prerequisite, not an implementation of that storage.
func (e Engine) Apply(ctx context.Context, r Request) (result Result, err error) {
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if r.Input.Platform != "darwin-arm64" && r.Input.Platform != "darwin-amd64" {
		return result, errors.New("unsupported platform")
	}
	if r.Policy.UID == 0 || r.Policy.SUDOUserSet {
		return result, errors.New("host_setup.running_as_root")
	}
	if r.Policy.Mode != "setup" && r.Policy.Mode != "fix" && r.Policy.Mode != "doctor" {
		return result, errors.New("invalid mode")
	}
	sealed, err := plan.Build(r.Input)
	if err != nil {
		return result, err
	}
	if err = plan.Recheck(sealed, r.ExpectedHash); err != nil {
		return result, err
	}
	if r.Policy.Mode == "doctor" || r.Policy.DryRun {
		return result, nil
	}
	if !r.Policy.Yes {
		return result, errors.New("confirmation required")
	}
	if e.Exec == nil || e.FS == nil || e.Clock == nil || e.Detector == nil || e.Bookkeeper == nil {
		return result, errors.New("package manager ports unavailable")
	}
	journal, err := e.Bookkeeper.Begin(ctx, sealed.Hash)
	if err != nil {
		return result, errors.New("host_setup.protocol_unwritable")
	}
	if journal == nil {
		return result, errors.New("host_setup.protocol_unwritable")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if closeErr := journal.Close(cleanup); closeErr != nil {
			err = errors.Join(err, errors.New("host_setup.protocol_unwritable"))
		}
	}()
	items := map[string]catalogue.Item{}
	for _, i := range r.Input.Catalogue.Items {
		items[i.ID] = i
	}
	completed := map[string]bool{}
	for id, ok := range r.Completed {
		completed[id] = ok
	}
	stopped := ""
	for _, step := range sealed.Steps {
		if step.Kind != "brew_install" && step.Kind != "brew_upgrade" {
			continue
		}
		item := items[step.Item]
		sr := StepResult{ID: step.ID, Status: "blocked"}
		event := Event{Stage: "finished", StepID: step.ID, Item: step.Item, Kind: step.Kind, Class: step.Class}
		var argv []string
		var stepErr error
		switch {
		case stopped != "":
			sr.Status = "skipped"
			sr.Reason = stopped
		case ctx.Err() != nil:
			sr.Status = "skipped"
			sr.Reason = "cancelled"
			stopped = "cancelled"
		case step.Blocked != "":
			sr.Reason = step.Blocked
		case !dependenciesReady(ctx, step, item, items, completed, e.Detector):
			sr.Reason = "dependency_unavailable"
		case step.Class == "modify" && !r.Policy.Confirmed[item.ID]:
			sr.Status = "declined"
			sr.Reason = "confirmation_required"
		case item.Tool.Source.MayElevate:
			sr.Reason = "privileged_step_unplanned"
		default:
			sr, argv, stepErr = e.applyStep(ctx, step, item, r.Environment, journal)
			if sr.Reason == "cancelled" {
				stopped = sr.Reason
			}
		}
		result.Steps = append(result.Steps, sr)
		event.Status = sr.Status
		event.Reason = sr.Reason
		event.Argv = argv
		event.Exit = sr.Exit
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		recordErr := journal.Record(cleanup, event)
		cancel()
		if recordErr != nil {
			return result, errors.Join(stepErr, errors.New("host_setup.protocol_unwritable"))
		}
		if stepErr != nil {
			return result, stepErr
		}
		completed[step.ID] = sr.Status == "applied" || sr.Status == "unchanged"
	}
	return result, nil
}
func dependenciesReady(ctx context.Context, s plan.Step, item catalogue.Item, items map[string]catalogue.Item, completed map[string]bool, d Detector) bool {
	for _, id := range s.DependsOn {
		if !completed[id] {
			return false
		}
	}
	for _, id := range item.Requires {
		required, ok := items[id]
		if !ok {
			return false
		}
		observed := d.Detect(ctx, required)
		if ctx.Err() != nil || observed.Item != id || observed.State != "present_ok" {
			return false
		}
	}
	return true
}
func (e Engine) applyStep(ctx context.Context, s plan.Step, item catalogue.Item, environment map[string]string, j Journal) (StepResult, []string, error) {
	sr := StepResult{ID: s.ID, Status: "blocked"}
	current := e.Detector.Detect(ctx, item)
	if ctx.Err() != nil {
		sr.Status = "skipped"
		sr.Reason = "cancelled"
		return sr, nil, nil
	}
	if current.Item != item.ID {
		sr.Reason = "state_changed"
		return sr, nil, nil
	}
	if current.State == "present_ok" {
		sr.Status = "unchanged"
		return sr, nil, nil
	}
	if (s.Kind == "brew_install" && current.State != "absent") || (s.Kind == "brew_upgrade" && (current.State != "present_outdated" || !current.Managed)) {
		sr.Reason = "state_changed"
		return sr, nil, nil
	}
	brew := ""
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		present, err := e.FS.BrewExecutable(p)
		if ctx.Err() != nil {
			sr.Status = "skipped"
			sr.Reason = "cancelled"
			return sr, nil, nil
		}
		if err != nil {
			sr.Reason = "package_manager_unverifiable"
			return sr, nil, nil
		}
		if present {
			brew = p
			break
		}
	}
	if brew == "" {
		sr.Reason = "package_manager_missing"
		return sr, nil, nil
	}
	cmd, err := brewCommand(brew, s.Kind, item.Tool.Source, environment)
	if err != nil {
		sr.Reason = "invalid_package_source"
		return sr, nil, nil
	}
	if err = j.Record(ctx, Event{Stage: "starting", StepID: s.ID, Item: item.ID, Kind: s.Kind, Class: s.Class, Argv: append([]string(nil), cmd.Argv...)}); err != nil {
		sr.Reason = "protocol_unwritable"
		return sr, nil, nil
	}
	completion, reason, started := e.run(ctx, cmd)
	sr.MutationAttempted = started
	var cleanup CleanupError
	if errors.As(completion.Err, &cleanup) {
		sr.Status = "failed"
		sr.Reason = "package_manager_failed"
		return sr, cmd.Argv, completion.Err
	}
	if completion.Exit >= 0 && completion.Exit <= 255 {
		n := completion.Exit
		sr.Exit = &n
	}
	if reason != "" {
		sr.Status = "failed"
		sr.Reason = reason
		return sr, cmd.Argv, nil
	}
	if completion.Err != nil || completion.Exit != 0 {
		sr.Status = "failed"
		sr.Reason = "package_manager_failed"
		return sr, cmd.Argv, nil
	}
	sr.Changed = true
	after := e.Detector.Detect(ctx, item)
	if ctx.Err() != nil {
		sr.Status = "failed"
		sr.Reason = "cancelled"
		return sr, cmd.Argv, nil
	}
	if after.Item != item.ID || after.State != "present_ok" {
		sr.Status = "failed"
		sr.Reason = "version_unverified"
		if after.State == "present_outdated" {
			sr.Reason = "version_below_minimum"
		}
		return sr, cmd.Argv, nil
	}
	sr.Status = "applied"
	return sr, cmd.Argv, nil
}

// Exec must enforce OutputLimit while draining output, never returning raw tails
// or credentials. Its process group must be reaped after Done or Kill. No native
// mutating adapter is supplied here. Start must honor its bounded context and
// use it for startup only: this engine owns child interrupt/grace/kill.
// Clock deadlines govern only the child step;
// detector observations use their own reviewed five-second probe budget.
func (e Engine) run(ctx context.Context, cmd Command) (Completion, string, bool) {
	unknown := Completion{Exit: -1}
	if ctx.Err() != nil {
		return unknown, "cancelled", false
	}
	runCtx, cancel := context.WithTimeout(ctx, 900*time.Second)
	defer cancel()
	process, err := e.Exec.Start(runCtx, cmd)
	// A nonnil process on failed startup means mutation may have started and
	// cleanup ownership must reach Apply's caller. Never label it not-started.
	if err != nil && process != nil {
		return Completion{Exit: -1, Err: NewCleanupError(process, err)}, "package_manager_failed", true
	}
	var startupCleanup CleanupError
	if errors.As(err, &startupCleanup) {
		return Completion{Exit: -1, Err: err}, "package_manager_failed", true
	}
	if err != nil || process == nil {
		if ctx.Err() != nil {
			return unknown, "cancelled", false
		}
		if runCtx.Err() != nil {
			return unknown, "deadline_exceeded", false
		}
		return unknown, "package_manager_failed", false
	}
	deadline := e.Clock.After(900 * time.Second)
	reason := ""
	select {
	case completion, ok := <-process.Done():
		if !ok {
			return unknown, "package_manager_failed", true
		}
		return completion, "", true
	case <-runCtx.Done():
		reason = "deadline_exceeded"
		if ctx.Err() != nil {
			reason = "cancelled"
		}
	case <-deadline:
		reason = "deadline_exceeded"
	}
	_ = process.Interrupt()
	grace := e.Clock.After(30 * time.Second)
	// Prefer an already-exited child over a simultaneously-ready grace timer.
	select {
	case completion, ok := <-process.Done():
		if ok {
			return completion, reason, true
		}
		return unknown, reason, true
	default:
	}
	select {
	case completion, ok := <-process.Done():
		if ok {
			return completion, reason, true
		}
	case <-grace:
		if cleanupErr := process.Kill(); cleanupErr != nil {
			return Completion{Exit: -1, Err: NewCleanupError(process, cleanupErr)}, "package_manager_failed", true
		}
	}
	return unknown, reason, true
}
func brewCommand(brew, kind string, s catalogue.Source, sourceEnv map[string]string) (Command, error) {
	if brew != "/opt/homebrew/bin/brew" && brew != "/usr/local/bin/brew" {
		return Command{}, errors.New("invalid brew path")
	}
	if s.Manager != "homebrew" || !regexp.MustCompile(`^[a-z0-9][a-z0-9@._+-]{0,63}$`).MatchString(s.Name) {
		return Command{}, errors.New("invalid source")
	}
	verb := "install"
	if kind == "brew_upgrade" {
		verb = "upgrade"
	} else if kind != "brew_install" {
		return Command{}, errors.New("invalid operation")
	}
	argv := []string{brew, verb}
	if s.Type == "cask" && s.Name == "orbstack" && s.Tap == "homebrew/cask" {
		argv = append(argv, "--cask")
	} else if s.Type != "formula" || s.Tap != "homebrew/core" {
		return Command{}, errors.New("invalid source")
	}
	argv = append(argv, s.Name)
	env := map[string]string{}
	for k, v := range sourceEnv {
		if strings.ContainsRune(v, 0) {
			return Command{}, errors.New("invalid environment")
		}
		if environmentKey(k) {
			env[k] = v
		}
	}
	if !path.IsAbs(env["HOME"]) || path.Clean(env["HOME"]) != env["HOME"] {
		return Command{}, errors.New("invalid HOME")
	}
	env["PATH"] = path.Dir(brew) + ":/usr/bin:/bin:/usr/sbin:/sbin"
	env["HOMEBREW_NO_AUTO_UPDATE"] = "1"
	env["HOMEBREW_NO_ANALYTICS"] = "1"
	env["HOMEBREW_NO_ENV_HINTS"] = "1"
	return Command{Argv: argv, Env: env, Stdin: "closed", OutputLimit: 64 * 1024}, nil
}
func environmentKey(k string) bool {
	switch k {
	case "HOME", "LANG", "TERM", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "LC_ALL", "LC_CTYPE", "LC_COLLATE", "LC_MESSAGES", "LC_MONETARY", "LC_NUMERIC", "LC_TIME", "LC_ADDRESS", "LC_IDENTIFICATION", "LC_MEASUREMENT", "LC_NAME", "LC_PAPER", "LC_TELEPHONE":
		return true
	}
	return false
}

// PrivilegedPreview validates only preflight. It deliberately cannot execute:
// the current sealed plan builder has no privileged_exec construction path.
// Future execution must enter that plan, preserve exact shown argv, use terminal
// stdin and invalidate sudo credentials with /usr/bin/sudo -k even on failure.
func PrivilegedPreview(item catalogue.Item, argv []string, p Policy) (Command, error) {
	if p.DryRun || p.Mode == "doctor" || (p.Mode != "setup" && p.Mode != "fix") || !p.Terminal || p.JSON || p.NonInteractive || !p.Confirmed[item.ID] || (p.Mode == "fix" && !p.Yes) {
		return Command{}, errors.New("host_setup.sudo_requires_terminal")
	}
	declared := false
	for _, entry := range item.Privileged {
		if reflect.DeepEqual(entry.Argv, argv) {
			declared = true
		}
	}
	if !declared || len(argv) == 0 || len(argv) > 8 || !path.IsAbs(argv[0]) || path.Clean(argv[0]) != argv[0] || path.Base(argv[0]) == "brew" || path.Base(argv[0]) == "sudo" {
		return Command{}, errors.New("invalid privileged command")
	}
	for _, arg := range argv {
		if !regexp.MustCompile(`^[A-Za-z0-9._/=:+-]{1,128}$`).MatchString(arg) {
			return Command{}, errors.New("invalid privileged argument")
		}
	}
	shown := append([]string{"/usr/bin/sudo", "--"}, argv...)
	if !reflect.DeepEqual(shown, p.Shown[item.ID]) {
		return Command{}, errors.New("privileged preview changed")
	}
	return Command{Argv: shown, Stdin: "terminal", OutputLimit: 64 * 1024}, nil
}

// Lookup never supplies host-derived headers, request bodies or credentials.
// The Network adapter must enforce no redirects and the request's byte/deadline
// bounds. A transport failure is the ordinary 'not verified' result, not evidence
// that a version meets its minimum. There is no production Network adapter here.
func (e Engine) Lookup(ctx context.Context, item catalogue.Item, raw string) (bool, error) {
	declared := false
	for _, s := range item.Network.LookupURLs {
		if s == raw {
			declared = true
		}
	}
	u, err := url.Parse(raw)
	if !declared || err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || u.Opaque != "" {
		return false, errors.New("lookup URL not permitted")
	}
	allowed := false
	for _, host := range item.Network.Hosts {
		if host == u.Host {
			allowed = true
		}
	}
	if !allowed || !regexp.MustCompile(`^https://[a-z0-9][a-z0-9.-]{0,252}/[A-Za-z0-9._~/%+-]{0,256}$`).MatchString(raw) {
		return false, errors.New("lookup host not permitted")
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if e.Network == nil {
		return false, errors.New("network port unavailable")
	}
	request := LookupRequest{URL: raw, Method: "GET", UserAgent: LookupUserAgent, Timeout: 5 * time.Second, BodyLimit: 64 * 1024}
	lookupCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	verified, err := e.Network.Lookup(lookupCtx, request)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil || lookupCtx.Err() != nil {
		return false, nil
	}
	return verified, nil
}

// ValidateBrewCommand checks the complete fixed invocation at a native execution
// boundary. Rebuilding with the same constructor keeps argv and environment
// policy identical to the sealed package engine; it does not authorize a run.
func ValidateBrewCommand(c Command) error {
	if len(c.Argv) < 3 || len(c.Argv) > 4 {
		return errors.New("invalid brew command")
	}
	source := catalogue.Source{Manager: "homebrew", Type: "formula", Tap: "homebrew/core", Name: c.Argv[len(c.Argv)-1]}
	if len(c.Argv) == 4 {
		if c.Argv[2] != "--cask" {
			return errors.New("invalid brew command")
		}
		source.Type = "cask"
		source.Tap = "homebrew/cask"
	}
	kind := "brew_install"
	if c.Argv[1] == "upgrade" {
		kind = "brew_upgrade"
	} else if c.Argv[1] != "install" {
		return errors.New("invalid brew command")
	}
	rebuilt, err := brewCommand(c.Argv[0], kind, source, c.Env)
	if err != nil || !reflect.DeepEqual(rebuilt, c) {
		return errors.New("invalid brew command")
	}
	return nil
}
