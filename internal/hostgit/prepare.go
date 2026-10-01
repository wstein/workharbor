package hostgit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Errors of preparing and pushing a topic.
var (
	ErrRebaseConflict = errors.New("the topic does not rebase onto the target without conflicts")
	ErrLint           = errors.New("a commit message breaks the commit rules")
	ErrNotSigned      = errors.New("a rewritten commit is not signed")
	ErrNoSigningKey   = errors.New("preparing a topic needs the bot's signing key")
	ErrNotFastForward = errors.New("the push is not a fast-forward")
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
}

// Prepared is a topic ready for review: the new tip and the commits on it.
type Prepared struct {
	SHA     string
	Commits []string // oldest first
}

// Prepare rebases the topic onto the target in a temporary worktree with
// autosquash (hooks off), folding fixup commits, with the bot as committer and
// every rewritten commit signed. It then lints every message, checks the
// signatures are there, and only then moves the topic branch to the new tip.
// On a conflict, a lint problem or an unsigned commit the branch is left as it
// was and nothing remains of the worktree. Commits already pushed must not be
// passed in: the caller prepares only unpushed work.
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
	topicRef, targetRef := "refs/heads/"+spec.Topic, "refs/heads/"+spec.Target
	oldTip, err := r.revParse(ctx, topicRef)
	if err != nil {
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
	if _, err := r.g.run(ctx, r.path, false, nil, "worktree", "add", "--quiet", "--detach", dir, topicRef); err != nil {
		return Prepared{}, err
	}

	env := []string{
		"GIT_SEQUENCE_EDITOR=:",
		"GIT_COMMITTER_NAME=" + spec.Committer.Name, "GIT_COMMITTER_EMAIL=" + spec.Committer.Email,
	}
	_, err = r.g.run(ctx, dir, false, env,
		"-c", "gpg.format=ssh", "-c", "user.signingkey="+spec.SigningKey,
		"rebase", "--quiet", "--interactive", "--autosquash", "--force-rebase", "--gpg-sign", targetRef)
	if err != nil {
		_, _ = r.g.run(ctx, dir, false, nil, "rebase", "--abort")
		return Prepared{}, fmt.Errorf("%w: %v", ErrRebaseConflict, err) //nolint:errorlint // the git output is the detail
	}

	newTip, err := r.g.run(ctx, dir, false, nil, "rev-parse", "HEAD")
	if err != nil {
		return Prepared{}, err
	}
	tip := strings.TrimSpace(string(newTip))
	list, err := r.g.run(ctx, dir, false, nil, "rev-list", "--reverse", targetRef+".."+tip)
	if err != nil {
		return Prepared{}, err
	}
	out := Prepared{SHA: tip, Commits: strings.Fields(string(list))}

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

// Push sends one agent branch to the remote at exactly the approved commit:
// "<sha>:refs/heads/<branch>", fast-forward only, never forced, no tags. Only
// agent/* branches go, and the commit must be one this repository holds. The
// remote is an absolute path or a plain https URL; credentials come with the
// forge adapter, not from the host (design §4.5).
func (r *Repo) Push(ctx context.Context, remote, branch, sha string) error {
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
	args := append(pre, "push", "--quiet", "--no-follow-tags", "--no-verify", "--", remote, sha+":refs/heads/"+branch)
	if _, err := r.g.run(ctx, r.path, true, env, args...); err != nil {
		if strings.Contains(err.Error(), "non-fast-forward") || strings.Contains(err.Error(), "rejected") {
			return fmt.Errorf("%w: %v", ErrNotFastForward, err) //nolint:errorlint // the git output is the detail
		}
		return err
	}
	return nil
}
