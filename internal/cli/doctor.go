package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/runtime"
)

// newDoctor is `whr doctor` (design §9.5, step 4 and the checks of the others).
// It reports each check as ok, fail, not verified or skipped; only fail is a
// non-zero exit.
func newDoctor(st *state) *cobra.Command {
	var skip []string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "check the configuration, the host and the supervisor; say what is not verified",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := st.configPath
			if path == "" {
				path = DefaultConfigPath(st.env.Getenv)
			}
			checks := doctor.Shared(doctor.Checks(doctor.Deps{
				ConfigPath: path,
				Home:       st.env.Getenv("HOME"),
				FS:         runtime.OSFS{},
				LookPath:   doctor.DefaultLookPath,
				Probe: func(ctx context.Context) error {
					c, err := st.api()
					if err != nil {
						return err
					}
					_, _, err = c.Do(ctx, "GET", "/v1/tasks?active=true", nil, "")
					return err
				},
			}))
			names := map[string]bool{}
			for _, c := range checks {
				names[c.Name] = true
			}
			skipped := map[string]bool{}
			for _, n := range skip {
				if !names[n] {
					return usageError{fmt.Sprintf("--skip: no check named %q", n)}
				}
				skipped[n] = true
			}
			rs := doctor.Run(cmd.Context(), checks, skipped)
			if st.asJSON {
				if err := json.NewEncoder(st.env.Stdout).Encode(map[string]any{"schema_version": 1, "ok": !doctor.Failed(rs), "checks": rs}); err != nil {
					return err
				}
			} else {
				printDoctor(st.env.Stdout, rs)
			}
			unknown := 0
			for _, r := range rs {
				if r.Status == doctor.NotVerified {
					unknown++
				}
			}
			if doctor.Failed(rs) {
				fmt.Fprintln(st.env.Stderr, "whr: some checks failed; fix them and run `whr doctor` again")
				return quietError{}
			}
			fmt.Fprintf(st.env.Stderr, "ready, with %d checks not verified; first command: whr run <issue-url>\n", unknown)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "leave a check out (repeatable); run `whr doctor` again to include it")
	_ = cmd.RegisterFlagCompletionFunc("skip", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"config", "server", "forge-key", "forge-app", "forge-board", "forge-limits", "agent-login", "runtime", "mounts", "egress", "reboot", "capacity", "notifications"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// quietError fails the command without a second line: it already explained itself.
type quietError struct{}

func (quietError) Error() string { return "" }
func (quietError) ExitCode() int { return exitcode.Error }

func printDoctor(w io.Writer, rs []doctor.Result) {
	for _, r := range rs {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Status, r.Check, clean(strings.TrimSpace(r.Detail)))
	}
}
