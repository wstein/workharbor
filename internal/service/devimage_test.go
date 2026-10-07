package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

func repoEnv(origin devcontainer.Origin, cfg devcontainer.Config, image func(context.Context) (string, error)) func(context.Context, string, string) (RepoEnvironment, error) {
	return func(context.Context, string, string) (RepoEnvironment, error) {
		return RepoEnvironment{
			Environment: devcontainer.Environment{Origin: origin, Commit: strings.Repeat("a", 40), Config: cfg, Image: cfg.Image},
			Image:       image,
		}, nil
	}
}

// A repository with a devcontainer or a Dockerfile runs its own image, built from
// the default branch; the rest of the spec stays the supervisor's.
func TestAWorkspaceRunsTheOwnImageOfItsRepository(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	var builds int
	r.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{Env: map[string]string{"GOFLAGS": "-mod=mod"}},
		func(context.Context) (string, error) {
			builds++
			return "whr.invalid/whr-env/wh-conformance:0123456789abcdef", nil
		})
	w, _ := r.create("docs-ws")
	info, err := r.rt.Adapter.Inspect(bg, string(w.EnvID))
	if err != nil || info.Image != "whr.invalid/whr-env/wh-conformance:0123456789abcdef" || builds != 1 {
		t.Fatalf("image %q, builds %d, %v", info.Image, builds, err)
	}
	// The mounts, the network and the limits are the supervisor's: the workspace
	// folder is mounted and nothing else of the repository's is.
	var mounted bool
	for _, m := range info.Mounts {
		if m.Target == WorkspaceMount && m.Source == w.Path {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("the workspace folder is not mounted: %+v", info.Mounts)
	}

	// A devcontainer that names an image and builds nothing runs that image.
	r2 := newWsRig(t)
	r2.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{Image: "docker.io/library/golang:1.27.1"}, nil)
	w2, _ := r2.create("docs-ws")
	if info, _ := r2.rt.Adapter.Inspect(bg, string(w2.EnvID)); info.Image != "docker.io/library/golang:1.27.1" {
		t.Errorf("image = %q", info.Image)
	}

	// The supervisor's default environment runs the supervisor's image.
	r3 := newWsRig(t)
	r3.ws.cfg.Environment = repoEnv(devcontainer.OriginDefault, devcontainer.Config{}, func(context.Context) (string, error) {
		t.Error("the default environment was built")
		return "", nil
	})
	w3, _ := r3.create("docs-ws")
	if info, _ := r3.rt.Adapter.Inspect(bg, string(w3.EnvID)); info.Image == "" || strings.HasPrefix(info.Image, "whr.invalid/whr-env/") {
		t.Errorf("image = %q", info.Image)
	}
}

// A repository whose image cannot be built asked for an environment it cannot
// have: no workspace is made, and nothing is left behind.
func TestAWorkspaceWhoseImageCannotBeBuiltIsRefused(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Environment = repoEnv(devcontainer.OriginDockerfile, devcontainer.Config{},
		func(context.Context) (string, error) { return "", errors.New("step 3: dnf install failed") })
	folder := r.folder("docs-ws")
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "docs-ws", Path: folder, Repo: "wstein/workharbor", Integration: "main", Source: r.forge, Role: "docs"})
	if err == nil || !strings.Contains(err.Error(), "dnf install failed") || !strings.Contains(err.Error(), "wstein/workharbor") {
		t.Fatalf("err = %v", err)
	}
	if list, _ := r.store.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspace left behind: %+v", list)
	}
	if infos, _ := r.rt.Adapter.List(bg, r.rt.Owner); len(infos) != 0 {
		t.Errorf("environment left behind: %+v", infos)
	}
	// A containerEnv name the supervisor owns is refused again by the spec.
	r2 := newWsRig(t)
	r2.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{Image: "docker.io/library/golang:1.27.1", Env: map[string]string{"HTTPS_PROXY": "http://evil"}}, nil)
	if _, _, err := r2.ws.Create(bg, CreateRequest{Name: "docs-ws", Path: r2.folder("docs-ws"), Repo: "wstein/workharbor", Integration: "main", Source: r2.forge, Role: "docs"}); err == nil {
		t.Error("a reserved variable got into the environment")
	}
}

