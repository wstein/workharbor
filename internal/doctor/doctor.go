// Package doctor is `whr doctor`: the checks of the onboarding steps (design
// §9.5). A check that has not been measured reports not verified, never ok:
// the point of the command is that a green line means something.
package doctor

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge/github"
	"github.com/wstein/workharbor/internal/runtime"
)

// Status is the outcome of one check.
type Status string

const (
	OK          Status = "ok"           // measured and fine
	Fail        Status = "fail"         // measured and wrong
	NotVerified Status = "not_verified" // no measurement exists, or it needs a live run
	Skipped     Status = "skipped"      // the human left it out (--skip), or the step is not offered
	// Warn is measured, working and weaker than recommended (D49). It is not a
	// failure: the exit code ignores it.
	Warn Status = "warn"
)

// Result is one line of the report.
type Result struct {
	Check  string `json:"check"`
	Step   int    `json:"step"` // the onboarding step of §9.5
	Status Status `json:"status"`
	Detail string `json:"detail"`
	// Phase is "host" or "user" for a setup step and empty for a shared check.
	Phase Phase `json:"phase,omitempty"`
	// Fix is the setup command that fixes a failing or not verified check, for
	// example "whr setup host --only power"; empty when passing or when no setup
	// step fixes it. `whr doctor` only names it, it never runs it.
	Fix string `json:"fix,omitempty"`
}

// Check is one named check. Its Run may assume nothing about the others. A
// check with a Fix is a setup step too (design D46): `whr doctor` runs the
// checks, `whr setup` runs the same values and offers the fixes, so the two
// cannot disagree.
type Check struct {
	Name  string
	Step  int
	Run   func(ctx context.Context) (Status, string)
	Phase Phase
	// FixOnWarn offers the fix on a warn too (drop-admin): for any other step a
	// warn is accepted as it is.
	FixOnWarn bool
	// Optional steps are left alone by `whr setup` unless it is told to run them.
	Optional bool
	// Title says in a few words what the step is for.
	Title string
	Fix   *Fix
	// UseUser, if set, names an account to run the setup commands as instead
	// of the requested one, for the status the last Run returned ("" for none).
	UseUser func(Status) string
	// Needs names a service the step's check or fix cannot work without, and
	// Provides one a successful fix of the step brings up. Setup refuses to run
	// a fix whose service no earlier step provided and a test holds the order to
	// it (#265).
	Needs, Provides string
}

// basic is a check as the list below writes it; base fills in the rest.
type basic struct {
	Name string
	Step int
	Run  func(ctx context.Context) (Status, string)
}

func base(in []basic) []Check {
	out := make([]Check, len(in))
	for i, b := range in {
		out[i] = Check{Name: b.Name, Step: b.Step, Run: b.Run}
	}
	return out
}

// Deps is what the checks touch outside themselves, so tests need no host.
type Deps struct {
	ConfigPath string
	Home       string
	// RepoDir is the checkout `whr doctor` runs in; empty skips the lane-agents check.
	RepoDir string
	FS      runtime.FS
	// SSHDFile overrides the sshd drop-in the ssh-keys-only check reads (tests).
	SSHDFile string
	LookPath func(string) (string, error)
	// GitHub builds the App's client from the configuration; nil uses
	// NewGitHub. Tests point it at a fake.
	GitHub func(*config.Config) (*github.Client, error)
	// Probe asks the running supervisor for something that needs the token.
	Probe func(ctx context.Context) error
	// The host: what the setup steps look at and change (design D46). Runner is
	// nil where nothing may be run, and the host checks then say not verified.
	Runner  Runner
	GOOS    string
	User    string // the account running this
	Account string // the account workharbor runs as (--user); empty means WhrUser
	UID     int
	Whr     string // the running whr binary
	Dev     bool   // allow a user-owned development installation: --dev, or the development_prefix key
	Managed bool   // `whr setup --managed`: the development_prefix key is to be removed
	Prefix  string // the selected installation prefix, "/opt/whr" by default
	Brewing string // the Brewfile's text; empty means the one in this package
}

// VendorTerms is where the manual explains a subscription login (D40).
const VendorTerms = "docs/manual/vendor-terms"

