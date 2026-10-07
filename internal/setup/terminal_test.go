package setup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup/protocol"
)

func TestTerminalConfirmationKeepsExplicitAssentAndAllowsQuit(t *testing.T) {
	for _, tc := range []struct {
		input     string
		yes, quit bool
	}{{"\n", false, false}, {"n\n", false, false}, {"yes\n", true, false}, {"q\n", false, true}, {"", false, false}} {
		var out bytes.Buffer
		h := Terminal{In: bufio.NewReader(strings.NewReader(tc.input)), Err: &out}
		yes, err := h.Confirm("Write this change?")
		if yes != tc.yes || errors.Is(err, render.ErrQuit) != tc.quit || tc.input == "" && err == nil {
			t.Errorf("%q: yes=%v err=%v", tc.input, yes, err)
		}
		if !strings.Contains(out.String(), "| ACTION  Write this change? [y/N/q]") {
			t.Errorf("unlabelled confirmation: %q", out.String())
		}
	}
}

func TestTerminalLineQuestionIsAnAction(t *testing.T) {
	var out bytes.Buffer
	h := Terminal{In: bufio.NewReader(strings.NewReader("owner/repo\n")), Err: &out}
	got, err := h.Line("Repository")
	if err != nil || got != "owner/repo" || out.String() != "| ACTION  Repository: " {
		t.Fatalf("%q %v %q", got, err, out.String())
	}
}

type nestedPromptHost struct {
	fakeHost
	terminal Terminal
}

func (h *nestedPromptHost) Ask(q string, d render.Default) (render.Answer, error) {
	return h.terminal.Ask(q, d)
}

func (h *nestedPromptHost) Confirm(q string) (bool, error) {
	return h.terminal.Confirm(q)
}

func TestNestedConfirmationQuitStopsDoAndBuild(t *testing.T) {
	for _, kind := range []string{"do", "build"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			confirm := func(p doctor.Prompter) error {
				yes, err := p.Confirm("Write this change?")
				if err != nil {
					return err
				}
				if !yes {
					return errors.New("not written")
				}
				return os.WriteFile(path, []byte("changed"), 0o600)
			}
			fix := &doctor.Fix{}
			if kind == "do" {
				fix.Do = func(_ context.Context, p doctor.Prompter) error { return confirm(p) }
			} else {
				fix.Build = func(_ context.Context, p doctor.Prompter) ([]doctor.Cmd, error) {
					return []doctor.Cmd{{Argv: []string{"must-not-run"}}}, confirm(p)
				}
			}
			var output, report bytes.Buffer
			h := &nestedPromptHost{terminal: Terminal{In: bufio.NewReader(strings.NewReader("y\nq\n")), Err: &report}}
			log := &rec{ev: &events{}}
			later := false
			steps := []doctor.Check{
				fixStep("config-github", doctor.Fail, "configuration missing", fix),
				{Name: "later", Phase: doctor.PhaseHost, Run: func(context.Context) (doctor.Status, string) { later = true; return doctor.OK, "done" }},
			}
			outs, err := Run(context.Background(), steps, h, Options{Phase: doctor.PhaseHost, Out: &output, Err: &report, Log: log, Resume: []string{"whr", "setup", "host"}})
			var quit *QuitError
			if !errors.As(err, &quit) || quit.Step != "config-github" || quit.Resume != "whr setup host --from config-github" || len(outs) != 0 {
				t.Fatalf("quit: %v, outcomes: %+v", err, outs)
			}
			data, readErr := os.ReadFile(path) //nolint:gosec // the fixture in this test's private directory
			if readErr != nil || string(data) != "original" || later || len(h.ran) != 0 {
				t.Fatalf("changed after quit: file=%q err=%v later=%v ran=%v", data, readErr, later, h.ran)
			}
			if got := log.find(protocol.EventStepAfter, "config-github"); len(got) != 1 || got[0].Outcome != protocol.OutQuit {
				t.Errorf("step outcome: %+v", got)
			}
			if end := log.entries[len(log.entries)-1]; end.Event != protocol.EventRunEnd || end.Outcome != protocol.RunQuit {
				t.Errorf("run outcome: %+v", end)
			}
		})
	}
}

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

func TestTerminalOutputIsNotStalledByABackgroundChild(t *testing.T) {
	var b bytes.Buffer
	tm := Terminal{Err: &b}
	start := time.Now()
	out, err := tm.Output(context.Background(), "sh", "-c", "sleep 30 & echo hi")
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Errorf("out %q err %v", out, err)
	}
	if d := time.Since(start); d > runWaitDelay+3*time.Second {
		t.Errorf("took %v", d)
	}
}
