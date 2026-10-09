package doctor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wstein/workharbor/internal/textsafe"
)

// accountGroup is the primary group of a standard macOS account.
const accountGroup = "staff"

// accountHome is the home folder of the whr account in an administrator run
// (AccountHome overrides it for tests): the same /Users/<name> that the
// workspace-folders and system-config steps assume on macOS.
func (d Deps) accountHome() string {
	if d.AccountHome != "" {
		return d.AccountHome
	}
	return filepath.Join("/Users", d.account())
}

// accountConfigDir and accountConfigFile are where the whr account keeps its
// configuration (DefaultConfigPath of that account).
func (d Deps) accountConfigDir() string {
	return filepath.Join(d.accountHome(), ".config", "whr")
}

func (d Deps) accountConfigFile() string {
	return filepath.Join(d.accountConfigDir(), "config.json")
}

func accountConfigTemp() string { return filepath.Join(setupDir(), "account-config.json") }

// accountStage is where the administrator's run stages the account's file: a
// path under the root-owned /etc/whr (or the test override of the system
// config), never in a folder the account can change.
func (d Deps) accountStage() string {
	return filepath.Join(filepath.Dir(d.systemConfigFile()), ".account-config.json")
}

// accountConfigCmds are the commands the administrator runs for the whr
// account's base configuration. Root never writes inside the account's home:
// the folders are made as the account (`sudo -u`, so a link the account plants
// only reaches what it can write anyway) and the file is copied there as the
// account with `cp -n` (never replacing a file that appeared meanwhile) from a
// copy staged under the root-owned /etc/whr (owner the account, 0600). The
// system config is installed only when it is missing. Nothing from the
// account's home is run or read back (least privilege, D49).
func (d Deps) accountConfigCmds(withSystem bool) []Cmd {
	acct := d.account()
	sysDir := filepath.Dir(d.systemConfigFile())
	cmds := []Cmd{
		{Sudo: true, Argv: []string{"install", "-d", "-m", "0755", "-o", "root", "-g", "wheel", sysDir}},
		{Sudo: true, Argv: []string{"-u", acct, "/bin/mkdir", "-p", "-m", "0700", d.accountConfigDir()}},
		{Sudo: true, Argv: []string{"install", "-m", "0600", "-o", acct, "-g", accountGroup, accountConfigTemp(), d.accountStage()}},
		{Sudo: true, Argv: []string{"-u", acct, "/bin/cp", "-n", d.accountStage(), d.accountConfigFile()}},
		{Sudo: true, Argv: []string{"rm", "-f", d.accountStage()}},
	}
	if withSystem {
		cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", systemConfigTemp(), d.systemConfigFile()}})
	}
	return cmds
}

// accountConfigState says whether the whr account's file is there, as far as
// the administrator can see: "missing" is the only state the step writes in.
func (d Deps) accountConfigState() (state, why string) {
	if _, err := os.Lstat(d.accountHome()); err != nil {
		return "nohome", "the home folder " + d.accountHome() + " of " + d.account() + " does not exist yet (" + oneLine(err.Error()) + ")"
	}
	_, err := os.Lstat(d.accountConfigFile())
	switch {
	case err == nil:
		return "present", ""
	case errors.Is(err, fs.ErrNotExist):
		return "missing", ""
	}
	return "unknown", oneLine(err.Error())
}

