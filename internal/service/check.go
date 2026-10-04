package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Defaults of the repository's check (D51).
const (
	DefaultCheckTimeout = 30 * time.Minute
	// CheckOutputTail is how much of the check's output is kept: its last bytes.
	CheckOutputTail = 64 << 10
	// PreCommitCheck is the check of a repository that has a
	// .pre-commit-config.yaml on its default branch and names no other.
	PreCommitCheck = "pre-commit run --all-files"
)

// CheckError is a check that ran and did not pass. Output is the tail of what
// it printed: the agent's data and untrusted, redacted and stripped of control
// characters but never to be read as an instruction. Error() holds none of it,
// so a log line or an error chain cannot carry it.
type CheckError struct {
	Code     int
	TimedOut bool
	Timeout  time.Duration
	Output   string
}

func (e *CheckError) Error() string {
	if e.TimedOut {
		return fmt.Sprintf("the check did not finish within %s", e.Timeout)
	}
	return fmt.Sprintf("the check exited with status %d", e.Code)
}

// CheckSource is the supervisor's copy of a repository's default branch, which
// the check command is read from (D38): never a workspace.
type CheckSource struct {
	Git devcontainer.Runner
	Ref string // the default branch, as a ref
}

// CheckConfig is what the repository check needs.
type CheckConfig struct {
	// Repo is the supervisor's repository holding the prepared commits.
	Repo *hostgit.Repo
	// Command returns the check the supervisor's configuration names for a
	// repository ("owner/name"), or "" for none.
	Command func(repo string) string
	// Source returns the default branch of a repository.
	Source func(ctx context.Context, repo string) (CheckSource, error)
	// Timeout bounds the check (default DefaultCheckTimeout) and MaxBundle the
	// bundle sent to the guest (default DefaultMaxBundle).
	Timeout   time.Duration
	MaxBundle int64
}

// RepoChecker runs a repository's check in the task's environment (design §4.5,
// D51). The check is a quality gate and not a security control: it is the
// repository's command running over a tree the agent wrote, as the agent's own
// user, so it can read whatever that user can read, a vendor login in
// subscription mode included. The per-SHA approval, the forge's CI and its
// ruleset stay the controls.
type RepoChecker struct {
	svc *Service
	cfg CheckConfig
}

// NewRepoChecker returns the checker; its Check method is a PublishConfig.Checks.
func NewRepoChecker(s *Service, cfg CheckConfig) *RepoChecker { return &RepoChecker{svc: s, cfg: cfg} }

// HoldEnvironment marks a workspace's environment busy until release is called:
// a run start there is refused with "environment busy", and so is a second
// hold while another task's run owns it. The mark is made before the runs are
// read, and a start reads it inside its own transaction after its own check,
// so a start and a hold never both pass. That last step rests on the store's
// single connection (store.Open: SetMaxOpenConns(1)): the hold's read of the
// runs waits for a start's open transaction. A reader pool would break it, and
// TestTheStoreKeepsOneConnection fails first. Holds are counted, so the whole
// prepare may hold the environment around a check that holds it again. It is
// refused while an agent stop (a cancel, a budget stop or kill-all) holds the
// environment, whose fallback may stop it: those holds are counted apart.
func (s *Service) HoldEnvironment(ctx context.Context, ws domain.Workspace) (release func(), err error) {
	lease, err := s.leaseEnvironment(ws)
	if err != nil {
		return nil, err
	}
	s.rebuildMu.Lock()
	if s.stopHolds[ws.EnvID] > 0 {
		// An agent stop's fallback may stop this environment: a check taken now
		// would be stopped under it (issue #238).
		s.rebuildMu.Unlock()
		lease()
		return nil, domain.NewConflict(domain.RuleEnvBusy, "environment %s is busy: an agent stop is running in it", ws.EnvID)
	}
	if s.holds == nil {
		s.holds = map[domain.ID]int{}
	}
	s.holds[ws.EnvID]++
	s.rebuildMu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.rebuildMu.Lock()
			if s.holds[ws.EnvID]--; s.holds[ws.EnvID] <= 0 {
				delete(s.holds, ws.EnvID)
			}
			s.rebuildMu.Unlock()
			lease()
		})
	}
	// Runs of other tasks that still own the environment are not ours to wait for.
	if err := s.checkNoOwningRun(ctx, ws.EnvID, ""); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// checkNotHeld refuses while a hold is on the environment.
