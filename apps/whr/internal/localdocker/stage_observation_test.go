//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageNativeObservationsKeepImmutableConfigAndExactIdentities(t *testing.T) {
	id := strings.Repeat("a", 64)
	image := "sha256:" + strings.Repeat("b", 64)
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	tool := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\nshift 2\ncase \"$1:$2\" in\ncontainer:inspect) printf '%s\\n' '{\"Id\":\"" + id + "\",\"Name\":\"/candidate\",\"ImageID\":\"" + image + "\",\"ConfigImage\":\"build:tag\",\"User\":\"1000\",\"Hostname\":\"candidate\",\"State\":\"running\",\"Running\":true,\"Healthcheck\":{\"Test\":[\"CMD\",\"true\"]},\"Health\":\"healthy\",\"Labels\":{},\"Mounts\":[],\"Environment\":[\"PUBLIC=private-observation-only\"],\"Entrypoint\":[\"/bin/sh\"],\"Command\":[\"-c\",\"fixture\"]}' ;;\nimage:inspect) printf '%s\\n' '{\"Id\":\"" + image + "\",\"Metadata\":\"[{\\\"postCreateCommand\\\":\\\"retained origin\\\"}]\"}' ;;\nvolume:inspect) printf '%s\\n' '{\"Name\":\"whr-stage-home-fixture\",\"Driver\":\"local\",\"CreatedAt\":\"2026-10-25T00:00:00Z\",\"Labels\":{}}' ;;\n*) exit 83;;\nesac\n"
	if e := os.WriteFile(tool, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	r, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": tool})
	if e != nil {
		t.Fatal(e)
	}
	c, e := readStageContainer(context.Background(), r, id)
	if e != nil || c.ImageID != image || !c.Healthcheck || len(c.Environment) != 1 {
		t.Fatalf("container: %+v %v", c, e)
	}
	digest := stageContainerBindingDigest(c)
	c.State = "exited"
	c.Running = false
	c.Health = stageString("unhealthy")
	if stageContainerBindingDigest(c) != digest {
		t.Fatal("mutable state changed immutable config binding")
	}
	c.Environment[0] = "PUBLIC=changed"
	if stageContainerBindingDigest(c) == digest {
		t.Fatal("environment mutation escaped config binding")
	}
	i, e := readStageImage(context.Background(), r, image)
	if e != nil || i.ID != image || len(i.Metadata) != 1 {
		t.Fatalf("image: %+v %v", i, e)
	}
	v, e := readStageVolume(context.Background(), r, "whr-stage-home-fixture")
	if e != nil || v.Driver != "local" || v.CreatedAt == "" {
		t.Fatalf("volume: %+v %v", v, e)
	}
}

func TestLocalStageArtifactCanonicalizesAllObjectKeys(t *testing.T) {
	value := struct {
		Z string `json:"z"`
		A string `json:"a"`
	}{"last", "first"}
	b, e := stageArtifactBytes(value)
	if e != nil || string(b) != "{\"a\":\"first\",\"z\":\"last\"}\n" {
		t.Fatalf("canonical artifact: %s %v", b, e)
	}
	if stageDigest(value) != stageDigest(map[string]string{"a": "first", "z": "last"}) {
		t.Fatal("struct field declaration order changed digest")
	}
}

func TestLocalStageCandidateVolumeNamesMustMatchDeclaredPlan(t *testing.T) {
	policy := &stageDriverPolicy{Home: "whr-stage-home-fixture", HomeDestination: "/home/vscode", AllowedMounts: []string{"type=volume,source=declared-cache,target=/cache"}}
	for _, target := range []string{"/home/vscode", "/cache"} {
		t.Run(target, func(t *testing.T) {
			mounts := []mount{{Type: "volume", Name: policy.Home, Destination: policy.HomeDestination}, {Type: "volume", Name: "declared-cache", Destination: "/cache"}}
			if err := checkStageCandidateVolumes(mounts, policy); err != nil {
				t.Fatalf("declared control: %v", err)
			}
			for i := range mounts {
				if mounts[i].Destination == target {
					mounts[i].Name = "different-volume"
				}
			}
			err := checkStageCandidateVolumes(mounts, policy)
			if err == nil || exitcode.FromError(err) != exitcode.Conflict || err.Error() != "local stage candidate volume mount not in declared plan" {
				t.Fatalf("wrong volume admitted or wrong reason: %v", err)
			}
		})
	}
}
