package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/render"
	rt "github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/setup"
	"github.com/wstein/workharbor/internal/textsafe"
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

func (e SetupEnv) resolve(st *state, style render.Style) (SetupEnv, error) {
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
		e.Host = setup.Terminal{In: bufio.NewReader(st.env.Stdin), Err: st.env.Stderr, Stdin: os.Stdin, Style: style}
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
		dev      bool
		managed  bool
		only     []string
		from     string
		whrUser  string
		prefix   string
		plain    bool
		verbose  bool
		doctorOn = func(env SetupEnv, path string) []doctor.Check {
			home := st.env.Getenv("HOME")
			exe, _ := env.Executable()
			return doctor.Checks(doctor.Deps{
				ConfigPath: path, Home: home, FS: rt.OSFS{}, LookPath: doctor.DefaultLookPath,
				Runner: env.Host, GOOS: env.GOOS, User: env.User, Account: whrUser, UID: env.UID, Whr: exe, Prefix: prefix, Dev: dev, Managed: managed,
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
		style := st.style(st.env.Stderr, plain)
		ui := render.Writer{W: st.env.Stderr, S: style}
		env, err := st.env.Setup.resolve(st, style)
		if err != nil {
			return err
		}
		if !dryRun && !env.IsTerminal() {
			return usageError{"whr setup asks you questions and runs commands after your answer, so it needs a terminal: run it in one, or add --dry-run to see what it would do"}
		}
		if err := plainFlag("--user", whrUser); err != nil {
			return err
		}
		if dev && managed {
			return usageError{"--dev and --managed cannot be combined: --dev remembers a development installation, --managed removes the memory"}
		}
		exe, exeErr := env.Executable()
		key, err := rememberedPrefix(cmd, dev, managed, configPath(), exe)
		if err != nil {
			return err
		}
		remembered := useRemembered(cmd, dev, key)
		if remembered {
			dev, prefix = true, key
		} else if prefix, err = installationPrefix(cmd, prefix, dev, st.env.Getenv("HOME")); err != nil {
			return err
		}
		// a rule sets the development warning apart from the steps (#320)
		switch {
		case remembered:
			ui.Rule()
			fmt.Fprintln(st.env.Stderr, rememberedWarning(configPath()))
			ui.Rule()
		case dev:
			ui.Rule()
			fmt.Fprintln(st.env.Stderr, developmentWarning)
			ui.Rule()
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
		if exeErr != nil {
			return exeErr
		}
		// --managed from a binary that is not installed (a source build, a user-writable
		// whr) may only leave development mode: the development-key step and the managed
		// prefix check run, every other step is refused as without --managed
		onlyKey := len(only) == 1 && only[0] == "development-key"
		if err := setup.CheckInstalled(exe, setupPrefixes(prefix, dev)...); err != nil {
			switch {
			case managed && onlyKey:
				fmt.Fprintf(st.env.Stderr, "note: %s; only the development-key step runs from this binary, and the managed prefix is checked afterwards\n", oneLineError(err))
			default:
				if managed {
					err = fmt.Errorf("%w; from this binary --managed runs only `--only development-key`: install the release for the other steps (manual, host setup step 13)", err)
				} else if !dev {
					err = fmt.Errorf("%w; for a source installation use --dev (or select its --prefix)", err)
				}
				if !dryRun {
					return usageError{err.Error()}
				}
				fmt.Fprintf(st.env.Stderr, "note (dry run): %s\n", oneLineError(err))
			}
		}
		steps := doctorOn(env, configPath())
		if dev && !dryRun {
			for _, c := range steps {
				if c.Name == "prefix" {
					if status, detail := c.Run(ctx); status == doctor.Fail {
						return usageError{detail}
					}
				}
			}
		}
		// D49: without separation and with remote access, one explicit y that
		// names the risk. Doctor fails on it too; nothing is enforced.
		for _, c := range steps {
			if c.Name != "account" {
				continue
			}
			if stt, detail := c.Run(ctx); stt == doctor.Fail && !dryRun {
				ui.Report(render.LevelFail, "account: "+clean(strings.TrimSpace(detail)))
				// accepting a risk is not undoable by running it again: Enter is no
				a, err := setup.Ask(env.Host, "Go on without a dedicated standard account, knowing this?", render.DefaultNo)
				if err == nil && a == render.Quit {
					ui.Report(render.LevelSkipped, "stopped at your request, nothing was run")
					return quitError{}
				}
				if err != nil || a != render.Yes {
					return usageError{"stopped: set up a dedicated standard account, or remove the remote access from the configuration"}
				}
			} else if stt == doctor.Fail {
				fmt.Fprintf(st.env.Stderr, "note (dry run): account: %s\n", clean(strings.TrimSpace(detail)))
			}
		}
		resume := []string{"whr", "setup"}
		if phase == doctor.PhaseHost {
			resume = append(resume, "host")
		}
		if dev && !remembered {
			resume = append(resume, "--dev")
		}
		if managed {
			resume = append(resume, "--managed")
		}
		if cmd.Flags().Changed("user") {
			resume = append(resume, "--user", whrUser)
		}
		if cmd.Flags().Changed("prefix") {
			resume = append(resume, "--prefix", prefix)
		}
		for i, name := range only {
			if name == "whr-user" {
				only[i] = "workharbor-user"
			}
		}
		if from == "whr-user" {
			from = "workharbor-user"
		}
		so := setup.Options{Phase: phase, DryRun: dryRun, Only: only, From: from, Resume: resume, Out: st.env.Stdout, Err: st.env.Stderr, Style: style, Verbose: verbose}
		outs, err := setup.Run(ctx, steps, env.Host, so)
		var quit *setup.QuitError
		if errors.As(err, &quit) {
			printQuit(ui, quit)
			return quitError{}
		}
		if err != nil {
			return usageError{err.Error()}
		}
		setup.Summary(st.env.Stderr, outs, so)
		if managed {
			// leaving development mode: the key is gone (or was refused above), and
			// the managed prefix is what the installation now relies on
			for _, c := range steps {
				if c.Name == "prefix" {
					stt, detail := c.Run(ctx)
					fmt.Fprintf(st.env.Stderr, "prefix: %s: %s\n", stt, clean(strings.TrimSpace(detail)))
					if stt == doctor.Fail {
						fmt.Fprintln(st.env.Stderr, "whr: the managed prefix is not ready: `whr setup host --only prefix` as the administrator prepares it")
						return quietError{}
					}
				}
			}
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
		f.BoolVar(&dev, "dev", false, "use a development installation (default prefix: $HOME/.local; explicit --prefix wins)")
		f.BoolVar(&managed, "managed", false, "leave development mode: remove development_prefix from the configuration (`--only development-key` does only that), then check the managed prefix; not with --dev")
		f.BoolVar(&dryRun, "dry-run", false, "run the read-only checks for real and print every fix without running any")
		f.BoolVar(&plain, "plain", false, "no colour and no symbols beyond ASCII, as when the output is not a terminal; also no fzf")
		f.BoolVar(&verbose, "verbose", false, "also show the raw text of the tools a step ran")
		f.StringSliceVar(&only, "only", nil, "run only these steps (optional steps too)")
		f.StringVar(&from, "from", "", "start at this step")
		f.StringVar(&whrUser, "user", doctor.WhrUser, "the account workharbor runs as")
		f.StringVar(&prefix, "prefix", doctor.DefaultPrefix, "the installation prefix (default: /opt/whr, or $HOME/.local with --dev)")
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

// Development mode is explicit on each setup/doctor invocation. The LaunchAgent
// retains the selected executable, so restart needs no mode flag or config key.
const developmentWarning = "warning: development installation; a user-writable supervisor lacks managed-install replacement protection"

func installationPrefix(cmd *cobra.Command, prefix string, dev bool, home string) (string, error) {
	if cmd.Flags().Changed("prefix") {
		if err := plainFlag("--prefix", prefix); err != nil {
			return "", err
		}
	}
	if dev && !cmd.Flags().Changed("prefix") {
		if !filepath.IsAbs(home) {
			return "", usageError{"--dev needs an absolute HOME or an explicit --prefix"}
		}
		prefix = filepath.Join(home, ".local")
	}
	if !filepath.IsAbs(prefix) {
		return "", usageError{"--prefix must be absolute"}
	}
	return filepath.Clean(prefix), nil
}

// plainFlag refuses a flag value with a control, bidirectional or separator
// character: the value is printed in the suggested next command, which a human
// may paste, and a newline in it would start a second command.
func plainFlag(name, value string) error {
	if strings.IndexFunc(value, func(r rune) bool { return textsafe.IsControl(r) || textsafe.IsBidiOrSeparator(r) || r == '\t' }) >= 0 {
		return usageError{name + " must not contain a control, bidirectional or separator character"}
	}
	return nil
}

func setupPrefixes(prefix string, dev bool) []string {
	if dev {
		return []string{prefix}
	}
	return installedPrefixes(prefix)
}
