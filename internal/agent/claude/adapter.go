package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/runtime"
)

// Runner is what the adapter needs of a runtime: Exec with stdin. Every
// runtime.Adapter is one.
type Runner interface {
	Exec(ctx context.Context, envID string, req runtime.ExecRequest) (runtime.ExecStream, error)
}

// Config is what the adapter needs to know about the installation.
type Config struct {
	Bin       string // the claude binary in the environment; default "claude"
	Model     string // optional --model
	ConfigDir string // a per-environment auth directory, passed as CLAUDE_CONFIG_DIR, never $HOME
	// SettingSources pins where the CLI may read settings from, so that the
	// checked-out repository cannot widen the allowlist with its own permission
	// rules or hooks. Default "user". The flag is unverified against the real
	// CLI (design §5.2); a CLI that rejects it fails the run, it does not widen.
	SettingSources string
	Env            []string // extra KEY=VALUE for the process
	// ResumeProbe is how long Resume waits to learn whether the session
	// exists. Default 10 s.
	ResumeProbe time.Duration
}

// Adapter is the Claude Code agent adapter. Until the approval route of spike
// #7 exists it reports no host approvals, so it runs in the degraded mode
// (design §5.2): mid-run messages work, permission prompts do not reach the
// host, and only an allowlist is permitted.
type Adapter struct {
	r   Runner
	cfg Config
	now func() time.Time
}

// New returns an adapter that runs the CLI through r.
func New(r Runner, cfg Config) *Adapter {
	if cfg.Bin == "" {
		cfg.Bin = "claude"
	}
	if cfg.SettingSources == "" {
		cfg.SettingSources = "user"
	}
	if cfg.ResumeProbe <= 0 {
		cfg.ResumeProbe = 10 * time.Second
	}
	return &Adapter{r: r, cfg: cfg, now: time.Now}
}

// Name implements agent.Adapter.
func (a *Adapter) Name() string { return "claude-code" }

// Capabilities implements agent.Adapter.
func (a *Adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		ContractVersion:   agent.ContractVersion,
		Headless:          true,
		StructuredEvents:  true,
		MidRunInstruction: true,
		HostApprovals:     false, // needs spike #7
		SessionResume:     true,
		AwaitingGuidance:  false,
		ReportsQuota:      true, // unverified rule, design §5.2
		ReportsUsage:      true,
		AuthModes:         []agent.AuthMode{agent.AuthSubscription, agent.AuthAPIKey},
	}
}

// Start implements agent.Adapter.
func (a *Adapter) Start(ctx context.Context, spec agent.StartSpec) (agent.Session, error) {
	return a.launch(ctx, spec, "")
}

// Resume implements agent.Adapter. The prompt of the spec is the next message.
// An unknown session is recognized by an error result before any init that is
// not a login failure; that is unverified (design §5.2).
func (a *Adapter) Resume(ctx context.Context, spec agent.StartSpec, sessionID string) (agent.Session, error) {
	if sessionID == "" {
		return nil, agent.ErrNoSession
	}
	return a.launch(ctx, spec, sessionID)
}

func (a *Adapter) args(spec agent.StartSpec, resume string) []string {
	args := []string{
		a.cfg.Bin, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-prompts", "none", "--permission-mode", string(agent.PermissionDontAsk),
		"--setting-sources", a.cfg.SettingSources,
	}
	if len(spec.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(spec.AllowedTools, ","))
	}
	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return args
}

func (a *Adapter) launch(ctx context.Context, spec agent.StartSpec, resume string) (agent.Session, error) {
	if err := a.Capabilities().CheckSpec(spec); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return nil, errors.Join(agent.ErrBadSpec, errors.New("a session starts with its first message, so the prompt is needed"))
	}

	pctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	env := append([]string(nil), a.cfg.Env...)
	if a.cfg.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+a.cfg.ConfigDir)
	}
	st, err := a.r.Exec(pctx, spec.EnvID, runtime.ExecRequest{Cmd: a.args(spec, resume), Env: env, Dir: spec.Workdir, Stdin: pr})
	if err != nil {
		cancel()
		_ = pw.Close()
		return nil, err
	}

	s := &session{
		cancel: cancel, stdin: pw, events: make(chan agent.Event, 256), done: make(chan struct{}),
		inited: make(chan struct{}), p: newParser(a.now, true, spec.AllowedTools), resume: resume != "",
	}
	go s.run(pctx, st)
	// The prompt is the first message; the CLI reports its session ID only after it.
	go func() { _ = s.send(spec.Prompt) }()

	if resume != "" {
		if err := s.probe(a.cfg.ResumeProbe); err != nil {
			return nil, err
		}
	}
	return s, nil
}

type session struct {
	cancel context.CancelFunc
	stdin  *io.PipeWriter
	events chan agent.Event
	done   chan struct{}
	inited chan struct{} // closed when the session event arrives
	resume bool
	p      *parser // used by run, and by probe after ready

	wmu         sync.Mutex  // one message at a time on stdin
	stdinClosed atomic.Bool // set before the pipe is closed

	mu       sync.Mutex // guards the fields below
	id       string
	stopped  bool
	result   agent.Result
	unknown  bool  // a resume that found no such session
	earlyErr error // a resume that failed before init for another reason
	initOnce sync.Once
}

