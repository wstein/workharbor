//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package profile

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
var versionPattern = regexp.MustCompile(`^[0-9]{4}\.[0-9]{2}\.[0-9]+$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var items = map[string]bool{"orbstack": true, "git": true, "gh": true, "jq": true, "fish": true, "starship": true, "ripgrep": true, "fd": true, "fzf": true, "zoxide": true, "bat": true, "jj": true, "herdr": true, "git-config": true}
var kinds = map[string]bool{"detect": true, "brew_install": true, "brew_upgrade": true, "dotfile_create": true, "dotfile_replace": true, "git_config_move": true, "socket_verify": true, "privileged_exec": true, "profile_write": true}
var statuses = map[string]bool{"applied": true, "unchanged": true, "declined": true, "opted_out": true, "blocked": true, "failed": true, "skipped": true, "rolled_back": true}

// Start permits only confirmed real runs. Callers do not call Begin for no,
// dry-run or plain doctor; invalid answers/commands are refused before any I/O.
type Start struct {
	RunID, Command, Platform, CatalogueVersion, Answer string
	Selected, OptedOut                                 []string
}

func validStart(s Start) bool {
	if !runIDPattern.MatchString(s.RunID) || !versionPattern.MatchString(s.CatalogueVersion) || (s.Command != "setup" && s.Command != "doctor_fix") || (s.Platform != "darwin-arm64" && s.Platform != "darwin-amd64") || (s.Answer != "yes" && s.Answer != "customize" && s.Answer != "flags") || !validItems(s.Selected) || !validItems(s.OptedOut) {
		return false
	}
	for _, id := range s.Selected {
		for _, off := range s.OptedOut {
			if id == off {
				return false
			}
		}
	}
	return true
}
func validItems(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !items[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
func validStepID(id string) bool {
	parts := strings.Split(id, ".")
	return len(parts) == 2 && items[parts[0]] && kinds[parts[1]]
}
func validStepIDs(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !validStepID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// StepAudit deliberately has no output, environment, error or configuration
// value fields. The current source slice accepts only exact catalogue Homebrew
// argv or empty argv for non-process and known no-child brew outcomes. Other
// process adapters must add
// their reviewed command forms before they can journal them.
type StepAudit struct {
	ID, Kind, Item, Result string
	Argv                   []string
	Sudo, Network          bool
	ExitStatus             *int
}

func safeArgv(a StepAudit) bool {
	if len(a.Argv) == 0 {
		if a.Kind != "brew_install" && a.Kind != "brew_upgrade" {
			return !a.Sudo
		}
		// T003 records these outcomes before any child is launched, including
		// re-detection that the selected tool is already present. A child exit,
		// sudo, or network effect cannot inherit this empty-command exception.
		if a.ExitStatus != nil || a.Sudo || a.Network {
			return false
		}
		switch a.Result {
		case "blocked", "declined", "skipped", "unchanged":
			return true
		}
		return false
	}
	if a.Sudo || a.Kind != "brew_install" && a.Kind != "brew_upgrade" {
		return false
	}
	v := a.Argv
	if v[0] != "/opt/homebrew/bin/brew" && v[0] != "/usr/local/bin/brew" {
		return false
	}
	action := "install"
	if a.Kind == "brew_upgrade" {
		action = "upgrade"
	}
	if len(v) == 4 {
		return a.Item == "orbstack" && v[1] == action && v[2] == "--cask" && v[3] == "orbstack"
	}
	return len(v) == 3 && a.Item != "orbstack" && a.Item != "git-config" && items[a.Item] && v[1] == action && v[2] == a.Item
}
func (r *Run) write(record map[string]any) error {
	if r.closed || r.finished || r.poisoned {
		return ErrProtocol
	}
	record["run_id"] = r.id
	record["time"] = r.clock.UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(record)
	if err != nil {
		return ErrProtocol
	}
	b = append(b, '\n')
	// Text is a pure rendering of these same JSON bytes, never an independent log.
	rendered := []byte(record["type"].(string) + ": " + string(b))
	if appendSync(r.json, b) != nil || appendSync(r.text, rendered) != nil {
		r.poisoned = true
		return ErrProtocol
	}
	return nil
}
func (r *Run) at(now time.Time) bool {
	if now.IsZero() || now.Before(r.clock) {
		return false
	}
	r.clock = now
	return true
}
func (r *Run) Step(a StepAudit, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !items[a.Item] || !kinds[a.Kind] || a.ID != a.Item+"."+a.Kind || !statuses[a.Result] || !safeArgv(a) || a.ExitStatus != nil && (*a.ExitStatus < 0 || *a.ExitStatus > 255) || !r.at(now) {
		return ErrUnsafe
	}
	return r.write(map[string]any{"type": "step", "step_id": a.ID, "kind": a.Kind, "item": a.Item, "argv": nonNil(a.Argv), "sudo": a.Sudo, "network": a.Network, "exit_status": a.ExitStatus, "result": a.Result})
}
func (r *Run) Backup(p, sha string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Only this run's backup namespace is admitted; raw caller paths never leak.
	prefix := "~/" + state + "/backups/" + r.id + "/"
	name := strings.TrimPrefix(p, prefix)
	if !strings.HasPrefix(p, prefix) || !regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`).MatchString(name) || name == "." || name == ".." || !shaPattern.MatchString(sha) || !r.at(now) {
		return ErrUnsafe
	}
	return r.write(map[string]any{"type": "backup", "path": p, "sha256": sha})
}
func (r *Run) Diff(id, item string, added, removed int, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !items[item] || (id != item+".dotfile_create" && id != item+".dotfile_replace") || added < 0 || removed < 0 || !r.at(now) {
		return ErrUnsafe
	}
	return r.write(map[string]any{"type": "diff", "step_id": id, "item": item, "added_lines": added, "removed_lines": removed})
}
func (r *Run) finish(code int, applied, remaining []string) error {
	if code != 0 && code != 64 && code != 68 && code != 70 && code != 71 && code != 72 && code != 73 && code != 74 || !validStepIDs(applied) || !validStepIDs(remaining) {
		return ErrUnsafe
	}
	for _, a := range applied {
		for _, b := range remaining {
			if a == b {
				return ErrUnsafe
			}
		}
	}
	err := r.write(map[string]any{"type": "run_end", "exit_code": code, "applied": nonNil(applied), "remaining": nonNil(remaining)})
	if err == nil {
		r.finished = true
	}
	return err
}
func (r *Run) Finish(code int, applied, remaining []string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.at(now) {
		return ErrUnsafe
	}
	return r.finish(code, applied, remaining)
}
