// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficialReadDriverHelper(t *testing.T) {
	if os.Getenv("WHR_CLI_READ_DRIVER") != "1" {
		return
	}
	raw, e := base64.StdEncoding.DecodeString(os.Getenv("WHR_CLI_READ_SNAPSHOT"))
	if e != nil {
		os.Exit(124)
	}
	var d PreflightDriver
	if json.Unmarshal(raw, &d) != nil {
		os.Exit(124)
	}
	split := -1
	for i, arg := range os.Args {
		if arg == "--" {
			split = i
			break
		}
	}
	if split < 0 {
		os.Exit(124)
	}
	out, e := d.Reply(os.Args[split+1:])
	if e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(125)
	}
	os.Stdout.Write(out)
	os.Exit(0)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func TestPinnedOfficialCLIReadConfigurationPreservesSourceAndMetadata(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, dir := range []string{"source/.devcontainer", "source/projects", "operation", "cli"} {
		os.MkdirAll(filepath.Join(root, dir), 0700)
	}
	source := imageSource()
	source["containerUser"] = "0:0"
	source["remoteUser"] = "1000:1000"
	source["remoteEnv"] = map[string]any{"A": "source"}
	source["postStartCommand"] = []any{"true"}
	input := Invocation{Phase: Preflight, Scope: scopeFixture(), Workspace: filepath.Join(root, "source/projects"), Config: filepath.Join(root, "source/.devcontainer/devcontainer.json"), Override: filepath.Join(root, "operation/override.json"), Driver: filepath.Join(root, "driver"), UserData: filepath.Join(root, "cli")}
	input.DataPath = filepath.Join(root, "future-storage/data")
	raw, _ := json.Marshal(source)
	os.WriteFile(input.Config, raw, 0600)
	override, e := FullOverride(source, input.Scope, input.DataPath)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(override)
	os.WriteFile(input.Override, raw, 0600)
	driver := readonlyFixture()
	image := driver.Images[source["image"].(string)]
	image["Config"].(map[string]any)["Labels"] = map[string]any{"devcontainer.metadata": `[{"remoteEnv":{"A":"base","B":"inherited"},"onCreateCommand":["true"]}]`}
	snapshot, _ := json.Marshal(driver)
	script := "#!/bin/sh\nWHR_CLI_READ_DRIVER=1 WHR_CLI_READ_SNAPSHOT=" + shellQuote(base64.StdEncoding.EncodeToString(snapshot)) + " exec " + shellQuote(os.Args[0]) + " -test.run='^TestOfficialReadDriverHelper$' -- \"$@\"\n"
	os.WriteFile(input.Driver, []byte(script), 0700)
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	toolsRoot, e := filepath.Abs("../../../tools/devcontainer")
	if e != nil {
		t.Fatal(e)
	}
	tools := devcontainer.Tools{NodePath: node, Root: toolsRoot}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, e := readConfiguration(ctx, tools, input, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	metadata := []map[string]any{{"remoteEnv": map[string]any{"A": "base", "B": "inherited"}, "onCreateCommand": []any{"true"}}}
	if e := ValidateEffective(result, source, metadata, input); e != nil {
		raw, _ := json.Marshal(result)
		t.Fatalf("effective gate: %v candidate=%s", e, raw)
	}
	merged, ok := result["mergedConfiguration"].(map[string]any)
	if !ok {
		t.Fatal("missing effective config")
	}
	if merged["remoteUser"] != "1000:1000" || merged["containerUser"] != "0:0" {
		t.Fatal("source users lost")
	}
	env, ok := merged["remoteEnv"].(map[string]any)
	if !ok || env["A"] != "source" || env["B"] != "inherited" {
		t.Fatalf("env=%#v", env)
	}
	if len(merged["onCreateCommands"].([]any)) != 1 || len(merged["postStartCommands"].([]any)) != 1 {
		t.Fatal("hook source lost")
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { delete(m["configuration"].(map[string]any), "image") },
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["remoteEnv"] = map[string]any{"A": "hostsecret"}
		},
		func(m map[string]any) { m["mergedConfiguration"].(map[string]any)["postStartCommands"] = []any{} },
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["initializeCommand"] = "touch MUST_NOT_RUN"
		},
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["mounts"] = []any{"/var/run/docker.sock"}
		},
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["configFilePath"].(map[string]any)["path"] = "/foreign/devcontainer.json"
		},
		func(m map[string]any) { m["mergedConfiguration"].(map[string]any)["unknown"] = "CANARY" },
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["runArgs"] = []any{"--name", "foreign"}
		},
		func(m map[string]any) {
			m["mergedConfiguration"].(map[string]any)["postAttachCommands"] = []any{"true"}
		},
	} {
		bad := clone(result).(map[string]any)
		mutate(bad)
		if e := ValidateEffective(bad, source, metadata, input); e == nil {
			t.Fatal("effective drift accepted")
		}
	}
	if _, e := os.Stat(input.DataPath); !os.IsNotExist(e) {
		t.Fatal("preflight seeded provider storage")
	}
}
