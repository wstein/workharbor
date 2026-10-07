package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
)

// Issue #378: a command with a SecretPrompt gets the secret on stdin only. The
// marker is random at test time; the fake child records argv, environment and
// stdin to files, and prompts for a password on its stdout as sysadminctl did.
func TestRunSecretGoesOnlyToStdin(t *testing.T) {
	var raw [24]byte
	_, _ = rand.Read(raw[:])
	marker := "fake-" + hex.EncodeToString(raw[:])
	dir := t.TempDir()
	rec := filepath.Join(dir, "argv")
	envf := filepath.Join(dir, "env")
	inf := filepath.Join(dir, "stdin")
	script := `printf '%s\n' "$@" > "$1"; env > "$2"; printf 'User password:'; IFS= read -r l; printf '%s\n' "$l" > "$3"; printf '\n'`
	var errBuf strings.Builder
	h := Terminal{Err: &errBuf, Style: render.Style{}, readSecret: func(string) (string, error) { return marker, nil }}
	c := doctor.Cmd{Argv: []string{"/bin/sh", "-c", script, "sh", rec, envf, inf}, SecretPrompt: "pw"}
	if err := h.Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(readFile(t, inf)) != marker {
		t.Fatalf("the child did not get the secret on stdin")
	}
	for name, text := range map[string]string{"relay output": errBuf.String(), "argv": strings.Join(c.Argv, " "), "recorded argv": readFile(t, rec), "env": readFile(t, envf)} {
		if strings.Contains(text, marker) {
			t.Errorf("the secret leaked into %s", name)
		}
	}
	if strings.Contains(strings.ToLower(errBuf.String()), "password:") || !strings.Contains(errBuf.String(), render.NeutralPromptLine) {
		t.Errorf("the child's secret prompt was relayed: %q", errBuf.String())
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p) //nolint:gosec // a path the test made
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