// Checks returns every check in the order of §9.5. They share one loaded
// configuration, read on first use.
func Checks(d Deps) []Check {
	var (
		cfg    *config.Config
		cfgErr error
		loaded bool
	)
	load := func() (*config.Config, error) {
		if !loaded {
			cfg, cfgErr = config.Load(d.ConfigPath)
			loaded = true
		}
		return cfg, cfgErr
	}
	needCfg := func(run func(*config.Config) (Status, string)) func(context.Context) (Status, string) {
		return func(context.Context) (Status, string) {
			c, err := load()
			if err != nil {
				return Fail, needsConfig + " (see the config check)"
			}
			return run(c)
		}
	}
	notVerified := func(why string) func(context.Context) (Status, string) {
		return func(context.Context) (Status, string) { return NotVerified, why }
	}
	return withFixes(d, base([]basic{
		{"config", 1, func(context.Context) (Status, string) {
			c, err := load()
			if err != nil {
				return Fail, problems(err)
			}
			return OK, fmt.Sprintf("%s: %d repositories, listening on %s", d.ConfigPath, len(c.Repositories), c.Listen)
		}},
		{"account", 2, d.accountCheck()},
		{"development-mode", 1, d.developmentModeCheck()},
		{"server", 1, func(ctx context.Context) (Status, string) {
			if d.Probe == nil {
				return NotVerified, "no client"
			}
			if err := d.Probe(ctx); err != nil {
				return Fail, "the supervisor did not accept the token: " + oneLine(err.Error()) + " (is `whr serve` running?)"
			}
			return OK, "the supervisor answered with the configured token"
		}},
		{"forge-key", 2, needCfg(func(c *config.Config) (Status, string) {
			b, err := config.ReadSecret(c.GitHub.KeyFile)
			if err != nil {
				return Fail, "github.key_file: " + oneLine(err.Error())
			}
			if !strings.Contains(string(b), "PRIVATE KEY") {
				return Fail, "github.key_file does not hold a PEM private key"
			}
			return OK, fmt.Sprintf("App %d: the key file is private and a PEM key; not tried against GitHub", c.GitHub.AppID)
		})},
		{"bot-key", 2, needCfg(botKeyCheck)},
		{"forge-app", 2, func(ctx context.Context) (Status, string) {
			c, err := load()
			if err != nil {
				return Fail, needsConfig + " (see the config check)"
			}
			mk := d.GitHub
			if mk == nil {
				mk = NewGitHub
			}
			gh, err := mk(c)
			if err != nil {
				return Fail, "github: " + oneLine(err.Error())
			}
			rep, err := gh.CheckApp(ctx)
			switch {
			case errors.Is(err, github.ErrAuth):
				return Fail, "GitHub refused the App's key: wrong github.app_id, or the key was revoked or is another App's"
			case err != nil:
				return NotVerified, "GitHub could not be asked: " + oneLine(err.Error())
			case rep.OK():
				return OK, fmt.Sprintf("App %s is installed on all %d repositories with exactly the expected permissions", rep.Slug, len(rep.Repos))
			}
			var bad []string
			bad = append(bad, rep.Problems...)
			for _, r := range rep.Repos {
				if !r.Installed {
					bad = append(bad, fmt.Sprintf("not installed on %s (install it at https://github.com/apps/%s/installations/new)", r.Repo, rep.Slug))
				}
				for _, p := range r.Problems {
					bad = append(bad, r.Repo+": "+p)
				}
			}
			return Fail, strings.Join(bad, "; ")
		}},
		{"forge-board", 2, func(ctx context.Context) (Status, string) {
			c, err := load()
			if err != nil {
				return Fail, needsConfig + " (see the config check)"
			}
			if c.Board == nil {
				return OK, "no project board is configured"
			}
			mk := d.GitHub
			if mk == nil {
				mk = NewGitHub
			}
			gh, err := mk(c)
			if err != nil {
				return Fail, "github: " + oneLine(err.Error())
			}
			rep, err := gh.CheckBoard(ctx)
			switch {
			case errors.Is(err, github.ErrBoardPermission):
				return Fail, "the App's installation lacks organization_projects: write: accept the new permission on the installation (the App's settings page); issues, comments and pushes are not affected"
			case errors.Is(err, github.ErrBoardNotWritable):
				return Fail, fmt.Sprintf("board not writable: the App cannot reach project %d of %s (it lacks the Projects permission, or this is a user-owned project, which an App's token may not reach: use an organization's project, D30)", c.Board.Number, c.Board.Owner)
			case errors.Is(err, github.ErrBoard):
				return Fail, oneLine(err.Error())
			case err != nil:
				return NotVerified, "GitHub could not be asked: " + oneLine(err.Error())
			case len(rep.MissingStatuses) > 0:
				return Fail, "the project's Status field lacks the options " + strings.Join(rep.MissingStatuses, ", ")
			}
			return NotVerified, "the project and its four Status options were found; writing a card is not tested until a task changes state"
		}},
		{"forge-workflow", 2, func(ctx context.Context) (Status, string) {
			c, err := load()
			if err != nil {
				return Fail, needsConfig + " (see the config check)"
			}
			mk := d.GitHub
			if mk == nil {
				mk = NewGitHub
			}
			gh, err := mk(c)
			if err != nil {
				return Fail, "github: " + oneLine(err.Error())
			}
			return workflowCheck(ctx, c, gh)
		}},
		{"forge-limits", 2, notVerified("that the bot cannot bypass branch protection, and that merge, tag, release and deploy stay forbidden, is enforced by the forge adapter but not checked against your repositories")},
		{"agent-login", 3, needCfg(func(c *config.Config) (Status, string) {
			if c.AgentAPIKeyEnvFile != "" {
				if _, err := c.AgentAPIKey(); err != nil {
					return Fail, oneLine(err.Error())
				}
				return OK, "api-key: the key file is private and holds only API keys; not tried against the vendor"
			}
			return NotVerified, "subscription: sign in inside each environment (D40); whr never sees the credential. Read " + VendorTerms + " first"
		})},
		{"runtime", 4, func(context.Context) (Status, string) {
			p, err := d.LookPath("container")
			if err != nil {
				return Fail, "the container CLI is not on the PATH"
			}
			return OK, p + " found; whether its service is running is not checked"
		}},
		{"mounts", 4, func(context.Context) (Status, string) {
			if err := runtime.CheckMount(d.FS, d.Home, d.Home); err == nil {
				return Fail, "the host-side mount check accepts the home directory"
			}
			return OK, "the host-side mount check refuses the home directory; the runtime's own refusal is not measured here"
		}},
		{"egress", 4, notVerified("default-deny egress needs a live environment; run the Apple Container live suite (-tags applecontainer)")},
		{"reboot", 4, notVerified("an agent session surviving a reboot is unverified (design §12)")},
		{"capacity", 4, notVerified("room for 4 concurrent environments (§8) is not measured")},
		{"lane-agents", 5, laneAgentsCheck(d)},
		{"notifications", 5, needCfg(func(c *config.Config) (Status, string) {
			if c.Ntfy == nil {
				return NotVerified, "no notification channel is configured: set an ntfy block to get pushes (design §9.5)"
			}
			server := c.Ntfy.Server
			if server == "" {
				server = "https://ntfy.sh"
			}
			host := server
			if u, err := url.Parse(server); err == nil && u.Host != "" {
				host = u.Scheme + "://" + u.Host // never a path, query or credential
			}
			return OK, "ntfy: " + host + ", topic and token files are private and valid; no test push is sent (§9.5)"
		})},
	}))
}

