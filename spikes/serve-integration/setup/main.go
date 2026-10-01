// Command setup creates one workspace with one agent through the real stack, as
// `whr serve` would on a `whr ws add`: the agent clone is seeded from a local
// repository, the environment is provisioned on Apple Container with the tool
// store, the agent home volume and the egress sidecar, and the first agent's
// worktree is made inside it. There is no CLI for this yet (a finding of the
// integration run); this program stands in for it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/serve"
	"github.com/wstein/workharbor/internal/service"
)

func main() {
	cfgPath := flag.String("config", "", "the configuration file")
	exe := flag.String("exe", "", "the installed whr (for the egress proxy)")
	name := flag.String("name", "docs-ws", "workspace name")
	role := flag.String("role", "docs", "the first agent's role")
	source := flag.String("source", "", "a repository path to seed the agent clone from")
	repo := flag.String("repo", "wstein/workharbor", "owner/name")
	folder := flag.String("folder", "", "the empty workspace folder below a workspace root")
	flag.Parse()
	if err := run(*cfgPath, *exe, *name, *role, *source, *repo, *folder); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
}

func run(cfgPath, exe, name, role, source, repo, folder string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	deps, closeAll, err := serve.Build(cfg, exe, home, func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) })
	if err != nil {
		return err
	}
	defer closeAll()
	svc := service.New(deps.Store, deps.Runtime, deps.Agent, deps.Clock, service.Config{
		Owner: deps.Owner, Spec: deps.AgentSpec, NewID: serve.NewID, ReadyTimeout: 60 * time.Second,
	})
	defer svc.Shutdown()
	ws := service.NewWorkspaces(svc, service.WorkspaceConfig{
		Config: cfg, Git: deps.Git, Spec: deps.Spec, Prepare: deps.Prepare, NewID: serve.NewID, Issues: deps.Issues,
	})
	if err := os.MkdirAll(folder, 0o750); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	w, a, err := ws.Create(ctx, service.CreateRequest{
		Name: name, Path: filepath.Clean(folder), Repo: repo, Integration: "main", Source: source, Role: role,
	})
	if err != nil {
		return err
	}
	fmt.Printf("workspace %s (%s) environment %s agent %s/%s worktree %s\n", w.Name, w.ID, w.EnvID, w.Name, a.Role, a.Worktree)
	return nil
}
