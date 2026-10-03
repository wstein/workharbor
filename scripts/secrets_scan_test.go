package scripts

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// secretsRange runs the pre-push hook from the repository root with gitleaks
// replaced by a stub command, so no real scan and no real secret is involved.
func secretsRange(t *testing.T, stub string) (string, error) {
	t.Helper()
	env := append(gittest.Env(t.TempDir()), "GITLEAKS_RUN="+stub)
	return bash(t, env, "cd .. && echo 'refs/heads/x 1111111111111111111111111111111111111111 refs/heads/x 0000000000000000000000000000000000000000' | .githooks/pre-push")
}

func TestPrePushTellsFindingFromMissingTool(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, stub string
		ok         bool
		want       string
		notWant    string
	}{
		{"clean", "true", true, "", ""},
		{"leak", "sh -c 'exit 1' --", false, "holds a secret", "could not run"},
		{"missing binary", "whtmp-no-such-gitleaks", false, "could not run", "holds a secret"},
		{"other exit", "sh -c 'exit 3' --", false, "could not run", "holds a secret"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := secretsRange(t, c.stub)
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok=%v\n%s", err, c.ok, out)
			}
			if c.want != "" && !strings.Contains(out, c.want) || c.notWant != "" && strings.Contains(out, c.notWant) {
				t.Errorf("want %q, not %q:\n%s", c.want, c.notWant, out)
			}
			if strings.Contains(out, "--no-verify") {
				t.Errorf("must never suggest --no-verify:\n%s", out)
			}
		})
	}
}