// Shared returns the checks that are no setup step of one account.
func Shared(checks []Check) []Check {
	var out []Check
	for _, c := range checks {
		if c.Phase == "" {
			out = append(out, c)
		}
	}
	return out
}

// sharedFixes names the setup step that fixes a shared check, where one does.
var sharedFixes = map[string]string{
	"config":    "whr setup --only config-base",
	"server":    "whr setup --only service-install",
	"forge-key": "whr setup --only github-app",
	"runtime":   "whr setup host --only brew-packages",
}

// needsConfig is what a shared check says when the configuration is unusable.
const needsConfig = "needs a valid configuration"

// FixCommand returns the setup command that fixes this check, or "" when none
// does. A step with a fix names `whr setup host --only <id>` or `whr setup
// --only <id>`; a shared check that cannot run without the configuration points
// at `whr setup`. It only names the command.
func (c Check) FixCommand(detail string) string {
	switch c.Phase {
	case PhaseHost:
		if c.Fix != nil {
			return "whr setup host --only " + c.Name
		}
	case PhaseUser:
		if c.Fix != nil {
			return "whr setup --only " + c.Name
		}
	default:
		if strings.HasPrefix(detail, needsConfig) {
			return "whr setup"
		}
		return sharedFixes[c.Name]
	}
	return ""
}

