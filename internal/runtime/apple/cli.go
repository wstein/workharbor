package apple

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// cli runs the `container` command and returns what it printed.
type cli func(ctx context.Context, stdin io.Reader, args ...string) (stdout, stderr []byte, err error)

// execCLI runs the real binary. An error is an *ExitError with the command and
// its standard error, so a caller can tell what the runtime said.
func execCLI(bin string) cli {
	return func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin is the container CLI found by LookPath; args are built by the adapter from checked values
		cmd.Stdin = stdin
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return out.Bytes(), errOut.Bytes(), &ExitError{Args: args, Err: err, Stderr: strings.TrimSpace(errOut.String())}
		}
		return out.Bytes(), errOut.Bytes(), nil
	}
}

// ExitError is a failed `container` command.
type ExitError struct {
	Args   []string
	Err    error
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("container %s: %v: %s", strings.Join(e.Args[:min(len(e.Args), 2)], " "), e.Err, e.Stderr)
}

func (e *ExitError) Unwrap() error { return e.Err }

// stderrOf returns the standard error of a failed command, or "".
func stderrOf(err error) string {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Stderr
	}
	return ""
}