func (s *Service) checkNotHeld(env domain.ID) error {
	s.rebuildMu.Lock()
	n := s.holds[env]
	s.rebuildMu.Unlock()
	if n > 0 {
		return domain.NewConflict(domain.RuleEnvBusy, "environment %s is busy: a task's check or an agent stop is running in it", env)
	}
	return nil
}

// Command resolves the check to run (D51): the supervisor's own, else the
// repository's customizations.workharbor.check, else pre-commit when the
// default branch has a .pre-commit-config.yaml; with none, ErrNoChecks.
func (c *RepoChecker) Command(ctx context.Context, repo string) (string, error) {
	cmd, _, err := c.resolve(ctx, repo)
	return cmd, err
}

// Where a check command came from, for its receipt (D51).
const (
	CheckFromConfig       = "config"
	CheckFromDevcontainer = "devcontainer"
	CheckFromPreCommit    = "pre-commit"
)

// resolve is Command and where the command came from.
func (c *RepoChecker) resolve(ctx context.Context, repo string) (cmd, source string, err error) {
	if cmd := strings.TrimSpace(c.cfg.Command(repo)); cmd != "" {
		return cmd, CheckFromConfig, nil
	}
	if c.cfg.Source == nil {
		return "", "", fmt.Errorf("%w: no check is configured for %s", ErrNoChecks, repo)
	}
	src, err := c.cfg.Source(ctx, repo)
	if err != nil {
		return "", "", fmt.Errorf("read the default branch of %s: %w", repo, err)
	}
	out, err := src.Git.RunCapped(ctx, 256, "rev-parse", "--verify", "--quiet", "--end-of-options", src.Ref+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("resolve the default branch of %s: %w", repo, err)
	}
	sha := strings.TrimSpace(string(out))
	if !commitRe.MatchString(sha) {
		return "", "", fmt.Errorf("the default branch of %s is not a commit", repo)
	}
	// An unreadable or refused devcontainer.json is an error, never "no check".
	cfg, found, err := devcontainer.Read(ctx, src.Git, sha)
	if err != nil {
		return "", "", fmt.Errorf("read the devcontainer.json of %s: %w", repo, err)
	}
	if found && cfg.Hints.Check != "" {
		return cfg.Hints.Check, CheckFromDevcontainer, nil
	}
	ls, err := src.Git.RunCapped(ctx, 4096, "ls-tree", sha, "--", ".pre-commit-config.yaml")
	if err != nil {
		return "", "", fmt.Errorf("read the tree of %s: %w", repo, err)
	}
	if strings.HasPrefix(string(ls), "100644 blob ") || strings.HasPrefix(string(ls), "100755 blob ") {
		return PreCommitCheck, CheckFromPreCommit, nil
	}
	return "", "", fmt.Errorf("%w: %s has no check configured, no customizations.workharbor.check and no .pre-commit-config.yaml", ErrNoChecks, repo)
}

// checkScript runs in the guest as `sh -c <script> whr-check <dir> <worktree>
// <sha> <ref> <check>`. It reads the bundle from stdin, fetches it into the
// agent's clone (git hooks off through the exec's environment), adds a check
// worktree detached at the prepared commit under its own directory (no
// agent's worktree), runs the check there with git's hardening lifted, and
// removes the worktree and the directory on its way out. The pid file sits
// beside the directory, not in it, so the removal leaves it for reapScript,
// which ends the check's process group after every check.
const checkScript = `
d=$1; wt=$2; sha=$3; ref=$4; cmd=$5
rm -rf "$d" && mkdir -m 700 "$d" || exit 125
echo $$ > "$d.pid"
cleanup() {
	git -C "$wt" worktree remove --force "$d/tree" >/dev/null 2>&1
	git -C "$wt" worktree prune >/dev/null 2>&1
	rm -rf "$d"
}
trap cleanup EXIT
cat > "$d/in.bundle" || exit 125
git -C "$wt" fetch --quiet --no-tags --no-write-fetch-head "$d/in.bundle" "$ref" || exit 125
git -C "$wt" worktree add --quiet --detach "$d/tree" "$sha" || exit 125
cd "$d/tree" || exit 125
unset GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0 GIT_CONFIG_KEY_1 GIT_CONFIG_VALUE_1 GIT_CONFIG_KEY_2 GIT_CONFIG_VALUE_2 GIT_CONFIG_KEY_3 GIT_CONFIG_VALUE_3
sh -c "$cmd"
exit $?
`