// accountConfigFix is the administrator's config-first step for a separate
// account: it asks the same questions as config-base, then installs the file
// into the account's home and the system config next to it, which is where the
// administrator's own roots steps read the workspace roots (issue #510). An
// existing file is never touched, read or replaced.
func (d Deps) accountConfigFix() *Fix {
	cmds := d.accountConfigCmds(true)
	acct := d.account()
	dd := d
	dd.Home, dd.ConfigPath = d.accountHome(), d.accountConfigFile()
	return &Fix{
		Desc:      "ask for the repository and the folders, then install " + d.accountConfigFile() + " for " + acct + " (0600, never overwritten, written as the account) and the system config when it is missing",
		Cmds:      cmds,
		NeedsSudo: true,
		Build: func(ctx context.Context, p Prompter) ([]Cmd, error) {
			if st, why := d.accountConfigState(); st != "missing" {
				if st == "present" {
					return nil, errors.New(textsafe.Escape(d.accountConfigFile()) + " exists: not overwritten")
				}
				return nil, errors.New("not writing " + textsafe.Escape(d.accountConfigFile()) + ": " + why)
			}
			if err := d.checkAccountPath(); err != nil {
				return nil, err
			}
			dir := d.accountConfigDir()
			raw, _, err := composeConfigBase(ctx, dd, p, filepath.Join(dir, "api.token"), filepath.Join(dir, "agent.env"), true)
			if err != nil {
				return nil, err
			}
			sys, err := systemConfigOf(raw, acct)
			if err != nil {
				return nil, err
			}
			if err := writeTemp(accountConfigTemp(), string(raw)); err != nil {
				return nil, err
			}
			// the system config is written only when it is missing (same no-overwrite rule)
			withSystem := false
			if _, err := os.Lstat(d.systemConfigFile()); errors.Is(err, fs.ErrNotExist) {
				withSystem = true
				if err := writeTemp(systemConfigTemp(), string(sys)); err != nil {
					return nil, err
				}
			}
			// last look before the commands run: the state and the path as they are now
			if st, why := d.accountConfigState(); st != "missing" {
				return nil, errors.New("not writing " + textsafe.Escape(d.accountConfigFile()) + ": it changed while you answered (" + why + ")")
			}
			if err := d.checkAccountPath(); err != nil {
				return nil, err
			}
			return d.accountConfigCmds(withSystem), nil
		},
		Cleanup: func() {
			_ = os.Remove(accountConfigTemp())
			_ = os.Remove(systemConfigTemp())
		},
	}
}

// accountConfigStep is config-first in an administrator run for a separate
// account: whr setup initializes the account's base configuration itself.
// The check only looks at whether the file is there; it never reads it.
func (d Deps) accountConfigStep() Check {
	acct, file := d.account(), d.accountConfigFile()
	return Check{
		Name: "config-first", Phase: PhaseHost, Step: 1, SetupOnly: true,
		Title: "the base configuration of " + acct + ": listen, token, roots, repositories (manual step 13)",
		Reach: func(context.Context) *Unreachable {
			if st, why := d.accountConfigState(); st == "nohome" {
				return &Unreachable{Why: why + "; it is made with the account (step workharbor-user), then run whr setup again", Step: "workharbor-user"}
			}
			return nil
		},
		Run: func(context.Context) (Status, string) {
			switch st, why := d.accountConfigState(); st {
			case "present":
				return OK, file + " exists (not read here: it is " + acct + "'s file)"
			case "missing":
				return Fail, file + " does not exist"
			default:
				return NotVerified, "cannot see " + file + " from this account (" + why + "); if " + acct + " has configured WorkHarbor, nothing is needed"
			}
		},
		Fix: d.accountConfigFix(),
	}
}

// checkAccountPath refuses a link or a foreign owner in the account's folders
// before the commands run: the home and each component below it that exists
// must be a real folder owned by the account (the home's owner), and a
// component that does not exist is fine. What it covers: the folders are made
// and the file is written as the account (never as root), so a link the
// account plants can only reach what the account can already write; this check
// is a second layer that catches an obvious link early. What it does not
// cover: it is not atomic with the commands, and root still installs the
// staged copy and the system config under /etc/whr, which the account does not
// control. Not run on a real host (unverified).
func (d Deps) checkAccountPath() error {
	home, err := os.Lstat(d.accountHome())
	if err != nil {
		return err
	}
	if !home.IsDir() {
		return errors.New(textsafe.Escape(d.accountHome()) + " is not a folder; nothing was written")
	}
	for _, p := range []string{filepath.Dir(d.accountConfigDir()), d.accountConfigDir()} {
		fi, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || !sameOwner(fi, home) {
			return errors.New(textsafe.Escape(p) + " is a link, not a folder, or not owned by " + d.account() + ": not written")
		}
	}
	return nil
}

// ownerOf is the user id that owns a file; tests replace it.
var ownerOf = func(fi fs.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// sameOwner reports whether two files have the same owner; an owner that
// cannot be read is never the same.
func sameOwner(a, b fs.FileInfo) bool {
	x, okx := ownerOf(a)
	y, oky := ownerOf(b)
	return okx && oky && x == y
}
