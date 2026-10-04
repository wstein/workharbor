package hostgit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Errors of preparing and pushing a topic.
var (
	ErrRebaseConflict = errors.New("the topic does not rebase onto the target without conflicts")
	ErrLint           = errors.New("a commit message breaks the commit rules")
	ErrNotSigned      = errors.New("a rewritten commit is not signed")
	ErrNoSigningKey   = errors.New("preparing a topic needs the bot's signing key")
	ErrNotFastForward = errors.New("the push is not a fast-forward")
	// ErrTransport is a push that failed for a transport fault (the network, a
	// timeout, a server error): trying again later can work. Anything else a push
	// reports is a refusal a retry does not change.
	ErrTransport = errors.New("the push failed for a transport fault")
	// ErrHistoryRewritten: the agent rewrote commits it had already handed in, so
	// a follow-up round cannot tell which of its commits are new.
	ErrHistoryRewritten = errors.New("the agent rewrote commits that were already prepared")
	ErrNoCommits        = errors.New("the topic has no commit after its target: nothing to publish")
	ErrNothingNew       = errors.New("the topic has no commits after the pushed revision")
)

// Identity is who commits the rewritten commits: the bot. The authors of the
// original commits are kept.
type Identity struct{ Name, Email string }

// PrepareSpec says how to prepare a topic for push (design §4.5).
type PrepareSpec struct {
	Target, Topic string   // branch names in the supervisor's copy
	Committer     Identity // the bot
	// SigningKey is the path of the bot's SSH private key. The rewritten
	// commits are signed with it.
	SigningKey string
	// Lint returns the problems of one commit message; none means acceptable.
	// The repository's own checks are not run here: they are the repository's
	// code and run in an environment, never on the host (design §4.5).
	Lint func(message string) []string
	// Onto and Upstream make a follow-up round after a push (design §4.5):
	// only the agent's commits after Upstream (the agent's own tip the pushed
	// revision was prepared from) are rebased onto Onto (the pushed commit),
	// so pushed commits are never rewritten and the next push is a
	// fast-forward. Both are full commit IDs, or both empty for a first round.
	Onto, Upstream string
}

// Prepared is a topic ready for review: the new tip and the commits on it.
type Prepared struct {
	SHA     string
	Commits []string // oldest first
	// Source is the agent's own tip the revision was prepared from: the
	// Upstream of the next follow-up round.
	Source string
	// Files, Added and Removed are the diff stat of the revision against what it was
	// rebased onto: the files it changes and its lines added and removed. A binary
	// file counts as a file and adds no lines. It is what the human is shown and what
	// the approval records (design §4.5, issue #111).
	Files, Added, Removed int64
}

// numstat reads `git diff-tree -r --numstat -z --no-renames` output: plumbing, as
// everything that touches an agent's commits is, so no diff driver or textconv runs.
func numstat(out []byte) (files, added, removed int64) {
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		f := strings.SplitN(string(rec), "\t", 3)
		if len(f) != 3 {
			continue
		}
		files++
		if a, err := strconv.ParseInt(f[0], 10, 64); err == nil && a > 0 {
			added += a
		}
		if r, err := strconv.ParseInt(f[1], 10, 64); err == nil && r > 0 {
			removed += r
		}
	}
	return files, added, removed
}

