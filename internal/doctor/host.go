package doctor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/credcheck"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/sshca"
	"github.com/wstein/workharbor/internal/toolstore"
)

// The setup steps beyond what `whr doctor` always checked (design D46, the manual's
// host setup). Every command and every output format below is that of the macOS version
// you are running (unverified), as its own tools document it, and none has been run against a fresh machine:
// all of it is unverified until the wizard has set up the reference Mac mini
// (issue #73). The checks only read; the fixes are shown before they run.

// MediaAnalysisMaxCPU is the percentage of one core at which the media-analysis
// check fails. Werner's headless Mac measured mediaanalysisd at 222 % while it
// ran agent environments; half a core is well above idle and well below that.
const MediaAnalysisMaxCPU = 50.0

// DefaultPrefix is where an admin-owned install of whr lives (D24).
const DefaultPrefix = "/opt/whr"

// WhrUser is the standard user workharbor runs as.
const WhrUser = "workharbor"

// DefaultBrewfile is the manual's Brewfile (step 5): the whole host software.
const DefaultBrewfile = `brew "container"
brew "git"
brew "gh"
cask "tailscale"
`

const (
	sshdFile    = "/etc/ssh/sshd_config.d/100-whr.conf"
	sshdContent = "PasswordAuthentication no\nKbdInteractiveAuthentication no\n"
)

// withFixes adds the titles and fixes of the checks doctor always had, and the
// host and user steps, in the order the wizard runs them.
func withFixes(d Deps, shared []Check) []Check {
	out := append([]Check(nil), hostSteps(d)...)
	out = append(out, userSteps(d)...)
	return append(out, shared...)
}

func (d Deps) output(ctx context.Context, argv ...string) (string, error) {
	if d.Runner == nil || d.GOOS != "darwin" {
		return "", errNotHere
	}
	b, err := d.Runner.Output(ctx, argv...)
	return string(b), err
}

var errNotHere = errors.New("this runs only on a Mac")

// DSCLNotFound reports whether a failed `dscl . -read` says the record is not
// there: exit status 56 (eDSRecordNotFound) or "does not exist" in what it said.
// Any other failure (permissions, a directory-service error, an unknown format)
// says nothing about the account.
func DSCLNotFound(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return commandExitIs(err, 56) || strings.Contains(m, "does not exist") || strings.Contains(m, "eDSRecordNotFound")
}

// commandExitIs prefers the real process status. The exact textual fallback
// supports Runners that expose only an error string, including scripted checks.
func commandExitIs(err error, code int) bool {
	if err == nil {
		return false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == code
	}
	status := "exit status " + strconv.Itoa(code)
	message := err.Error()
	return message == status || strings.HasPrefix(message, status+":")
}

// dsclFailure preserves stdout as well as the wrapped status and stderr.
func dsclFailure(out string, err error) error {
	if err == nil || strings.TrimSpace(out) == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
}

// LegacyUser is the account name before D49: an installation made then runs as
// it, and the host steps recognise it without ever renaming or removing it.
const LegacyUser = "whr"

// userStep is the workharbor-user check. When the account is missing and only
// the legacy account exists, it says so and points at `--user whr` instead of
// offering a second account; it never creates, renames or deletes one. Its
// Fix is the one the last Run left (the wizard shows it after the check).
func userStep(d Deps, setupCommand string) Check {
	create := []Cmd{{Sudo: true, Argv: []string{"sysadminctl", "-addUser", d.account(), "-fullName", "WorkHarbor", "-password", "-"}}}
	createGuide := "sysadminctl asks you for the new user's password itself; whr never sees it. Then log in as " + d.account() + " on the Mac (or over Screen Sharing) and run `" + setupCommand + "` there."
	fix := &Fix{Cmds: create, Guide: createGuide}
	legacy := false // set by the last Run: only the Fail with a found whr
	return Check{
		Name: "workharbor-user", Phase: PhaseHost, Step: 2, Title: "the standard user " + d.account() + " (manual step 2)",
		Run: func(ctx context.Context) (Status, string) {
			fix.Cmds, fix.Guide = create, createGuide
			legacy = false
			if out, err := d.output(ctx, "dscl", ".", "-read", "/Users/"+d.account(), "UniqueID"); err != nil {
				err = dsclFailure(out, err)
				if st, msg, ok := notHere(err); ok {
					return st, msg
				}
				if DSCLNotFound(err) {
					st, msg, found := d.missingUser(ctx, fix)
					legacy = found
					return st, msg
				}
				return NotVerified, "dscl did not say whether " + d.account() + " exists: " + oneLine(err.Error())
			}
			admin, known := d.isAdmin(ctx)
			if !known {
				return NotVerified, d.account() + " exists; dseditgroup did not say whether it is an administrator"
			}
			if admin {
				return Warn, d.account() + " is an administrator: allowed, but a dedicated standard user is the recommended account (D49; see the account check and the drop-admin step)"
			}
			return OK, d.account() + " exists and is a standard user"
		},
		Fix: fix,
		UseUser: func(Status) string {
			if legacy {
				return LegacyUser
			}
			return ""
		},
	}
}

// missingUser says what a missing account means: a legacy whr account that
// exists is named, with the commands that use it (the Fix becomes guidance
// only); only a real not-found of whr leaves the creation offered, and any
// other answer is not verified.
func (d Deps) missingUser(ctx context.Context, fix *Fix) (st Status, msg string, legacyFound bool) {
	missing := "there is no user " + d.account()
	// only the default account has a legacy name (#344): an account asked for
	// by --user is created as asked
	if d.account() != WhrUser {
		return Fail, missing, false
	}
	out, err := d.output(ctx, "dscl", ".", "-read", "/Users/"+LegacyUser, "UniqueID")
	err = dsclFailure(out, err)
	switch {
	case err == nil:
		fix.Cmds = nil
		fix.Guide = "Nothing is created, renamed or deleted. Run `whr setup host --user " + LegacyUser + "` and `whr doctor --user " + LegacyUser + "` to keep using the legacy account."
		return Fail, missing + ", but the legacy account " + LegacyUser + " exists: run `whr setup host --user " + LegacyUser + "` or `whr doctor --user " + LegacyUser + "` to use it instead of creating a second account", true
	case DSCLNotFound(err):
		return Fail, missing, false
	}
	fix.Cmds = nil
	fix.Guide = "Inspect the legacy account lookup failure, then retry `whr setup host --only workharbor-user`. Account creation is unavailable until the lookup confirms that " + LegacyUser + " does not exist."
	return NotVerified, "there is no user " + d.account() + ", and dscl did not say whether the legacy account " + LegacyUser + " exists: " + oneLine(err.Error()), false
}

// serviceContainerSystem is the service the container-start step brings up and
// the steps after it need.
const serviceContainerSystem = "container-system"

// desktopSession names the session the container steps run in for the person
// reading the guide: this one when the target account is the current user (for
// example `--dev --user <you>`), else the target account's own.
func (d Deps) desktopSession() string {
	account := d.Account
	if account == "" {
		account = WhrUser
	}
	if d.User != "" && d.User == account {
		return "this desktop session (Terminal on the Mac)"
	}
	return account + "'s own desktop session (Screen Sharing)"
}

// desktopOnly is what a check that needs Apple Container says outside the
// desktop session of the target account, where the container services do not
// answer (#156).
func (d Deps) desktopOnly() string {
	return "Apple Container answers only in " + d.desktopSession() + "; run this check there, or use `whr ls` or `whr show <task>` over SSH"
}

