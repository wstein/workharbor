// Package serve runs the supervisor: the service, its reconciler and the JSON
// API on a loopback address (design §5.3, §9.7). It is wiring: everything it
// does is in the packages it joins, so a test can run it on the fakes.
package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/api"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/notify"
	"github.com/wstein/workharbor/internal/passkey"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
	"github.com/wstein/workharbor/internal/web"
)

// Deps is everything Run needs, already built. `whr serve` builds the real ones
// (cmd/whr); tests pass fakes.
type Deps struct {
	Config  *config.Config
	Store   *store.Store
	Runtime runtime.Adapter
	Agent   agent.Adapter
	// Issues loads issues from the forge, and Forge is the whole adapter (the
	// publisher takes it behind a forge.Guard).
	Issues service.IssueSource
	Forge  forge.Adapter
	Git    *hostgit.Git
	// Owner is the owner label of every environment this supervisor makes.
	Owner string
	// Spec returns an environment's spec for a workspace, and Prepare is
	// runtime.Prepare with this host's options.
	// Topics and EditorDir make `whr open` work (optional).
	Topics    service.TopicsFunc
	EditorDir string
	Spec      func(domain.Workspace) runtime.Spec
	Prepare   func(runtime.Spec) (runtime.PreparedSpec, error)
	// ConsoleSpec returns the console's spec for the workspaces mounted
	// read-write, and ConsoleImage builds the console image on first use (D43).
	// Both are optional: without them there is no console.
	ConsoleSpec  func(rw []domain.Workspace) runtime.Spec
	ConsoleImage func(ctx context.Context) error
	// Environment reads a repository's environment from the supervisor's own
	// copy of its default branch (D38), for the egress requests a run's start
	// asks about. Optional.
	Environment func(ctx context.Context, repo, branch string) (service.RepoEnvironment, error)
	// AgentSpec returns how an agent is started for a run.
	AgentSpec func(domain.Task, domain.Run) agent.StartSpec
	Clock     service.Clock
	Logf      func(format string, args ...any)
	// ReconcileEvery is how often the reconciler compares the database with the
	// runtime. Default 30 s.
	ReconcileEvery time.Duration
	// AcceptWorkflowChange confirms that a repository's workflow preset in the
	// configuration may differ from the one recorded: a policy change, which the
	// operator confirms on the host CLI (`whr serve --accept-workflow-change`)
	// until the passkey of D45 exists.
	AcceptWorkflowChange bool
	// Ready, if set, is called with the address the API is listening on.
	Ready func(addr net.Addr)
}

// NewID returns a random ID with a short prefix.
func NewID() domain.ID {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("serve: no randomness: " + err.Error())
	}
	return domain.ID("x-" + hex.EncodeToString(b))
}

