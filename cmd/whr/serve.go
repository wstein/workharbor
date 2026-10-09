package main

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/cli"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/serve"
)

// serveCommand is `whr serve`: the supervisor, with the JSON API on the
// configuration's loopback address (design §9.7). It validates the whole
// configuration at start and reports every problem with its key.
func serveCommand(stderr io.Writer) *cobra.Command {
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the supervisor: the reconciler and the JSON API on a loopback address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("config")
			if path == "" {
				path = cli.DefaultConfigPath(os.Getenv)
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			logf := func(format string, args ...any) { fmt.Fprintf(stderr, "whr serve: "+format+"\n", args...) }
			deps, closeAll, err := serve.Build(cfg, exe, home, logf)
			if err != nil {
				return err
			}
			defer closeAll()
			deps.Setup, err = newServeSetup(cfg, path, exe, home)
			if err != nil {
				return err
			}
			deps.AcceptWorkflowChange, _ = cmd.Flags().GetBool("accept-workflow-change")
			deps.Ready = func(web, api net.Addr) { logf("web UI on %s, API on %s", web, api) }
			return serve.Run(cmd.Context(), deps)
		},
	}
	c.Flags().Bool("accept-workflow-change", false, "confirm that a repository's workflow preset in the configuration differs from the recorded one (a policy change, D47)")
	return c
}
