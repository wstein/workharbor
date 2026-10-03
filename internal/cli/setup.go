package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
	rt "github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/setup"
)

// SetupEnv is what `whr setup` needs of the machine, so a test runs it with a
// fake Host and no terminal. Zero values mean this machine.
type SetupEnv struct {
	Host       setup.Host
	User       string
	UID        int
	GOOS       string
	IsTerminal func() bool
	Executable func() (string, error)
	Manager    *launchd.Manager
}

// installedPrefixes are the admin-owned places a whr may be installed (D24): the
// chosen prefix, and Homebrew's.
func installedPrefixes(prefix string) []string {
	out := []string{prefix}
	for _, p := range []string{"/opt/homebrew/opt/whr", "/opt/homebrew/Cellar/whr", "/usr/local/opt/whr"} {
		if p != prefix {
			out = append(out, p)
		}
	}
	return out
}

func (e SetupEnv) resolve(st *state) (SetupEnv, error) {
	if e.GOOS == "" {
		e.GOOS = runtime.GOOS
	}
	if e.User == "" {
		u, err := user.Current()
		if err != nil {
			return e, err
		}
		e.User, e.UID = u.Username, os.Getuid()
	}
	if e.IsTerminal == nil {
		e.IsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) } //nolint:gosec // a file descriptor of this process
	}
	if e.Executable == nil {
		e.Executable = os.Executable
	}
	if e.Host == nil {
		e.Host = setup.Terminal{In: bufio.NewReader(st.env.Stdin), Err: st.env.Stderr, Stdin: os.Stdin}
	}
	return e, nil
}

// newSetup is `whr setup` and `whr setup host` (provisional, design D46, issue
// #104): the first-time setup wizard. `whr setup host` is the administrator's
// part of the manual's host setup and `whr setup` the whr user's, in its desktop
// session. Each step is a check with an optional fix, shown before it runs.
func newSetup(st *state) *cobra.Command {
	var (
		dryRun   bool
		only     []string
		from     string
		whrUser  string
		prefix   string
		doctorOn = func(env SetupEnv, path string) []doctor.Check {
			home, _ := os.UserHomeDir()
			exe, _ := env.Executable()
			return doctor.Checks(doctor.Deps{
				ConfigPath: path, Home: home, FS: rt.OSFS{}, LookPath: doctor.DefaultLookPath,
				Runner: env.Host, GOOS: env.GOOS, User: env.User, Account: whrUser, UID: env.UID, Whr: exe, Prefix: prefix,
			})
		}
	)
	configPath := func() string {
		if st.configPath != "" {
			return st.configPath
		}
		return DefaultConfigPath(st.env.Getenv)
	}
	run := func(cmd *cobra.Command, phase doctor.Phase) error {
		env, err := st.env.Setup.resolve(st)
		if err != nil {
			return err
		}
		if !dryRun && !env.IsTerminal() {
			return usageError{"whr setup asks you questions and runs commands after your answer, so it needs a terminal: run it in one, or add --dry-run to see what it would do"}
		}
		ctx := cmd.Context()
		if phase == doctor.PhaseHost {
			admin := false
			if env.User == whrUser {
				admin, _, _ = doctor.Membership(ctx, env.Host, env.User)
			}
			if err := setup.GuardHost(env.User, env.UID, whrUser, admin); err != nil {
				return usageError{err.Error()}
			}
		} else {
			m := launchd.Manager{R: launchd.ExecRunner{}, UID: env.UID, GOOS: env.GOOS}
			if env.Manager != nil {
				m = *env.Manager
			}
			if err := setup.GuardUser(ctx, m, env.User, env.UID, whrUser); err != nil {
				if !dryRun {
					return usageError{err.Error()}
				}
				fmt.Fprintf(st.env.Stderr, "note (dry run): %s\n", oneLineError(err))
			}
		}
		if exe, err := env.Executable(); err != nil {
			return err
		} else if err := setup.CheckInstalled(exe, installedPrefixes(prefix)...); err != nil {
			if !dryRun {
				return usageError{err.Error()}
			}
			fmt.Fprintf(st.env.Stderr, "note (dry run): %s\n", oneLineError(err))
		}
		steps := doctorOn(env, configPath())
		// D49: without separation and with remote access, one explicit y that
		// names the risk. Doctor fails on it too; nothing is enforced.
		for _, c := range steps {
			if c.Name != "account" {
				continue
			}
			if stt, detail := c.Run(ctx); stt == doctor.Fail && !dryRun {
				fmt.Fprintf(st.env.Stderr, "account: %s\n", clean(strings.TrimSpace(detail)))
				ok, err := env.Host.Confirm("Go on without a dedicated standard account, knowing this?")
				if err != nil || !ok {
					return usageError{"stopped: set up a dedicated standard account, or remove the remote access from the configuration"}
				}
			} else if stt == doctor.Fail {
				fmt.Fprintf(st.env.Stderr, "note (dry run): account: %s\n", clean(strings.TrimSpace(detail)))
			}
		}
		outs, err := setup.Run(ctx, steps, env.Host, setup.Options{Phase: phase, DryRun: dryRun, Only: only, From: from, Out: st.env.Stdout, Err: st.env.Stderr})
		if err != nil {
			return usageError{err.Error()}
		}
		for _, o := range outs {
			if o.Status == doctor.Fail {
				if dryRun {
					return quietError{}
				}
				fmt.Fprintln(st.env.Stderr, "whr: some steps are not done; run `whr setup` again after you have dealt with them")
				return quietError{}
			}
		}
		fmt.Fprintln(st.env.Stderr, "all selected steps are done (or were not checked: see the lines marked not_verified); `whr doctor` checks the rest")
		return nil
	}
	flags := func(c *cobra.Command) {
		f := c.Flags()
		f.BoolVar(&dryRun, "dry-run", false, "run the read-only checks for real and print every fix without running any")
		f.StringSliceVar(&only, "only", nil, "run only these steps (optional steps too)")
		f.StringVar(&from, "from", "", "start at this step")
		f.StringVar(&whrUser, "user", doctor.WhrUser, "the account workharbor runs as")
		f.StringVar(&prefix, "prefix", doctor.DefaultPrefix, "the admin-owned prefix whr is installed under")
		names := func(phase doctor.Phase) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				var out []string
				for _, s := range doctor.Steps(doctor.Checks(doctor.Deps{ConfigPath: "x/config.json"}), phase) {
					out = append(out, s.Name+"\t"+s.Title)
				}
				return out, cobra.ShellCompDirectiveNoFileComp
			}
		}
		phase := doctor.PhaseUser
		if c.Name() == "host" {
			phase = doctor.PhaseHost
		}
		_ = c.RegisterFlagCompletionFunc("only", names(phase))
		_ = c.RegisterFlagCompletionFunc("from", names(phase))
	}
	root := &cobra.Command{
		Use:   "setup",
		Short: "first-time setup of the whr user's part, in its desktop session (provisional)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return run(cmd, doctor.PhaseUser) },
	}
	flags(root)
	hostCmd := &cobra.Command{
		Use:   "host",
		Short: "first-time setup of the host, as the administrator (provisional)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return run(cmd, doctor.PhaseHost) },
	}
	flags(hostCmd)
	root.AddCommand(hostCmd)
	return root
}
