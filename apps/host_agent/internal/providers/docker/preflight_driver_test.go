// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"strings"
	"testing"
)

func readonlyFixture() PreflightDriver {
	ref := imageSource()["image"].(string)
	return PreflightDriver{Scope: scopeFixture(), EngineVersion: "29.8.2", BuildxVersion: "v0.38.0", CandidateAbsent: true, Images: map[string]map[string]any{ref: {"Id": "sha256:" + strings.Repeat("b", 64), "Os": "linux", "Architecture": "arm64", "Config": map[string]any{"User": "0:0", "Env": []any{}, "Labels": map[string]any{}}}}}
}
func TestReadOnlyDriverKnownImageAndCompleteDiscoveryShapes(t *testing.T) {
	d := readonlyFixture()
	ref := imageSource()["image"].(string)
	raw, e := d.Reply([]string{"inspect", "--type", "image", ref})
	if e != nil {
		t.Fatal(e)
	}
	var images []map[string]any
	if json.Unmarshal(raw, &images) != nil || len(images) != 1 || images[0]["Id"] == nil {
		t.Fatal("image snapshot lost")
	}
	args := []string{"ps", "-q", "-a"}
	for _, label := range d.Scope.LabelList() {
		args = append(args, "--filter", "label="+label)
	}
	raw, e = d.Reply(args)
	if e != nil || len(raw) != 0 {
		t.Fatal("fresh candidate discovery must be empty", e)
	}
	for _, args := range [][]string{{"-v"}, {"version", "--format", "{{.Server.Version}}"}, {"buildx", "version"}} {
		raw, e := d.Reply(args)
		if e != nil || len(raw) == 0 {
			t.Fatal("verified version read missing", e)
		}
	}
}
func TestReadOnlyDriverRefusesEveryMutationUnknownAndIncompleteShape(t *testing.T) {
	d := readonlyFixture()
	for _, args := range [][]string{
		{"pull", imageSource()["image"].(string)}, {"run", "--name", d.Scope.Name(), "ubuntu"}, {"start", strings.Repeat("a", 64)}, {"exec", strings.Repeat("a", 64), "true"}, {"rm", "-f", strings.Repeat("a", 64)}, {"volume", "create"}, {"buildx", "build", "--load", "."}, {"--context", "foreign", "version"},
		{"inspect", "--type", "image", "ubuntu:latest"}, {"inspect", "--type", "image", imageSource()["image"].(string), "foreign"}, {"ps", "-q", "-a"}, {"ps", "-q", "-a", "--filter", "label=workharbor.owner=" + d.Scope.OwnerID}, {"version", "--format", "{{json .}}"}, {"buildx", "version", "--format", "json"},
	} {
		_, e := d.Reply(args)
		if e == nil || e.Error() == "unimplemented" {
			t.Fatalf("refusal gate missing for %q: %v", args, e)
		}
	}
}
