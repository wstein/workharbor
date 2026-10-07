package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed" // the OpenAPI document
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/initiation"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
	"github.com/wstein/workharbor/internal/version"
)

//go:embed openapi.json
var openAPI []byte

// OpenAPI returns the OpenAPI 3.1 document that is this API's contract.
func OpenAPI() []byte { return append([]byte(nil), openAPI...) }

// Backend is what the API needs from the service layer. *service.Service and
// *service.Workspaces together provide it (see NewBackend); tests use a fake.
type Backend interface {
	List(ctx context.Context, onlyActive bool) ([]store.TaskSummary, error)
	Show(ctx context.Context, task domain.ID) (service.TaskView, error)
	Run(ctx context.Context, req service.RunRequest) (service.RunResult, error)
	Say(ctx context.Context, task domain.ID, message string) (agent.Delivery, error)
	Cancel(ctx context.Context, task domain.ID) error
	Pause(ctx context.Context, task domain.ID) error
	Resume(ctx context.Context, task domain.ID) (domain.ID, error)
	TranscriptSize(ctx context.Context, task domain.ID) (service.TranscriptSize, error)
	PurgeTranscript(ctx context.Context, task domain.ID, actor string) (store.PurgeResult, error)
	KillAll(ctx context.Context, actor string) (service.KillReport, error)
	Usage(ctx context.Context, q service.UsageQuery) (service.UsageReport, error)
	Answer(ctx context.Context, id domain.ID, r domain.Response) (newRun domain.ID, err error)
	Inbox(ctx context.Context) ([]domain.Decision, error)
	WorkspaceList(ctx context.Context) ([]service.WorkspaceView, error)
	CreateWorkspace(ctx context.Context, req service.CreateRequest) (domain.Workspace, domain.Agent, error)
	RemoveWorkspace(ctx context.Context, workspace string) error
	RebuildWorkspace(ctx context.Context, workspace, actor string) (service.RebuildResult, error)
	AddAgent(ctx context.Context, workspace, role, instructions, profile string) (domain.Agent, error)
	RemoveAgent(ctx context.Context, workspace, role string) error
	OpenCopy(ctx context.Context, workspace, role string) (service.EditorCopy, error)
	Subscribe(ctx context.Context, task domain.ID, since int64) (<-chan domain.Event, error)
	Log(ctx context.Context, task domain.ID, since int64, limit int) ([]domain.Event, error)
}

type backend struct {
	*service.Service
	*service.Workspaces
	consoles *service.Consoles
}

// The console operations, under the API's names. They answer with a conflict
// when the supervisor was built without a console.
func (b backend) ConsoleOpen(ctx context.Context, rw []string) (service.ConsoleInfo, error) {
	if b.consoles == nil {
		return service.ConsoleInfo{}, errNoConsole
	}
	return b.consoles.Open(ctx, rw)
}

func (b backend) ConsoleStatus(ctx context.Context) (*service.ConsoleInfo, error) {
	if b.consoles == nil {
		return nil, nil
	}
	return b.consoles.Status(ctx)
}

func (b backend) ConsoleClose(ctx context.Context) error {
	if b.consoles == nil {
		return errNoConsole
	}
	return b.consoles.Close(ctx)
}

func (b backend) ConsoleShell(ctx context.Context, req service.ShellRequest) (runtime.Terminal, error) {
	if b.consoles == nil {
		return nil, errNoConsole
	}
	return b.consoles.Shell(ctx, req)
}

func (b backend) ConsoleSSHCertificate(ctx context.Context, req service.SSHRequest) (service.SSHCertificate, error) {
	if b.consoles == nil {
		return service.SSHCertificate{}, errNoConsole
	}
	return b.consoles.SSHCertificate(ctx, req)
}

func (b backend) ConsoleSSH(ctx context.Context, actor string) (service.SSHConn, error) {
	if b.consoles == nil {
		return nil, errNoConsole
	}
	return b.consoles.SSH(ctx, actor)
}

// CreateWorkspace, RemoveWorkspace and the agent methods give the API's names to
// the workspace operations.
func (b backend) CreateWorkspace(ctx context.Context, req service.CreateRequest) (domain.Workspace, domain.Agent, error) {
	return b.Create(ctx, req)
}