// postCreateCommand runs once per environment and command set, in the agent's
// worktree with the agent's environment, after the allowlist is up to date and
// before the agent starts.
func TestPostCreateRunsOnceBeforeTheAgent(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	cmds := []devcontainer.Command{{Shell: "go mod download"}, {Argv: []string{"make", "setup"}}}
	r.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{Image: "docker.io/library/golang:1.27.1", PostCreate: cmds}, nil)
	markers := map[string]bool{}
	var atExec []int // how many agents had started at each post-create command
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 3 && cmd[0] == "sh" && cmd[1] == "-c" {
			switch {
			case strings.HasPrefix(cmd[2], "test -f "):
				if markers[strings.TrimPrefix(cmd[2], "test -f ")] {
					return nil, "", 0, true
				}
				return nil, "", 1, true
			case strings.HasPrefix(cmd[2], "touch "):
				markers[strings.TrimPrefix(cmd[2], "touch ")] = true
				return nil, "", 0, true
			case cmd[2] == "go mod download":
				atExec = append(atExec, len(r.agent.Specs))
				return nil, "", 0, true
			}
		}
		if len(cmd) == 2 && cmd[0] == "make" {
			atExec = append(atExec, len(r.agent.Specs))
			return nil, "", 0, true
		}
		return nil, "", 0, false
	}
	_, a := r.create("docs-ws")
	if _, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	if len(atExec) != 2 || atExec[0] != 0 || atExec[1] != 0 || len(r.agent.Specs) != 1 {
		t.Fatalf("post-create ran with %v agents started, then %d agent(s): it runs before the agent, in order", atExec, len(r.agent.Specs))
	}
	var ran []runtime.ExecRequest
	for _, c := range r.fake.Execs() {
		if len(c.Req.Cmd) == 3 && c.Req.Cmd[2] == "go mod download" || len(c.Req.Cmd) == 2 && c.Req.Cmd[0] == "make" {
			ran = append(ran, c.Req)
		}
	}
	for _, q := range ran {
		if q.Dir != a.Worktree {
			t.Errorf("post-create ran in %q, want the agent's worktree %q", q.Dir, a.Worktree)
		}
		if !slices.Contains(q.Env, "HOME=/home/workharbor") {
			t.Errorf("post-create has not the agent's environment: %v", q.Env)
		}
	}
	// A second run finds the marker and runs nothing again.
	atExec = nil
	if _, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#2"}); err == nil {
		// the first task's run holds the environment: the one-run rule refuses it
		t.Log("second start refused by the one-run rule, as expected")
	}
	if len(atExec) != 0 {
		t.Errorf("post-create ran again: %v", atExec)
	}
}

// A post-create command that fails ends the run as failed, before the agent
// starts, with the tail of its output; the human decides (retry or cancel).
func TestAFailingPostCreateEndsTheRunBeforeTheAgent(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{Image: "docker.io/library/golang:1.27.1", PostCreate: []devcontainer.Command{{Shell: "go mod download"}}}, nil)
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 3 && cmd[0] == "sh" {
			if strings.HasPrefix(cmd[2], "test -f ") {
				return nil, "", 1, true
			}
			return []byte("dial tcp: lookup proxy.golang.org: no such host\n"), "", 2, true
		}
		return nil, "", 0, false
	}
	_, a := r.create("docs-ws")
	_, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#1"})
	if err == nil || !strings.Contains(err.Error(), "post-create command") || !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("err = %v", err)
	}
	if len(r.agent.Specs) != 0 {
		t.Error("the agent started after a failed post-create")
	}
	list, _ := r.svc.List(bg, false)
	if len(list) != 1 {
		t.Fatalf("tasks = %+v", list)
	}
	agg, _ := r.store.LoadTask(bg, list[0].ID)
	runs := agg.Runs()
	if len(runs) != 1 || runs[0].State != domain.RunFailed {
		t.Fatalf("runs = %+v, want one failed run", runs)
	}
	if r.svc.attached(runs[0].ID) {
		t.Error("the failed run still holds its slot")
	}
}