// inDesktop asks launchd.CheckSession whether this is the Aqua session, before a
// check runs any `container` command. When it is not, the check reports
// not_verified and runs nothing.
func (d Deps) inDesktop(ctx context.Context) (Status, string, bool) {
	if d.Runner == nil || d.GOOS != "darwin" {
		return NotVerified, "not checked: " + errNotHere.Error(), false
	}
	m := launchd.Manager{R: runnerAdapter{d.Runner}, UID: d.UID, GOOS: d.GOOS}
	if err := m.CheckSession(ctx); err != nil {
		return NotVerified, d.desktopOnly(), false
	}
	return "", "", true
}

func notHere(err error) (Status, string, bool) {
	if errors.Is(err, errNotHere) {
		return NotVerified, "not checked: " + err.Error(), true
	}
	return "", "", false
}

func (d Deps) prefix() string {
	if d.Prefix != "" {
		return d.Prefix
	}
	return DefaultPrefix
}

// sshdPath is the sshd drop-in the ssh-keys-only check reads; SSHDFile
// overrides it for tests.
func (d Deps) sshdPath() string {
	if d.SSHDFile != "" {
		return d.SSHDFile
	}
	return sshdFile
}

func (d Deps) configDir() string { return filepath.Dir(d.ConfigPath) }

// kv reads "name value" lines, as `pmset -g` prints them.
func kv(out string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			m[f[0]] = f[1]
		}
	}
	return m
}

