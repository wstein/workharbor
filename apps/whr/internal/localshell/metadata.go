// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"bytes"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"path"
	"regexp"
	"strings"
)

const directoryMount = "workspace_mount"
const directoryFallback = "container_workdir_fallback"

var (
	containerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	userName      = regexp.MustCompile(`^([a-z_][a-z0-9_-]{0,31}|[0-9]+(:[0-9]+)?)$`)
	terminalName  = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

// Metadata holds only validated exec inputs and the retained directory source.
// Inspect's container environment and all unrelated fields are ignored.
type Metadata struct {
	ID, Name, User, Directory, Term string
	DirectorySource                 string
}
type inspectMetadata struct {
	ID    string `json:"Id"`
	Name  string
	State *struct {
		Status                      string
		Running, Paused, Restarting bool
	}
	Config *struct {
		User, WorkingDir json.RawMessage
		Labels           map[string]string
	}
	Mounts []struct{ Source, Destination string }
}

func invalidValue(field, value string) error {
	return exitcode.Errorf(exitcode.PathConflict, "invalid local shell %s: %s", field, terminal.Escape(value))
}
func stringValue(raw json.RawMessage, field string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return "", invalidValue(field, "non-string value")
	}
	return value, nil
}

// ParseMetadata reads the fixed preflight inspect response and selects values
// under FR-005. A selected invalid value is refused rather than repaired.
func ParseMetadata(raw []byte, selected localdocker.Workspace, term string) (Metadata, error) {
	var info inspectMetadata
	if err := json.Unmarshal(raw, &info); err != nil || info.Config == nil || info.State == nil {
		return Metadata{}, exitcode.Errorf(exitcode.PathConflict, "invalid local Docker inspect metadata")
	}
	if info.ID != selected.ID || len(info.ID) != 64 {
		return Metadata{}, invalidValue("container ID", info.ID)
	}
	name := strings.TrimPrefix(info.Name, "/")
	if !containerName.MatchString(name) {
		return Metadata{}, invalidValue("container name", name)
	}
	if info.State.Status != "running" || !info.State.Running || info.State.Paused || info.State.Restarting {
		return Metadata{}, exitcode.Errorf(exitcode.PathWrongState, "local workspace %s is not running (state %s)", terminal.Escape(name), terminal.Escape(info.State.Status))
	}
	user, present, err := remoteUser(info.Config.Labels)
	if err != nil {
		return Metadata{}, err
	}
	if !present {
		user, err = stringValue(info.Config.User, "user")
		if err != nil {
			return Metadata{}, err
		}
		if user == "" {
			user = "captain"
		}
	}
	if !userName.MatchString(user) {
		return Metadata{}, invalidValue("user", user)
	}
	dir := ""
	destinations := map[string]bool{}
	if selected.WorkspaceFolder != "" {
		for _, m := range info.Mounts {
			if m.Source == selected.WorkspaceFolder {
				destinations[m.Destination] = true
			}
		}
	}
	fallback := len(destinations) != 1
	if fallback {
		dir, err = stringValue(info.Config.WorkingDir, "working directory")
		if err != nil {
			return Metadata{}, err
		}
	} else {
		for d := range destinations {
			dir = d
		}
	}
	if !validDirectory(dir) {
		return Metadata{}, invalidValue("working directory", dir)
	}
	if !terminalName.MatchString(term) {
		term = "xterm-256color"
	}
	source := directoryMount
	if fallback {
		source = directoryFallback
	}
	return Metadata{ID: info.ID, Name: name, User: user, Directory: dir, Term: term, DirectorySource: source}, nil
}
func validDirectory(dir string) bool {
	return path.IsAbs(dir) && len(dir) <= 4096 && !strings.ContainsAny(dir, "\x00\n")
}

func remoteUser(labels map[string]string) (string, bool, error) {
	raw, present := labels["devcontainer.metadata"]
	if !present {
		return "", false, nil
	}
	var entries []map[string]json.RawMessage
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if json.Unmarshal([]byte(raw), &entries) != nil {
			return "", false, exitcode.Errorf(exitcode.PathConflict, "invalid devcontainer.metadata label")
		}
	} else {
		var entry map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &entry) != nil || entry == nil {
			return "", false, exitcode.Errorf(exitcode.PathConflict, "invalid devcontainer.metadata label")
		}
		entries = []map[string]json.RawMessage{entry}
	}
	var chosen json.RawMessage
	for _, entry := range entries {
		if entry == nil {
			return "", false, exitcode.Errorf(exitcode.PathConflict, "invalid devcontainer.metadata label")
		}
		if value, ok := entry["remoteUser"]; ok {
			chosen = value
		}
	}
	if chosen == nil {
		return "", false, nil
	}
	user, err := stringValue(chosen, "remoteUser")
	return user, true, err
}
