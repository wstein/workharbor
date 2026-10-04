package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type workspaceRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Repo        string `json:"repo"`
	Integration string `json:"integration"`
	Path        string `json:"path"`
	Agents      []struct {
		ID     string `json:"id"`
		Role   string `json:"role"`
		Branch string `json:"branch"`
	} `json:"agents"`
}

func (s *state) workspaces(ctx context.Context) ([]workspaceRow, []byte, error) {
	c, err := s.api()
	if err != nil {
		return nil, nil, err
	}
	raw, data, err := c.Do(ctx, "GET", "/v1/workspaces", nil, "")
	if err != nil {
		return nil, nil, err
	}
	var rows []workspaceRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, fmt.Errorf("the workspace list is not what this whr expects: %w", err)
	}
	return rows, raw, nil
}

// newWs builds `whr ws add|ls|rm` (provisional, issue #99).
func newWs(s *state) *cobra.Command {
	ws := &cobra.Command{Use: "ws", Short: "Workspaces: folders with an agent clone and their environment (provisional)"}
	group(ws)
	ws.AddCommand(newWsAdd(s), newWsLs(s), newWsRm(s), newWsRebuild(s), newWsShell(s))
	return ws
}

func newWsAdd(s *state) *cobra.Command {
	var path, repo, role, from, branch, instructions, key string
	cmd := &cobra.Command{
		Use:   "add <name> --path <folder> --repo <owner/name> --role <role>",
		Short: "Create a workspace and its first agent (it seeds the clone and starts the environment; this takes a while)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for flag, v := range map[string]string{"--path": path, "--repo": repo, "--role": role} {
				if v == "" {
					return usageError{flag + " is needed"}
				}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			if key == "" {
				key = newKey()
			}
			body := map[string]string{"name": args[0], "path": path, "repo": repo, "role": role}
			if branch != "" {
				body["integration"] = branch
			}
			if from != "" {
				body["source"] = from
			}
			if instructions != "" {
				body["instructions"] = instructions
			}
			fmt.Fprintf(s.env.Stderr, "whr: creating workspace %s: this seeds the clone and starts the environment\n", clean(args[0]))
			raw, data, err := c.DoSlow(cmd.Context(), "POST", "/v1/workspaces", body, key)
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				var r workspaceRow
				if err := json.Unmarshal(data, &r); err != nil {
					return err
				}
				_, err := fmt.Fprintf(w, "%s\t%s/%s\n", clean(r.Name), clean(r.Name), clean(role))
				return err
			})
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "an empty folder below a workspace root")
	cmd.Flags().StringVar(&repo, "repo", "", "the repository, owner/name")
	cmd.Flags().StringVar(&role, "role", "", "the first agent's role")
	cmd.Flags().StringVar(&from, "from", "", "seed the clone from this repository path or https URL (default: the forge)")
	cmd.Flags().StringVar(&branch, "branch", "", "the branch the agents rebase onto (default: the repository's publication target; any other is refused)")
	cmd.Flags().StringVar(&instructions, "instructions", "", "standing instructions for the first agent")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "reuse a key to make a retry safe (default: a new one)")
	return cmd
}

func newWsLs(s *state) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List workspaces and their agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, raw, err := s.workspaces(cmd.Context())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				out := make([][]string, len(rows))
				for i, r := range rows {
					roles := make([]string, len(r.Agents))
					for j, a := range r.Agents {
						roles[j] = a.Role
					}
					out[i] = []string{r.Name, r.Repo, r.Integration, r.Path, strings.Join(roles, ",")}
				}
				return table(w, []string{"NAME", "REPO", "BRANCH", "PATH", "AGENTS"}, out)
			})
		},
	}
}

func newWsRm(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "rm <name>",
		Short:             "Remove a workspace that has no agents: its environment and home volume go, its folder stays",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, _, err := c.Do(cmd.Context(), "DELETE", "/v1/workspaces/"+url.PathEscape(args[0]), nil, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(args[0])); return err })
		},
	}
}