func hostSteps(d Deps) []Check {
	setupCommand := "whr setup"
	if d.account() != WhrUser {
		setupCommand += " --user '" + strings.ReplaceAll(d.account(), "'", "'\"'\"'") + "'"
	}
	brewfile := d.Brewing
	if brewfile == "" {
		brewfile = DefaultBrewfile
	}
	return []Check{
		userStep(d, setupCommand),

		{
			Name: "autologout", Phase: PhaseHost, Step: 2, Title: "no automatic log-out after inactivity (manual step 2)",
			Run: func(ctx context.Context) (Status, string) {
				out, err := d.output(ctx, "defaults", "read", "/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					// defaults exits non-zero and says "does not exist" when the key is
					// not set, which is the default: automatic log-out is off. Any
					// other failure (a timeout, a bad plist) says nothing about the
					// setting, so it is not a pass (unverified on macOS 26).
					if strings.Contains(err.Error(), "does not exist") {
						return OK, "automatic log-out is not set"
					}
					// the key is absent in the way macOS 26 words it (one observed
					// data point, unverified elsewhere): the system default applies,
					// which is off. Only this exact message; anything else stays
					// not verified.
					if strings.Contains(err.Error(), "Could not find key 'com.apple.autologout.AutoLogOutDelay' in domain 'kCFPreferencesAnyApplication'") {
						return OK, "key not set: the system default applies (default off)"
					}
					return NotVerified, "defaults did not answer, so the setting is not known: " + oneLine(err.Error())
				}
				delay := strings.TrimSpace(out)
				if delay == "" || delay == "0" {
					return OK, "automatic log-out is off"
				}
				return Fail, "the Mac logs out automatically after " + oneLine(delay) + " seconds of inactivity, which ends Apple Container's services and every agent"
			},
			Fix: &Fix{
				Guide: "Open System Settings → Privacy & Security → Advanced and turn off \"Log out automatically after inactivity\". whr does not change it for you. The key it reads (com.apple.autologout.AutoLogOutDelay in /Library/Preferences/.GlobalPreferences) is unverified on macOS 26.",
				Open:  "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension",
			},
		},

		{
			Name: "workspace-volume", Phase: PhaseHost, Step: 3, Title: "workspace volumes encrypted, with ownership honoured (manual step 3)",
			Run: func(ctx context.Context) (Status, string) {
				if d.GOOS != "darwin" || d.Runner == nil {
					return NotVerified, "not checked: " + errNotHere.Error()
				}
				vols, st, msg := d.workspaceVolumes(ctx)
				if st != "" {
					return st, msg
				}
				if len(vols) == 0 {
					return OK, "the workspace roots are on the internal disk, which FileVault covers"
				}
				var bad []string
				for _, v := range vols {
					out, err := d.output(ctx, "diskutil", "info", v)
					if err != nil {
						return NotVerified, "diskutil did not answer for " + v + ": " + oneLine(err.Error())
					}
					info := colonLines(out)
					if enc := info["FileVault"]; !strings.HasPrefix(enc, "Yes") && !strings.HasPrefix(info["Encrypted"], "Yes") {
						bad = append(bad, v+" is not encrypted")
					}
					if !strings.HasPrefix(info["Owners"], "Enabled") {
						bad = append(bad, v+" ignores file ownership (Owners: "+orNone(info["Owners"])+")")
					}
				}
				if len(bad) > 0 {
					return Fail, strings.Join(bad, "; ")
				}
				return OK, "every workspace volume is encrypted and honours ownership (diskutil's output format is unverified on macOS 26)"
			},
			Fix: &Fix{
				Cmds: []Cmd{{Sudo: true, Argv: []string{"diskutil", "enableOwnership", "<volume>"}}},
				Build: func(ctx context.Context, _ Prompter) ([]Cmd, error) {
					vols, _, msg := d.workspaceVolumes(ctx)
					if msg != "" && len(vols) == 0 {
						return nil, errors.New(msg)
					}
					var cmds []Cmd
					for _, v := range vols {
						out, err := d.output(ctx, "diskutil", "info", v)
						if err == nil && !strings.HasPrefix(colonLines(out)["Owners"], "Enabled") {
							cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"diskutil", "enableOwnership", v}})
						}
					}
					return cmds, nil
				},
				Guide: "Ownership is turned on for you (sudo diskutil enableOwnership <volume>). Encryption is not: it needs a passphrase that only you may know, so erase the volume as APFS (Encrypted) in Disk Utility, or run `diskutil apfs encryptVolume /Volumes/<ssd> -user disk` yourself, and keep the passphrase in your password manager (manual step 3).",
				Open:  "/System/Applications/Utilities/Disk Utility.app",
			},
		},

		{
			Name: "power", Phase: PhaseHost, Step: 4, Title: "never sleep, restart after a power cut, no Power Nap (manual step 4)",
			Run: func(ctx context.Context) (Status, string) {
				out, err := d.output(ctx, "pmset", "-g")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "pmset did not answer: " + oneLine(err.Error())
				}
				m := kv(out)
				var bad []string
				for _, want := range [][2]string{{"sleep", "0"}, {"disksleep", "0"}, {"autorestart", "1"}, {"womp", "1"}, {"powernap", "0"}} {
					if m[want[0]] != want[1] {
						bad = append(bad, want[0]+" is "+orNone(m[want[0]])+", want "+want[1])
					}
				}
				if len(bad) > 0 {
					return Fail, strings.Join(bad, "; ")
				}
				return OK, "the Mac does not sleep, restarts after a power cut and has Power Nap off"
			},
			Fix: &Fix{Cmds: []Cmd{{Sudo: true, Argv: []string{"pmset", "-a", "sleep", "0", "disksleep", "0", "autorestart", "1", "womp", "1", "powernap", "0"}}}},
		},

		{
			Name: "media-analysis", Phase: PhaseHost, Step: 4, Title: "Apple's media analysis is not eating the CPU (manual step 4, headless Mac)",
			Run: func(ctx context.Context) (Status, string) {
				out, err := d.output(ctx, "ps", "-axo", "user=,pcpu=,time=,comm=")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "ps did not answer: " + oneLine(err.Error())
				}
				// The cache is the whr user's; only that account can read it.
				cache := "its cache is not read from here (run whr doctor as " + d.account() + ")"
				if d.User == d.account() {
					cache = "its cache is not known"
					if kb, err := d.output(ctx, "du", "-sk", filepath.Join(d.Home, "Library", "Caches", "com.apple.mediaanalysisd")); err == nil {
						if f := strings.Fields(kb); len(f) > 0 {
							if n, err := strconv.ParseInt(f[0], 10, 64); err == nil {
								cache = "its cache is " + strconv.FormatInt(n/1024, 10) + " MiB"
							}
						}
					}
				}
				// Every account's instance counts: the busiest one decides.
				found, bad := false, false
				var top float64
				var topUser, topTime string
				for _, l := range strings.Split(out, "\n") {
					f := strings.Fields(l)
					if len(f) < 4 || filepath.Base(strings.Join(f[3:], " ")) != "mediaanalysisd" {
						continue
					}
					cpu, err := strconv.ParseFloat(f[1], 64)
					if err != nil {
						bad = true
						continue
					}
					if !found || cpu > top {
						top, topUser, topTime = cpu, f[0], f[2]
					}
					found = true
				}
				if found {
					detail := fmt.Sprintf("mediaanalysisd of %s uses %.0f%% CPU, has used %s of CPU time, %s", topUser, top, topTime, cache)
					if top >= MediaAnalysisMaxCPU {
						return Fail, detail + fmt.Sprintf(" (limit %.0f%%)", MediaAnalysisMaxCPU)
					}
					if bad {
						return NotVerified, "ps's answer for another mediaanalysisd could not be read; " + detail
					}
					return OK, detail
				}
				if bad {
					return NotVerified, "ps's answer for mediaanalysisd could not be read"
				}
				return OK, "mediaanalysisd is not running; " + cache
			},
			Fix: &Fix{
				Guide: "whr never stops or deletes anything of Apple's. Turn off Apple Intelligence and Siri in System Settings, and keep Photos' analysis from running on this Mac; then keep the workspace roots out of Spotlight (step spotlight). Clearing mediaanalysisd's cache or killing it only helps for a while, the OS undoes it, so they are described in the manual and are not fixes.",
				Open:  "x-apple.systempreferences:com.apple.Siri-Settings.extension",
			},
		},

		{
			Name: "spotlight", Phase: PhaseHost, Step: 4, Title: "Spotlight does not index the workspaces (manual step 4, headless Mac)",
			Run: func(ctx context.Context) (Status, string) {
				if d.GOOS != "darwin" || d.Runner == nil {
					return NotVerified, "not checked: " + errNotHere.Error()
				}
				vols, st, msg := d.workspaceVolumes(ctx)
				if st != "" {
					return st, msg
				}
				if len(vols) == 0 {
					return NotVerified, "the workspace roots are on the internal disk: Spotlight's privacy list cannot be read, so add them there yourself"
				}
				var on, unknown []string
				for _, v := range vols {
					out, err := d.output(ctx, "mdutil", "-s", v)
					if err != nil {
						return NotVerified, "mdutil did not answer for " + v + ": " + oneLine(err.Error())
					}
					switch lo := strings.ToLower(out); {
					case strings.Contains(lo, "indexing enabled"):
						on = append(on, v)
					case strings.Contains(lo, "indexing disabled"):
					default:
						unknown = append(unknown, v)
					}
				}
				if len(on) > 0 {
					return Fail, "Spotlight indexes " + strings.Join(on, ", ")
				}
				if len(unknown) > 0 {
					return NotVerified, "mdutil's answer for " + strings.Join(unknown, ", ") + " is not one this check knows (its format is unverified on macOS 26)"
				}
				return OK, "Spotlight indexing is off on every workspace volume (mdutil's output format is unverified on macOS 26)"
			},
			Fix: &Fix{
				Cmds: []Cmd{{Sudo: true, Argv: []string{"mdutil", "-i", "off", "<volume>"}}},
				Build: func(ctx context.Context, _ Prompter) ([]Cmd, error) {
					vols, _, msg := d.workspaceVolumes(ctx)
					if msg != "" && len(vols) == 0 {
						return nil, errors.New(msg)
					}
					var cmds []Cmd
					for _, v := range vols {
						out, err := d.output(ctx, "mdutil", "-s", v)
						if err == nil && strings.Contains(strings.ToLower(out), "indexing enabled") {
							cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"mdutil", "-i", "off", v}})
						}
					}
					return cmds, nil
				},
				Guide: "On a workspace volume of its own, `sudo mdutil -i off <volume>` turns indexing off. A root on the internal disk cannot be turned off that way: add it in System Settings → Spotlight → Search Privacy.",
				Open:  "x-apple.systempreferences:com.apple.Siri-Settings.extension",
			},
		},

		{
			Name: "firewall", Phase: PhaseHost, Step: 8, Title: "the firewall on, in stealth mode (manual step 8)",
			Run: func(ctx context.Context) (Status, string) {
				const fw = "/usr/libexec/ApplicationFirewall/socketfilterfw"
				state, err := d.output(ctx, fw, "--getglobalstate")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "socketfilterfw did not answer: " + oneLine(err.Error())
				}
				stealth, _ := d.output(ctx, fw, "--getstealthmode")
				var bad []string
				if !strings.Contains(strings.ToLower(state), "enabled") {
					bad = append(bad, "the firewall is off")
				}
				if !strings.Contains(strings.ToLower(stealth), "enabled") && !strings.Contains(strings.ToLower(stealth), "on") {
					bad = append(bad, "stealth mode is off")
				}
				if len(bad) > 0 {
					return Fail, strings.Join(bad, "; ")
				}
				return OK, "the firewall is on, in stealth mode"
			},
			Fix: &Fix{Cmds: []Cmd{
				{Sudo: true, Argv: []string{"/usr/libexec/ApplicationFirewall/socketfilterfw", "--setglobalstate", "on"}},
				{Sudo: true, Argv: []string{"/usr/libexec/ApplicationFirewall/socketfilterfw", "--setstealthmode", "on"}},
			}},
		},

		{
			Name: "ssh-keys-only", Phase: PhaseHost, Step: 8, Title: "SSH with keys only (manual step 8)",
			Run: func(context.Context) (Status, string) {
				if d.GOOS != "darwin" {
					return NotVerified, "not checked: " + errNotHere.Error()
				}
				b, err := os.ReadFile(d.sshdPath())
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return NotVerified, "could not read " + d.sshdPath() + ": " + oneLine(err.Error())
				}
				if err != nil {
					return Fail, d.sshdPath() + " is not there: password logins are not refused"
				}
				if !strings.Contains(string(b), "PasswordAuthentication no") || !strings.Contains(string(b), "KbdInteractiveAuthentication no") {
					return Fail, d.sshdPath() + " does not turn both password methods off"
				}
				return OK, "password logins are refused (" + d.sshdPath() + ")"
			},
			Fix: &Fix{
				Desc:  "write the two settings to a private temporary file, then install it as root's",
				Do:    func(context.Context, Prompter) error { return writeTemp(sshdTemp(), sshdContent) },
				Cmds:  []Cmd{{Sudo: true, Argv: []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", sshdTemp(), sshdFile}}},
				Guide: "Remote Login itself is switched on in System Settings → General → Sharing → Remote Login, for your administrator account only; it cannot be checked here without a privileged command, so it is not verified.",
				Open:  "x-apple.systempreferences:com.apple.Sharing-Settings.extension",
			},
		},

		{
			Name: "filevault", Phase: PhaseHost, Step: 3, Title: "FileVault on (manual step 3)",
			Run: func(ctx context.Context) (Status, string) {
				out, err := d.output(ctx, "fdesetup", "status")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "fdesetup did not answer: " + oneLine(err.Error())
				}
				if strings.Contains(out, "FileVault is On") {
					return OK, "FileVault is on"
				}
				return Fail, "FileVault is off"
			},
			Fix: &Fix{
				Guide: "Enabling FileVault is interactive and prints a recovery key that must be yours alone, so whr does not run it. Run `sudo fdesetup enable` yourself in this terminal (or use System Settings → Privacy & Security → FileVault) and keep the key safe.",
				Open:  "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension",
			},
		},

		{
			Name: "homebrew", Phase: PhaseHost, Step: 5, Title: "Homebrew (manual step 5)",
			Run: func(ctx context.Context) (Status, string) {
				if _, err := d.output(ctx, "/opt/homebrew/bin/brew", "--version"); err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return Fail, "Homebrew is not installed at /opt/homebrew"
				}
				return OK, "Homebrew is installed"
			},
			Fix: &Fix{
				Guide: "Install the Xcode Command Line Tools (`xcode-select --install`) and Homebrew from https://brew.sh as your administrator. Its installer is a script from the internet, so whr does not run it for you.",
				Open:  "https://brew.sh",
			},
		},

		{
			Name: "brew-packages", Phase: PhaseHost, Step: 5, Title: "the host packages of the Brewfile (manual step 5)",
			Run: func(ctx context.Context) (Status, string) {
				var missing []string
				for _, f := range []string{"container", "git", "gh"} {
					out, err := d.output(ctx, "/opt/homebrew/bin/brew", "list", "--formula", "--versions", f)
					if err != nil || strings.TrimSpace(out) == "" {
						if st, msg, ok := notHere(err); ok {
							return st, msg
						}
						// brew list exits 1 for a formula that is not installed;
						// any other failure says nothing about the package
						if err != nil && !commandExitIs(err, 1) {
							return NotVerified, "brew did not say whether " + f + " is installed: " + oneLine(err.Error())
						}
						missing = append(missing, f)
					}
				}
				if len(missing) > 0 {
					return Fail, "not installed: " + strings.Join(missing, ", ")
				}
				return OK, "container, git and gh are installed"
			},
			Fix: &Fix{
				Desc: "write the Brewfile to a temporary file, then brew bundle it",
				Do: func(context.Context, Prompter) error {
					return writeTemp(brewfilePath(), brewfile)
				},
				Cmds: []Cmd{{Argv: []string{"/opt/homebrew/bin/brew", "bundle", "--file=" + brewfilePath()}}},
			},
		},

		{
			Name: "brew-pin", Phase: PhaseHost, Step: 6, Title: "container pinned, so brew upgrade leaves it (manual step 5)",
			Run: func(ctx context.Context) (Status, string) {
				out, err := d.output(ctx, "/opt/homebrew/bin/brew", "list", "--pinned")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "brew did not answer: " + oneLine(err.Error())
				}
				for _, l := range strings.Fields(out) {
					if l == "container" {
						return OK, "container is pinned"
					}
				}
				return Fail, "container is not pinned"
			},
			Fix: &Fix{Cmds: []Cmd{{Argv: []string{"/opt/homebrew/bin/brew", "pin", "container"}}}},
		},

		{
			Name: "prefix", Phase: PhaseHost, Step: 13, Title: prefixTitle(d),
			Run: func(ctx context.Context) (Status, string) {
				if d.Dev {
					return d.developmentPrefix(ctx)
				}
				fi, err := os.Stat(d.prefix())
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return NotVerified, "could not read " + d.prefix() + ": " + oneLine(err.Error())
				}
				if err != nil {
					return Fail, d.prefix() + " does not exist"
				}
				if !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
					return Fail, d.prefix() + " is not a directory only its owner can write"
				}
				for _, p := range []string{d.prefix(), filepath.Join(d.prefix(), "bin"), filepath.Join(d.prefix(), "bin", "whr")} {
					own, err := ownedBy(p, d.account())
					if errors.Is(err, fs.ErrNotExist) {
						continue // not installed yet
					}
					if err != nil {
						return NotVerified, "could not read the owner of " + p + ": " + oneLine(err.Error())
					}
					// never the configured account, administrator or not: it could
					// replace its own supervisor, and D24 would not come back after
					// drop-admin (D49)
					if own {
						return Fail, p + " belongs to " + d.account() + ", the account the supervisor runs as, which could then replace its own supervisor: it must belong to root or another administrator"
					}
					if fi, err := os.Lstat(p); err == nil && fi.Mode().Perm()&0o022 != 0 {
						return Fail, p + " can be written by others than its owner"
					}
				}
				return OK, d.prefix() + " exists and only the administrator writes it"
			},
			Fix: &Fix{
				Cmds:  prefixInstallCommands(d),
				Guide: prefixInstallGuide(d),
			},
		},

		{
			Name: "screen-sharing", Phase: PhaseHost, Step: 8, Title: "Screen Sharing for whr's desktop session (manual steps 2 and 8)", Optional: true,
			Run: func(context.Context) (Status, string) {
				return NotVerified, "macOS keeps this under privacy controls (TCC) that a command cannot read or set"
			},
			Fix: &Fix{
				Guide: "System Settings → General → Sharing → Screen Sharing: allow it only for your administrator and whr, if you want to reach whr's desktop session from afar.",
				Open:  "x-apple.systempreferences:com.apple.Sharing-Settings.extension",
			},
		},

		{
			Name: "tailscale", Phase: PhaseHost, Step: 7, Title: "Tailscale signed in (manual step 7)", Optional: true,
			Run: func(context.Context) (Status, string) {
				return NotVerified, "signing in is the human's; whr does not check a third party's state"
			},
			Fix: &Fix{
				Guide: "Open the Tailscale app, sign in, and forward whr's name to its loopback port with HTTPS (`tailscale serve`, manual step 7); or use another option of that step.",
				Open:  "https://login.tailscale.com",
			},
		},
	}
}