func (b backend) RemoveWorkspace(ctx context.Context, workspace string) error {
	return b.Remove(ctx, workspace)
}

func (b backend) RebuildWorkspace(ctx context.Context, workspace, actor string) (service.RebuildResult, error) {
	return b.Rebuild(ctx, workspace, actor)
}

// NewBackend joins the service, the workspace operations and the console (nil
// when there is none) into a Backend.
func NewBackend(s *service.Service, w *service.Workspaces, c *service.Consoles) Backend {
	return backend{Service: s, Workspaces: w, consoles: c}
}

// Options configures a Server.
type Options struct {
	// Token is the API token. It is required and never logged or echoed.
	Token []byte
	// Store holds the idempotency keys.
	Store *store.Store
	// Heartbeat is the interval of the event stream's keep-alive comments.
	// Default 15 s.
	Heartbeat time.Duration
	// Now is the clock for the time an answer arrives. Default time.Now.
	Now func() time.Time
	// OnError hears an internal error, which the client only sees as a generic
	// message. Optional.
	OnError func(error)
	// Passkeys serves the host's passkey commands (D45). Nil leaves them off.
	Passkeys Passkeys
	// Previews serves the preview commands (D33). Nil leaves them off.
	Previews Previews
}

// Server is the API. Its Handler is the whole thing.
type Server struct {
	be    Backend
	opt   Options
	token [sha256.Size]byte

	mu    sync.Mutex
	locks map[string]*keyLock // idempotency keys in flight
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

// New returns a server. It refuses an empty token.
func New(be Backend, opt Options) (*Server, error) {
	if len(opt.Token) == 0 {
		return nil, errors.New("api: a token is required")
	}
	if opt.Store == nil {
		return nil, errors.New("api: a store is required for idempotency keys")
	}
	if opt.Heartbeat <= 0 {
		opt.Heartbeat = 15 * time.Second
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	s := &Server{be: be, opt: opt, locks: map[string]*keyLock{}}
	s.token = sha256.Sum256(opt.Token)
	s.opt.Token = nil // the digest is all that is kept
	return s, nil
}

// TokenFromConfig reads the API token with config.ReadSecret and trims the
// line ending the file may have.
func TokenFromConfig(c *config.Config) ([]byte, error) {
	raw, err := config.ReadSecret(c.APITokenFile)
	if err != nil {
		return nil, err
	}
	tok := []byte(strings.TrimSpace(string(raw)))
	if len(tok) == 0 {
		return nil, errors.New("api: the token file is empty")
	}
	return tok, nil
}

type route struct {
	method, path string
	handle       func(*Server, http.ResponseWriter, *http.Request)
}

// routes is the table the handler is built from. The test compares it with
// openapi.json.
var routes = []route{
	{http.MethodGet, "/v1/health", (*Server).health},
	{http.MethodGet, "/v1/openapi.json", (*Server).openapi},
	{http.MethodGet, "/v1/tasks", (*Server).listTasks},
	{http.MethodPost, "/v1/tasks", (*Server).runTask},
	{http.MethodGet, "/v1/tasks/{task}", (*Server).showTask},
	{http.MethodPost, "/v1/tasks/{task}/say", (*Server).say},
	{http.MethodPost, "/v1/tasks/{task}/cancel", (*Server).cancel},
	{http.MethodPost, "/v1/tasks/{task}/pause", (*Server).pause},
	{http.MethodPost, "/v1/tasks/{task}/resume", (*Server).resume},
	{http.MethodGet, "/v1/tasks/{task}/transcript", (*Server).transcriptSize},
	{http.MethodPost, "/v1/tasks/{task}/purge", (*Server).purge},
	{http.MethodPost, "/v1/kill-all", (*Server).killAll},
	{http.MethodGet, "/v1/console", (*Server).consoleStatus},
	{http.MethodPost, "/v1/console", (*Server).consoleOpen},
	{http.MethodDelete, "/v1/console", (*Server).consoleClose},
	{http.MethodGet, "/v1/console/shell", (*Server).consoleShell},
	{http.MethodPost, "/v1/console/ssh/certificate", (*Server).consoleSSHCertificate},
	{http.MethodGet, "/v1/console/ssh", (*Server).consoleSSH},
	{http.MethodGet, "/v1/usage", (*Server).usage},
	{http.MethodGet, "/v1/usage/summary", (*Server).usageSummary},
	{http.MethodGet, "/v1/tasks/{task}/events", (*Server).events},
	{http.MethodGet, "/v1/tasks/{task}/log", (*Server).log},
	{http.MethodGet, "/v1/inbox", (*Server).inbox},
	{http.MethodPost, "/v1/decisions/{decision}/answer", (*Server).answer},
	{http.MethodGet, "/v1/workspaces", (*Server).workspaces},
	{http.MethodPost, "/v1/workspaces", (*Server).createWorkspace},
	{http.MethodDelete, "/v1/workspaces/{workspace}", (*Server).removeWorkspace},
	{http.MethodPost, "/v1/workspaces/{workspace}/agents", (*Server).addAgent},
	{http.MethodDelete, "/v1/workspaces/{workspace}/agents/{role}", (*Server).removeAgent},
	{http.MethodPost, "/v1/workspaces/{workspace}/open", (*Server).openCopy},
	{http.MethodPost, "/v1/workspaces/{workspace}/rebuild", (*Server).rebuildWorkspace},
	{http.MethodPost, "/v1/workspaces/{workspace}/shell", (*Server).workspaceShell},
	{http.MethodGet, "/v1/previews", (*Server).listPreviews},
	{http.MethodPost, "/v1/tasks/{task}/previews", (*Server).openPreview},
	{http.MethodPost, "/v1/previews/{preview}/link", (*Server).previewLink},
	{http.MethodDelete, "/v1/previews/{preview}", (*Server).closePreview},
	{http.MethodGet, "/v1/passkeys", (*Server).listPasskeys},
	{http.MethodPost, "/v1/passkeys/enrolments", (*Server).newEnrolment},
	{http.MethodDelete, "/v1/passkeys/{passkey}", (*Server).revokePasskey},
}

// Routes returns the "METHOD path" of every route, for the contract test.
func Routes() []string {
	out := make([]string, len(routes))
	for i, r := range routes {
		out[i] = r.method + " " + r.path
	}
	return out
}

// Handler returns the API as an http.Handler. Every request, whatever its path,
// needs the token first, so an unauthenticated caller learns nothing about which
// routes exist.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	anyMethod := http.NewServeMux() // to tell a wrong method from an unknown path
	paths := map[string]bool{}
	for _, r := range routes {
		mux.HandleFunc(r.method+" "+r.path, func(w http.ResponseWriter, req *http.Request) { r.handle(s, w, req) })
		if !paths[r.path] {
			paths[r.path] = true
			anyMethod.HandleFunc(r.path, func(http.ResponseWriter, *http.Request) {})
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, authError{})
			return
		}
		if _, pattern := mux.Handler(r); pattern == "" {
			if _, p := anyMethod.Handler(r); p != "" {
				writeJSON(w, http.StatusMethodNotAllowed, Envelope{Error: &ErrorBody{Code: "method_not_allowed", ExitCode: 2, Message: "method not allowed"}})
				return
			}
			writeError(w, &domain.NotFoundError{Kind: "route", ID: r.URL.Path})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// authorized checks the bearer token in constant time. Both sides are hashed
// first, so neither the value nor its length is compared byte by byte.
func (s *Server) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		return false
	}
	got := sha256.Sum256([]byte(tok))
	return subtle.ConstantTimeCompare(got[:], s.token[:]) == 1
}

// Listen binds addr, which must be a loopback address (D29): a guest reaches
// every other address of the host. It checks the address it actually got, so a
// name that resolves elsewhere is caught too.
func Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("api: %q is not host:port", addr)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("api: %q is not a loopback address: a guest reaches every other address of the host (D29)", host)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, err
	}
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("api: bound %s, which is not a loopback address", ln.Addr())
	}
	return ln, nil
}

