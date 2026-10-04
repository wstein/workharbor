package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/runtime"
)

// newDoctor is `whr doctor` (design §9.5, step 4 and the checks of the others).
// It runs every check read-only (the shared ones, then the host and user steps
// of `whr setup`), reports each as ok, warn, fail, not verified or skipped, and names
// the setup command that fixes a failing or not verified one; only fail is a
// non-zero exit. It never fixes anything.
func newDoctor(st *state) *cobra.Command {
	var (
		dev     bool
		skip    []string
		whrUser string
		prefix  string
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "check the configuration, the host and the supervisor; say what is not verified",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := st.configPath
			if path == "" {
				path = DefaultConfigPath(st.env.Getenv)
			}
			env, err := st.env.Setup.resolve(st)
			if err != nil {
				return err
			}
			prefix, err = installationPrefix(cmd, prefix, dev, st.env.Getenv("HOME"))
			if err != nil {
				return err
			}
			if dev {
				fmt.Fprintln(st.env.Stderr, developmentWarning)
			}
			exe, _ := env.Executable()
			repoDir, _ := os.Getwd()
			checks := doctor.Checks(doctor.Deps{
				ConfigPath: path,
				RepoDir:    repoDir,
				Home:       st.env.Getenv("HOME"),
				FS:         runtime.OSFS{},
				LookPath:   doctor.DefaultLookPath,
				// Output only: the checks read the machine and nothing writes.
				Runner: env.Host, GOOS: env.GOOS, User: env.User, Account: whrUser, UID: env.UID, Whr: exe, Prefix: prefix, Dev: dev,
				Probe: func(ctx context.Context) error {
					c, err := st.api()
					if err != nil {
						return err
					}
					_, _, err = c.Do(ctx, "GET", "/v1/tasks?active=true", nil, "")
					return err
				},
			})
			other := env.User != whrUser
			if other { // the user phase describes whr's account, not this one
				for i, c := range checks {
					if c.Phase == doctor.PhaseUser {
						checks[i].Run = func(context.Context) (doctor.Status, string) {
							return doctor.NotVerified, fmt.Sprintf("this check describes the account %s runs as: run `whr doctor` as %s", whrUser, whrUser)
						}
					}
				}
			}
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
			for i, r := range rs {
				if dev {
					rs[i].Fix = strings.Replace(rs[i].Fix, "whr setup host ", "whr setup host --dev ", 1)
					rs[i].Fix = strings.Replace(rs[i].Fix, "whr setup --only ", "whr setup --dev --only ", 1)
					if cmd.Flags().Changed("prefix") && strings.HasPrefix(rs[i].Fix, "whr setup ") {
						rs[i].Fix += " --prefix " + shellArgument(prefix)
					}
				}
				if other && r.Fix != "" && r.Phase != doctor.PhaseHost {
					rs[i].Fix += " (run as " + whrUser + ")"
				}
			}
			if st.asJSON {
				if err := encodeJSON(st.env.Stdout, map[string]any{"schema_version": 1, "ok": !doctor.Failed(rs), "checks": rs}); err != nil {
					return err
				}
			} else {
				printDoctor(st.env.Stdout, rs)
			}
			for _, r := range rs {
				if r.Fix != "" {
					fmt.Fprintf(st.env.Stderr, "%s: %s\n  → %s\n", r.Check, clean(strings.TrimSpace(r.Detail)), r.Fix)
				}
			}
			unknown, warned := 0, 0
			for _, r := range rs {
				switch r.Status {
				case doctor.NotVerified:
					unknown++
				case doctor.Warn:
					warned++
				}
			}
			if doctor.Failed(rs) {
				fmt.Fprintln(st.env.Stderr, "whr: some checks failed; fix them and run `whr doctor` again")
				return quietError{}
			}
			fmt.Fprintf(st.env.Stderr, "ready, with %d checks not verified and %d warnings; first command: whr run <issue-url>\n", unknown, warned)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dev, "dev", false, "check a development installation (default prefix: $HOME/.local; explicit --prefix wins)")
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "leave a check out (repeatable); run `whr doctor` again to include it")
	cmd.Flags().StringVar(&whrUser, "user", doctor.WhrUser, "the account workharbor runs as")
	cmd.Flags().StringVar(&prefix, "prefix", doctor.DefaultPrefix, "the installation prefix (default: /opt/whr, or $HOME/.local with --dev)")
	_ = cmd.RegisterFlagCompletionFunc("skip", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		var names []string
		for _, c := range doctor.Checks(doctor.Deps{}) {
			names = append(names, c.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// quietError fails the command without a second line: it already explained itself.
type quietError struct{}

func (quietError) Error() string { return "" }
func (quietError) ExitCode() int { return exitcode.Error }

func printDoctor(w io.Writer, rs []doctor.Result) {
	for _, r := range rs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Status, r.Check, clean(strings.TrimSpace(r.Detail)), r.Fix)
	}
}

// shellArgument quotes a diagnostic command argument without interpreting it.
func shellArgument(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