// Steps returns the checks of one phase, in the order the wizard runs them.
func Steps(checks []Check, phase Phase) []Check {
	var out []Check
	for _, c := range checks {
		if c.Phase == phase {
			out = append(out, c)
		}
	}
	return out
}

// Run runs the checks except the skipped ones, in order.
func Run(ctx context.Context, checks []Check, skip map[string]bool) []Result {
	out := make([]Result, 0, len(checks))
	for _, c := range checks {
		if skip[c.Name] {
			out = append(out, Result{Check: c.Name, Step: c.Step, Status: Skipped, Detail: "skipped on request", Phase: c.Phase})
			continue
		}
		st, detail := c.Run(ctx)
		r := Result{Check: c.Name, Step: c.Step, Status: st, Detail: detail, Phase: c.Phase}
		if st == Fail || st == NotVerified || (st == Warn && c.FixOnWarn) {
			r.Fix = c.FixCommand(detail)
			if c.UseUser != nil {
				if u := c.UseUser(st); u != "" {
					r.Fix = "whr setup host --user " + u
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// Failed reports whether any check failed. Not verified and skipped do not
// fail: they say what is unknown.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// botKeyCheck checks the bot's signing key (D51): the configuration check has
// refused a key file that is not 0600, not owned by this user, linked or inside
// a root; this one reads it the way a prepare does and wants an unencrypted SSH
// ed25519 private key, which git signs with and nothing can type a passphrase for.
func botKeyCheck(c *config.Config) (Status, string) {
	if c.BotSigningKeyFile == "" {
		return Warn, "bot_signing_key_file is not set: no topic can be prepared for review, because nothing is ever committed unsigned (D51)"
	}
	b, err := config.ReadSecret(c.BotSigningKeyFile)
	if err != nil {
		return Fail, "bot_signing_key_file: " + oneLine(err.Error())
	}
	key, err := ssh.ParseRawPrivateKey(b)
	if err != nil {
		return Fail, "bot_signing_key_file does not hold an unencrypted SSH private key"
	}
	if _, ok := key.(*ed25519.PrivateKey); !ok {
		return Fail, "bot_signing_key_file holds an SSH key that is not ed25519"
	}
	return OK, "the bot's signing key is a private, unencrypted ed25519 key (mode 0600, owned by this user); not tried against git"
}

// NewGitHub builds the App's client from the configuration: the key file read
// as a secret, the configured repositories, the configured API address.
func NewGitHub(c *config.Config) (*github.Client, error) {
	pemBytes, err := config.ReadSecret(c.GitHub.KeyFile)
	if err != nil {
		return nil, err
	}
	key, err := github.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}
	repos := make([]string, len(c.Repositories))
	for i, r := range c.Repositories {
		repos[i] = r.Name
	}
	gc := github.Config{AppID: c.GitHub.AppID, Key: key, Repos: repos, BaseURL: c.GitHub.APIURL}
	if b := c.Board; b != nil { // the App is expected to have the board's permission too
		gc.Board = &github.BoardConfig{Owner: b.Owner, Organization: b.Organization, Number: b.Number}
	}
	return github.New(gc)
}

// DefaultLookPath is exec.LookPath.
var DefaultLookPath = exec.LookPath

func problems(err error) string {
	if e, ok := err.(*config.Error); ok { //nolint:errorlint // the config error is returned unwrapped
		if len(e.Problems) > 3 {
			return strings.Join(e.Problems[:3], "; ") + fmt.Sprintf("; and %d more", len(e.Problems)-3)
		}
		return strings.Join(e.Problems, "; ")
	}
	return oneLine(err.Error())
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
