package serve

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
	"github.com/wstein/workharbor/internal/store"
)

const token = "serve-test-token"

func newDeps(t *testing.T) (Deps, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api.token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(dir, "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := runtimetest.NewFakeHarness(t)
	return Deps{
		Config:  &config.Config{Listen: "127.0.0.1:0", APITokenFile: tokenFile},
		Store:   st,
		Runtime: h.Adapter,
		Agent:   agenttest.NewFake(agenttest.FullCaps()),
		Owner:   h.Owner,
		Spec:    func(domain.Workspace) runtime.Spec { return h.NewSpec() },
		Prepare: h.Prepare,
		AgentSpec: func(domain.Task, domain.Run) agent.StartSpec {
			return agent.StartSpec{Auth: agent.AuthSubscription}
		},
		ReconcileEvery: 20 * time.Millisecond,
	}, st
}

func get(t *testing.T, url, tok string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestRunServesTheAPIOnLoopbackAndStopsCleanly(t *testing.T) {
	d, _ := newDeps(t)
	addr := make(chan net.Addr, 1)
	d.Ready = func(a net.Addr) { addr <- a }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d) }()
	var a net.Addr
	select {
	case a = <-addr:
	case err := <-done:
		t.Fatalf("Run ended early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not come up")
	}
	if tcp, ok := a.(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		t.Fatalf("listening on %v, want a loopback address", a)
	}
	base := "http://" + a.String()
	if code, body := get(t, base+"/v1/health", token); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Errorf("health = %d %s", code, body)
	}
	if code, _ := get(t, base+"/v1/health", ""); code != 401 {
		t.Errorf("health without the token = %d", code)
	}
	if code, body := get(t, base+"/v1/tasks", token); code != 200 || !strings.Contains(body, `"data":[]`) {
		t.Errorf("tasks = %d %s", code, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v after a clean stop", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunRefusesANonLoopbackAddress(t *testing.T) {
	d, _ := newDeps(t)
	d.Config.Listen = "0.0.0.0:0"
	if err := Run(context.Background(), d); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("Run = %v, want a loopback refusal", err)
	}
}

func TestRunRefusesALooseTokenFile(t *testing.T) {
	d, _ := newDeps(t)
	if err := os.Chmod(d.Config.APITokenFile, 0o644); err != nil { //nolint:gosec // a deliberately loose file
		t.Fatal(err)
	}
	if err := Run(context.Background(), d); err == nil {
		t.Error("a world-readable token file was accepted")
	}
}

// What was running when the supervisor died is reconciled before the first
// request is served: a run with no session is not left running (D6).
func TestRunReconcilesBeforeItAcceptsRequests(t *testing.T) {
	d, st := newDeps(t)
	prep, err := d.Prepare(d.Spec(domain.Workspace{ID: "w1"}))
	if err != nil {
		t.Fatal(err)
	}
	envID, err := d.Runtime.Provision(context.Background(), prep)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Runtime.Start(context.Background(), envID); err != nil {
		t.Fatal(err)
	}
	agg := domain.NewTaskAggregate(domain.Task{ID: "t1", Repo: "a/b", Issue: "#1", State: domain.TaskQueued, CreatedAt: time.Now()})
	agg.AddEnvironment(domain.Environment{ID: domain.ID(envID), Backend: "fake", State: domain.EnvRunning})
	if err := agg.StartRun(domain.Run{ID: "r1", WorkspaceID: "w1", EnvID: domain.ID(envID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTask(context.Background(), agg); err != nil {
		t.Fatal(err)
	}

	ready := make(chan net.Addr, 1)
	d.Ready = func(a net.Addr) { ready <- a }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, d) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("no server")
	}
	got, err := st.LoadTask(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := got.Run("r1"); r.State != domain.RunFailed {
		t.Errorf("the run is %s: it had no session and must have been reconciled", r.State)
	}
}

func TestNewIDIsRandomAndPrefixed(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b || !strings.HasPrefix(string(a), "x-") || len(a) != 18 {
		t.Errorf("ids %q %q", a, b)
	}
}
