// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratedDockerfilePreservesSourceAndOverridesHostileAuthoredMetadata(t *testing.T) {
	p := registryParser(t)
	base := "ubuntu@sha256:" + strings.Repeat("a", 64)
	source := []byte("FROM " + base + " AS app\nRUN printf original\nLABEL devcontainer.metadata=\"{\\\"initializeCommand\\\":\\\"bad\\\"}\"\n")
	metadata := []map[string]any{{"remoteUser": "1000:1000", "containerUser": "0:0", "userEnvProbe": "none", "updateRemoteUserUID": false, "waitFor": "postStartCommand", "shutdownAction": "none"}}
	raw, _ := json.Marshal(metadata)
	quoted, _ := json.Marshal(string(raw))
	generated := []byte("ARG _DEV_CONTAINERS_BASE_IMAGE=scratch\n" + string(source) + "\nFROM $_DEV_CONTAINERS_BASE_IMAGE AS dev_containers_target_stage\nLABEL devcontainer.metadata=" + string(quoted) + "\n")
	if e := ValidateGeneratedDockerfile(context.Background(), p, source, generated, "app", metadata); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(generated), "printf original", "printf changed", 1), strings.Replace(string(generated), "LABEL devcontainer.metadata="+string(quoted), "", 1), string(generated) + "RUN touch added\n", "# syntax=evil/frontend\n" + string(generated), strings.Replace(string(generated), "AS dev_containers_target_stage", "AS unexpected", 1)} {
		if e := ValidateGeneratedDockerfile(context.Background(), p, source, []byte(bad), "app", metadata); e == nil {
			t.Fatal("generated mutation admitted", bad)
		}
	}
}
