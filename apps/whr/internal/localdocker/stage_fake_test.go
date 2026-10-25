//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeStageState struct {
	Primary                  map[string]any
	Candidate                map[string]any
	Volume                   *stageVolumeDetails
	Calls                    [][]string
	SmokeStdin               string
	Health                   bool
	StateRoot                string
	RequireDurableHomeIntent bool
}

func readFakeStageState(t *testing.T, path string) fakeStageState {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var s fakeStageState
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	return s
}
func fixtureStageLifecycle(t *testing.T, health bool) (Client, StageOptions, stagePreparationPorts, string) {
	t.Helper()
	c, opts, p := fixtureStagePreparation(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := fakeStageState{Primary: map[string]any{"Id": strings.Repeat("a", 64), "Name": "/primary", "ImageID": "sha256:" + strings.Repeat("b", 64), "ConfigImage": "example:local", "User": "", "Hostname": "primary", "State": "running", "Running": true, "Healthcheck": nil, "Health": nil, "Labels": map[string]string{"devcontainer.local_folder": opts.Source, "devcontainer.config_file": filepath.Join(opts.Source, ".devcontainer/devcontainer.json")}, "Mounts": []map[string]any{{"Type": "bind", "Source": opts.Source, "Destination": "/workspaces/Anchorage", "RW": true}, {"Type": "volume", "Name": "anchorage-home-owner", "Source": "/var/lib/docker/volumes/anchorage-home-owner/_data", "Destination": "/home/vscode", "RW": true}}, "Environment": []string{}, "Entrypoint": []string{"/bin/sh"}, "Command": []string{"-c", "fixture"}}, Calls: [][]string{}, Health: health}
	b, _ := json.Marshal(state)
	if e := os.WriteFile(statePath, b, 0600); e != nil {
		t.Fatal(e)
	}
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"docker", "node"} {
		script := "#!/bin/sh\nexec " + stageShellLiteral(binary) + " -test.run '^TestLocalStageFakeToolProcess$' -- whr-stage-fake " + name + " " + stageShellLiteral(statePath) + " \"$@\"\n"
		if e = os.WriteFile(p.Tools[name], []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
	}
	return c, opts, p, statePath
}

// A temporary test executable models only declared Docker/official-CLI vectors.
// It never calls a daemon, builds an image, or executes a source hook or smoke.
func TestLocalStageFakeToolProcess(t *testing.T) {
	marker := -1
	for i, arg := range os.Args {
		if arg == "whr-stage-fake" {
			marker = i
			break
		}
	}
	if marker < 0 {
		return
	}
	if len(os.Args) < marker+4 {
		os.Exit(90)
	}
	kind, path, args := os.Args[marker+1], os.Args[marker+2], os.Args[marker+3:]
	b, e := os.ReadFile(path)
	if e != nil {
		os.Exit(91)
	}
	var state fakeStageState
	if json.Unmarshal(b, &state) != nil {
		os.Exit(92)
	}
	save := func() {
		b, _ := json.Marshal(state)
		if os.WriteFile(path, b, 0600) != nil {
			os.Exit(93)
		}
	}
	emit := func(value any) { b, _ := json.Marshal(value); fmt.Println(string(b)) }
	argValue := func(vector []string, key string) string {
		for i, v := range vector {
			if v == key && i+1 < len(vector) {
				return vector[i+1]
			}
		}
		return ""
	}
	if kind == "node" {
		if len(args) == 1 && args[0] == "--version" {
			fmt.Println("v22.0.0")
			os.Exit(0)
		}
		if len(args) == 2 && args[1] == "--version" {
			fmt.Println("0.89.0")
			os.Exit(0)
		}
		if len(args) < 2 {
			os.Exit(94)
		}
		phase := args[1]
		args = args[2:]
		switch phase {
		case "read-configuration":
			emit(map[string]any{"configuration": map[string]any{"image": "example:local", "remoteUser": "vscode"}, "mergedConfiguration": map[string]any{"image": "example:local", "remoteUser": "vscode"}})
		case "up":
			data, e := os.ReadFile(argValue(args, "--override-config"))
			if e != nil {
				os.Exit(95)
			}
			var cfg map[string]any
			if json.Unmarshal(data, &cfg) != nil {
				os.Exit(96)
			}
			vector := []string{"run", "--sig-proxy=false", "-a", "STDOUT", "-a", "STDERR", "--mount", cfg["workspaceMount"].(string)}
			for _, value := range cfg["mounts"].([]any) {
				vector = append(vector, "--mount", value.(string))
			}
			for _, value := range cfg["runArgs"].([]any) {
				vector = append(vector, value.(string))
			}
			for i, v := range args {
				if v == "--id-label" && i+1 < len(args) {
					vector = append(vector, "-l", args[i+1])
				}
			}
			vector = append(vector, "--entrypoint", "/bin/sh", "example:local", "-c", "fixture native entrypoint")
			cmd := exec.Command(argValue(args, "--docker-path"), vector...)
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr
			if e = cmd.Run(); e != nil {
				os.Exit(97)
			}
			emit(map[string]any{"outcome": "success", "containerId": strings.Repeat("c", 64), "remoteUser": "vscode", "remoteWorkspaceFolder": "/workspaces/Anchorage", "configuration": cfg, "mergedConfiguration": cfg})
		case "run-user-commands":
			cmd := exec.Command(argValue(args, "--docker-path"), "exec", "--user", "vscode", "--workdir", "/workspaces/Anchorage", argValue(args, "--container-id"), "/bin/sh", "-c", "fixture hooks")
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr
			if e = cmd.Run(); e != nil {
				os.Exit(98)
			}
			emit(map[string]string{"outcome": "success"})
		default:
			os.Exit(99)
		}
		os.Exit(0)
	}
	if kind != "docker" || len(args) < 3 || args[0] != "--host" || !strings.HasPrefix(args[1], "unix:///") {
		os.Exit(100)
	}
	args = args[2:]
	state.Calls = append(state.Calls, append([]string{}, args...))
	save()
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("Docker version fixture")
		os.Exit(0)
	}
	if len(args) < 2 {
		os.Exit(101)
	}
	switch args[0] + ":" + args[1] {
	case "info:--format":
		emit(map[string]string{"ID": "daemon", "OSType": "linux", "OperatingSystem": "Debian", "KernelVersion": "fixture", "Name": "fixture", "Architecture": "x86_64"})
	case "container:ls":
		emit(map[string]string{"ID": state.Primary["Id"].(string), "Status": "Up"})
		if state.Candidate != nil {
			emit(map[string]string{"ID": state.Candidate["Id"].(string), "Status": "Up"})
		}
	case "container:inspect":
		format := argValue(args, "--format")
		for _, id := range args[4:] {
			item := state.Primary
			if id != state.Primary["Id"] {
				item = state.Candidate
			}
			if item == nil || item["Id"] != id {
				os.Exit(102)
			}
			labels := item["Labels"].(map[string]any)
			if strings.Contains(format, "{{.Name}} {{index") {
				fmt.Printf("%s %s\n", item["Name"], labels["workharbor.stage.operation"])
				continue
			}
			if format == inspectFormat {
				emit(map[string]any{"Id": item["Id"], "Name": item["Name"], "Image": item["ConfigImage"], "State": item["State"], "Labels": labels, "Mounts": item["Mounts"]})
			} else if format == stageContainerProjection {
				emit(item)
			} else {
				os.Exit(103)
			}
		}
	case "volume:ls":
		emit("anchorage-home-owner")
		if state.Volume != nil {
			emit(state.Volume.Name)
		}
	case "volume:inspect":
		name := args[len(args)-1]
		if state.Volume == nil || state.Volume.Name != name {
			os.Exit(104)
		}
		emit(state.Volume)
	case "volume:create":
		if state.RequireDurableHomeIntent {
			entries, _ := os.ReadDir(state.StateRoot)
			valid := false
			for _, daemon := range entries {
				operations, _ := os.ReadDir(filepath.Join(state.StateRoot, daemon.Name()))
				for _, op := range operations {
					base := filepath.Join(state.StateRoot, daemon.Name(), op.Name())
					var receipt stageReceipt
					raw, e := os.ReadFile(filepath.Join(base, "receipt.json"))
					if e != nil || json.Unmarshal(raw, &receipt) != nil {
						continue
					}
					var binding stageExecutionBinding
					raw, e = os.ReadFile(filepath.Join(base, "execution-binding.json"))
					if e != nil || json.Unmarshal(raw, &binding) != nil || stageDigest(binding) != receipt.ExecutionManifestDigest {
						continue
					}
					var settings stageSettings
					raw, e = os.ReadFile(filepath.Join(base, "stage-settings.json"))
					if e != nil || json.Unmarshal(raw, &settings) != nil || stageDigest(settings) != binding.SettingsDigest {
						continue
					}
					var plan CandidateHomePlan
					raw, e = os.ReadFile(filepath.Join(base, "candidate-home.json"))
					if e != nil || json.Unmarshal(raw, &plan) != nil || stageDigest(plan) != settings.HomePlanDigest {
						continue
					}
					if plan.Mount.Name == args[len(args)-1] && plan.OperationID == op.Name() {
						if _, e = os.Stat(filepath.Join(base, "intent.json")); e == nil {
							valid = true
						}
					}
				}
			}
			if !valid {
				os.Exit(107)
			}
		}
		labels := map[string]string{}
		for i, v := range args {
			if v == "--label" && i+1 < len(args) {
				key, value, _ := strings.Cut(args[i+1], "=")
				labels[key] = value
			}
		}
		state.Volume = &stageVolumeDetails{Name: args[len(args)-1], Driver: "local", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Labels: labels}
		save()
		fmt.Println(state.Volume.Name)
	case "image:inspect":
		emit(map[string]any{"Id": "sha256:" + strings.Repeat("d", 64), "Metadata": nil})
	default:
		if args[0] == "run" {
			mounts := []map[string]any{}
			labels := map[string]string{}
			for i, v := range args {
				if i+1 >= len(args) {
					continue
				}
				switch v {
				case "--mount":
					m, e := stageNativeMount(args[i+1])
					if e != nil {
						os.Exit(105)
					}
					entry := map[string]any{"Type": m["type"], "Source": m["source"], "Destination": m["target"], "RW": m["readonly"] != "true"}
					if m["type"] == "volume" {
						entry["Name"] = m["source"]
					}
					mounts = append(mounts, entry)
				case "-l", "--label":
					key, value, _ := strings.Cut(args[i+1], "=")
					labels[key] = value
				}
			}
			var healthcheck any
			var health any
			if state.Health {
				healthcheck = map[string]any{"Test": []string{"CMD", "true"}}
				health = "healthy"
			}
			state.Candidate = map[string]any{"Id": strings.Repeat("c", 64), "Name": "/" + argValue(args, "--name"), "ImageID": "sha256:" + strings.Repeat("d", 64), "ConfigImage": "example:local", "User": "", "Hostname": argValue(args, "--hostname"), "State": "running", "Running": true, "Healthcheck": healthcheck, "Health": health, "Labels": labels, "Mounts": mounts, "Environment": []string{}, "Entrypoint": []string{"/bin/sh"}, "Command": []string{"-c", "fixture native entrypoint"}}
			save()
			fmt.Println(strings.Repeat("c", 64))
		} else if args[0] == "exec" {
			for i, v := range args {
				if v == "bash" && i+1 < len(args) && args[i+1] == "-s" {
					data, _ := io.ReadAll(os.Stdin)
					state.SmokeStdin = string(data)
					save()
					fmt.Print("\x1b[31msmoke-marker\n")
				}
			}
		} else {
			os.Exit(106)
		}
	}
	os.Exit(0)
}
