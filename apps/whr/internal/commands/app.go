// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/version"
)

// Env is everything a run reads from its surroundings, so tests can fix it.
type Env struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	InputTerminal  bool
	// Getenv reads an environment variable (NO_COLOR, TERM).
	Getenv func(string) string
	// IsTerminal reports whether a stream is an interactive terminal.
	IsTerminal func(io.Writer) bool
}

// Call is what a handler receives.
type Call struct {
	Entry Entry
	// Args are the positional arguments after the command words.
	Args []string
}

// Result is a successful outcome: Value is the --json "result" object and
// Human the text for standard output in human mode (escape untrusted text).
type Result struct {
	Value any
	Human string
}

// Handler implements one command. A command without a handler is registered
// but unavailable and refuses with a usage error before any I/O. Return an
// *exitcode.Error to choose the exit code; any other error is an internal
// failure, except context cancellation and deadline which map to 72 and 71.
type Handler func(ctx context.Context, call Call) (Result, error)

// DefaultHandlers returns the handlers of the commands implemented so far.
// `help` and `completion` are built in. Later work items add their commands
// here.
func DefaultHandlers() map[string]Handler {
	return map[string]Handler{
		"version":            versionHandler,
		"host.doctor":        DoctorHandler(doctor.SystemPorts(nil)),
		"workspace.validate": validateHandler,
		"workspace.ls": LocalLsHandler(func(ctx context.Context) ([]localdocker.Workspace, error) {
			return (localdocker.Client{}).Discover(ctx, doctor.SystemPorts(nil))
		}),
		"workspace.init": initHandler,
	}
}

func versionHandler(_ context.Context, call Call) (Result, error) {
	if len(call.Args) > 0 {
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "version takes no arguments")
	}
	v := version.Current()
	return Result{Value: v, Human: v.Human()}, nil
}

type app struct {
	env      Env
	handlers map[string]Handler
	builtin  map[string]Handler
	root     cobraRoot

	jsonFlag, noColor bool
	jsonScanned       bool
	colorFlag         string

	w    *output.Writer
	exit exitcode.Code
	done bool
}

// Execute runs whr with args (without the program name) and returns the exit
// status. It never exits the process and writes only to env.
func Execute(ctx context.Context, args []string, env Env, handlers map[string]Handler) int {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.IsTerminal == nil {
		env.IsTerminal = func(io.Writer) bool { return false }
	}
	a := &app{env: env, handlers: handlers, jsonScanned: scanJSON(args), colorFlag: string(output.ColorAuto)}
	a.build()
	a.root.SetArgs(args)
	c, err := a.root.ExecuteContextC(ctx)
	if err != nil {
		id := "unknown"
		if c != nil {
			if v := c.Annotations[annotationID]; v != "" {
				id = v
			}
		}
		var ee *exitcode.Error
		if !errors.As(err, &ee) {
			ee = exitcode.Errorf(exitcode.PathBadArguments, "%v", err)
		}
		a.exit = a.writer().Failure(id, ee)
	}
	return int(a.exit)
}

// scanJSON finds --json before the arguments are parsed, so that even a flag
// error is reported as an envelope when the caller asked for JSON.
func scanJSON(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--json" || a == "--json=true" || a == "--json=1" || a == "--json=t" || a == "--json=T" || a == "--json=TRUE" || a == "--json=True" {
			return true
		}
	}
	return false
}

func (a *app) jsonMode() bool { return a.jsonFlag || a.jsonScanned }

func (a *app) writer() *output.Writer {
	if a.w == nil {
		mode, err := output.ParseColorMode(a.colorFlag)
		if err != nil {
			mode = output.ColorAuto
		}
		a.w = &output.Writer{
			Out: a.env.Stdout, Err: a.env.Stderr, JSON: a.jsonMode(),
			Color: output.UseColor(mode, a.noColor, a.env.Getenv, a.env.IsTerminal(a.env.Stderr)),
		}
	}
	return a.w
}

func (a *app) lookup(id string) Handler {
	if h, ok := a.builtin[id]; ok {
		return h
	}
	return a.handlers[id]
}

func (a *app) available(id string) bool { return a.lookup(id) != nil }

// run executes a table entry: it refuses unavailable commands and --json where
// the command does not support it, then calls the handler and reports.
func (a *app) run(ctx context.Context, e Entry, args []string) error {
	w := a.writer()
	h := a.lookup(e.ID)
	if h == nil {
		a.exit = w.Failure(e.ID, exitcode.Errorf(exitcode.PathUnavailableCommand,
			"%s is not available yet: it waits for %s", e.ID, WaitsFor(e.ID)))
		return nil
	}
	if e.JSON == "rejected" && a.jsonMode() {
		a.exit = w.Failure(e.ID, exitcode.Errorf(exitcode.PathUnsupportedJSON, "%s does not support --json", e.ID))
		return nil
	}
	res, err := callHandler(ctx, h, Call{Entry: e, Args: args})
	if err != nil {
		// A handler may fail with data to show, such as a doctor report
		// or a validate findings list.
		var hf *humanFailure
		if errors.As(err, &hf) && !a.jsonMode() {
			_, _ = io.WriteString(a.env.Stdout, hf.human)
		} else if res.Human != "" && !a.jsonMode() {
			_, _ = io.WriteString(a.env.Stdout, res.Human)
		}
		a.exit = w.Failure(e.ID, asError(err))
		return nil
	}
	if err := w.Success(e.ID, res.Value, res.Human); err != nil {
		a.exit = w.Failure(e.ID, exitcode.Errorf(exitcode.PathUnclassified, "writing the result failed: %v", err))
	}
	return nil
}

// callHandler contains unexpected handler panics before any result is written.
// It does not recover output failures or change the handler's side effects.
func callHandler(ctx context.Context, h Handler, call Call) (res Result, err error) {
	defer func() {
		if recover() != nil {
			res = Result{}
			err = exitcode.Errorf(exitcode.PathUnclassified, "command handler failed internally")
		}
	}()
	return h(ctx, call)
}

func asError(err error) *exitcode.Error {
	var ee *exitcode.Error
	if errors.As(err, &ee) {
		return ee
	}
	switch exitcode.FromError(err) {
	case exitcode.Timeout:
		return exitcode.Errorf(exitcode.PathDeadlineExceeded, "the deadline was exceeded")
	case exitcode.Interrupted:
		return exitcode.Errorf(exitcode.PathUserInterrupt, "interrupted")
	}
	return exitcode.Errorf(exitcode.PathUnclassified, "%s", strings.TrimSpace(err.Error()))
}
