package doctor

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/config"
)

// account is the account workharbor runs as: --user, WhrUser by default.
func (d Deps) account() string {
	if d.Account != "" {
		return d.Account
	}
	return WhrUser
}

// Membership asks `dseditgroup -o checkmember` whether user is in the admin
// group. known is false when the answer is not one of yes and "NOT a member"
// (unverified output format on macOS 26), so a caller never takes silence for a
// standard account.
func Membership(ctx context.Context, r Runner, user string) (admin, known bool, err error) {
	b, err := r.Output(ctx, "dseditgroup", "-o", "checkmember", "-m", user, "admin")
	text := strings.TrimSpace(string(b))
	if err != nil {
		text += " " + err.Error()
	}
	switch {
	case strings.HasPrefix(strings.TrimSpace(string(b)), "yes"):
		return true, true, nil
	case strings.Contains(text, "NOT a member"):
		return false, true, nil
	}
	return false, false, err
}

// isAdmin reports whether the account is an administrator; known is false when
// that could not be read.
func (d Deps) isAdmin(ctx context.Context) (admin, known bool) {
	if d.Runner == nil || d.GOOS != "darwin" {
		return false, false
	}
	a, k, _ := Membership(ctx, d.Runner, d.account())
	return a, k
}

// otherAdmins lists the members of the admin group except root and the
// account itself, from `dscl . -read /Groups/admin GroupMembership`. An answer
// it cannot read is an error, never "none".
func (d Deps) otherAdmins(ctx context.Context) ([]string, error) {
	out, err := d.output(ctx, "dscl", ".", "-read", "/Groups/admin", "GroupMembership")
	if err != nil {
		return nil, err
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(out), "GroupMembership:")
	if !ok {
		return nil, errors.New("dscl's answer for the admin group is not one this check knows")
	}
	var others []string
	for _, m := range strings.Fields(rest) {
		if m != "root" && m != d.account() {
			others = append(others, m)
		}
	}
	return others, nil
}

// accountView is what the account checks read of the configuration. They read
// the file as generic JSON and not through config.Load, because the file is
// partial until the last setup steps and these checks run all along; a value
// of a wrong type reads as absent.
type accountView struct{ shared, remote bool }