func orNone(s string) string {
	if s == "" {
		return "unset"
	}
	return s
}

// setupDir is where files wait before a command reads them: a 0700 directory
// of the administrator in their own home, never the shared temporary directory,
// where another account could create it first and swap a file that root then
// installs.
func setupDir() string {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		home = "/nonexistent" // writeTemp then fails; nothing is written elsewhere
	}
	return filepath.Join(home, "Library", "Caches", "whr-setup")
}

// sshdTemp is where the sshd settings wait before sudo install copies them.
func sshdTemp() string { return filepath.Join(setupDir(), "100-whr.conf") }

func brewfilePath() string { return filepath.Join(setupDir(), "Brewfile") }

// writeTemp writes a file the next command reads. The directory must be a real
// directory of this user with mode 0700, so no other account can enter it, and
// the file is created anew, never through a symbolic link. It holds no secret,
// so an earlier copy is replaced.
func writeTemp(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // the path is the private setup directory checked above
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// privateDir refuses a directory that is a link, belongs to another user or
// can be entered by anyone else.
func privateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	switch {
	case !fi.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return fmt.Errorf("%s does not belong to you", dir)
	case fi.Mode().Perm() != 0o700:
		return fmt.Errorf("%s has mode %o, want 700", dir, fi.Mode().Perm())
	}
	return nil
}