// Run starts the supervisor and blocks until ctx ends. It reconciles once
// before it accepts a request, so a restart resumes what was running (D6), then
// again every ReconcileEvery. On the way out it stops the sessions and leaves
// their runs resumable.
func Run(ctx context.Context, d Deps) error {
	logf := d.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	token, err := api.TokenFromConfig(d.Config)
	if err != nil {
		return err
	}
	scfg := service.Config{
		Owner:             d.Owner,
		PostCreateTimeout: d.Config.Environment.PostCreate(),
		Budgets:           Budgets(d.Config.Budgets),
		Spec:              d.AgentSpec,
		NewID:             NewID,
		OnError:           func(err error) { logf("background error: %v", err) },
	}
	addBoard(&scfg, d)
	addRevoker(&scfg, d)
	svc := service.New(d.Store, d.Runtime, d.Agent, d.Clock, scfg)
	defer svc.Shutdown()
	if err := applyWorkflows(ctx, d, logf); err != nil {
		return err
	}
	ws := service.NewWorkspaces(svc, service.WorkspaceConfig{
		Workflow: func(repo string) string {
			for _, r := range d.Config.Repositories {
				if strings.EqualFold(r.Name, repo) {
					return string(r.Preset())
				}
			}
			return ""
		},
		Config: d.Config, Git: d.Git, Spec: d.Spec, Prepare: d.Prepare, NewID: NewID, Issues: d.Issues,
		Topics: d.Topics, EditorDir: d.EditorDir, Environment: d.Environment,
	})
	be := api.NewBackend(svc, ws)
	auth, err := web.NewTokenAuth(token, nil)
	if err != nil {
		return err
	}
	// Passkeys (D45) are bound to whr's HTTPS name, so they need public_url.
	var keys *passkey.Service
	if origin, host := d.Config.PublicOrigin(); origin != "" {
		if keys, err = passkey.New(passkey.Config{RPID: host, Origin: origin, DisplayName: "workharbor", Now: d.Clock.Now}, d.Store); err != nil {
			return err
		}
	} else {
		logf("passkeys are off: set public_url to whr's https name to enrol one (D45)")
	}
	apiOpt := api.Options{Token: token, Store: d.Store, OnError: func(err error) { logf("api error: %v", err) }}
	webOpt := web.Options{Auth: auth, Store: d.Store, OnError: func(err error) { logf("web error: %v", err) }}
	if keys != nil { // a typed nil would look like a configured interface
		apiOpt.Passkeys, webOpt.Passkeys = keys, keys
	}
	srv, err := api.New(be, apiOpt)
	if err != nil {
		return err
	}
	// Bind before the first reconcile: a wrong address is the quickest failure.
	ln, err := api.Listen(d.Config.Listen)
	if err != nil {
		return err
	}
	rep, err := svc.Reconcile(ctx)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("reconcile at start: %w", err)
	}
	logf("reconciled: %d interrupted, %d resumed, %d failed", len(rep.Interrupted), len(rep.Resumed), len(rep.Failed))
	if d.Ready != nil {
		d.Ready(ln.Addr())
	}

	every := d.ReconcileEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	loopCtx, stop := context.WithCancel(ctx)
	defer stop()
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				if _, err := svc.Reconcile(loopCtx); err != nil && loopCtx.Err() == nil {
					logf("reconcile: %v", err)
				}
			}
		}
	}()

	ui, err := web.New(be, webOpt)
	if err != nil {
		return err
	}
	root := http.NewServeMux()
	root.Handle("/v1/", srv.Handler()) // the JSON API: bearer token on every request
	root.Handle("/", ui.Handler())     // the web UI: a session, set from the same token
	err = srv.ServeHandler(ctx, ln, root)
	stop()
	<-loopDone
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// addBoard gives the service the project board of D30: the supervisor's own
// writes, through the autonomy table, with a link to the task on each card. The
// guard's pusher and verifier are for the push flow, not for this.
func addBoard(scfg *service.Config, d Deps) {
	b := d.Config.Board
	if b == nil || d.Forge == nil {
		return
	}
	scfg.Board = forge.NewGuard(d.Forge, nil, policy.Default(), nil)
	if b.PublicURL != "" {
		scfg.BoardLink = func(task domain.ID) string { return notify.Link(b.PublicURL, notify.Message{TaskID: task}) }
	}
}

// Budgets turns the configured budgets into the service's: dollars become
// millionths of a dollar, so no float reaches a comparison with a total.
func Budgets(b config.Budgets) service.Budgets {
	limit := func(l config.BudgetLimit) service.Limit {
		return service.Limit{MaxTokens: l.MaxTokens, MaxCostMicroUSD: int64(math.Round(l.MaxCostUSD * 1e6))}
	}
	return service.Budgets{PerRun: limit(b.PerRun), PerTask: limit(b.PerTask), SoftPercent: b.SoftPercent}
}

// addRevoker gives kill-all the forge's way to revoke the tokens it holds, when
// the forge adapter has one (the GitHub App client does).
func addRevoker(scfg *service.Config, d Deps) {
	if r, ok := d.Forge.(interface {
		RevokeTokens(context.Context) (int, error)
	}); ok {
		scfg.RevokeTokens = r.RevokeTokens
	}
}

// applyWorkflows records the preset of each configured repository. A change of
// one is a policy change: it is refused unless the operator confirmed it, and when
// confirmed it is appended to the audit log of changes. A task already started
// keeps the preset it started under either way.
func applyWorkflows(ctx context.Context, d Deps, logf func(string, ...any)) error {
	for _, r := range d.Config.Repositories {
		want := string(r.Preset())
		prev, ok, err := d.Store.RecordedWorkflow(ctx, r.Name)
		if err != nil {
			return err
		}
		if ok && prev != want && !d.AcceptWorkflowChange {
			return fmt.Errorf("the workflow of %s is %s in the configuration and was %s: that is a policy change; check it and start with --accept-workflow-change to confirm it (tasks already started keep the %s they started under)", r.Name, want, prev, prev)
		}
		by := "serve"
		if d.AcceptWorkflowChange {
			by = "host-cli"
		}
		if _, changed, err := d.Store.ApplyWorkflow(ctx, r.Name, want, by, time.Now()); err != nil {
			return err
		} else if changed {
			logf("workflow of %s changed from %s to %s (confirmed on the host CLI)", r.Name, prev, want)
		}
	}
	return nil
}
