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
	"github.com/wstein/workharbor/internal/render"
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
		plain   bool
		verbose bool
		report  bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "check the configuration, the host and the supervisor; say what is not verified",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// the user is printed in each repair line, which a human may paste:
			// refused here as `whr setup` does, not escaped on print (#280)
			if err := plainFlag("--user", whrUser); err != nil {
				return err
			}
			path := st.configPath
			if path == "" {
				path = DefaultConfigPath(st.env.Getenv)
			}
			style := st.style(st.env.Stderr, plain)
			ui := render.Writer{W: st.env.Stderr, S: style}
			env, err := st.env.Setup.resolve(st, style)
			if err != nil {
				return err
			}
			exe, _ := env.Executable()
			key, err := rememberedPrefix(cmd, dev, false, path, exe)
			if err != nil {
				return err
			}
			remembered := useRemembered(cmd, dev, key)
			if remembered {
				dev, prefix = true, key
			} else if prefix, err = installationPrefix(cmd, prefix, dev, st.env.Getenv("HOME")); err != nil {
				return err
			}
			switch {
			case remembered:
				ui.Rule()
				fmt.Fprintln(st.env.Stderr, rememberedWarning(path))
				ui.Rule()
			case dev:
				ui.Rule()
				fmt.Fprintln(st.env.Stderr, developmentWarning)
				ui.Rule()
			}
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
				if n == "whr-user" {
					n = "workharbor-user"
				}
				if !names[n] {
					return usageError{fmt.Sprintf("--skip: no check named %q", n)}
				}
				skipped[n] = true
			}
			rs := doctor.Run(cmd.Context(), checks, skipped)
			repair := repairContext{Dev: dev && !remembered, Account: whrUser}
			if repair.Dev && cmd.Flags().Changed("prefix") {
				repair.Prefix = prefix
			}
			for i, r := range rs {
				context := repair
				if other && r.Fix != "" && r.Phase != doctor.PhaseHost {
					context.RunAs = whrUser
				}
				rs[i].Fix = context.command(r.Fix)
			}
			presentation := doctor.PresentResults(rs)
			if report {
				if err := writeSetupReport(st, path, presentation, "doctor", "", whrUser, dev); err != nil {
					return err
				}
			}
			if st.asJSON {
				if err := encodeJSON(st.env.Stdout, map[string]any{"schema_version": 1, "ok": presentation.OK, "checks": presentation.Results()}); err != nil {
					return err
				}
			} else {
				// stdout is data: the tab-separated lines, unchanged, unless stdout is
				// a terminal, where the readable report on stderr says the same
				if !st.isTTY(st.env.Stdout) {
					printDoctor(st.env.Stdout, rs)
				}
			}
			printDoctorHuman(ui, rs, verbose)
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
	cmd.Flags().BoolVar(&report, "report", false, "save a private, redacted setup report in the state directory")
	cmd.Flags().BoolVar(&dev, "dev", false, "check a development installation (default prefix: $HOME/.local; explicit --prefix wins)")
	cmd.Flags().BoolVar(&plain, "plain", false, "no colour and no symbols beyond ASCII, as when the output is not a terminal")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "also show the raw text of the tools a check ran")
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Status, r.Check, clean(strings.TrimSpace(r.Detail)), clean(r.Fix))
	}
}

// shellArgument quotes a diagnostic command argument without interpreting it.
func shellArgument(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