func (d Deps) accountConfig() (accountView, error) {
	m, err := readConfigMap(d.ConfigPath)
	if err != nil {
		return accountView{}, err
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	sub := func(k string) map[string]any { v, _ := m[k].(map[string]any); return v }
	// remote access (D49): a forwarder's public URL (the web UI over the VPN,
	// D29) or the console's SSH authority (`whr ssh`); the tailscale step cannot
	// be read and does not count
	remote := str(m, "public_url") != "" || str(sub("board"), "public_url") != "" || str(sub("console"), "ssh_ca_key_file") != ""
	return accountView{shared: str(m, "account") == config.AccountShared, remote: remote}, nil
}

// accountCheck is the shared `account` check (D49): the account whr runs as is
// a dedicated standard user (ok), or an administrator or the developer's own
// (warn, and fail when whr is also reachable from another device).
// hostRemote lists the host's own remote logins that are on: Remote Login
// (sshd) and Screen Sharing, read from launchd's system domain. A job that
// launchctl cannot print counts as off, so an unreadable answer never makes
// the check fail; the labels and that reading are unverified on macOS 26.
func (d Deps) hostRemote(ctx context.Context) []string {
	var on []string
	for label, name := range map[string]string{"com.openssh.sshd": "Remote Login", "com.apple.screensharing": "Screen Sharing"} {
		if out, err := d.output(ctx, "launchctl", "print", "system/"+label); err == nil && strings.TrimSpace(out) != "" {
			on = append(on, name)
		}
	}
	sort.Strings(on)
	return on
}

func (d Deps) accountCheck() func(context.Context) (Status, string) {
	return func(ctx context.Context) (Status, string) {
		if d.Runner == nil || d.GOOS != "darwin" {
			return NotVerified, "not checked: " + errNotHere.Error()
		}
		acct := d.account()
		if acct == "root" {
			return Fail, "workharbor never runs as root"
		}
		admin, known := d.isAdmin(ctx)
		if !known {
			return NotVerified, "dseditgroup did not say whether " + acct + " is an administrator (its output format is unverified on macOS 26)"
		}
		c, err := d.accountConfig()
		if err != nil && !admin {
			return NotVerified, acct + " is a standard user; " + needsConfig + " to know whether it is dedicated"
		}
		if err != nil {
			return NotVerified, acct + " is an administrator; " + needsConfig + " to know whether it is shared and whether whr is reachable from afar"
		}
		shared := c.shared
		if !admin && !shared {
			return OK, acct + " is a dedicated standard user"
		}
		var what []string
		if admin {
			what = append(what, "an administrator")
		}
		if shared {
			what = append(what, "your own account, shared with your daily work")
		}
		desc := acct + " is " + strings.Join(what, " and ")
		var gives []string
		if shared {
			gives = append(gives, "every process you run can read the API token and drive the supervisor")
		}
		gives = append(gives, "the account can replace the whr binary in the prefix")
		gave := " It gives up that " + strings.Join(gives, " and that ") + "."
		if on := d.hostRemote(ctx); c.remote || len(on) > 0 {
			via := "whr is configured to be reached from other devices (public_url or the console's SSH)"
			if len(on) > 0 {
				via = "the Mac's " + strings.Join(on, " and ") + " is on"
				if c.remote {
					via += " and whr is configured to be reached from other devices"
				}
			}
			return Fail, desc + ", and " + via + ": an agent's escape or a stolen token would reach the host." + gave + " Use a dedicated standard account (D49)"
		}
		hint := ""
		if admin && !shared {
			hint = "; `whr setup --only drop-admin` removes the privilege after setup"
		}
		return Warn, desc + ", with no remote access configured: allowed, weaker than a dedicated standard user (D49)." + gave + hint
	}
}

// dropAdmin is the last step of `whr setup` (D49): remove the account from the
// admin group once setup no longer needs it.
func dropAdmin(d Deps) Check {
	acct := d.account()
	drop := []Cmd{
		{Sudo: true, Argv: []string{"dseditgroup", "-o", "edit", "-d", acct, "-t", "user", "admin"}},
		{Sudo: true, Argv: []string{"-k"}},
	}
	return Check{
		Name: "drop-admin", Phase: PhaseUser, Step: 2, FixOnWarn: true,
		Title: "remove " + acct + " from the administrator group (manual step 2, D49)",
		Run: func(ctx context.Context) (Status, string) {
			if d.Runner == nil || d.GOOS != "darwin" {
				return NotVerified, "not checked: " + errNotHere.Error()
			}
			admin, known := d.isAdmin(ctx)
			if !known {
				return NotVerified, "dseditgroup did not say whether " + acct + " is an administrator"
			}
			if !admin {
				return OK, acct + " is a standard user: nothing to drop"
			}
			if c, err := d.accountConfig(); err != nil {
				return NotVerified, needsConfig + " to know whether " + acct + " is your own account"
			} else if c.shared {
				return Skipped, "not offered: " + acct + " is your own account (account: shared), and keeps its administrator group"
			}
			others, err := d.otherAdmins(ctx)
			if err != nil {
				return Skipped, "refused: the other administrators could not be read (" + oneLine(err.Error()) + "), and dropping the only one would lock you out of the Mac"
			}
			if len(others) == 0 {
				return Skipped, "refused: no other administrator than root and " + acct + " exists, so dropping it would lock you out of the Mac"
			}
			return Warn, acct + " is an administrator; " + others[0] + " can stay one"
		},
		Fix: &Fix{
			Cmds: drop,
			Build: func(ctx context.Context, _ Prompter) ([]Cmd, error) {
				// the same refusals again, after the confirmation
				if c, err := d.accountConfig(); err != nil || c.shared {
					return nil, errors.New("refused: the account is shared, or the configuration is not valid")
				}
				others, err := d.otherAdmins(ctx)
				if err != nil || len(others) == 0 {
					return nil, errors.New("refused: no other administrator was found, so dropping this one would lock you out of the Mac")
				}
				return drop, nil
			},
			Guide: "This removes only the administrator group membership (sudo, writes to /Applications and the Homebrew prefix); " + acct + "'s files and the supervisor are untouched. Log out and back in, or restart the Mac, afterwards: the desktop session and the whr serve LaunchAgent that already run keep the old group membership until then (unverified).",
		},
	}
}
