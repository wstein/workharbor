// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"net/url"
	"reflect"
)

func exactKeys(m map[string]any, required []string, optional ...string) bool {
	if m == nil {
		return false
	}
	allowed := map[string]bool{}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			return false
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range m {
		if !allowed[key] {
			return false
		}
	}
	return true
}
func provenance(v any, path string) bool {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, []string{"$mid", "scheme", "path"}, "fsPath", "external", "authority", "query", "fragment") || m["$mid"] != json.Number("1") || m["scheme"] != "file" || m["path"] != path {
		return false
	}
	for _, key := range []string{"authority", "query", "fragment"} {
		if value, present := m[key]; present && value != "" {
			return false
		}
	}
	if value, present := m["fsPath"]; present && value != path {
		return false
	}
	if value, present := m["external"]; present {
		raw, ok := value.(string)
		if !ok {
			return false
		}
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "file" || u.Host != "" || u.Path != path || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	return true
}
func emptyGenerated(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case bool:
		return !x
	}
	return false
}

// ValidateEffective compares the CLI candidate with independently admitted raw
// inputs. It never promotes an unknown field or the CLI's image-metadata fallback.
func ValidateEffective(result, source map[string]any, metadata []map[string]any, input Invocation) error {
	if !exactKeys(result, []string{"configuration", "mergedConfiguration", "workspace", "ociAuthDiagnostics"}, "featuresConfiguration") {
		return fail("unsupported_configuration")
	}
	if value, present := result["featuresConfiguration"]; present && !emptyGenerated(value) {
		return fail("unsupported_configuration")
	}
	diagnostics, ok := result["ociAuthDiagnostics"].(map[string]any)
	if !ok || !exactKeys(diagnostics, []string{"authLookupWouldBeBlocked", "authServerRedirect", "registryRedirectWouldPreventCredentialForwarding"}) {
		return fail("unsupported_configuration")
	}
	for _, value := range diagnostics {
		if value != false {
			return fail("metadata_unavailable")
		}
	}
	expected, e := FullOverride(source, input.Scope, input.DataPath)
	if e != nil {
		return e
	}
	raw, ok := result["configuration"].(map[string]any)
	if !ok || !provenance(raw["configFilePath"], input.Config) {
		return fail("unsupported_configuration")
	}
	copy := clone(raw).(map[string]any)
	delete(copy, "configFilePath")
	if !reflect.DeepEqual(copy, expected) {
		return fail("unsupported_configuration")
	}
	workspace, ok := result["workspace"].(map[string]any)
	if !ok || !reflect.DeepEqual(workspace, map[string]any{"workspaceFolder": "/workspace", "workspaceMount": expected["workspaceMount"]}) {
		return fail("unsupported_configuration")
	}
	entries := append(append([]map[string]any{}, metadata...), source)
	for _, entry := range metadata {
		if e := validateMetadataEntry(entry); e != nil {
			return e
		}
	}
	mergedExpected := clone(expected).(map[string]any)
	for _, hook := range []string{"onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand"} {
		delete(mergedExpected, hook)
		commands := []any{}
		for _, entry := range entries {
			if value, present := entry[hook]; present {
				if text, ok := value.(string); ok && text == "" {
					continue
				}
				commands = append(commands, clone(value))
			}
		}
		mergedExpected[hook+"s"] = commands
	}
	for _, key := range []string{"containerEnv", "remoteEnv"} {
		env := map[string]any{}
		for _, entry := range entries {
			if values, ok := entry[key].(map[string]any); ok {
				for name, value := range values {
					env[name] = value
				}
			}
		}
		mergedExpected[key] = env
	}
	for _, entry := range entries {
		if value, present := entry["overrideCommand"]; present {
			mergedExpected["overrideCommand"] = value
		}
	}
	mergedExpected["init"] = false
	mergedExpected["privileged"] = false
	mergedExpected["portsAttributes"] = map[string]any{}
	mergedExpected["postAttachCommands"] = []any{}
	merged, ok := result["mergedConfiguration"].(map[string]any)
	if !ok || !provenance(merged["configFilePath"], input.Config) {
		return fail("unsupported_configuration")
	}
	actual := clone(merged).(map[string]any)
	delete(actual, "configFilePath")
	for _, key := range []string{"capAdd", "securityOpt", "entrypoints", "mounts", "customizations", "forwardPorts", "otherPortsAttributes", "hostRequirements"} {
		if value, present := actual[key]; present {
			valid := false
			switch key {
			case "customizations", "otherPortsAttributes", "hostRequirements":
				v, ok := value.(map[string]any)
				valid = ok && len(v) == 0
			default:
				v, ok := value.([]any)
				valid = ok && len(v) == 0
			}
			if !valid {
				return fail("unsupported_configuration")
			}
			delete(actual, key)
		}
	}
	if !reflect.DeepEqual(actual, mergedExpected) {
		return fail("unsupported_configuration")
	}
	return nil
}
func validateMetadataEntry(m map[string]any) error {
	if m == nil {
		return fail("unsupported_configuration")
	}
	for key := range m {
		switch key {
		case "containerUser", "remoteUser", "containerEnv", "remoteEnv", "overrideCommand", "userEnvProbe", "updateRemoteUserUID", "waitFor", "shutdownAction", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand":
		default:
			return fail("unsupported_configuration")
		}
	}
	check := clone(m).(map[string]any)
	check["image"] = "workharbor-validation@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	return config.ValidateSource(check)
}
