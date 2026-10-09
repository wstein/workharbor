package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// SystemConfigPath is the fixed place of the system config that tells an administrator
// what WorkHarbor uses, without reading the account's own configuration (#431).
// It does not depend on where whr is installed.
const (
	SystemConfigName = "config.json"
	SystemConfigPath = "/etc/whr/" + SystemConfigName
)

// SystemConfig is everything the system config holds. It is built from named
// fields only and never copies the configuration, so a new configuration key
// (a token path, a key file) cannot reach it by accident. No secret, ever.
type SystemConfig struct {
	Version       int      `json:"version"`
	User          string   `json:"user"`
	Workspaces    []string `json:"workspaces"`
	DashboardPort int      `json:"dashboard_port,omitempty"`
}

// systemConfigOf reads the allowed fields out of the configuration's JSON and
// returns the system config as it is written: indented, one trailing newline.
func systemConfigOf(raw []byte, user string) ([]byte, error) {
	var cfg struct {
		Listen string `json:"listen"`
		Roots  struct {
			Workspaces []string `json:"workspaces"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	rec := SystemConfig{Version: 1, User: user, Workspaces: cfg.Roots.Workspaces}
	if rec.Workspaces == nil {
		rec.Workspaces = []string{}
	}
	if _, port, err := net.SplitHostPort(cfg.Listen); err == nil {
		if n, err := strconv.Atoi(port); err == nil && n >= 1 && n <= 65535 {
			rec.DashboardPort = n
		}
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func (d Deps) systemConfigFile() string {
	if d.SystemConfigFile != "" {
		return d.SystemConfigFile
	}
	return SystemConfigPath
}

func systemConfigTemp() string { return filepath.Join(setupDir(), "system-config.json") }

func (d Deps) wantSystemConfig() ([]byte, error) {
	raw, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return nil, err
	}
	return systemConfigOf(raw, d.account())
}

// systemConfigStep publishes the system config at the fixed place /etc/whr/config.json
// (root:wheel, directory 0755, file 0644), installed with sudo like the sshd
// drop-in; /etc is a link to /private/etc and `install` follows it. The system config
// never fails the doctor: missing, stale or unwritable is a warn, because it is
// a convenience for the administrator, not a security property. UNVERIFIED on
// a real host: the paths, the modes, and a run without sudo (single account).
func (d Deps) systemConfigStep() Check {
	file := d.systemConfigFile()
	desc := "write " + file + " (root:wheel, 0644, directory 0755: workspace roots, user, dashboard port; no secrets). Needs root: without sudo this step only warns"
	if b, err := d.wantSystemConfig(); err == nil {
		desc += ":\n" + string(b)
	}
	return Check{
		Name: "system-config", Phase: PhaseHost, Step: 13, Title: "the system config in /etc/whr (issue #431)", FixOnWarn: true,
		Reach: d.needsConfigFile,
		Run: func(context.Context) (Status, string) {
			want, err := d.wantSystemConfig()
			if errors.Is(err, fs.ErrNotExist) {
				return NotVerified, needsConfig + ": the system config is built from it, and it is not written yet"
			}
			if err != nil {
				return NotVerified, "the configuration cannot be read: " + oneLine(err.Error())
			}
			got, err := os.ReadFile(file) //nolint:gosec // the fixed system config path or a test override
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return Warn, file + " is not there; writing it needs root (sudo), and it is optional"
			case err != nil:
				return NotVerified, "could not read " + file + ": " + oneLine(err.Error())
			case !bytes.Equal(got, want):
				return Warn, file + " is out of date; updating it needs root (sudo), and it is optional"
			}
			return OK, file + " is current"
		},
		Fix: &Fix{
			Desc: desc,
			Do: func(context.Context, Prompter) error {
				want, err := d.wantSystemConfig()
				if err != nil {
					return err
				}
				return writeTemp(systemConfigTemp(), string(want))
			},
			Cmds: []Cmd{
				{Sudo: true, Argv: []string{"install", "-d", "-m", "0755", "-o", "root", "-g", "wheel", filepath.Dir(file)}},
				{Sudo: true, Argv: []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", systemConfigTemp(), file}},
			},
		},
	}
}