// prefixInstallArgv creates the prefix. When the account running setup is the
// configured account (an administrator whr, D49) it must not own the prefix, so
// root does; otherwise the running administrator does.
func (d Deps) prefixInstallArgv() []string {
	if d.User == d.account() {
		return []string{"install", "-d", "-o", "root", "-g", "wheel", "-m", "755", d.prefix()}
	}
	return []string{"install", "-d", "-o", d.User, "-g", "admin", "-m", "755", d.prefix()}
}

// ownedBy reports whether a path belongs to the named account, which must
// never own what runs the supervisor (D24, D49).
func ownedBy(path, name string) (bool, error) {
	u, err := user.Lookup(name)
	var unknown user.UnknownUserError
	if errors.As(err, &unknown) {
		return false, nil // no such user yet: nothing it could own
	}
	if err != nil {
		return false, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && strconv.FormatUint(uint64(st.Uid), 10) == u.Uid, nil
}

// WriteSecret writes a secret file: mode 0600, exclusive create, so it never
// overwrites one, and the directory it lies in must be private. It never prints
// the value.
func WriteSecret(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the secret's own path, in the user's configuration directory
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists: not overwritten", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

var appKeyRE = regexp.MustCompile(`^github-app-([0-9]+)\.pem$`)

// appKey finds the private key `whr github app create` wrote and the App ID in
// its name. More than one is ambiguous.
func appKey(dir string) (id int64, path string, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, "", err
	}
	for _, e := range ents {
		if m := appKeyRE.FindStringSubmatch(e.Name()); m != nil {
			if path != "" {
				return 0, "", errors.New("more than one github-app-<id>.pem is in " + dir + ": keep the one in use")
			}
			id, _ = strconv.ParseInt(m[1], 10, 64)
			path = filepath.Join(dir, e.Name())
		}
	}
	if path == "" {
		return 0, "", fs.ErrNotExist
	}
	return id, path, nil
}

func userSteps(d Deps) []Check {
	dir := d.configDir()
	tokenPath := filepath.Join(dir, "api.token")
	envPath := filepath.Join(dir, "agent.env")
	caPath := filepath.Join(dir, "ssh-ca")
	return ordered([]Check{
		{
			Name: "container-start", Phase: PhaseUser, Step: 4, Provides: serviceContainerSystem, Title: "the container system running (manual step 6)",
			Run: func(ctx context.Context) (Status, string) {
				if st, msg, ok := d.inDesktop(ctx); !ok {
					return st, msg
				}
				out, err := d.output(ctx, "container", "system", "status")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return Fail, "the container system is not running"
				}
				if strings.Contains(out, "running") {
					return OK, "the container system is running"
				}
				return Fail, "the container system is not running"
			},
			Fix: &Fix{
				Cmds:  []Cmd{{Argv: []string{"container", "system", "start", "--disable-kernel-install"}}},
				Guide: "Run this in " + d.desktopSession() + ", not over SSH.",
			},
		},
		{
			Name: "container-kernel", Phase: PhaseUser, Step: 4, Title: "the Linux kernel containers boot (manual step 6)",
			Needs: serviceContainerSystem,
			Run: func(ctx context.Context) (Status, string) {
				if st, msg, ok := d.inDesktop(ctx); !ok {
					return st, msg
				}
				out, err := d.output(ctx, "container", "system", "status")
				if err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return NotVerified, "whether the kernel is installed is not visible until the container system runs (step container-start)"
				}
				if !strings.Contains(out, "running") {
					return NotVerified, "whether the kernel is installed is not visible until the container system runs (step container-start)"
				}
				// Where `container system kernel set` puts a kernel is unverified
				// (issue #73): the start fix skips the kernel install, so a running
				// system says nothing about it.
				kernels := filepath.Join(d.Home, "Library", "Application Support", "com.apple.container", "kernels")
				ents, _ := os.ReadDir(kernels)
				if len(ents) == 0 {
					return Fail, "no Linux kernel is installed: containers cannot boot without one (read " + kernels + ", a location that is unverified until issue #73; if a kernel is installed elsewhere, tell issue #73)"
				}
				return OK, "a Linux kernel is installed (found in " + kernels + ", a location that is unverified until issue #73)"
			},
			Fix: &Fix{
				Cmds:  []Cmd{{Argv: []string{"container", "system", "kernel", "set", "--recommended"}}},
				Guide: "This must run in " + d.desktopSession() + ": the services live in that user's GUI launchd domain.",
			},
		},
		{
			Name: "standard-user-check", Phase: PhaseUser, Step: 4, Title: "containers answer for this standard user (manual steps 2 and 6)",
			Run: func(ctx context.Context) (Status, string) {
				if st, msg, ok := d.inDesktop(ctx); !ok {
					return st, msg
				}
				if _, err := d.output(ctx, "container", "list", "--all"); err != nil {
					if st, msg, ok := notHere(err); ok {
						return st, msg
					}
					return Fail, "container list failed: " + oneLine(err.Error()) + " (tell issue #38 before making this user an administrator)"
				}
				out, _ := d.output(ctx, "launchctl", "print", "gui/"+strconv.Itoa(d.UID))
				if !strings.Contains(out, "com.apple.container") {
					return Fail, "the container services are not in this user's GUI launchd domain (gui/" + strconv.Itoa(d.UID) + ")"
				}
				return OK, "the container services answer in gui/" + strconv.Itoa(d.UID)
			},
			Fix: &Fix{Guide: "Run `container system start --disable-kernel-install` in " + d.desktopSession() + ", not over SSH or sudo. If it fails with a permission or bootstrap error for this standard user, note the message in issue #38."},
		},

		{
			Name: "config-dir", Phase: PhaseUser, Step: 4, Title: "the private configuration directory (manual step 12)",
			Run: func(context.Context) (Status, string) {
				fi, err := os.Stat(dir)
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return NotVerified, "could not read " + dir + ": " + oneLine(err.Error())
				}
				if err != nil {
					return Fail, dir + " does not exist"
				}
				if fi.Mode().Perm()&0o077 != 0 {
					return Fail, dir + " can be entered by others"
				}
				return OK, dir + " is private"
			},
			Fix: &Fix{Desc: "make " + dir + " with mode 0700", Do: func(context.Context, Prompter) error {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return err
				}
				return os.Chmod(dir, 0o700) //nolint:gosec // a directory needs the execute bit
			}},
		},

		{
			Name: "api-token", Phase: PhaseUser, Step: 1, Title: "the API token (manual step 12)",
			Run: func(context.Context) (Status, string) {
				if _, err := config.ReadSecret(tokenPath); err != nil {
					return Fail, oneLine(err.Error())
				}
				return OK, tokenPath + " is a private file"
			},
			Fix: &Fix{Desc: "generate a random token into " + tokenPath + " (0600, never overwritten, never shown)", Do: func(context.Context, Prompter) error {
				b := make([]byte, 32)
				if _, err := rand.Read(b); err != nil {
					return err
				}
				return WriteSecret(tokenPath, []byte(base64.StdEncoding.EncodeToString(b)+"\n"))
			}},
		},

		{
			Name: "agent-key", Phase: PhaseUser, Step: 3, Title: "an agent API key (optional; a subscription needs none, D40)", Optional: true,
			Run: func(context.Context) (Status, string) {
				if _, err := os.Stat(envPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return NotVerified, "could not read " + envPath + ": " + oneLine(err.Error())
				} else if err != nil {
					return OK, "no API key: the agent signs in inside the environment (subscription)"
				}
				if _, err := (&config.Config{AgentAPIKeyEnvFile: envPath}).AgentAPIKey(); err != nil {
					return Fail, oneLine(err.Error())
				}
				return OK, envPath + " is a private file"
			},
			Fix: &Fix{Desc: agentKeyPrompt + "; written to " + envPath + " (0600, never overwritten, never shown)", Do: func(_ context.Context, p Prompter) error {
				key, err := p.Secret(agentKeyPrompt)
				if err != nil {
					return err
				}
				key = strings.TrimSpace(key)
				if err := credcheck.Check("ANTHROPIC_API_KEY", key); err != nil {
					return errors.New("refused: " + err.Error() + "; nothing was written. " + credcheck.Advice)
				}
				if len(key) < 20 || strings.ContainsAny(key, " \t\r\n=") {
					return errors.New("that does not look like an API key; nothing was written")
				}
				return WriteSecret(envPath, []byte("ANTHROPIC_API_KEY="+key+"\n"))
			}},
		},

		{
			Name: "ssh-ca", Phase: PhaseUser, Step: 3, Title: "the console's SSH certificate authority (optional; for whr ssh, issue #32)", Optional: true,
			Run: func(context.Context) (Status, string) {
				if _, err := os.Stat(caPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return NotVerified, "could not read " + caPath + ": " + oneLine(err.Error())
				} else if err != nil {
					return OK, "no SSH authority: `whr ssh` is off"
				}
				if _, err := sshca.Load(caPath); err != nil {
					return Fail, oneLine(err.Error())
				}
				return OK, caPath + " is a private authority key; set console.ssh_ca_key_file to it in the configuration to turn `whr ssh` on"
			},
			Fix: &Fix{Desc: "generate an Ed25519 authority key into " + caPath + " (0600, never overwritten, never shown); add \"console\": {\"ssh_ca_key_file\": \"" + caPath + "\"} to the configuration to use it", Do: func(context.Context, Prompter) error {
				return sshca.Generate(caPath)
			}},
		},

		{
			Name: "github-app", Phase: PhaseUser, Step: 2, Title: "your own GitHub App (manual step 11)",
			Run: func(context.Context) (Status, string) {
				if id, path, err := appKey(dir); err == nil {
					return OK, fmt.Sprintf("App %d: its key is %s; installing it on your repositories and the ruleset check stay yours (whr doctor checks the installation)", id, path)
				}
				return Fail, "no GitHub App key in " + dir
			},
			Fix: &Fix{
				Cmds: []Cmd{{Argv: []string{d.Whr, "github", "app", "create", "--config", d.ConfigPath, "--public-url", "<https name>"}}},
				Build: func(_ context.Context, p Prompter) ([]Cmd, error) {
					u, err := p.Line("whr's HTTPS name behind the forwarder (manual step 7), for example https://whr.example.ts.net")
					if err != nil {
						return nil, err
					}
					u = strings.TrimSpace(u)
					if !strings.HasPrefix(u, "https://") || strings.ContainsAny(u, " \t\r\n") {
						return nil, errors.New("that is not an https address; nothing was run")
					}
					return []Cmd{{Argv: []string{d.Whr, "github", "app", "create", "--config", d.ConfigPath, "--public-url", u}}}, nil
				},
				Guide: "whr github app create prints a link: open it, press Continue to GitHub and confirm. Then install the App on your selected repositories and check that main's ruleset does not list it as a bypass actor (D15). The command asks for --public-url: the HTTPS name your forwarder gives whr (manual step 7).",
				Open:  "https://github.com/settings/apps",
			},
		},

		{
			Name: "config-base", Phase: PhaseUser, Step: 1, Title: "the base configuration: listen, token, roots, repositories (manual step 13)",
			Run: func(context.Context) (Status, string) {
				m, err := readConfigMap(d.ConfigPath)
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return Fail, d.ConfigPath + " does not exist"
					}
					return Fail, oneLine(err.Error())
				}
				for _, k := range []string{"listen", "api_token_file", "roots", "repositories"} {
					if _, ok := m[k]; !ok {
						return Fail, d.ConfigPath + " lacks " + k
					}
				}
				return OK, d.ConfigPath + " has the base settings"
			},
			Fix: &Fix{Desc: "ask for the repository and the folders, then write " + d.ConfigPath + " (0600, never overwritten)", Do: func(_ context.Context, p Prompter) error {
				return writeConfigBase(d, p, tokenPath, envPath)
			}},
		},
		{
			Name: "config-github", Phase: PhaseUser, Step: 2, Title: "the GitHub App in the configuration (manual step 13)",
			Run: func(context.Context) (Status, string) {
				if _, err := config.Load(d.ConfigPath); err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return Fail, d.ConfigPath + " does not exist"
					}
					return Fail, problems(err)
				}
				return OK, d.ConfigPath + " is valid"
			},
			Fix: &Fix{Desc: "add github.app_id and github.key_file to " + d.ConfigPath + " as a diff, after a y, written atomically, keeping every other key", Do: func(_ context.Context, p Prompter) error {
				return addGitHub(d, p)
			}},
		},
		{
			Name: "tool-store", Phase: PhaseUser, Step: 4, Title: "the tool store with Claude Code (manual step 13)",
			Run: func(context.Context) (Status, string) {
				c, err := config.Load(d.ConfigPath)
				if err != nil {
					return Fail, "needs a valid configuration (the config-file step)"
				}
				ents, err := os.ReadDir(filepath.Join(c.Roots.ToolStore, "profiles"))
				if err != nil || len(ents) == 0 {
					return Fail, "the tool store " + c.Roots.ToolStore + " has no profile"
				}
				// The tools are what every environment runs: check them against the
				// hashes recorded when they were installed.
				var severe []string
				unchecked := 0
				for _, p := range (&toolstore.Store{Root: c.Roots.ToolStore}).Verify() {
					if p.Severe {
						severe = append(severe, p.String())
					} else {
						unchecked++
					}
				}
				if len(severe) > 0 {
					return Fail, "the tool store does not verify: " + oneLine(strings.Join(severe, "; "))
				}
				if unchecked > 0 {
					return OK, fmt.Sprintf("the tool store has a profile; %d tool(s) have no full hash recorded and were checked only by the hash in their name", unchecked)
				}
				return OK, "the tool store has a profile and every tool matches its recorded hash"
			},
			Fix: &Fix{
				Cmds: []Cmd{{Argv: []string{d.Whr, "tools", "build", "-store", "<tool_store>", "-shim", filepath.Join(d.prefix(), "libexec", "whr", "whr-shim-linux-arm64")}}},
				Build: func(context.Context, Prompter) ([]Cmd, error) {
					c, err := config.Load(d.ConfigPath)
					if err != nil {
						return nil, errors.New("needs a valid configuration first (config-base, github-app, config-github)")
					}
					return []Cmd{{Argv: []string{d.Whr, "tools", "build", "-store", c.Roots.ToolStore, "-shim", filepath.Join(d.prefix(), "libexec", "whr", "whr-shim-linux-arm64")}}}, nil
				},
			},
		},

		d.developmentKeyStep(),
		{
			Name: "service-install", Phase: PhaseUser, Step: 4, Title: "whr serve as a LaunchAgent (manual step 13)",
			Run: func(ctx context.Context) (Status, string) {
				if d.Runner == nil || d.GOOS != "darwin" {
					return NotVerified, "not checked: " + errNotHere.Error()
				}
				m := launchd.Manager{R: runnerAdapter{d.Runner}, UID: d.UID, GOOS: d.GOOS}
				st, err := m.Status(ctx, launchd.Spec{Label: launchd.Label, Home: d.Home})
				if err != nil {
					return NotVerified, oneLine(err.Error())
				}
				if !st.Loaded {
					return Fail, "the job is not loaded"
				}
				return OK, "the job is loaded (" + st.State + ")"
			},
			Fix: &Fix{Cmds: []Cmd{{Argv: serviceInstallArgv(d)}}},
		},
		dropAdmin(d),
	}, userOrder)
}

