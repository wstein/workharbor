// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
)

var stageLifecycleHooks = []string{"onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand"}
var stageMetadataKeys = []string{"onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand", "waitFor", "customizations", "mounts", "containerEnv", "containerUser", "init", "privileged", "capAdd", "securityOpt", "remoteUser", "userEnvProbe", "remoteEnv", "overrideCommand", "portsAttributes", "otherPortsAttributes", "forwardPorts", "shutdownAction", "updateRemoteUserUID", "hostRequirements"}

type stageCLIInvocation struct {
	Workspace, Config, Override, Driver, UserData, ContainerID string
	Labels                                                     []string
}

func (v stageCLIInvocation) Arguments(phase string) ([]string, error) {
	for _, path := range []string{v.Workspace, v.Config, v.Override, v.Driver, v.UserData} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
			return nil, stageConflict("official CLI path is not admitted")
		}
	}
	switch phase {
	case "read-configuration", "up":
	case "run-user-commands":
		if !fullID(v.ContainerID) {
			return nil, stageConflict("official hook container identity invalid")
		}
	default:
		return nil, stageConflict("official CLI phase unsupported")
	}
	args := []string{phase, "--workspace-folder", v.Workspace, "--config", v.Config, "--override-config", v.Override, "--docker-path", v.Driver, "--mount-workspace-git-root", "false", "--mount-git-worktree-common-dir", "false", "--log-format", "json", "--log-level", "info"}
	for _, label := range v.Labels {
		if !strings.Contains(label, "=") || strings.ContainsAny(label, "\x00\r\n") {
			return nil, stageConflict("official CLI identity label invalid")
		}
		args = append(args, "--id-label", label)
	}
	switch phase {
	case "read-configuration":
		args = append(args, "--include-merged-configuration", "true")
	case "up":
		args = append(args, "--user-data-folder", v.UserData, "--skip-post-create", "true", "--skip-post-attach", "true", "--omit-syntax-directive", "true", "--include-configuration", "true", "--include-merged-configuration", "true")
	case "run-user-commands":
		args = append(args, "--user-data-folder", v.UserData, "--container-id", v.ContainerID, "--skip-post-attach", "true")
	}
	return args, nil
}

type stagePrimaryConfiguration struct {
	Source, Workspace, Home, HomeDestination, Name, Hostname, RemoteUser, ContainerUser string
	Mounts                                                                              []any
	RunArgs                                                                             []any
	ContainerEnv, RemoteEnv                                                             map[string]any
	OverrideCommand                                                                     bool
}

func stageCopyConfig(source map[string]any) map[string]any {
	b, _ := json.Marshal(source)
	var copy map[string]any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	_ = d.Decode(&copy)
	return copy
}
func stageSourceMetadata(source map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range stageMetadataKeys {
		if value, ok := source[key]; ok {
			result[key] = value
		}
	}
	return stageCopyConfig(result)
}

// Only build and feature extension branches bake the current raw source
// contribution into the image. A plain image may contain identical text from
// an independent origin, which must remain independent.
func stageSourceContributionBaked(source map[string]any) bool {
	for _, key := range []string{"build", "dockerFile", "dockerfile"} {
		if _, ok := source[key]; ok {
			return true
		}
	}
	features, ok := source["features"].(map[string]any)
	return ok && len(features) != 0
}
func stageCheckBakedSourceMetadata(source map[string]any, metadata []map[string]any) error {
	expected := stageSourceMetadata(source)
	if len(expected) == 0 {
		return nil
	}
	if len(metadata) == 0 || !reflect.DeepEqual(metadata[len(metadata)-1], expected) {
		return stageConflict("source metadata origin does not match admitted full contribution")
	}
	return nil
}
func renderStageMount(fields ...string) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write(fields)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}
