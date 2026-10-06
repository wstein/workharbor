package setup

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
)

func runSh(ctx context.Context, script string) (string, error) {
	var buf bytes.Buffer
	t := Terminal{Err: &buf}
	err := t.Run(ctx, doctor.Cmd{Argv: []string{"sh", "-c", script}})
	return buf.String(), err
}

func TestTerminalRunReportsTheExitStatusAfterTheOutput(t *testing.T) {
	out, err := runSh(context.Background(), "echo first; echo second >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v, want exit status 3", err)
	}
	i, j := strings.Index(out, "first"), strings.Index(out, "second")
	if i < 0 || j < i {
		t.Errorf("output out of order or lost:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("the block is not closed with a newline: %q", out)
	}
	if _, err := runSh(context.Background(), "printf done"); err != nil {
		t.Errorf("a successful command failed: %v", err)
	}
}

func TestTerminalRunIsNotStalledByABackgroundChild(t *testing.T) {
	limit := runWaitDelay + 3*time.Second
	start := time.Now()
	if _, err := runSh(context.Background(), "sleep 30 & echo hi"); err != nil {
		t.Errorf("normal exit: %v", err)
	}
	if d := time.Since(start); d > limit {
		t.Errorf("normal exit took %v, want about %v", d, runWaitDelay)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start = time.Now()
	_, _ = runSh(ctx, "sleep 30 & wait")
	if d := time.Since(start); d > limit {
		t.Errorf("after cancel took %v, want about %v", d, runWaitDelay)
	}
}
