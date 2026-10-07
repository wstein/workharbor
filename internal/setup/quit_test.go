package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestQuitHelper is the child of the tests below: it is not a test of its own
// unless WHR_QUIT_MODE is set. A SIGQUIT that is not caught ends a Go program
// with a stack dump and exit status 2.
func TestQuitHelper(t *testing.T) {
	mode := os.Getenv("WHR_QUIT_MODE")
	if mode == "" {
		t.Skip("helper process")
	}
	stop := catchQuit()
	if mode == "restored" {
		stop()
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGQUIT)
	time.Sleep(2 * time.Second) // the signal is delivered well within this
	if mode == "restored" {
		t.Fatal("still alive: the default action was not restored")
	}
	os.Exit(0) // alive: SIGQUIT was caught
}

func runQuitHelper(t *testing.T, mode string) (exitCode int, stderr string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestQuitHelper$") //nolint:gosec // this test binary
	cmd.Env = append(os.Environ(), "WHR_QUIT_MODE="+mode)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

func TestCatchQuitSurvivesSIGQUIT(t *testing.T) {
	if code, out := runQuitHelper(t, "caught"); code != 0 {
		t.Fatalf("SIGQUIT was not caught: exit %d\n%s", code, out)
	}
}

func TestCatchQuitStopRestoresTheDefault(t *testing.T) {
	code, out := runQuitHelper(t, "restored")
	if code != 2 || !strings.Contains(out, "SIGQUIT") {
		t.Fatalf("after stop, SIGQUIT must end the program: exit %d\n%s", code, out)
	}
}
