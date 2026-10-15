// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func scopeFixture() Scope {
	return Scope{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000005", "00000000-0000-4000-8000-000000000006", 1}
}
func imageSource() map[string]any {
	return map[string]any{"image": "ubuntu@sha256:" + strings.Repeat("a", 64)}
}
func errorCode(t *testing.T, e error, want string) {
	t.Helper()
	v, ok := e.(*config.Error)
	if !ok || v.Code != want {
		t.Fatalf("error=%v want %s", e, want)
	}
}
func TestFullOverridePreservesSourceAndAddsOnlyProviderPolicy(t *testing.T) {
	source := imageSource()
	source["remoteEnv"] = map[string]any{"APP": "literal"}
	source["postCreateCommand"] = []any{"sh", "./script"}
	override, e := FullOverride(source, scopeFixture(), "/private/data/storage/data")
	if e != nil {
		t.Fatal(e)
	}
	for key, value := range source {
		if !reflect.DeepEqual(value, override[key]) {
			t.Fatalf("lost %s", key)
		}
	}
	if _, found := source["workspaceMount"]; found {
		t.Fatal("source mutated")
	}
	if override["workspaceFolder"] != "/workspace" || override["workspaceMount"] != "type=bind,source=/private/data/storage/data,target=/workspace" || !reflect.DeepEqual(override["runArgs"], []any{"--name", "workharbor-runtime-00000000-0000-4000-8000-000000000004"}) {
		t.Fatal("provider identity missing")
	}
	override["remoteEnv"].(map[string]any)["APP"] = "changed"
	if source["remoteEnv"].(map[string]any)["APP"] != "literal" {
		t.Fatal("override aliases source")
	}
	source["runArgs"] = []any{"--name", "foreign"}
	_, e = FullOverride(source, scopeFixture(), "/private/data/storage/data")
	errorCode(t, e, "unsupported_configuration")
}
func TestRawMetadataStrictGateBeforeCLIFallback(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	dir, e := filepath.Abs("../../../tools/devcontainer")
	if e != nil {
		t.Fatal(e)
	}
	p := config.JSONC{NodePath: node, CompanionDir: dir}
	entries, e := ValidateImage(context.Background(), p, ImageConfig{Metadata: `[{"remoteEnv":{"A":"one"}},{"postStartCommand":["true"]}]`})
	if e != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, e)
	}
	for _, raw := range []string{`not-json`, `null`, `[] trailing`, `{"remoteUser":"x","remoteUser":"y"}`, `{"unknown":"CANARY"}`, `{"initializeCommand":"touch SHOULD_NOT_RUN"}`, `{"mounts":[]}`, `{"features":{}}`, `{"remoteEnv":{"A":"${localEnv:SECRET}"}}`, `{"remoteUser":"\ud800"}`} {
		_, e := ValidateImage(context.Background(), p, ImageConfig{Metadata: raw})
		if e == nil || e.Error() == "unimplemented" {
			t.Fatalf("raw accepted/gate absent: %v", e)
		}
	}
	for _, image := range []ImageConfig{{Volumes: map[string]any{"/workspace/subdir": map[string]any{}}}, {OnBuild: []string{"RUN true"}}} {
		_, e := ValidateImage(context.Background(), p, image)
		errorCode(t, e, "unsupported_configuration")
	}
}
func TestRuntimeIdentityRejectsDuplicatesAndWrongMountOrName(t *testing.T) {
	s := scopeFixture()
	runtime := Container{ID: strings.Repeat("a", 64), Name: "/workharbor-runtime-" + s.RuntimeID, Image: strings.Repeat("b", 64), Labels: s.Labels(), Mounts: []Mount{{Type: "bind", Source: "/private/storage/data", Destination: "/workspace", RW: true}}}
	if _, e := VerifyRuntime(s, "/private/storage/data", []Container{runtime}); e != nil {
		t.Fatal(e)
	}
	for _, cases := range [][]Container{{runtime, runtime}, {func() Container { c := runtime; c.Name = "/foreign"; return c }()}, {func() Container {
		c := runtime
		c.Mounts = append(c.Mounts, Mount{Type: "volume", Destination: "/other"})
		return c
	}()}, {func() Container { c := runtime; c.Labels = map[string]string{}; return c }()}} {
		_, e := VerifyRuntime(s, "/private/storage/data", cases)
		errorCode(t, e, "runtime_conflict")
	}
	_, e := VerifyRuntime(s, "/private/storage/data", nil)
	errorCode(t, e, "runtime_absent")
}

func TestVerifiedTargetRequiresExactRecordedImageAndContainerID(t *testing.T) {
	s := scopeFixture()
	c := Container{ID: strings.Repeat("a", 64), Name: "/" + s.Name(), Image: "sha256:" + strings.Repeat("b", 64), Labels: s.Labels(), Mounts: []Mount{{Type: "bind", Source: "/private/storage/data", Destination: "/workspace", RW: true}}}
	target := Target{Scope: s, ContainerID: c.ID, ImageID: c.Image, DataPath: "/private/storage/data"}
	if _, e := VerifyTarget(target, []Container{c}); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Container){func(c *Container) { c.Image = "sha256:" + strings.Repeat("c", 64) }, func(c *Container) { c.ID = strings.Repeat("c", 64) }} {
		wrong := c
		mutate(&wrong)
		_, e := VerifyTarget(target, []Container{wrong})
		errorCode(t, e, "runtime_conflict")
	}
}

func TestNumericUsersAndUnmappedEngineRequiredBeforeSeed(t *testing.T) {
	source := imageSource()
	source["containerUser"] = "0:0"
	source["remoteUser"] = "1000:1000"
	engine := EngineInfo{OSType: "linux", Architecture: "arm64", ServerVersion: "29.8.2", SecurityOptions: []string{"name=seccomp,profile=builtin", "name=cgroupns"}}
	merged := map[string]any{"containerUser": "0:0", "remoteUser": "1000:1000"}
	u, e := AdmitUsers(source, merged, engine, nil)
	if e != nil || u.UID != 1000 || u.GID != 1000 {
		t.Fatalf("%#v %v", u, e)
	}
	for _, value := range []string{"root", "vscode", "1000", "01:1", "1:01", "4294967295:1", "1:4294967295", "-1:1", "1:2:3", ""} {
		bad := clone(source).(map[string]any)
		bad["remoteUser"] = value
		_, e := AdmitUsers(bad, merged, engine, nil)
		errorCode(t, e, "unsupported_configuration")
	}
	for _, options := range [][]string{nil, {"name=rootless"}, {"name=userns"}, {"name=unknown"}} {
		bad := engine
		bad.SecurityOptions = options
		_, e := AdmitUsers(source, merged, bad, nil)
		errorCode(t, e, "unsupported_configuration")
	}
	for _, remote := range []string{"0:0", "1000:999"} {
		_, e := AdmitUsers(source, map[string]any{"containerUser": "0:0", "remoteUser": remote}, engine, nil)
		errorCode(t, e, "unsupported_configuration")
	}
	existing := User{UID: 1001, GID: 1000}
	_, e = AdmitUsers(source, merged, engine, &existing)
	errorCode(t, e, "unsupported_configuration")
}
