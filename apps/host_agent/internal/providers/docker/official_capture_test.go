// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
)

type capturedCLI struct {
	Args       []string
	Dockerfile string
}

func TestOfficialMutationRefusalCaptureHelper(t *testing.T) {
	if os.Getenv("WHR_CAPTURE_DRIVER") != "1" {
		return
	}
	raw, e := base64.StdEncoding.DecodeString(os.Getenv("WHR_CAPTURE_SNAPSHOT"))
	if e != nil {
		os.Exit(124)
	}
	var snapshot PreflightDriver
	if json.Unmarshal(raw, &snapshot) != nil {
		os.Exit(124)
	}
	split := -1
	for i, a := range os.Args {
		if a == "--" {
			split = i
			break
		}
	}
	if split < 0 {
		os.Exit(124)
	}
	args := os.Args[split+1:]
	capture := capturedCLI{Args: args}
	if len(args) > 1 && args[0] == "buildx" && args[1] == "build" {
		for i, a := range args {
			if a == "-f" && i+1 < len(args) {
				raw, e := os.ReadFile(args[i+1])
				if e != nil {
					os.Exit(124)
				}
				capture.Dockerfile = string(raw)
			}
		}
	}
	raw, _ = json.Marshal(capture)
	f, e := os.OpenFile(os.Getenv("WHR_CAPTURE_LOG"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		os.Exit(124)
	}
	f.Write(append(raw, '\n'))
	f.Close()
	raw, e = snapshot.Reply(args)
	if e != nil {
		fmt.Fprintln(os.Stderr, "intentional closed mutation refusal")
		os.Exit(125)
	}
	os.Stdout.Write(raw)
	os.Exit(0)
}
func TestPinnedOfficialUpImageAndDockerfileReachClosedMutationGate(t *testing.T) {
	for _, kind := range []string{"image", "dockerfile"} {
		t.Run(kind, func(t *testing.T) {
			root := bridgeRoot(t)
			for _, d := range []string{"source/.devcontainer", "source/projects", "operation", "cli", "tmp"} {
				if e := os.MkdirAll(filepath.Join(root, d), 0700); e != nil {
					t.Fatal(e)
				}
			}
			source := imageSource()
			source["containerUser"] = "0:0"
			source["remoteUser"] = "1000:1000"
			source["overrideCommand"] = true
			original := []byte("FROM " + source["image"].(string) + "\nRUN printf fixture > /opt/fixture\n")
			if kind == "dockerfile" {
				delete(source, "image")
				source["build"] = map[string]any{"dockerfile": "Dockerfile", "context": "."}
				os.WriteFile(filepath.Join(root, "source/.devcontainer/Dockerfile"), original, 0600)
			}
			input := Invocation{Phase: Resources, Scope: scopeFixture(), Workspace: filepath.Join(root, "source/projects"), Config: filepath.Join(root, "source/.devcontainer/devcontainer.json"), Override: filepath.Join(root, "operation/override.json"), Driver: filepath.Join(root, "driver"), UserData: filepath.Join(root, "cli"), DataPath: filepath.Join(root, "storage/data")}
			raw, _ := json.Marshal(source)
			os.WriteFile(input.Config, raw, 0600)
			override, e := FullOverride(source, input.Scope, input.DataPath)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ = json.Marshal(override)
			os.WriteFile(input.Override, raw, 0600)
			snapshot, _ := json.Marshal(readonlyFixture())
			log := filepath.Join(root, "capture.jsonl")
			os.WriteFile(log, nil, 0600)
			script := "#!/bin/sh\nWHR_CAPTURE_DRIVER=1 WHR_CAPTURE_SNAPSHOT=" + shellQuote(base64.StdEncoding.EncodeToString(snapshot)) + " WHR_CAPTURE_LOG=" + shellQuote(log) + " exec " + shellQuote(os.Args[0]) + " -test.run='^TestOfficialMutationRefusalCaptureHelper$' -- \"$@\"\n"
			os.WriteFile(input.Driver, []byte(script), 0700)
			node, e := exec.LookPath("node")
			if e != nil {
				t.Fatal(e)
			}
			toolsRoot, _ := filepath.Abs("../../../tools/devcontainer")
			tools := devcontainer.Tools{NodePath: node, Root: toolsRoot}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if e = tools.Verify(ctx, true); e != nil {
				t.Fatal(e)
			}
			args, e := input.Arguments()
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.CommandContext(ctx, node, append([]string{filepath.Join(toolsRoot, "node_modules/@devcontainers/cli/dist/spec-node/devContainersSpecCLI.js")}, args...)...)
			cmd.Dir = input.Workspace
			cmd.Env = append(tools.Environment(), "HOME="+root, "TMPDIR="+filepath.Join(root, "tmp"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
			output, e := cmd.CombinedOutput()
			if e == nil {
				t.Fatal("mutation refusal unexpectedly succeeded")
			}
			raw, le := os.ReadFile(log)
			if le != nil {
				t.Fatal(le)
			}
			_, metadata, me := ExpectedMetadata(source, nil)
			if me != nil {
				t.Fatal(me)
			}
			reached := false
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var call capturedCLI
				if json.Unmarshal([]byte(line), &call) != nil {
					t.Fatal("bad capture")
				}
				if kind == "image" && len(call.Args) > 0 && call.Args[0] == "run" {
					reached = true
					policy := policyFixture(Resources)
					policy.Scope = input.Scope
					policy.DataPath = input.DataPath
					policy.ContainerEnv = map[string]string{}
					policy.Build = nil
					policy.StartupSHA256 = ""
					for i, a := range call.Args {
						if a == "-c" && i+1 < len(call.Args) {
							sum := sha256.Sum256([]byte(call.Args[i+1]))
							policy.StartupSHA256 = hex.EncodeToString(sum[:])
							if policy.StartupSHA256 != "1a63da31389392b4e5ae4276c29a2c9a81b466739351488bef177c13fbcd7813" {
								t.Fatal("pinned startup changed", policy.StartupSHA256)
							}
						}
					}
					c, ce := Classify(policy, call.Args)
					if ce != nil {
						t.Fatalf("actual run not admitted: %v argv=%q", ce, call.Args)
					}
					actual, ae := ValidateImage(ctx, registryParser(t), ImageConfig{Metadata: c.Run.Labels["devcontainer.metadata"]})
					if ae != nil || fmt.Sprint(actual) != fmt.Sprint(metadata) {
						t.Fatalf("metadata mismatch actual=%#v expected=%#v error=%v", actual, metadata, ae)
					}
					t.Logf("measured official startup SHA256=%s", policy.StartupSHA256)
				}
				if kind == "dockerfile" && call.Dockerfile != "" {
					reached = true
					policy := policyFixture(Resources)
					policy.Scope = input.Scope
					policy.Build = &BuildPolicy{Context: filepath.Dir(input.Config), Scratch: filepath.Join(root, "tmp"), Tag: folderImage(input.Workspace), Target: "dev_containers_target_stage", Args: map[string]string{"BUILDKIT_INLINE_CACHE": "1", "_DEV_CONTAINERS_BASE_IMAGE": "dev_container_auto_added_stage_label"}}
					if _, ce := Classify(policy, call.Args); ce != nil {
						t.Fatalf("actual build argv rejected: %v %q", ce, call.Args)
					}
					if e = ValidateGeneratedDockerfile(ctx, registryParser(t), original, []byte(call.Dockerfile), "", metadata); e != nil {
						t.Fatalf("actual generated file rejected: %v\n%s", e, call.Dockerfile)
					}
				}
			}
			if !reached {
				t.Fatalf("required mutation gate not reached: captures=%s output=%s", raw, output)
			}
		})
	}
}