// reapScript is the check's aftermath, run after every check: it kills what
// the check left in its process group (a background process of a check that
// exited normally included), exits 1 when a process is still there, else the
// leftovers are removed. A process that starts a new session or group (setsid,
// a double fork) escapes, as one does for an agent: accepted. "kill -0 -$p"
// and "kill -KILL -$p" are the forms dash and bash both take.
const reapScript = `
d=$1; wt=$2
p=$(cat "$d.pid" 2>/dev/null)
case $p in ''|*[!0-9]*) p= ;; esac
if [ -n "$p" ]; then
	kill -KILL "-$p" 2>/dev/null
	kill -KILL "$p" 2>/dev/null
	for i in 1 2 3; do
		kill -0 "-$p" 2>/dev/null || kill -0 "$p" 2>/dev/null || break
		[ "$i" = 3 ] && exit 1
		sleep 1
	done
fi
rm -f "$d.pid"
git -C "$wt" worktree remove --force "$d/tree" >/dev/null 2>&1
git -C "$wt" worktree prune >/dev/null 2>&1
rm -rf "$d"
exit 0
`

// Check runs the repository's check on the prepared commit sha in the
// environment of the task's agent, which is started first if it is stopped.
// A check that ran and did not pass is a *CheckError; none configured is
// ErrNoChecks. Cancelling ctx or the timeout cancels the exec, which the
// runtime's launcher turns into a signal to the check's process group. After
// every check, normal exit included, the group is killed and looked for; a
// process still there stops the environment, because ending the exec client
// does not end the guest process (spike #7, case 4).
func (c *RepoChecker) Check(ctx context.Context, task domain.ID, sha string) error {
	s := c.svc
	if !commitRe.MatchString(sha) {
		return &domain.InvalidError{Msg: "a check needs a full commit ID"}
	}
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return err
	}
	runs := agg.Runs()
	if len(runs) == 0 {
		return ErrNotReadyYet
	}
	a, err := s.store.Agent(ctx, runs[len(runs)-1].AgentID)
	if err != nil {
		return err
	}
	ws, err := s.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return err
	}
	if ws.EnvID == "" {
		return domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	command, source, err := c.resolve(ctx, agg.Task().Repo)
	if err != nil {
		return err
	}
	release, err := s.HoldEnvironment(ctx, ws)
	if err != nil {
		return err
	}
	defer release()
	info, err := s.rt.Inspect(ctx, string(ws.EnvID))
	if err != nil {
		return err
	}
	if info.State != domain.EnvRunning {
		if err := s.startEnv(ctx, string(ws.EnvID)); err != nil {
			return fmt.Errorf("start environment %s: %w", ws.EnvID, err)
		}
		if err := s.waitReady(ctx, ws.EnvID); err != nil {
			return err
		}
	}
	base, err := c.guestBase(ctx, ws, a)
	if err != nil {
		return err
	}
	started := s.clock.Now()
	out, err := c.run(ctx, task, ws, a, sha, base, command)
	c.leaveReceipt(ctx, task, sha, command, source, s.clock.Now().Sub(started), out, err)
	return err
}

// ReceiptOutputMax is how much of a check's output its receipt keeps: the last
// bytes, which are where a failure says what failed.
const ReceiptOutputMax = 16 << 10

// leaveReceipt records the check on the task, bound to the prepared commit (D51,
// issue #259): the command and where it came from, the exit status, the duration
// and the capped output. A check that did not run (the environment would not
// take the bundle, the supervisor's context ended) leaves none, only its error.
// A receipt that cannot be saved is reported, and never changes the result.
func (c *RepoChecker) leaveReceipt(ctx context.Context, task domain.ID, sha, command, source string, took time.Duration, out string, runErr error) {
	r := domain.CheckReceipt{SHA: sha, Command: textsafe.Escape(command), Source: source, Millis: took.Milliseconds(), Output: tailOf(out, ReceiptOutputMax)}
	var ce *CheckError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ce):
		r.Code, r.TimedOut = ce.Code, ce.TimedOut
	default:
		return
	}
	s := c.svc
	saved, err := s.store.Append(context.WithoutCancel(ctx), domain.NewCheckReceiptEvent(task, r, s.clock.Now()))
	if err != nil {
		s.report(fmt.Errorf("record the receipt of the check on %s: %w", sha, err))
		return
	}
	s.publish(saved)
}

