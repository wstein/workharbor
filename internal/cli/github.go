package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/githubapp"
	"github.com/wstein/workharbor/internal/redact"
)

func newGitHub(st *state) *cobra.Command {
	g := &cobra.Command{Use: "github", Short: "set up the GitHub App (provisional)"}
	group(g)
	app := &cobra.Command{Use: "app", Short: "the workharbor GitHub App"}
	group(app)
	app.AddCommand(newAppCreate(st))
	g.AddCommand(app)
	return g
}

// timeoutError is a wait that ran out: exit code 7.
type timeoutError struct{ msg string }

func (e timeoutError) Error() string { return e.msg }
func (timeoutError) ExitCode() int   { return exitcode.Timeout }

// newAppCreate is `whr github app create` (design §9.5, D15, D31): it creates
// the operator's own GitHub App through the manifest flow. It runs a short-lived
// server on the configured loopback address for the redirect (the forwarder of
// D29 points there), so `whr serve` must not be running: the supervisor needs
// the App, which is what this makes. The command name is provisional.
func newAppCreate(st *state) *cobra.Command {
	var (
		publicURL, listen, keyDir, name, org string
		ttl                                  time.Duration
		githubURL, apiURL                    string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create your own GitHub App from a manifest, no manual download",
		Long: `Creates a private GitHub App with exactly the permissions workharbor needs.

It prints a link; open it on any device that reaches whr's HTTPS name, press
Continue to GitHub, and confirm there. GitHub redirects back, whr stores the
private key in a 0600 file, and this command prints the two lines to add to
the configuration and the link to install the App on your repositories.

It listens on the configuration's "listen" address while it waits, so stop
"whr serve" first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if publicURL == "" {
				return usageError{"--public-url is needed: whr's HTTPS name behind the forwarder, for example https://whr.example.ts.net"}
			}
			path := st.configPath
			if path == "" {
				path = DefaultConfigPath(st.env.Getenv)
			}
			if listen == "" {
				cc, err := ReadClientConfig(path)
				if err != nil {
					return usageError{"no listen address: pass --listen or put it in the configuration (" + err.Error() + ")"}
				}
				listen = cc.Listen
			}
			if keyDir == "" {
				abs, err := filepath.Abs(filepath.Dir(path))
				if err != nil {
					return err
				}
				keyDir = abs
			}
			if name == "" {
				b := make([]byte, 2)
				_, _ = rand.Read(b)
				name = "workharbor-" + hex.EncodeToString(b)
			}
			rd := redact.New()
			setup, err := githubapp.NewSetup(githubapp.Config{
				PublicURL: publicURL, Name: name, Org: org, KeyDir: keyDir, TTL: ttl,
				GitHubURL: githubURL, APIURL: apiURL, Redactor: rd,
			})
			if err != nil {
				return usageError{err.Error()}
			}
			host, _, err := net.SplitHostPort(listen)
			if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
				return usageError{fmt.Sprintf("--listen %q is not a loopback address: the forwarder reaches whr there (D29)", listen)}
			}
			var lc net.ListenConfig
			ln, err := lc.Listen(cmd.Context(), "tcp", listen)
			if err != nil {
				return fmt.Errorf("listen on %s: %w (is `whr serve` running? stop it for the setup)", listen, err)
			}
			srv := &http.Server{Handler: setup.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func() { _ = srv.Serve(ln) }()
			defer func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(cmd.Context()), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(ctx)
			}()

			start, exp := setup.StartURL()
			fmt.Fprintf(st.env.Stderr, "Open this link (valid until %s, once):\n  %s\nThen press Continue to GitHub and confirm. Waiting for GitHub to send you back...\n", exp.Local().Format("15:04"), start)

			wctx, cancel := context.WithDeadline(cmd.Context(), exp)
			defer cancel()
			res, err := setup.Wait(wctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return timeoutError{"the link expired before GitHub sent you back: run the command again"}
				}
				return err
			}
			if st.asJSON {
				return json.NewEncoder(st.env.Stdout).Encode(map[string]any{"schema_version": 1, "ok": true, "data": res})
			}
			fmt.Fprintf(st.env.Stdout, "app_id\t%d\nkey_file\t%s\ninstall_url\t%s\n", res.AppID, clean(res.KeyFile), clean(res.InstallURL))
			fmt.Fprintf(st.env.Stderr, "\nAdd this to the configuration (whr does not edit it):\n\n    \"github\": {\"app_id\": %d, \"key_file\": %q}\n\n", res.AppID, res.KeyFile)
			fmt.Fprintf(st.env.Stderr, "Then install the App on your selected repositories: %s\n", res.InstallURL)
			fmt.Fprintln(st.env.Stderr, "Check that the main branch's ruleset does not list the App as a bypass actor (D15).")
			fmt.Fprintln(st.env.Stderr, "Run whr doctor afterwards: it checks the installation and its permissions.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&publicURL, "public-url", "", "whr's HTTPS name behind the forwarder (required)")
	f.StringVar(&listen, "listen", "", "loopback address to wait on (default: the configuration's listen)")
	f.StringVar(&keyDir, "key-dir", "", "where to write the private key (default: the configuration's directory)")
	f.StringVar(&name, "name", "", "the App's name, unique on GitHub (default workharbor-<random>)")
	f.StringVar(&org, "org", "", "create the App in this organization instead of your account")
	f.DurationVar(&ttl, "ttl", 10*time.Minute, "how long the link stays valid")
	f.StringVar(&githubURL, "github-url", "", "GitHub's web address, for a test double")
	f.StringVar(&apiURL, "api-url", "", "GitHub's API address, for a test double")
	_ = f.MarkHidden("github-url")
	_ = f.MarkHidden("api-url")
	return cmd
}
