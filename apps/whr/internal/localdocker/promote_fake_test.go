// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

type fakePromotionState struct {
	fakeStageState
	Promotion                   bool
	Failure                     string
	MutationPath, MutationAfter string
	Phases                      [][]string
	Retained                    map[string]any
	Failed                      map[string]any
}

func readFakePromotionState(t *testing.T, path string) fakePromotionState {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var s fakePromotionState
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	if extra, e := os.ReadFile(path + ".extra"); e == nil {
		var v struct {
			Failure                     string
			MutationPath, MutationAfter string
			Phases                      [][]string
			Retained                    map[string]any
			Failed                      map[string]any
		}
		if json.Unmarshal(extra, &v) != nil {
			t.Fatal("fake extras")
		}
		s.Failure = v.Failure
		s.MutationPath, s.MutationAfter = v.MutationPath, v.MutationAfter
		s.Phases = v.Phases
		s.Retained = v.Retained
		s.Failed = v.Failed
	}
	return s
}
func writeFakePromotionState(t *testing.T, path string, s fakePromotionState) {
	t.Helper()
	b, _ := json.Marshal(s)
	extra, _ := json.Marshal(struct {
		Failure                     string
		MutationPath, MutationAfter string
		Phases                      [][]string
		Retained                    map[string]any
		Failed                      map[string]any
	}{s.Failure, s.MutationPath, s.MutationAfter, s.Phases, s.Retained, s.Failed})
	if err := os.WriteFile(path+".extra", extra, 0600); err != nil {
		t.Fatal(err)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func fixturePromotionLifecycle(t *testing.T, health ...bool) (Client, StageOptions, stagePreparationPorts, string) {
	t.Helper()
	c, opts, p, path := fixtureStageLifecycle(t, len(health) > 0 && health[0])
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"docker", "node"} {
		cover := ""
		if testing.CoverMode() != "" {
			private := t.TempDir()
			cover = "GOCOVERDIR=" + stageShellLiteral(private) + "\nexport GOCOVERDIR\n"
		}
		script := "#!/bin/sh\n" + cover + "exec " + stageShellLiteral(binary) + " -test.run '^TestLocalPromotionFakeToolProcess$' -- whr-promote-fake " + name + " " + stageShellLiteral(path) + " \"$@\"\n"
		if e = os.WriteFile(p.Tools[name], []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
	}
	return c, opts, p, path
}

// Before promotion this delegates to the existing stage-only fake. Afterward it
// models only the declared promotion vectors; no daemon or source hook executes.
func TestLocalPromotionFakeToolProcess(t *testing.T) {
	marker := -1
	for i, a := range os.Args {
		if a == "whr-promote-fake" {
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
	state := readFakePromotionState(t, path)
	delegate := func() { os.Args[marker] = "whr-stage-fake"; TestLocalStageFakeToolProcess(t); os.Exit(91) }
	if _, err := os.Stat(path + ".promotion"); os.IsNotExist(err) {
		delegate()
	}
	save := func() { writeFakePromotionState(t, path, state) }
	mutateConfiguration := func(after string) {
		if state.MutationAfter == after {
			if err := os.WriteFile(state.MutationPath, []byte("{}\n"), 0600); err != nil {
				os.Exit(120)
			}
		}
	}

	emit := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	value := func(a []string, k string) string {
		for i, v := range a {
			if v == k && i+1 < len(a) {
				return a[i+1]
			}
		}
		return ""
	}
	if kind == "node" {
		if len(args) < 2 || args[len(args)-1] == "--version" {
			delegate()
		}
		phase := args[1]
		args = args[2:]
		state.Phases = append(state.Phases, append([]string{phase}, args...))
		save()
		c, o := value(args, "--config"), value(args, "--override-config")
		if c != o {
			os.Exit(92)
		}
		b, e := os.ReadFile(c)
		if e != nil {
			os.Exit(93)
		}
		var cfg map[string]any
		if json.Unmarshal(b, &cfg) != nil {
			os.Exit(94)
		}
		if cfg["image"] != "sha256:"+strings.Repeat("d", 64) || cfg["initializeCommand"] != nil || cfg["build"] != nil || cfg["features"] != nil {
			os.Exit(95)
		}
		switch phase {
		case "read-configuration":
			emit(map[string]any{"configuration": cfg, "mergedConfiguration": cfg})
		case "up":
			// The official CLI ANDs all id-label filters when finding existing containers.
			lookup := []string{"ps", "-a", "--format", "{{.ID}}"}
			for i, arg := range args {
				if arg == "--id-label" && i+1 < len(args) {
					lookup = append(lookup, "--filter", "label="+args[i+1])
				}
			}
			probe := exec.Command(value(args, "--docker-path"), lookup...)
			probe.Env = os.Environ()
			probe.Stderr = os.Stderr
			matches, lookupErr := probe.Output()
			if lookupErr != nil {
				os.Exit(114)
			}
			if ids := strings.Fields(string(matches)); len(ids) > 0 {
				emit(map[string]any{"outcome": "success", "containerId": ids[0], "remoteUser": "vscode", "remoteWorkspaceFolder": "/workspaces/Anchorage", "configuration": cfg, "mergedConfiguration": cfg})
				os.Exit(0)
			}
			if state.Failure == "candidate_reply" {
				emit(map[string]any{"outcome": "success", "containerId": state.Candidate["Id"], "remoteUser": "vscode", "remoteWorkspaceFolder": "/workspaces/Anchorage", "configuration": cfg, "mergedConfiguration": cfg})
				os.Exit(0)
			}
			if state.Failure == "up" {
				os.Exit(96)
			}
			vector := []string{"run", "--mount", cfg["workspaceMount"].(string)}
			for _, raw := range cfg["mounts"].([]any) {
				vector = append(vector, "--mount", raw.(string))
			}
			for _, raw := range cfg["runArgs"].([]any) {
				vector = append(vector, raw.(string))
			}
			for key, v := range cfg["containerEnv"].(map[string]any) {
				vector = append(vector, "-e", key+"="+v.(string))
			}
			if user := cfg["containerUser"].(string); user != "" {
				vector = append(vector, "--user", user)
			}
			for i, v := range args {
				if v == "--id-label" && i+1 < len(args) {
					vector = append(vector, "-l", args[i+1])
				}
			}
			vector = append(vector, "--entrypoint", "/bin/sh", cfg["image"].(string), "-c", "fixture native entrypoint")
			cmd := exec.Command(value(args, "--docker-path"), vector...)
			cmd.Env = os.Environ()
			cmd.Stderr = os.Stderr
			if cmd.Run() != nil {
				os.Exit(97)
			}
			if state.Failure == "wrong_reply_image" {
				cfg["image"] = "sha256:" + strings.Repeat("f", 64)
			}
			emit(map[string]any{"outcome": "success", "containerId": strings.Repeat("e", 64), "remoteUser": "vscode", "remoteWorkspaceFolder": "/workspaces/Anchorage", "configuration": cfg, "mergedConfiguration": cfg})
		case "run-user-commands":
			if strings.HasPrefix(state.Failure, "hooks") {

				switch state.Failure {
				case "hooks_client_stopped":
					state.Primary["Running"], state.Primary["State"] = false, "exited"
					save()
					os.Exit(1)
				case "hooks_client_unreachable":
					if err := os.WriteFile(path+".unavailable", nil, 0600); err != nil {
						os.Exit(121)
					}
					os.Exit(1)
				case "hooks_client_binding":
					state.Primary["Hostname"] = "changed-after-client-failure"
					save()
					os.Exit(1)
				case "hooks_client_live":
					os.Exit(1)
				}
				if state.Failure == "hooks_signal" {
					syscall.Kill(os.Getpid(), syscall.SIGKILL)
				}
				if state.Failure == "hooks_shared" || state.Failure == "hooks_occupied" {
					raw, _ := json.Marshal(state.Primary)
					json.Unmarshal(raw, &state.Failed)
					state.Failed["Id"] = strings.Repeat("f", 64)
					state.Failed["Name"] = "/foreign"
					if state.Failure == "hooks_occupied" {
						state.Failed["Name"] = "/whr-failed-primary-" + state.Primary["Labels"].(map[string]any)["workharbor.promote.operation"].(string)
					}
					save()
				}
				os.Exit(98)
			}
			emit(map[string]string{"outcome": "success"})
		default:
			os.Exit(99)
		}
		os.Exit(0)
	}
	if kind != "docker" || len(args) < 3 || args[0] != "--host" {
		os.Exit(100)
	}
	args = args[2:]
	if len(args) < 2 {
		delegate()
	}
	state.Calls = append(state.Calls, append([]string{}, args...))
	save()

	if _, err := os.Stat(path + ".unavailable"); err == nil {
		os.Exit(1)
	}
	if args[0] == "ps" {
		for _, item := range []map[string]any{state.Primary, state.Candidate, state.Retained, state.Failed} {
			if item == nil {
				continue
			}
			labels, _ := item["Labels"].(map[string]any)
			matches := true
			for i, arg := range args {
				if arg == "--filter" && i+1 < len(args) {
					raw := strings.TrimPrefix(args[i+1], "label=")
					key, v, ok := strings.Cut(raw, "=")
					if !ok || labels[key] != v {
						matches = false
					}
				}
			}
			if matches {
				fmt.Println(item["Id"])
			}
		}
		os.Exit(0)
	}
	switch args[0] + ":" + args[1] {
	case "container:ls":
		for _, item := range []map[string]any{state.Primary, state.Candidate, state.Retained, state.Failed} {
			if item != nil {
				emit(map[string]any{"ID": item["Id"], "Status": item["State"]})
			}
		}
	case "container:inspect":
		format := value(args, "--format")
		for _, id := range args[4:] {
			var item map[string]any
			for _, v := range []map[string]any{state.Primary, state.Candidate, state.Retained, state.Failed} {
				if v != nil && v["Id"] == id {
					item = v
				}
			}
			if item == nil {
				os.Exit(101)
			}
			if strings.Contains(format, "{{.Name}} {{index") {
				labels := item["Labels"].(map[string]any)
				fmt.Printf("%s %s\n", item["Name"], labels["workharbor.stage.operation"])
			} else if format == inspectFormat {
				emit(map[string]any{"Id": item["Id"], "Name": item["Name"], "Image": item["ConfigImage"], "State": item["State"], "Labels": item["Labels"], "Mounts": item["Mounts"]})
			} else if format == stageContainerProjection {
				emit(item)
			} else {
				os.Exit(102)
			}
		}
	case "container:stop":
		if state.Failure == "hooks_stop_failure" && state.Primary != nil && state.Primary["Id"] == strings.Repeat("e", 64) {
			os.Exit(118)
		}
		if state.Primary == nil || state.Primary["Id"] != args[len(args)-1] {
			os.Exit(103)
		}
		if state.Failure == "still_running" {
			os.Exit(0)
		}
		state.Primary["Running"] = false
		state.Primary["State"] = "exited"
		save()

		if state.Primary["Id"] == strings.Repeat("e", 64) {
			mutateConfiguration("stop_failed")
		}
	case "container:rename":
		id, name := args[len(args)-2], args[len(args)-1]
		if state.Retained != nil && state.Retained["Id"] == id {
			state.Retained["Name"] = "/" + name
			state.Primary, state.Retained = state.Retained, nil
		} else if state.Primary != nil && state.Primary["Id"] == id {
			state.Primary["Name"] = "/" + name
			if strings.HasPrefix(name, "whr-failed-primary-") {
				state.Failed = state.Primary
			} else {
				state.Retained = state.Primary
			}
			state.Primary = nil
		} else {
			os.Exit(115)
		}
		save()
		if strings.HasPrefix(name, "whr-failed-primary-") {
			mutateConfiguration("retain_failed")
		}
		if name == "primary" && id == strings.Repeat("a", 64) {
			mutateConfiguration("restore_name")
		}

	case "container:start":
		if state.Failure == "hooks_start_failure" {
			os.Exit(119)
		}
		if state.Primary == nil || state.Primary["Id"] != args[len(args)-1] {
			os.Exit(117)
		}
		state.Primary["Running"], state.Primary["State"] = true, "running"
		save()
	case "container:rm":
		id := args[len(args)-1]
		if state.Candidate != nil && state.Candidate["Id"] == id {
			if state.Failure == "candidate_rm" {
				os.Exit(104)
			}
			state.Candidate = nil
			if state.Failure == "late_home_drift" {
				state.Volume.CreatedAt = "2000-01-01T00:00:00Z"
			}
		} else if state.Retained != nil && state.Retained["Id"] == id {
			if state.Failure == "retained_rm" {
				os.Exit(116)
			}
			state.Retained = nil
			if state.Failure == "retained_rm_late_shared" {
				state.Failed = map[string]any{"Id": strings.Repeat("f", 64), "Name": "/late-sharing", "State": "exited", "Labels": map[string]string{}, "Mounts": []map[string]any{{"Type": "volume", "Name": state.Volume.Name, "Source": state.Volume.Name, "Destination": "/foreign", "RW": true}}}
			}
		} else if state.Primary != nil && state.Primary["Id"] == id {
			state.Primary = nil
		} else {
			os.Exit(105)
		}
		save()
	case "volume:rm":
		if state.Failure == "home_rm" {
			os.Exit(106)
		}
		if state.Volume == nil || state.Volume.Name != args[len(args)-1] || args[len(args)-1] == "anchorage-home-owner" {
			os.Exit(107)
		}
		state.Volume = nil
		if state.Failure == "cleanup_input_drift" {
			labels := state.Primary["Labels"].(map[string]any)
			source := labels["devcontainer.local_folder"].(string)
			if err := os.WriteFile(filepath.Join(source, ".devcontainer", "setup.sh"), []byte("changed after cleanup\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		save()
	default:
		if args[0] == "run" {
			if state.Primary != nil {
				os.Exit(108)
			}
			mounts := []map[string]any{}
			labels := map[string]string{}
			env := []string{}
			for i, a := range args {
				if i+1 >= len(args) {
					continue
				}
				switch a {
				case "--mount":
					m, e := stageNativeMount(args[i+1])
					if e != nil {
						os.Exit(109)
					}
					entry := map[string]any{"Type": m["type"], "Source": m["source"], "Destination": m["target"], "RW": m["readonly"] != "true"}
					if m["type"] == "volume" {
						entry["Name"] = m["source"]
					}
					mounts = append(mounts, entry)
				case "-l", "--label":
					k, v, _ := strings.Cut(args[i+1], "=")
					labels[k] = v
				case "-e", "--env":
					env = append(env, args[i+1])
				}
			}
			state.Primary = map[string]any{"Id": strings.Repeat("e", 64), "Name": "/" + value(args, "--name"), "ImageID": "sha256:" + strings.Repeat("d", 64), "ConfigImage": "sha256:" + strings.Repeat("d", 64), "User": value(args, "--user"), "Hostname": value(args, "--hostname"), "State": "running", "Running": true, "Healthcheck": nil, "Health": nil, "Labels": labels, "Mounts": mounts, "Environment": env, "Entrypoint": []string{"/bin/sh"}, "Command": []string{"-c", "fixture native entrypoint"}}
			if state.Health {
				state.Primary["Healthcheck"] = map[string]any{"Test": []string{"CMD", "true"}}
				state.Primary["Health"] = "healthy"
			}
			if state.Failure == "unhealthy" {
				state.Primary["Health"] = "unhealthy"
			}
			if state.Failure == "missing_verification" {
				state.Primary["Healthcheck"] = nil
				state.Primary["Health"] = nil
			}
			if state.Failure == "wrong_image" {
				state.Primary["ImageID"] = "sha256:" + strings.Repeat("f", 64)
				state.Primary["ConfigImage"] = "sha256:" + strings.Repeat("f", 64)
			}
			save()
			fmt.Println(strings.Repeat("e", 64))
		} else if args[0] == "exec" {

			switch state.Failure {
			case "smoke_client_stopped":
				state.Primary["Running"], state.Primary["State"] = false, "exited"
				save()
				os.Exit(1)
			case "smoke_client_unreachable":
				if err := os.WriteFile(path+".unavailable", nil, 0600); err != nil {
					os.Exit(121)
				}
				os.Exit(1)
			case "smoke_client_live":
				os.Exit(1)
			}
			if strings.HasPrefix(state.Failure, "smoke_reserved_") {
				code, _ := strconv.Atoi(strings.TrimPrefix(state.Failure, "smoke_reserved_"))
				os.Exit(code)
			}
			if state.Failure == "smoke" {
				os.Exit(110)
			}
			data, _ := io.ReadAll(os.Stdin)
			state.SmokeStdin = string(data)
			save()
			fmt.Print("\x1b[31mpromote-smoke\n")
		} else {
			delegate()
		}
	}
	os.Exit(0)
}
