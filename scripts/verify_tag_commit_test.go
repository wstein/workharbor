package scripts

import (
	"path/filepath"
	"strings"
	"testing"
)

func verifyTag(t *testing.T, dir, tag, sha string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("verify-tag-commit.sh")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"TAG=" + tag, "GITHUB_SHA=" + sha}
	return bash(t, env, "cd '"+dir+"' && "+script)
}

func TestVerifyTagCommit(t *testing.T) {
	t.Parallel()
	clone, _, first := mainFixture(t)
	gitRun(t, clone, "tag", "v1.0.0")                  // lightweight
	gitRun(t, clone, "tag", "-a", "v1.0.1", "-m", "x") // annotated: needs the peel
	for _, tag := range []string{"v1.0.0", "v1.0.1"} {
		out, err := verifyTag(t, clone, tag, first)
		if err != nil || strings.TrimSpace(out) != first {
			t.Errorf("%s: err = %v, out = %q", tag, err, out)
		}
	}

	// The tag is re-pushed to another commit after the run started.
	gitRun(t, clone, "commit", "--allow-empty", "-m", "two")
	second := gitRun(t, clone, "rev-parse", "HEAD")
	gitRun(t, clone, "tag", "-f", "-a", "v1.0.1", "-m", "y", second)
	out, err := verifyTag(t, clone, "v1.0.1", first)
	if err == nil || !strings.Contains(out, "::error::") || !strings.Contains(out, "v1.0.1") ||
		!strings.Contains(out, second) || !strings.Contains(out, first) {
		t.Errorf("a moved tag must fail naming the tag and both commits: err = %v\n%s", err, out)
	}
}

func TestVerifyTagCommitBadInput(t *testing.T) {
	t.Parallel()
	clone, _, first := mainFixture(t)
	for name, c := range map[string][2]string{
		"missing tag": {"v9.9.9", first},
		"empty tag":   {"", first},
		"bad sha":     {"v1.0.0", "abc"},
	} {
		if out, err := verifyTag(t, clone, c[0], c[1]); err == nil || !strings.Contains(out, "::error::") {
			t.Errorf("%s: err = %v\n%s", name, err, out)
		}
	}
}