// Prepare rebases the topic onto the target in a temporary worktree with
// autosquash (hooks off), folding fixup commits, with the bot as committer and
// every rewritten commit signed. It then lints every message, checks the
// signatures are there, and only then moves the topic branch to the new tip.
// On a conflict, a lint problem or an unsigned commit the branch is left as it
// was and nothing remains of the worktree. Commits already pushed must not be
// passed in: a follow-up round after a push sets Onto and Upstream.
func (r *Repo) Prepare(ctx context.Context, spec PrepareSpec) (Prepared, error) {
	if !validBranch(spec.Target) || !validBranch(spec.Topic) {
		return Prepared{}, fmt.Errorf("%w: %q or %q", ErrBadBranch, spec.Target, spec.Topic)
	}
	if spec.SigningKey == "" || !filepath.IsAbs(spec.SigningKey) {
		return Prepared{}, ErrNoSigningKey
	}
	if _, err := os.Stat(spec.SigningKey); err != nil {
		return Prepared{}, fmt.Errorf("%w: %v", ErrNoSigningKey, err) //nolint:errorlint // the cause is not needed by callers
	}
	if spec.Committer.Name == "" || spec.Committer.Email == "" {
		return Prepared{}, fmt.Errorf("%w: no committer identity", ErrNoSigningKey)
	}
	ctx, cancel := context.WithTimeout(ctx, PrepareTimeout)
	defer cancel()
	topicRef, targetRef := "refs/heads/"+spec.Topic, "refs/heads/"+spec.Target
	// The topic is resolved once: the check and the checkout use this commit ID, so a
	// branch that moves meanwhile cannot slip past the check.
	oldTip, err := r.revParse(ctx, topicRef)
	if err != nil {
		return Prepared{}, err
	}
	base := targetRef // what the topic is rebased onto, and what its commits are counted from
	rebaseArgs := []string{targetRef}
	if spec.Onto != "" || spec.Upstream != "" {
		if !shaRe.MatchString(spec.Onto) || !shaRe.MatchString(spec.Upstream) {
			return Prepared{}, fmt.Errorf("%w: a follow-up round needs both Onto and Upstream as full commit IDs", ErrBadPath)
		}
		for _, sha := range []string{spec.Onto, spec.Upstream} {
			if _, err := r.revParse(ctx, sha); err != nil {
				return Prepared{}, fmt.Errorf("%w: %s is not in this repository", ErrBadPath, sha)
			}
		}
		if _, err := r.g.run(ctx, r.path, false, nil, "merge-base", "--is-ancestor", spec.Upstream, oldTip); err != nil {
			return Prepared{}, fmt.Errorf("%w: %.12s is not an ancestor of the topic", ErrHistoryRewritten, spec.Upstream)
		}
		if spec.Upstream == oldTip {
			return Prepared{}, ErrNothingNew
		}
		base = spec.Onto
		rebaseArgs = []string{"--onto", spec.Onto, spec.Upstream}
	}

	// The rebase writes every commit it replays to a worktree on the host, so the
	// expanded size of each is checked first (a small bundle can name billions of
	// paths, and a later commit may delete what an earlier one added).
	exclude := targetRef
	if spec.Upstream != "" {
		exclude = spec.Upstream
	}
	if exclude != "" {
		if exclude, err = r.revParse(ctx, exclude); err != nil {
			return Prepared{}, err
		}
	}
	if err := r.CheckCommits(ctx, oldTip, exclude); err != nil {
		return Prepared{}, err
	}

	dir, err := os.MkdirTemp("", "whr-prepare-")
	if err != nil {
		return Prepared{}, err
	}
	defer func() {
		_, _ = r.g.run(ctx, r.path, false, nil, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
		_, _ = r.g.run(ctx, r.path, false, nil, "worktree", "prune")
	}()
	if _, err := r.g.run(ctx, r.path, false, nil, "worktree", "add", "--quiet", "--detach", dir, oldTip); err != nil {
		return Prepared{}, err
	}

	env := []string{
		"GIT_SEQUENCE_EDITOR=:",
		"GIT_COMMITTER_NAME=" + spec.Committer.Name, "GIT_COMMITTER_EMAIL=" + spec.Committer.Email,
	}
	args := append([]string{
		"-c", "gpg.format=ssh", "-c", "user.signingkey=" + spec.SigningKey,
		"rebase", "--quiet", "--interactive", "--autosquash", "--force-rebase", "--gpg-sign",
	}, rebaseArgs...)
	_, err = r.g.run(ctx, dir, false, env, args...)
	if err != nil {
		_, _ = r.g.run(ctx, dir, false, nil, "rebase", "--abort")
		return Prepared{}, fmt.Errorf("%w: %v", ErrRebaseConflict, err) //nolint:errorlint // the git output is the detail
	}

	newTip, err := r.g.run(ctx, dir, false, nil, "rev-parse", "HEAD")
	if err != nil {
		return Prepared{}, err
	}
	tip := strings.TrimSpace(string(newTip))
	list, err := r.g.run(ctx, dir, false, nil, "rev-list", "--reverse", base+".."+tip)
	if err != nil {
		return Prepared{}, err
	}
	if len(strings.Fields(string(list))) == 0 {
		return Prepared{}, fmt.Errorf("%w: %s", ErrNoCommits, spec.Topic)
	}
	out := Prepared{SHA: tip, Commits: strings.Fields(string(list)), Source: oldTip}
	if stat, err := r.g.run(ctx, dir, false, nil, "diff-tree", "-r", "--numstat", "-z", "--no-renames", base, tip); err == nil {
		out.Files, out.Added, out.Removed = numstat(stat)
	}

	var problems []string
	for _, c := range out.Commits {
		raw, err := r.g.run(ctx, dir, false, nil, "cat-file", "commit", c)
		if err != nil {
			return Prepared{}, err
		}
		header, msg, _ := bytes.Cut(raw, []byte("\n\n"))
		if !bytes.Contains(header, []byte("\ngpgsig ")) && !bytes.HasPrefix(header, []byte("gpgsig ")) {
			return Prepared{}, fmt.Errorf("%w: %s", ErrNotSigned, c)
		}
		if spec.Lint != nil {
			for _, p := range spec.Lint(strings.TrimSpace(string(msg))) {
				problems = append(problems, fmt.Sprintf("%.12s: %s", c, p))
			}
		}
	}
	if len(problems) > 0 {
		return Prepared{}, fmt.Errorf("%w:\n  %s", ErrLint, strings.Join(problems, "\n  "))
	}

	// Move the branch only now, and only if nobody moved it meanwhile.
	if _, err := r.g.run(ctx, r.path, false, nil, "update-ref", topicRef, tip, oldTip); err != nil {
		return Prepared{}, err
	}
	return out, nil
}

func (r *Repo) revParse(ctx context.Context, ref string) (string, error) {
	out, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadBranch, ref)
	}
	return strings.TrimSpace(string(out)), nil
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Push sends one agent branch to a local remote at exactly the approved commit:
// "<sha>:refs/heads/<branch>", fast-forward only, never forced, no tags. Only
// agent/* branches go, and the commit must be one this repository holds. The
// remote is an absolute path: an https remote needs a credential, which only
// PushToken hands to git (design §4.5, §7.3).
func (r *Repo) Push(ctx context.Context, remote, branch, sha string) error {
	if strings.HasPrefix(remote, "https://") {
		return fmt.Errorf("%w: %q needs a credential: use PushToken", ErrBadSource, remote)
	}
	return r.push(ctx, remote, branch, sha, "")
}