// userOrder is the order of `whr setup`: nothing needs a later step, so there is
// no cycle (the base configuration comes before the App, which comes before the
// configuration that names it; the container system starts before the kernel is
// set, because `container system kernel set` needs the running system). A step
// that needs a service says so with Needs, and a test holds the order to it.
var userOrder = []string{
	"config-dir", "api-token", "agent-key", "ssh-ca", "container-start", "container-kernel", "standard-user-check",
	"config-base", "development-key", "github-app", "config-github", "tool-store", "service-install", "drop-admin",
}

// ordered returns the checks in the given order. A name with no check is a bug
// in this package, caught by its tests.
func ordered(checks []Check, order []string) []Check {
	by := map[string]Check{}
	for _, c := range checks {
		by[c.Name] = c
	}
	out := make([]Check, 0, len(order))
	for _, n := range order {
		if c, ok := by[n]; ok {
			out = append(out, c)
		}
	}
	return out
}

// runnerAdapter lets the launchd manager ask launchctl through the same Runner.
type runnerAdapter struct{ r Runner }

func (a runnerAdapter) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return a.r.Output(ctx, append([]string{name}, args...)...)
}

func marshalConfig(m map[string]any) ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

func readConfigMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the operator's own configuration file
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s is not JSON: %w", path, err)
	}
	return m, nil
}