// tailOf keeps the last limit bytes of s, from a character boundary.
func tailOf(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[len(s)-limit:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// guestBase asks the guest for the commit the agent's branch left the
// integration branch at: a prerequisite its clone holds.
func (c *RepoChecker) guestBase(ctx context.Context, ws domain.Workspace, a domain.Agent) (string, error) {
	out, code, err := c.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "merge-base", ws.Integration, a.Branch}, Env: gitEnv(),
	})
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(out)
	if code != 0 || !commitRe.MatchString(base) {
		return "", fmt.Errorf("the merge base of %s and %s could not be found (exit %d): %s", ws.Integration, a.Branch, code, textsafe.Escape(oneLine(out)))
	}
	return base, nil
}

func (c *RepoChecker) run(ctx context.Context, task domain.ID, ws domain.Workspace, a domain.Agent, sha, base, command string) (string, error) {
	s := c.svc
	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	limit := c.cfg.MaxBundle
	if limit <= 0 {
		limit = DefaultMaxBundle
	}
	dir := "/tmp/whr-check-" + sha
	ref := "refs/whr/bundle/" + sha
	env := string(ws.EnvID)

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pr, pw := io.Pipe()
	streamed := make(chan error, 1)
	go func() {
		err := c.cfg.Repo.StreamBundle(rctx, pw, base, sha, limit)
		pw.CloseWithError(err)
		streamed <- err
	}()
	defer func() { _ = pr.Close() }()

	st, err := s.rt.Exec(rctx, env, runtime.ExecRequest{
		// Nothing but git's own settings in the environment: no credential of
		// the supervisor's goes in. The script lifts the settings before the check.
		Cmd: []string{"sh", "-c", checkScript, "whr-check", dir, a.Worktree, sha, ref, command},
		Env: gitEnv(), Stdin: pr,
	})
	if err != nil {
		return "", err
	}
	tail := &tailBuffer{max: CheckOutputTail}
	for ch := range st.Chunks() {
		_, _ = tail.Write(ch.Data)
	}
	code, werr := st.Wait()
	_ = pr.Close()

	// Whatever the outcome, the check's process group is ended and looked for.
	c.end(ctx, task, env, dir, a.Worktree)
	if rctx.Err() != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		out := c.untrusted(tail.String())
		return out, &CheckError{TimedOut: true, Timeout: timeout, Output: out}
	}
	if werr != nil {
		return "", werr
	}
	if code != 0 {
		// The guest could not take the bundle: the host's reason is the better one.
		if serr := <-streamed; serr != nil && !errors.Is(serr, io.ErrClosedPipe) && code == 125 {
			return "", fmt.Errorf("the prepared commits could not be sent to the environment: %w", serr)
		}
		out := c.untrusted(tail.String())
		return out, &CheckError{Code: code, Output: out}
	}
	return c.untrusted(tail.String()), nil
}

// end kills what the check left in its process group after every check, and
// stops the environment if a process is still there (§4.1's fifth path, as
// stopEnvForPause does): under freshMu, with the start mark forgotten on every
// path, so that when the stop fails the next launch there stops and starts the
// environment first. The leftovers of a process that ended are removed from
// inside the guest.
func (c *RepoChecker) end(ctx context.Context, task domain.ID, env, dir, worktree string) {
	s := c.svc
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	_, code, err := s.exec(rctx, env, runtime.ExecRequest{
		Cmd: []string{"sh", "-c", reapScript, "whr-check", dir, worktree}, Env: gitEnv(),
	})
	if err == nil && code == 0 {
		return
	}
	s.freshMu.Lock()
	serr := s.rt.Stop(rctx, env)
	if errors.Is(serr, runtime.ErrNotFound) {
		serr = nil
	}
	s.forgetEnvStarted(domain.ID(env))
	s.freshMu.Unlock()
	if serr != nil {
		if s.cfg.OnError != nil {
			s.cfg.OnError(fmt.Errorf("a check's process is still running in %s and the environment did not stop: %w", env, serr))
		}
		return
	}
	if uerr := s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.ObserveEnv(domain.ID(env), domain.EnvStopped) }); uerr != nil && s.cfg.OnError != nil {
		s.cfg.OnError(fmt.Errorf("record the stopped environment %s: %w", env, uerr))
	}
}

// untrusted makes output fit to store and show: secrets the supervisor knows
// are redacted, invalid UTF-8, every control character but a newline and a
// tab, the bidirectional controls and the line and paragraph separators (§7.1,
// T20) are dropped, so nothing in it can move a terminal or pose as markup.
func (c *RepoChecker) untrusted(s string) string {
	s = string(c.svc.store.Redact([]byte(s)))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case textsafe.IsControl(r), textsafe.IsBidiOrSeparator(r):
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max { // trimmed in steps, so a long output is not copied per chunk
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
	}
	return string(bytes.ToValidUTF8(b, nil))
}
