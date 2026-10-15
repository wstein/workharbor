// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
)

// ExpectedMetadata independently derives the admitted no-Features metadata, not
// authority from CLI output. Its fixed probe/UID policy makes the final label
// nonempty even when the original source has no lifecycle commands.
func ExpectedMetadata(source map[string]any, base []map[string]any) (string, []map[string]any, error) {
	if e := config.ValidateSource(source); e != nil {
		return "", nil, e
	}
	entries := []map[string]any{}
	for _, entry := range base {
		if e := validateMetadataEntry(entry); e != nil {
			return "", nil, e
		}
		if len(entry) != 0 {
			entries = append(entries, clone(entry).(map[string]any))
		}
	}
	last := map[string]any{}
	for key, value := range source {
		if key != "image" && key != "build" && key != "name" {
			last[key] = clone(value)
		}
	}
	for key, value := range map[string]any{"userEnvProbe": "none", "updateRemoteUserUID": false, "waitFor": "postStartCommand", "shutdownAction": "none"} {
		last[key] = value
	}
	if e := validateMetadataEntry(last); e != nil {
		return "", nil, e
	}
	entries = append(entries, last)
	raw, e := json.Marshal(entries)
	if e != nil {
		return "", nil, fail("invalid_configuration")
	}
	return string(raw), entries, nil
}