// Serve serves the API on ln until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	return s.ServeHandler(ctx, ln, s.Handler())
}

// ServeHandler serves h on ln until ctx ends, with the server's timeouts and its
// orderly shutdown. `whr serve` uses it to put the web UI next to the API on one
// listener (design §9.3).
func (s *Server) ServeHandler(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return ctx.Err()
	}
}

// ---- request helpers ----

const maxBody = 1 << 20

// decodeStrict reads one JSON value: an unknown field is an error, and there
// is nothing after it.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return usageError{"the request body is not valid: " + err.Error()}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return usageError{"the request body has more than one JSON value"}
	}
	return nil
}

// idParam returns a path parameter as an ID: opaque, but bounded and without
// control characters.
func idParam(r *http.Request, name string) (domain.ID, error) {
	v := r.PathValue(name)
	if v == "" || len(v) > 200 || strings.ContainsFunc(v, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return "", usageError{"the " + name + " in the path is not valid"}
	}
	return domain.ID(v), nil
}

func (s *Server) internal(err error) {
	if s.opt.OnError != nil {
		s.opt.OnError(err)
	}
}

// ---- handlers ----

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeOK(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
}

func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(openAPI)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	active := r.URL.Query().Get("active") == "true"
	list, err := s.be.List(r.Context(), active)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, summaries(list))
}