func (s *session) markInited() { s.initOnce.Do(func() { close(s.inited) }) }

// send writes one user message as a stream-json line.
func (s *session) send(text string) error {
	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}},
	})
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.stdinClosed.Load() {
		return agent.ErrNotRunning
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		return agent.ErrNotRunning
	}
	return nil
}

// closeStdin ends the input so that the process exits after its result.
// It does not wait for a write in progress: closing the pipe ends it.
func (s *session) closeStdin() {
	s.stdinClosed.Store(true)
	_ = s.stdin.Close()
}

func (s *session) emit(e agent.Event) { s.events <- e }

func (s *session) run(ctx context.Context, st runtime.ExecStream) {
	defer close(s.done)
	defer close(s.events)
	defer s.cancel()

	var stderr []byte
	var lines int
	for c := range st.Chunks() {
		switch c.Stream {
		case runtime.Stdout:
			for _, e := range s.p.feed(c.Data) {
				s.observe(e)
				s.emit(e)
			}
			if s.p.result != nil {
				s.closeStdin()
			}
		case runtime.Stderr:
			stderr = append(stderr, c.Data...)
			stderr = s.flushStderr(stderr, &lines)
		}
	}
	code, err := st.Wait()
	s.closeStdin()

	id := s.p.sessionID
	res, ok := s.p.outcome()
	s.mu.Lock()
	stopped := s.stopped || ctx.Err() != nil
	s.mu.Unlock()

	var final agent.Result
	var early error
	unknown := false
	switch {
	case ok:
		// A result that arrived is the outcome, even if Stop came after it.
		final = res
		if s.resume && !s.p.sawInit && !s.p.authFailed && s.p.result.IsError {
			if missingSession(s.p.result.Result) {
				unknown = true
			} else {
				early = fmt.Errorf("claude: resume failed: %s", capText(s.p.result.Result))
			}
		}
	case stopped:
		final = agent.Result{Status: agent.ResultStopped, SessionID: id}
	default:
		text := "the agent ended without a result"
		if err != nil || code != 0 {
			text = "the agent exited with code " + itoa(code)
		}
		s.emit(s.p.errorEvent(text))
		final = agent.Result{Status: agent.ResultFailed, SessionID: id, Text: text}
	}
	s.mu.Lock()
	s.result, s.unknown, s.earlyErr = final, unknown, early
	s.mu.Unlock()
}

// observe records the session ID the moment its event arrives.
func (s *session) observe(e agent.Event) {
	if e.Kind == agent.EventSession {
		s.mu.Lock()
		s.id = e.SessionID
		s.mu.Unlock()
		s.markInited()
	}
}

// Limits on stderr: it is untrusted and must not grow without bound.
const (
	maxStderrLines = 200
	maxStderrLine  = 64 << 10 // bytes kept of one line, and of an unfinished one
)

// flushStderr turns each complete stderr line into an error event and returns
// the incomplete rest. At most maxStderrLines lines become events per session,
// the rest are dropped after one note, and no line or unfinished line keeps
// more than maxStderrLine bytes.
func (s *session) flushStderr(buf []byte, lines *int) []byte {
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			if len(buf) > maxStderrLine {
				buf = buf[:maxStderrLine] // an unfinished line is kept to its cap
			}
			return buf
		}
		line := buf[:i]
		if len(line) > maxStderrLine {
			line = line[:maxStderrLine]
		}
		if text := strings.TrimSpace(string(line)); text != "" {
			*lines++
			switch {
			case *lines < maxStderrLines:
				s.emit(s.p.errorEvent(text))
			case *lines == maxStderrLines:
				s.emit(s.p.errorEvent("stderr has more lines than are kept; the rest are dropped"))
			}
		}
		buf = buf[i+1:]
	}
}

// probe waits until a resumed session says whether it exists: the session
// event means it does, and an end before one means it may not.
func (s *session) probe(limit time.Duration) error {
	select {
	case <-s.inited:
		return nil
	case <-s.done:
	case <-time.After(limit):
		return nil // no verdict: go on as if it exists
	}
	s.mu.Lock()
	unknown, early := s.unknown, s.earlyErr
	s.mu.Unlock()
	if unknown {
		return agent.ErrNoSession
	}
	return early
}

// missingSession reports whether an error result says the session does not
// exist. What the CLI really prints is unverified, so this is a heuristic on
// the wording; any other failure is an ordinary error.
func missingSession(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "no conversation") ||
		(strings.Contains(t, "session") && (strings.Contains(t, "not found") || strings.Contains(t, "no such") || strings.Contains(t, "does not exist")))
}

func (s *session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *session) Events() <-chan agent.Event { return s.events }

// Instruct writes a user message into the running process, which picks it up
// at its next model step, after the running tool (spike #1).
func (s *session) Instruct(_ context.Context, message string) (agent.Delivery, error) {
	select {
	case <-s.done:
		return "", agent.ErrNotRunning
	default:
	}
	if err := s.send(message); err != nil {
		return "", err
	}
	return agent.DeliveryNextTurn, nil
}

// Stop cancels the Exec, which the runtime contract requires to end the
// process in the guest (design §5.1). The session stays resumable by its ID.
func (s *session) Stop(context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.cancel()
	return nil
}

func (s *session) Wait() (agent.Result, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