func newWsRebuild(s *state) *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild <workspace>",
		Short: "Recreate a workspace's environment from the image its repository resolves to now",
		Long: "A new devcontainer commit, an allowed feature source or a bumped base image reaches a long-lived workspace " +
			"only when its environment is next built; this builds it now. The home and build volumes, the workspace folder " +
			"and so every worktree and branch stay; the container, its network and its egress sidecar are new. The old " +
			"environment is removed only after the new one is up, and started again if the new one cannot be brought up. " +
			"It is refused while any run of the workspace is live, naming it, and it takes minutes when the image has to " +
			"be built. The rebuild is audited with the old and new image digests.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			fmt.Fprintln(s.env.Stderr, "rebuilding the environment (an image that has to be built takes minutes)...")
			raw, data, err := c.DoSlow(cmd.Context(), "POST", "/v1/workspaces/"+url.PathEscape(args[0])+"/rebuild", nil, newKey())
			if err != nil {
				return err
			}
			var r struct {
				OldEnv    string `json:"old_env"`
				NewEnv    string `json:"new_env"`
				OldImage  string `json:"old_image"`
				NewImage  string `json:"new_image"`
				OldDigest string `json:"old_digest"`
				NewDigest string `json:"new_digest"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return fmt.Errorf("the answer is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error {
				return table(w, []string{"", "ENVIRONMENT", "IMAGE", "DIGEST"}, [][]string{
					{"before", clean(r.OldEnv), clean(r.OldImage), clean(r.OldDigest)},
					{"after", clean(r.NewEnv), clean(r.NewImage), clean(r.NewDigest)},
				})
			})
		},
	}
}

// newAgent builds `whr agent add|ls|rm` (provisional, issue #99).
func newAgent(s *state) *cobra.Command {
	ag := &cobra.Command{Use: "agent", Short: "Named agents in a workspace (provisional)"}
	group(ag)
	var instructions string
	add := &cobra.Command{
		Use:               "add <workspace> <role>",
		Short:             "Add a named agent: a worktree and the branch agent/<role>",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: s.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := s.api()
			if err != nil {
				return err
			}
			body := map[string]string{"role": args[1]}
			if instructions != "" {
				body["instructions"] = instructions
			}
			raw, _, err := c.Do(cmd.Context(), "POST", "/v1/workspaces/"+url.PathEscape(args[0])+"/agents", body, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s/%s\n", clean(args[0]), clean(args[1]))
				return err
			})
		},
	}
	add.Flags().StringVar(&instructions, "instructions", "", "standing instructions for the agent")
	ls := &cobra.Command{
		Use:               "ls [workspace]",
		Short:             "List agents (all, or one workspace's)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: s.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, raw, err := s.workspaces(cmd.Context())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				var out [][]string
				for _, r := range rows {
					if len(args) == 1 && r.Name != args[0] {
						continue
					}
					for _, a := range r.Agents {
						out = append(out, []string{r.Name + "/" + a.Role, a.Branch, a.ID})
					}
				}
				return table(w, []string{"AGENT", "BRANCH", "ID"}, out)
			})
		},
	}
	rm := &cobra.Command{
		Use:               "rm <workspace>/<role>",
		Short:             "Remove an agent without an unfinished task; its worktree and branch stay in the clone",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeAgentRefs,
		RunE: func(cmd *cobra.Command, args []string) error {
			wsName, role, ok := strings.Cut(args[0], "/")
			if !ok || wsName == "" || role == "" {
				return usageError{"name the agent as <workspace>/<role>"}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, _, err := c.Do(cmd.Context(), "DELETE", "/v1/workspaces/"+url.PathEscape(wsName)+"/agents/"+url.PathEscape(role), nil, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(args[0])); return err })
		},
	}
	ag.AddCommand(add, ls, rm)
	return ag
}

func (s *state) completeWorkspaces(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	rows, _, err := s.workspaces(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name + "\t" + clean(r.Repo)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func (s *state) completeAgentRefs(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return s.completeAgents(cmd, nil, "")
}

// group makes a command that only holds subcommands print its help, and refuse
// an argument that is not one of them with a usage error (cobra otherwise prints
// the help and exits 0, which a script cannot tell from success).
func group(c *cobra.Command) {
	c.Args = cobra.ArbitraryArgs
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageError{fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())}
		}
		return cmd.Help()
	}
}

// newOpen is `whr open <workspace>[/<role>]` (design §4.5, issue #59): it asks
// the supervisor to make its own copy of the agent's branch and prints the
// copy's path, so `code "$(whr open docs)"` opens it. It is never the agent's
// checkout: the agent writes that, and its config and hooks would run in the
// developer's editor.
func newOpen(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "open <workspace>[/<role>]",
		Short:             "Make the supervisor's own copy of an agent's branch for your editor and print its path",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeAgentRefs,
		RunE: func(cmd *cobra.Command, args []string) error {
			wsName, role, _ := strings.Cut(args[0], "/")
			if wsName == "" {
				return usageError{"name the workspace, and the agent as <workspace>/<role> when it has several"}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			var body any
			if role != "" {
				body = map[string]string{"role": role}
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/workspaces/"+url.PathEscape(wsName)+"/open", body, newKey())
			if err != nil {
				return err
			}
			var cp struct {
				Path     string   `json:"path"`
				Warnings []string `json:"warnings"`
			}
			if err := json.Unmarshal(data, &cp); err != nil {
				return fmt.Errorf("the answer is not what this whr expects: %w", err)
			}
			if err := s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(cp.Path)); return err }); err != nil {
				return err
			}
			if len(cp.Warnings) > 0 { // file names from the repository: untrusted text
				fmt.Fprintln(s.env.Stderr, "These files in the copy can run things when an editor opens the folder; trust it only after reading them:")
				for _, f := range cp.Warnings {
					fmt.Fprintln(s.env.Stderr, "  "+clean(f))
				}
			}
			return nil
		},
	}
}
