package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor/internal/textsafe"
)

// workspaceMode is the mode of a workspace root: the owner alone (issue #395).
// The same 0700 is what `whr` gives its configuration, state and log folders;
// the design documents no other mode for the workspaces.
const workspaceMode = "700"

// folder is what is known about one workspace root.
type folder struct {
	Path   string // resolved: no link in it, and what the commands use
	Exists bool
	Owner  string
	Mode   string // octal, as `stat -f %Lp` prints it
}

// workspaceRoots reads roots.workspaces without validating the rest of the
// configuration, like the volume check. Lookup order (issue #510): the user
// configuration of the whr account; when the running account cannot read it
// (a separate administrator), the system config /etc/whr/config.json; when
// neither is readable, a not_verified that explains both, never a failure. A
// user config that does not exist yet is the config-first step's business and
// does not fall back, except in an administrator run (--user names another
// account): there the default path is the administrator's own home, which
// never holds the whr account's config, so a missing file falls back too. A status other than "" means the roots are not known.
func (d Deps) workspaceRoots() ([]string, Status, string) {
	raw, err := os.ReadFile(d.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		if d.adminRun() {
			if roots, st, _ := d.systemConfigRoots(err); st == "" {
				return roots, "", ""
			}
		}
		return nil, NotVerified, needsConfig + ": the workspace roots are read from it, and it is not written yet"
	}
	if err != nil {
		return d.systemConfigRoots(err)
	}
	roots, err := rootsOf(raw)
	if err != nil {
		return nil, NotVerified, needsConfig + ": the workspace roots cannot be read from it, " + oneLine(err.Error())
	}
	return roots, "", ""
}

// adminRun is a host run by an account other than the whr account (--user).
func (d Deps) adminRun() bool { return d.User != "" && d.User != d.account() }

// systemConfigRoots is the fallback of workspaceRoots: userErr is why the user
// configuration could not be read. The system config is read only for the
// roots; it is informational and never whr's own configuration.
func (d Deps) systemConfigRoots(userErr error) ([]string, Status, string) {
	file := d.systemConfigFile()
	why := needsConfig + ": the workspace roots cannot be read from " + d.ConfigPath + " (" + oneLine(userErr.Error()) + ")"
	raw, err := os.ReadFile(file) //nolint:gosec // the fixed system config path or a test override
	if err != nil {
		return nil, NotVerified, why + " and not from " + file + " (" + oneLine(err.Error()) +
			"); the system-config step writes it from a run that can read the user config, or run this step as " + d.account()
	}
	var sc SystemConfig
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, NotVerified, why + " and " + file + " is not valid: " + oneLine(err.Error())
	}
	if len(sc.Workspaces) == 0 {
		return nil, NotVerified, why + " and " + file + " names no workspace root"
	}
	return sc.Workspaces, "", ""
}

// inspectFolder looks at a workspace root without changing anything. A path
// that is not a real folder inside a mounted volume is refused (Fail with the
// reason, and no folder): the nearest existing ancestor must be reached without
// a link, because a link could point anywhere, and a path under /Volumes must
// be on that very volume, not a plain folder on the boot disk that an unplugged
// disk left behind. The returned path is the one the commands use.
func (d Deps) inspectFolder(ctx context.Context, root string) (folder, Status, string) {
	shown := textsafe.Escape(root)
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || shown != root {
		return folder{}, Fail, "the workspace root " + shown + " is not a clean absolute path"
	}
	ancestor := root
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return folder{}, NotVerified, "the workspace root " + shown + " cannot be looked at: " + oneLine(err.Error())
		}
		ancestor = filepath.Dir(ancestor)
	}
	if res, err := filepath.EvalSymlinks(ancestor); err != nil || res != ancestor {
		return folder{}, Fail, "the workspace root " + shown + " is under a symbolic link (" + textsafe.Escape(ancestor) + "): give the real path"
	}
	out, err := d.output(ctx, "df", "-P", ancestor)
	if err != nil {
		return folder{}, NotVerified, "df did not say which disk " + textsafe.Escape(ancestor) + " is on: " + oneLine(err.Error())
	}
	m := dfMount.FindStringSubmatch(out)
	if m == nil {
		return folder{}, NotVerified, "df's answer for " + textsafe.Escape(ancestor) + " could not be read"
	}
	mount := strings.TrimSpace(m[1])
	if rest, ok := strings.CutPrefix(root, "/Volumes/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		if mount != "/Volumes/"+name {
			return folder{}, Fail, "the volume for " + shown + " is not mounted (it would land on " + textsafe.Escape(mount) + ")"
		}
	}
	if !internalMounts[mount] && root != mount && !strings.HasPrefix(root, mount+"/") {
		return folder{}, Fail, "the workspace root " + shown + " is outside its volume " + textsafe.Escape(mount)
	}
	f := folder{Path: root}
	if ancestor != root {
		return f, Fail, "the workspace root " + shown + " does not exist"
	}
	if fi, err := os.Lstat(root); err != nil || !fi.IsDir() {
		return folder{}, Fail, shown + " is not a folder"
	}
	f.Exists = true
	st, err := d.output(ctx, "stat", "-f", "%Su %Lp", root)
	if err != nil {
		return f, NotVerified, "stat did not answer for " + shown + ": " + oneLine(err.Error())
	}
	fields := strings.Fields(st)
	if len(fields) != 2 {
		return f, NotVerified, "stat's answer for " + shown + " could not be read"
	}
	f.Owner, f.Mode = fields[0], fields[1]
	switch {
	case f.Owner != d.account():
		return f, Fail, shown + " belongs to " + textsafe.Escape(f.Owner) + ", not " + d.account()
	case f.Mode != workspaceMode:
		return f, Fail, shown + " has mode " + textsafe.Escape(f.Mode) + ", want " + workspaceMode
	}
	return f, OK, shown + " belongs to " + d.account() + ", mode " + workspaceMode
}