func (s *Server) showTask(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.be.Show(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := taskOf(v)
	// The usage line is part of the card; a failure to read it leaves it out.
	if rep, uerr := s.be.Usage(r.Context(), service.UsageQuery{TaskID: id, Group: store.GroupTask}); uerr == nil {
		out.UsageLine = service.FormatUsageLine(rep)
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	in, err := s.be.Inbox(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]decisionView, 0, len(in))
	for _, d := range in {
		out = append(out, decisionOf(d))
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.be.WorkspaceList(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, workspacesOf(list))
}

type runBody struct {
	IssueURL string `json:"issue_url"`
	Agent    string `json:"agent"`
	Prompt   string `json:"prompt,omitempty"`
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	var body runBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		res, err := s.be.Run(initiation.With(r.Context(), initiation.UserAction("api", "api")), service.RunRequest{IssueURL: body.IssueURL, Agent: body.Agent, Prompt: body.Prompt})
		if err != nil {
			return 0, nil, err
		}
		if res.Held { // an issue by an untrusted author: nothing started, a question waits (202)
			return http.StatusAccepted, map[string]any{"task_id": string(res.Task), "held": true, "decision_id": string(res.Decision)}, nil
		}
		return http.StatusCreated, map[string]any{"task_id": string(res.Task), "run_id": string(res.Run), "held": false}, nil
	})
}

type sayBody struct {
	Message string `json:"message"`
}

func (s *Server) say(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	var body sayBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		writeError(w, usageError{"message must not be empty"})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		d, err := s.be.Say(initiation.With(r.Context(), initiation.UserAction("api", "api")), id, body.Message)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{"delivery": string(d)}, nil
	})
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := s.be.Cancel(r.Context(), id); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := s.be.Pause(r.Context(), id); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		run, err := s.be.Resume(initiation.With(r.Context(), initiation.UserAction("api", "api")), id)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{"run_id": string(run)}, nil
	})
}

// transcriptSize says what a purge would delete, for the confirmation.
func (s *Server) transcriptSize(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	size, err := s.be.TranscriptSize(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, map[string]any{"events": size.Events, "bytes": size.Bytes})
}

type purgeBody struct {
	Confirm bool `json:"confirm"`
}

// purge deletes a task's transcript content (design §5.4). It keeps the audit
// entries, usage rows and Decisions, and is refused without a confirmation.
func (s *Server) purge(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	var body purgeBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if !body.Confirm {
		writeError(w, usageError{`purge needs {"confirm": true}`})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		res, err := s.be.PurgeTranscript(r.Context(), id, "api")
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]any{"events": res.Events, "bytes": res.Bytes, "digest": res.Digest}, nil
	})
}

