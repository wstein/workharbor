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
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/api"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/notify"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
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
	// AgentSpec returns how an agent is started for a run.
	AgentSpec func(domain.Task, domain.Run) agent.StartSpec
	Clock     service.Clock
	Logf      func(format string, args ...any)
	// ReconcileEvery is how often the reconciler compares the database with the
	// runtime. Default 30 s.
	ReconcileEvery time.Duration
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
		Owner:   d.Owner,
		Budgets: Budgets(d.Config.Budgets),
		Spec:    d.AgentSpec,
		NewID:   NewID,
		OnError: func(err error) { logf("background error: %v", err) },
	}
	addBoard(&scfg, d)
	svc := service.New(d.Store, d.Runtime, d.Agent, d.Clock, scfg)
	defer svc.Shutdown()
	ws := service.NewWorkspaces(svc, service.WorkspaceConfig{
		Config: d.Config, Git: d.Git, Spec: d.Spec, Prepare: d.Prepare, NewID: NewID, Issues: d.Issues,
		Topics: d.Topics, EditorDir: d.EditorDir,
	})
	srv, err := api.New(api.NewBackend(svc, ws), api.Options{
		Token: token, Store: d.Store, OnError: func(err error) { logf("api error: %v", err) },
	})
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

	err = srv.Serve(ctx, ln)
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
