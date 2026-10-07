package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/config"
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
		board, local                         bool
		githubURL, apiURL                    string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create your own GitHub App from a manifest, no manual download",
		Long: `Creates a private GitHub App with exactly the permissions workharbor needs.

It prints a link; open it on any device that reaches whr's HTTPS name (the
configuration's public_url, or --public-url), press Continue to GitHub, and
confirm there. With --local the link points at the loopback listener instead:
open it in a browser on the host itself. GitHub redirects back, whr stores the
private key in a 0600 file, and this command prints the two lines to add to
the configuration and the link to install the App on your repositories.

It listens on the configuration's "listen" address while it waits, so stop
"whr serve" first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if local && publicURL != "" {
				return usageError{"--local and --public-url exclude each other"}
			}
			path := st.configPath
			if path == "" {
				path = DefaultConfigPath(st.env.Getenv)
			}
			cc, ccErr := ReadClientConfig(path)
			if listen == "" {
				if ccErr != nil {
					return usageError{"no listen address: pass --listen or put it in the configuration (" + ccErr.Error() + ")"}
				}
				listen = cc.Listen
				if listen == "" {
					return usageError{"no listen address: pass --listen or put it in the configuration"}
				}
			}
			host, _, err := net.SplitHostPort(listen)
			if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
				return usageError{fmt.Sprintf("--listen %q is not a loopback address: the forwarder reaches whr there (D29)", listen)}
			}
			switch {
			case local:
				publicURL = "http://" + listen
			case publicURL == "" && ccErr == nil:
				publicURL = cc.PublicURL
			}
			if publicURL == "" {
				return usageError{"no public name: set public_url in the configuration (whr setup asks for it) or pass --public-url whr.example.ts.net; to try it on the host itself, pass --local and open the link in a browser on this Mac"}
			}
			if !local && !loopbackHTTP(publicURL) {
				n, err := config.NormalizePublicURL(publicURL)
				if err != nil {
					return usageError{"public name: " + err.Error()}
				}
				publicURL = n
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
				PublicURL: publicURL, Name: name, Org: org, KeyDir: keyDir, TTL: ttl, Board: board,
				GitHubURL: githubURL, APIURL: apiURL, Redactor: rd,
			})
			if err != nil {
				return usageError{err.Error()}
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
			fmt.Fprintf(st.env.Stderr, "Open this link (valid until %s, once):\n  %s\nThen press Continue to GitHub and confirm. Waiting for GitHub to send you back...\n", exp.Local().Format("15:04"), clean(start))
			if local {
				fmt.Fprintln(st.env.Stderr, "This link points at the loopback listener: open it in a browser on this Mac. GitHub's redirect back to it is unverified.")
			} else {
				fmt.Fprintf(st.env.Stderr, "If the link times out, the name or the forwarder is at fault, not this command: whr waits on http://%s (loopback only). Check that the forwarder serves %s to that port, or use --local.\n", listen, clean(publicURL))
			}

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
				return encodeJSON(st.env.Stdout, map[string]any{"schema_version": 1, "ok": true, "data": res})
			}
			fmt.Fprintf(st.env.Stdout, "app_id\t%d\nkey_file\t%s\ninstall_url\t%s\n", res.AppID, clean(res.KeyFile), clean(res.InstallURL))
			fmt.Fprintf(st.env.Stderr, "\nAdd this to the configuration (whr does not edit it):\n\n    \"github\": {\"app_id\": %d, \"key_file\": %q}\n\n", res.AppID, res.KeyFile)
			fmt.Fprintf(st.env.Stderr, "Then install the App on your selected repositories: %s\n", clean(res.InstallURL))
			fmt.Fprintln(st.env.Stderr, "Check that the main branch's ruleset does not list the App as a bypass actor (D15).")
			fmt.Fprintln(st.env.Stderr, "Run whr doctor afterwards: it checks the installation and its permissions.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&publicURL, "public-url", "", "whr's HTTPS name behind the forwarder (default: the configuration's public_url); https:// is added when missing")
	f.BoolVar(&local, "local", false, "use the loopback listener as the link, to open on the host itself")
	f.StringVar(&listen, "listen", "", "loopback address to wait on (default: the configuration's listen)")
	f.StringVar(&keyDir, "key-dir", "", "where to write the private key (default: the configuration's directory)")
	f.StringVar(&name, "name", "", "the App's name, unique on GitHub (default workharbor-<random>)")
	f.StringVar(&org, "org", "", "create the App in this organization instead of your account")
	f.BoolVar(&board, "board", false, "also ask for the permission to write an organization's project board (D30)")
	f.DurationVar(&ttl, "ttl", 10*time.Minute, "how long the link stays valid")
	f.StringVar(&githubURL, "github-url", "", "GitHub's web address, for a test double")
	f.StringVar(&apiURL, "api-url", "", "GitHub's API address, for a test double")
	_ = f.MarkHidden("github-url")
	_ = f.MarkHidden("api-url")
	return cmd
}

// loopbackHTTP is an explicit http:// address on a loopback host: a test double
// or a local trial, which githubapp.CheckBaseURL still checks.
func loopbackHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
}