// usage is the report behind `whr usage` (design §5.7): the totals of what the
// agents reported, grouped, with the account's usage windows and balance.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := service.UsageQuery{Repo: q.Get("repo"), Group: store.UsageGroup(q.Get("by"))}
	if t := q.Get("task"); t != "" {
		if len(t) > 200 || strings.ContainsFunc(t, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
			writeError(w, usageError{"the task is not valid"})
			return
		}
		query.TaskID = domain.ID(t)
	}
	for _, f := range []struct {
		name string
		to   *time.Time
	}{{"since", &query.Since}, {"until", &query.Until}} {
		if v := q.Get(f.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeError(w, usageError{f.name + " must be a time like 2026-10-01T00:00:00Z"})
				return
			}
			*f.to = t
		}
	}
	switch query.Group {
	case "", store.GroupAll, store.GroupRun, store.GroupTask, store.GroupRepo, store.GroupDay, store.GroupMonth, store.GroupAgent, store.GroupModel:
	default:
		writeError(w, usageError{"by must be all, run, task, repo, agent, model, day or month"})
		return
	}
	rep, err := s.be.Usage(r.Context(), query)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, rep)
}

// UsageSummarizer is what the dashboard's usage card needs of the backend (issue
// #111): the same numbers as Usage, for a period, in total, by agent and by model.
type UsageSummarizer interface {
	UsageSummary(ctx context.Context, period string) (service.UsageSummary, error)
}

// LimitsReporter is what the dashboard's limits panel needs of the backend
// (issue #172): each provider's limits as its adapter reported them.
type LimitsReporter interface {
	Limits(ctx context.Context) ([]service.ProviderLimits, error)
}

// usageSummary serves the card of a period: today, 7d, 30d or all (default 7d), in the
// supervisor's time zone.
func (s *Server) usageSummary(w http.ResponseWriter, r *http.Request) {
	sum, ok := s.be.(UsageSummarizer)
	if !ok {
		writeError(w, domain.NewConflict("usage_summary_off", "this backend has no usage summary"))
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = service.Period7Days
	}
	rep, err := sum.UsageSummary(r.Context(), period)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, rep)
}

type killAllBody struct {
	Confirm bool `json:"confirm"`
}

// killAll is the kill switch: it stops every run, cancels every unfinished task
// and revokes the forge tokens (design §7.7). It is refused without an explicit
// confirmation, so a stray request cannot pull it.
func (s *Server) killAll(w http.ResponseWriter, r *http.Request) {
	var body killAllBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if !body.Confirm {
		writeError(w, usageError{`kill-all needs {"confirm": true}`})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		rep, err := s.be.KillAll(r.Context(), "api")
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, rep, nil
	})
}

type answerBody struct {
	Option string `json:"option"`
	Reason string `json:"reason,omitempty"`
	SHA    string `json:"sha,omitempty"` // the commit the human was shown, for a review Decision
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "decision")
	if err != nil {
		writeError(w, err)
		return
	}
	var body answerBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if body.Option == "" {
		writeError(w, usageError{"option must not be empty"})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		run, err := s.be.Answer(initiation.With(r.Context(), initiation.UserAction("api", "api")), id, domain.Response{By: "api", Option: body.Option, Reason: body.Reason, SHA: body.SHA, At: s.opt.Now()})
		if err != nil {
			return 0, nil, err
		}
		out := map[string]string{}
		if run != "" {
			out["new_run_id"] = string(run)
		}
		return http.StatusOK, out, nil
	})
}

// readBody reads a JSON body, bounded, and decodes it into v. It returns the
// bytes for the idempotency hash.
func readBody(w http.ResponseWriter, r *http.Request, v any) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		return nil, usageError{"the request body could not be read: " + err.Error()}
	}
	if err := decodeStrict(raw, v); err != nil {
		return nil, err
	}
	return raw, nil
}

// fail writes an error; one that is not a coded error is reported to OnError
// first, because the client only sees a generic message.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var c interface{ ExitCode() int }
	if !errors.As(err, &c) {
		s.internal(err)
	}
	writeError(w, err)
}

type createWorkspaceBody struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Repo         string `json:"repo"`
	Integration  string `json:"integration,omitempty"` // default: the repository's publication target
	Source       string `json:"source,omitempty"`      // a path or an https URL; default the forge's URL of repo
	Role         string `json:"role"`
	Instructions string `json:"instructions,omitempty"`
	Profile      string `json:"profile,omitempty"`
}