// folderCmds are the commands that make a root right, as argument vectors run
// by sudo. They name the resolved path inspectFolder returned and are built
// again after the confirmation, so a path that changed in between is looked at
// again.
func (d Deps) folderCmds(f folder) []Cmd {
	var cmds []Cmd
	if !f.Exists {
		cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"mkdir", "-p", f.Path}})
	}
	if !f.Exists || f.Owner != d.account() {
		cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"chown", "-h", d.account(), f.Path}})
	}
	if !f.Exists || f.Mode != workspaceMode || f.Owner != d.account() {
		cmds = append(cmds, Cmd{Sudo: true, Argv: []string{"chmod", "-h", "0" + workspaceMode, f.Path}})
	}
	return cmds
}

func (d Deps) workspaceFoldersStep() Check {
	return Check{
		Name: "workspace-folders", Phase: PhaseHost, Step: 3, Title: "workspace folders exist, owned by the whr account, mode 0700 (manual step 3)",
		Reach: d.needsRoots,
		Run: func(ctx context.Context) (Status, string) {
			if d.GOOS != "darwin" || d.Runner == nil {
				return NotVerified, "not checked: " + errNotHere.Error()
			}
			roots, st, msg := d.workspaceRoots()
			if st != "" {
				return st, msg
			}
			if len(roots) == 0 {
				return NotVerified, "the configuration names no workspace root"
			}
			var bad, unknown, good []string
			for _, r := range roots {
				_, st, msg := d.inspectFolder(ctx, r)
				switch st {
				case OK:
					good = append(good, msg)
				case Fail:
					bad = append(bad, msg)
				default:
					unknown = append(unknown, msg)
				}
			}
			switch {
			case len(bad) > 0:
				return Fail, strings.Join(bad, "; ")
			case len(unknown) > 0:
				return NotVerified, strings.Join(unknown, "; ")
			}
			return OK, strings.Join(good, "; ")
		},
		Fix: &Fix{
			// No Sudo preview commands: the wizard asks for the password before
			// Build when a fix has them, even if Build then refuses. Desc shows
			// them, and sudo is asked only once Build has returned commands.
			Desc: "for each workspace root, with sudo: mkdir -p <root>; chown -h " + d.account() + " <root>; chmod -h 0" + workspaceMode + " <root> (only what is missing)",
			Build: func(ctx context.Context, p Prompter) ([]Cmd, error) {
				roots, _, msg := d.workspaceRoots()
				if msg != "" {
					return nil, errors.New(msg)
				}
				var cmds []Cmd
				for _, r := range roots {
					f, st, msg := d.inspectFolder(ctx, r)
					switch {
					case st == OK:
						continue
					case st == NotVerified || f.Path == "":
						return nil, errors.New(msg + "; nothing was changed")
					case f.Exists && f.Owner != "" && f.Owner != d.account():
						ok, err := p.Confirm(msg + ". Change the owner to " + d.account() + " with sudo chown")
						if err != nil {
							return nil, err
						}
						if !ok {
							return nil, errors.New(textsafe.Escape(r) + " was left as it is")
						}
						// the answer took a human's time: look again, and act only
						// on what is still the same
						g, st2, msg2 := d.inspectFolder(ctx, r)
						if st2 == NotVerified || g.Path == "" || g != f {
							return nil, errors.New(textsafe.Escape(r) + " changed while you were asked (" + msg2 + "); nothing was changed")
						}
					}
					cmds = append(cmds, d.folderCmds(f)...)
				}
				return cmds, nil
			},
		},
	}
}

// rootsOf reads roots.workspaces out of the configuration's JSON.
func rootsOf(raw []byte) ([]string, error) {
	var cfg struct {
		Roots struct {
			Workspaces []string `json:"workspaces"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg.Roots.Workspaces, nil
}