func (r *Repo) push(ctx context.Context, remote, branch, sha, token string) error {
	if !strings.HasPrefix(branch, "agent/") || !validBranch(branch) {
		return fmt.Errorf("%w: %q is not an agent branch", ErrBadBranch, branch)
	}
	if !shaRe.MatchString(sha) {
		return fmt.Errorf("%w: %q is not a full commit ID", ErrBadPath, sha)
	}
	if err := validSource(remote); err != nil {
		return err
	}
	if _, err := r.revParse(ctx, sha); err != nil {
		return fmt.Errorf("%w: %s is not in this repository", ErrBadPath, sha)
	}
	env, pre := netArgs(remote)
	if token != "" {
		ta, err := tokenArgs(remote)
		if err != nil {
			return err
		}
		pre = append(pre, ta...)
	}
	args := append(pre, "push", "--quiet", "--no-follow-tags", "--no-verify", "--", remote, sha+":refs/heads/"+branch)
	if _, err := r.g.runToken(ctx, r.path, env, token, args...); err != nil {
		if strings.Contains(err.Error(), "non-fast-forward") || strings.Contains(err.Error(), "rejected") {
			return fmt.Errorf("%w: %v", ErrNotFastForward, err) //nolint:errorlint // the git output is the detail
		}
		if transportFault(ctx, err) {
			return fmt.Errorf("%w: %v", ErrTransport, err) //nolint:errorlint // the git output is the detail
		}
		return err
	}
	return nil
}

// transportMarkers are what git prints only when the network or the server
// failed and not the request: DNS, connection and TLS faults, a dropped
// transfer, a server error (HTTP 5xx) or a rate limit.
var transportMarkers = []string{
	"could not resolve host", "connection refused", "connection timed out", "connection reset",
	"operation timed out", "network is unreachable", "no route to host", "ssl connection",
	"tls connection", "unexpected disconnect", "returned error: 500", "returned error: 502",
	"returned error: 503", "returned error: 504", "returned error: 429", "rate limit",
	"http2 framing", "gnutls_handshake",
}

// weakTransportMarkers are what git also prints when the server refused the
// request over HTTP: remote-curl's "RPC failed; HTTP 413 curl 22 ... returned
// error: 413" is followed by "the remote end hung up unexpectedly". They count
// as a transport fault only when the output names no HTTP 4xx refusal, so a pack
// that is too large or a permission refused at the POST ends as a refusal and
// is not retried for ever. Unknown output stays a refusal (fail closed).
var weakTransportMarkers = []string{"rpc failed", "remote end hung up", "early eof"}

// refusedOverHTTP matches an HTTP 4xx status git reports for a refused request,
// 429 (a rate limit, a transport fault) excepted.
var refusedOverHTTP = regexp.MustCompile(`(?:http\s+|returned error:\s*)4(?:[01][0-9]|2[0-8]|[3-9][0-9])\b`)

// gitOwnLines returns the lowercased lines of git's output that git wrote
// itself. A line that starts with "remote:" is the server's text (a hook's or a
// ruleset's message), which a refusal can word as it likes, so it never counts
// as evidence of a transport fault. git runs under the C locale (Env), so its
// own lines are English and the "remote:" prefix is stable.
func gitOwnLines(out string) string {
	var keep []string
	for line := range strings.SplitSeq(out, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "remote:") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// transportFault reports whether a failed push looks like a transport fault,
// judging only git's own lines (gitOwnLines).
func transportFault(ctx context.Context, err error) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	msg := gitOwnLines(err.Error())
	for _, m := range transportMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	if refusedOverHTTP.MatchString(msg) {
		return false
	}
	for _, m := range weakTransportMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// PushToken is Push to an https remote with a per-push token (D51, §7.3). The
// remote is built by the caller from the configured "owner/name", never read
// from a repository. The token reaches git only through an inherited pipe (see
// pipeHelper), never argv, the environment, the URL, a file or a log, and it is
// removed from any error text.
func (r *Repo) PushToken(ctx context.Context, remote, branch, sha, token string) error {
	if !strings.HasPrefix(remote, "https://") {
		return fmt.Errorf("%w: %q is not an https URL", ErrBadSource, remote)
	}
	if token == "" || strings.ContainsAny(token, "\n\r\x00 \t") {
		return errors.New("hostgit: the push token is empty or not a plain token")
	}
	return r.push(ctx, remote, branch, sha, token)
}