// createWorkspace creates a workspace and its first agent. It takes a while (the
// clone, the environment, the worktree), and the request context ends it: a
// client that hangs up takes back what was made.
func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var b createWorkspaceBody
	raw, err := readBody(w, r, &b)
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		ws, ag, err := s.be.CreateWorkspace(r.Context(), service.CreateRequest{
			Name: b.Name, Path: b.Path, Repo: b.Repo, Integration: b.Integration, Source: b.Source,
			Role: b.Role, Instructions: b.Instructions, Profile: b.Profile,
		})
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, workspacesOf([]service.WorkspaceView{{Workspace: ws, Agents: []domain.Agent{ag}}})[0], nil
	})
}

func (s *Server) removeWorkspace(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := s.be.RemoveWorkspace(r.Context(), string(name)); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}

type addAgentBody struct {
	Role         string `json:"role"`
	Instructions string `json:"instructions,omitempty"`
	Profile      string `json:"profile,omitempty"`
}

func (s *Server) addAgent(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	var b addAgentBody
	raw, err := readBody(w, r, &b)
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		a, err := s.be.AddAgent(r.Context(), string(name), b.Role, b.Instructions, b.Profile)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, agentView{ID: string(a.ID), Role: a.Role, Branch: a.Branch}, nil
	})
}

func (s *Server) removeAgent(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	role, err := idParam(r, "role")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := s.be.RemoveAgent(r.Context(), string(name), string(role)); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}

// rebuildWorkspace replaces the workspace's environment with one made from the
// image its repository resolves to now (issue #128). It takes minutes when the
// image has to be built, and is refused while a run of the workspace is live.
func (s *Server) rebuildWorkspace(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	s.idempotent(w, r, nil, func() (int, any, error) {
		res, err := s.be.RebuildWorkspace(r.Context(), string(name), "api")
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, rebuildView(res), nil
	})
}

type openCopyBody struct {
	Role string `json:"role,omitempty"`
}

// openCopy makes the supervisor-owned editor copy of an agent's branch (design
// §4.5). The body is optional: without a role it is the workspace's only agent.
func (s *Server) openCopy(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	var b openCopyBody
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, usageError{"the request body could not be read: " + err.Error()})
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := decodeStrict(raw, &b); err != nil {
			writeError(w, err)
			return
		}
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		c, err := s.be.OpenCopy(r.Context(), string(name), b.Role)
		if err != nil {
			return 0, nil, err
		}
		warnings := c.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		return http.StatusOK, editorCopyView{Path: c.Path, Warnings: warnings}, nil
	})
}

// ShellBackend is what the agent shell route needs. A backend without it answers
// with a conflict.
type ShellBackend interface {
	OpenShell(ctx context.Context, workspace, actor string) (service.ShellSession, error)
}

// workspaceShell answers where a shell in a workspace's environment is opened and
// with which variables. It carries no terminal: the `whr` process of the human
// runs the runtime's interactive exec itself (design §7.3, "The sign-in shell").
func (s *Server) workspaceShell(w http.ResponseWriter, r *http.Request) {
	name, err := idParam(r, "workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	sb, ok := s.be.(ShellBackend)
	if !ok {
		writeError(w, domain.NewConflict(domain.RuleEnvRunning, "this supervisor has no agent shell"))
		return
	}
	if r.Header.Get("Accept") != "application/x-ndjson" {
		writeError(w, usageError{"the sign-in shell needs Accept: application/x-ndjson and a connection held until its child exits"})
		return
	}
	session, err := sb.OpenShell(r.Context(), string(name), "api")
	if err != nil {
		s.fail(w, err)
		return
	}
	defer session.Release()
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(Envelope{SchemaVersion: SchemaVersion, OK: true, Data: shellView(session.Target)}); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-session.Done:
			return
		case <-tick.C:
			// Metadata-only liveness; no terminal stream is accepted or sent.
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			if _, err := io.WriteString(w, "{\"held\":true}\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}