// writeConfigBase asks for what `whr github app create` and the rest need before
// the App exists, and writes a new file. It never overwrites one. It is not the
// whole configuration: github comes later (config-github), so it is not validated
// as one.
func writeConfigBase(d Deps, p Prompter, tokenPath, envPath string) error {
	repo, err := p.Line("Repository to work on (owner/name)")
	if err != nil {
		return err
	}
	ws, err := p.Line("Folder for workspaces [" + filepath.Join(d.Home, "workspaces") + "]")
	if err != nil {
		return err
	}
	if strings.TrimSpace(ws) == "" {
		ws = filepath.Join(d.Home, "workspaces")
	}
	store := filepath.Join(d.Home, "tools")
	acct, err := p.Line("Is " + d.account() + " dedicated to workharbor, or your own account that you also work in (D49)? [dedicated/shared, default dedicated]")
	if err != nil {
		return err
	}
	acct = strings.ToLower(strings.TrimSpace(acct))
	if acct == "" {
		acct = config.AccountDedicated
	}
	if acct != config.AccountDedicated && acct != config.AccountShared {
		return errors.New("that is not dedicated or shared; nothing was written")
	}
	m := map[string]any{
		"account":             acct,
		"listen":              "127.0.0.1:8787",
		"repositories":        []map[string]any{{"name": strings.TrimSpace(repo)}},
		"roots":               map[string]any{"workspaces": []string{strings.TrimSpace(ws)}, "tool_store": store},
		"api_token_file":      tokenPath,
		"agent_allowed_tools": []string{"Read", "Edit", "Write", "Bash(git status:*)", "Bash(make check:*)"},
	}
	if _, err := os.Stat(envPath); err == nil {
		m["agent_api_key_env_file"] = envPath
	}
	if !ownerNameRE.MatchString(strings.TrimSpace(repo)) {
		return errors.New("that is not owner/name; nothing was written")
	}
	for _, dd := range []string{strings.TrimSpace(ws), store} {
		if err := os.MkdirAll(dd, 0o700); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteSecret(d.ConfigPath, append(raw, '\n'))
}

var ownerNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// addGitHub adds the App to the configuration: the file is read as generic JSON,
// so a key this version does not know is kept, the change is shown as a diff,
// and the file is replaced atomically only after a y.
func addGitHub(d Deps, p Prompter) error {
	id, key, err := appKey(d.configDir())
	if err != nil {
		return errors.New("run the github-app step first: no github-app-<id>.pem in " + d.configDir())
	}
	m, err := readConfigMap(d.ConfigPath)
	if err != nil {
		return err
	}
	old, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	gh, _ := m["github"].(map[string]any)
	if gh == nil {
		gh = map[string]any{}
	}
	gh["app_id"], gh["key_file"] = id, key
	m["github"] = gh
	updated, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := validateKeepingUnknown(m); err != nil {
		return errors.New("the configuration would still not start, so it was not changed: " + problems(err))
	}
	p.Show(lineDiff(string(old), string(updated)))
	ok, err := p.Confirm("Write this change to " + d.ConfigPath)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("not written")
		}
		return err
	}
	return replaceFile(d.ConfigPath, append(updated, '\n'))
}

// lineDiff shows the lines that were added and removed, one per line.
func lineDiff(before, after string) string {
	seen := func(s string) map[string]int {
		m := map[string]int{}
		for _, l := range strings.Split(s, "\n") {
			m[l]++
		}
		return m
	}
	b, a := seen(before), seen(after)
	var out []string
	for _, l := range strings.Split(before, "\n") {
		if a[l] == 0 {
			out = append(out, "- "+l)
		} else {
			a[l]--
		}
	}
	for _, l := range strings.Split(after, "\n") {
		if b[l] == 0 {
			out = append(out, "+ "+l)
		} else {
			b[l]--
		}
	}
	return strings.Join(out, "\n")
}

// replaceFile replaces a file atomically with mode 0600: the new content is
// written beside it and renamed over it.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".whr-config-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := os.Chmod(tmp.Name(), 0o600); err != nil { //nolint:gosec // the configuration is private
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var unknownFieldRE = regexp.MustCompile(`unknown field "([^"]+)"`)

// validateKeepingUnknown checks a configuration the way `whr serve` would, but a
// top-level key this version does not know (a newer whr wrote it) does not stop
// the wizard: the key is kept in the file, and only the rest is validated.
func validateKeepingUnknown(m map[string]any) error {
	probe := map[string]any{}
	for k, v := range m {
		probe[k] = v
	}
	for range 20 {
		raw, err := json.Marshal(probe)
		if err != nil {
			return err
		}
		_, err = config.Parse(raw)
		if err == nil {
			return nil
		}
		match := unknownFieldRE.FindStringSubmatch(err.Error())
		if match == nil {
			return err
		}
		if _, top := probe[match[1]]; !top {
			return err
		}
		delete(probe, match[1])
	}
	return errors.New("too many unknown keys")
}

// internalMounts are the mount points of the Mac's own disk, which FileVault covers.
var internalMounts = map[string]bool{"/": true, "/System/Volumes/Data": true}

// dfMount reads the mount point out of `df -P`: the last line's sixth column on,
// since a mount point may hold spaces.
var dfMount = regexp.MustCompile(`(?m)^\S+\s+\d+\s+\d+\s+\d+\s+\d+%\s+(.+)$`)

