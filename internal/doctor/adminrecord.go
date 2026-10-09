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

// AdminRecordPath is the fixed place of the record that tells an administrator
// what WorkHarbor uses, without reading the account's own configuration (#431).
// It does not depend on where whr is installed.
const (
	AdminRecordName = "admin.json"
	AdminRecordPath = "/etc/whr/" + AdminRecordName
)

// AdminRecord is everything the admin record holds. It is built from named
// fields only and never copies the configuration, so a new configuration key
// (a token path, a key file) cannot reach it by accident. No secret, ever.
type AdminRecord struct {
	Version       int      `json:"version"`
	User          string   `json:"user"`
	Workspaces    []string `json:"workspaces"`
	DashboardPort int      `json:"dashboard_port,omitempty"`
}

// adminRecordOf reads the allowed fields out of the configuration's JSON and
// returns the record as it is written: indented, one trailing newline.
func adminRecordOf(raw []byte, user string) ([]byte, error) {
	var cfg struct {
		Listen string `json:"listen"`
		Roots  struct {
			Workspaces []string `json:"workspaces"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	rec := AdminRecord{Version: 1, User: user, Workspaces: cfg.Roots.Workspaces}
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

func (d Deps) adminRecordFile() string {
	if d.AdminRecordFile != "" {
		return d.AdminRecordFile
	}
	return AdminRecordPath
}

func adminRecordTemp() string { return filepath.Join(setupDir(), AdminRecordName) }

func (d Deps) wantAdminRecord() ([]byte, error) {
	raw, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return nil, err
	}
	return adminRecordOf(raw, d.account())
}

// adminRecordStep publishes the record at the fixed place /etc/whr/admin.json
// (root:wheel, directory 0755, file 0644), installed with sudo like the sshd
// drop-in; /etc is a link to /private/etc and `install` follows it. The record
// never fails the doctor: missing, stale or unwritable is a warn, because it is
// a convenience for the administrator, not a security property. UNVERIFIED on
// a real host: the paths, the modes, and a run without sudo (single account).
func (d Deps) adminRecordStep() Check {
	file := d.adminRecordFile()
	desc := "write " + file + " (root:wheel, 0644, directory 0755: workspace roots, user, dashboard port; no secrets). Needs root: without sudo this step only warns"
	if b, err := d.wantAdminRecord(); err == nil {
		desc += ":\n" + string(b)
	}
	return Check{
		Name: "admin-record", Phase: PhaseHost, Step: 13, Title: "the admin record in /etc/whr (issue #431)", FixOnWarn: true,
		Reach: d.needsConfigFile,
		Run: func(context.Context) (Status, string) {
			want, err := d.wantAdminRecord()
			if errors.Is(err, fs.ErrNotExist) {
				return NotVerified, needsConfig + ": the admin record is built from it, and it is not written yet"
			}
			if err != nil {
				return NotVerified, "the configuration cannot be read: " + oneLine(err.Error())
			}
			got, err := os.ReadFile(file) //nolint:gosec // the fixed record path or a test override
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
				want, err := d.wantAdminRecord()
				if err != nil {
					return err
				}
				return writeTemp(adminRecordTemp(), string(want))
			},
			Cmds: []Cmd{
				{Sudo: true, Argv: []string{"install", "-d", "-m", "0755", "-o", "root", "-g", "wheel", filepath.Dir(file)}},
				{Sudo: true, Argv: []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", adminRecordTemp(), file}},
			},
		},
	}
}
