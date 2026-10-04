package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/launchd"
)

// Host is what `whr service` needs of the machine, so a test runs it without
// launchd. Zero values mean the real ones.
type Host struct {
	Manager    *launchd.Manager
	Executable func() (string, error)
	LookPath   func(string) (string, error)
}

func (h Host) manager() launchd.Manager {
	if h.Manager != nil {
		return *h.Manager
	}
	return launchd.Manager{R: launchd.ExecRunner{}, UID: os.Getuid(), GOOS: runtime.GOOS}
}

// newService is `whr service install|uninstall|status` (provisional, issue
// #38): `whr serve` as a LaunchAgent in the user's graphical session, the only
// place Apple Container's services can be reached.
func newService(st *state) *cobra.Command {
	var whr, container string
	g := &cobra.Command{Use: "service", Short: "Run whr serve as a macOS LaunchAgent in this user's login session (provisional)"}
	group(g)

	// spec describes the job. Only install needs the binaries: uninstall and
	// status work from the label and the home directory, so they still work when
	// whr or container is gone.
	spec := func(full bool) (launchd.Spec, error) {
		path := st.configPath
		if path == "" {
			path = DefaultConfigPath(st.env.Getenv)
		}
		cfgPath, err := filepath.Abs(path)
		if err != nil {
			return launchd.Spec{}, err
		}
		home := st.env.Getenv("HOME")
		if !full {
			return launchd.Spec{Label: launchd.Label, Config: cfgPath, Home: home}, nil
		}
		if whr == "" {
			exe := st.env.Host.Executable
			if exe == nil {
				exe = os.Executable
			}
			if whr, err = exe(); err != nil {
				return launchd.Spec{}, err
			}
		}
		if resolved, err := filepath.EvalSymlinks(whr); err == nil {
			whr = resolved
		}
		if container == "" {
			look := st.env.Host.LookPath
			if look == nil {
				look = exec.LookPath
			}
			if container, err = look("container"); err != nil {
				return launchd.Spec{}, usageError{"the container CLI is not on the PATH: install Apple Container (host setup step 6) or pass --container"}
			}
		}
		return launchd.Spec{Label: launchd.Label, Whr: whr, Config: cfgPath, Container: container, Home: home}, nil
	}

	install := &cobra.Command{
		Use:   "install",
		Short: "Write the LaunchAgent plist and load it: container system start, then whr serve, kept alive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := spec(true)
			if err != nil {
				return err
			}
			if err := launchd.CheckBinary(s.Whr); err != nil {
				return usageError{err.Error()}
			}
			cfg, err := config.Load(s.Config) // a job that cannot start is worse than none
			if err != nil {
				return fmt.Errorf("the configuration %s is not valid: %w", s.Config, err)
			}
			// development_prefix reads as --dev --prefix (D24, #276): Load has checked its
			// value and its file; the job's binary must be the one under that prefix, and
			// a managed whr refuses the key
			if cfg.DevelopmentPrefix != "" {
				if config.UnderManagedPrefix(s.Whr) {
					return usageError{fmt.Sprintf("%s holds %s, which a whr in a managed prefix refuses: run `whr setup --managed`, or delete the key", s.Config, config.DevelopmentPrefixKey)}
				}
				if !config.Within(s.Whr, cfg.DevelopmentPrefix) {
					return usageError{fmt.Sprintf("%s is not under the %s %s: install it there, or run `whr setup --managed`", s.Whr, config.DevelopmentPrefixKey, cfg.DevelopmentPrefix)}
				}
				fmt.Fprintln(st.env.Stderr, rememberedWarning(s.Config))
			}
			if err := st.env.Host.manager().Install(cmd.Context(), s); err != nil {
				return err
			}
			fmt.Fprintln(st.env.Stdout, s.PlistPath())
			fmt.Fprintf(st.env.Stderr, "installed: %s runs `container system start`, then `whr serve`, kept alive (restarts at most every 30 s); logs in %s\n", launchd.Label, s.LogDir())
			return nil
		},
	}
	install.Flags().StringVar(&whr, "whr", "", "the installed whr binary (default: this one; refused inside a git working tree)")
	install.Flags().StringVar(&container, "container", "", "the container CLI (default: from the PATH)")

	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Unload the job and remove its plist; the logs stay",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := spec(false)
			if err != nil {
				return err
			}
			return st.env.Host.manager().Uninstall(cmd.Context(), s)
		},
	}
	uninstall.Flags().StringVar(&whr, "whr", "", "")
	uninstall.Flags().StringVar(&container, "container", "", "")
	_ = uninstall.Flags().MarkHidden("whr")
	_ = uninstall.Flags().MarkHidden("container")

	status := &cobra.Command{
		Use:   "status",
		Short: "Say whether the job is installed and loaded, and what launchd knows of it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := spec(false)
			if err != nil {
				return err
			}
			res, err := st.env.Host.manager().Status(cmd.Context(), s)
			if err != nil {
				return err
			}
			if st.asJSON {
				return encodeJSON(st.env.Stdout, map[string]any{"schema_version": 1, "ok": true, "data": res})
			}
			fmt.Fprintf(st.env.Stdout, "installed\t%t\nloaded\t%t\n", res.Installed, res.Loaded)
			if res.Loaded {
				fmt.Fprintf(st.env.Stdout, "state\t%s\npid\t%d\nlast_exit\t%s\n", clean(res.State), res.PID, clean(res.LastExit))
			}
			return nil
		},
	}
	status.Flags().StringVar(&whr, "whr", "", "")
	status.Flags().StringVar(&container, "container", "", "")
	_ = status.Flags().MarkHidden("whr")
	_ = status.Flags().MarkHidden("container")
	g.AddCommand(install, uninstall, status)
	return g
}