// workspaceVolumes returns the mount points, other than the Mac's own disk, that
// hold a configured workspace root, without duplicates. Each root is resolved
// first, as the configuration does, so a link to an external disk counts as that
// disk. A status other than "" means the roots or their disks are not known (no
// configuration yet, a root that does not exist, a `df` that does not answer),
// which is not verified rather than a pass.
func (d Deps) workspaceVolumes(ctx context.Context) ([]string, Status, string) {
	// Only the roots are read, and not through config.Load: this check says what is
	// wrong with a volume even while the rest of the configuration is not valid yet.
	raw, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return nil, NotVerified, "the workspace roots are not known until the configuration is written: " + oneLine(err.Error())
	}
	var cfg struct {
		Roots struct {
			Workspaces []string `json:"workspaces"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, NotVerified, "the workspace roots could not be read from the configuration: " + oneLine(err.Error())
	}
	var vols []string
	seen := map[string]bool{}
	for _, root := range cfg.Roots.Workspaces {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, NotVerified, "the workspace root " + root + " cannot be resolved, so its disk is not known: " + oneLine(err.Error())
		}
		out, err := d.output(ctx, "df", "-P", resolved)
		if err != nil {
			return nil, NotVerified, "df did not say which disk " + resolved + " is on: " + oneLine(err.Error())
		}
		m := dfMount.FindStringSubmatch(out)
		if m == nil {
			return nil, NotVerified, "df's answer for " + resolved + " could not be read"
		}
		mount := strings.TrimSpace(m[1])
		if internalMounts[mount] || seen[mount] {
			continue
		}
		seen[mount] = true
		vols = append(vols, mount)
	}
	return vols, "", ""
}

// colonLines reads "Key: value" lines, as `diskutil info` prints them.
func colonLines(out string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func prefixInstallCommands(d Deps) []Cmd {
	if d.Dev {
		return nil
	}
	return []Cmd{{Sudo: true, Argv: d.prefixInstallArgv()}}
}

func serviceInstallArgv(d Deps) []string {
	args := []string{d.Whr, "service", "install", "--config", d.ConfigPath}
	if d.Dev {
		args = append(args, "--whr", d.Whr)
	}
	return args
}

func (d Deps) developmentPrefix(ctx context.Context) (Status, string) {
	if d.UID == 0 {
		return Fail, "whr never runs as root"
	}
	if !filepath.IsAbs(d.Home) { // without the home, "too broad" cannot be judged
		return Fail, "a development installation needs an absolute HOME: it is how the prefix is kept from holding the home directory"
	}
	// $HOME is the caller's word (`HOME=/tmp/x whr doctor --dev`, or one kept by
	// `sudo -u`): the account's home comes from the directory service too, and
	// without it the check fails closed (#278).
	accountHome, err := d.directoryHome(ctx)
	if err != nil {
		return Fail, "a development installation needs the account's home from the directory service, to keep the prefix from holding it: " + err.Error()
	}
	if err := launchd.CheckBinary(d.Whr); err != nil {
		return Fail, err.Error()
	}
	prefix, err := filepath.EvalSymlinks(d.prefix())
	if err != nil {
		return Fail, err.Error()
	}
	binary, err := filepath.EvalSymlinks(d.Whr)
	if err != nil {
		return Fail, err.Error()
	}
	rel, err := filepath.Rel(prefix, binary)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return Fail, binary + " is not under " + prefix
	}
	if prefix == "/" || prefix == filepath.Dir(prefix) || prefixHoldsDir(prefix, d.Home) || prefixHoldsDir(prefix, accountHome) {
		return Fail, prefix + " is too broad for a development prefix (the home directory and every directory above it are refused): name a directory of its own, such as " + filepath.Join(d.homeOrDefault(), ".local")
	}
	// The binary up to the prefix, then every directory above it: none may be
	// written by group or other (a sticky directory above the prefix, such as
	// /tmp, only lets an owner replace its own entries), and each belongs to
	// the account that runs whr or to root, so no other account can replace
	// the supervisor binary.
	inside := true
	for p := binary; ; p = filepath.Dir(p) {
		fi, err := os.Stat(p)
		if err != nil {
			return Fail, err.Error()
		}
		if fi.Mode().Perm()&0o022 != 0 && (inside || fi.Mode()&os.ModeSticky == 0) {
			return Fail, p + " can be written by others than its owner"
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != d.UID && st.Uid != 0 {
			return Fail, p + " belongs to another account than " + d.User + " or root"
		}
		if p == prefix {
			inside = false
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return Warn, prefix + ": development installation; a user-writable supervisor lacks managed-install replacement protection"
}

func prefixTitle(d Deps) string {
	if d.Dev {
		return "the development prefix " + d.prefix()
	}
	return "the admin-owned prefix " + d.prefix() + " (manual step 13, D24)"
}

func prefixInstallGuide(d Deps) string {
	if d.Dev {
		return "Install approved source with `make install` using the selected PREFIX, then run `whr doctor --dev` with the same --prefix."
	}
	return "Then install whr there from a draft release: `make install-release VERSION=<tag>` (manual step 13)."
}

// directoryHome is the home directory of the account that runs whr, from
// `/usr/bin/dscl . -read /Users/<user> NFSHomeDirectory`, run by its absolute
// path because $PATH is the caller's word as much as $HOME is (no shell; the
// runner's timeout and scrubbed environment apply). An error names why the answer
// is not an absolute path.
func (d Deps) directoryHome(ctx context.Context) (string, error) {
	out, err := d.output(ctx, "/usr/bin/dscl", ".", "-read", "/Users/"+d.User, "NFSHomeDirectory")
	if err != nil {
		return "", errors.New("dscl NFSHomeDirectory for " + d.User + " failed: " + err.Error())
	}
	_, val, ok := strings.Cut(out, "NFSHomeDirectory:")
	val = strings.TrimSpace(val)
	if !ok || !filepath.IsAbs(val) {
		return "", errors.New("dscl gave no absolute NFSHomeDirectory for " + d.User)
	}
	return val, nil
}

// statDir is how prefixHoldsDir looks at the file system; a test swaps it to
// stand in for a spelling no temporary directory has (a firmlink).
var statDir = os.Stat

// prefixHoldsDir reports whether prefix is dir or a directory above it. It
// compares by identity, never by spelling: it stats prefix once, then dir and
// every directory above it, and answers true when os.SameFile matches. So
// `/users` on a case-insensitive volume, or `/System/Volumes/Data/Users`
// through the firmlink, is refused as `/Users` is (D24, #278). A directory
// that does not exist is skipped (it cannot be the prefix, and its parents are
// still walked); any other stat error, and an empty or relative dir, fails
// closed. #276's file checks reuse it.
func prefixHoldsDir(prefix, dir string) bool {
	if dir == "" {
		return false
	}
	pfi, err := statDir(prefix)
	if err != nil {
		return true
	}
	if !filepath.IsAbs(dir) {
		return true
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		fi, err := statDir(p)
		switch {
		case err == nil:
			if os.SameFile(pfi, fi) {
				return true
			}
		case !errors.Is(err, fs.ErrNotExist):
			return true
		}
		if p == filepath.Dir(p) {
			return false
		}
	}
}

func (d Deps) homeOrDefault() string {
	if d.Home != "" {
		return d.Home
	}
	return "$HOME"
}

// agentKeyPrompt says what the agent-key step asks for: an API key, never a
// subscription login (D40, issue #348).
const agentKeyPrompt = "Agent API key from the vendor's console (ANTHROPIC_API_KEY; not a `claude setup-token` or login token; not echoed)"
