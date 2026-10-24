// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package commands_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

func TestBundleDriftDefaultHandlerOffline(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "whr-drift-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(source, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	pin := "quay.io/bundledock/toolbox:fixture@sha256:" + strings.Repeat("a", 64)
	volume := "bundledock-toolbox-" + strings.Repeat("a", 64)
	config := fmt.Sprintf(`{"initializeCommand":{"toolbox":["sh","${localWorkspaceFolder}/.devcontainer/initialize.sh",%q,%q,"toolbox"]},"mounts":[%q]}`, pin, volume, "type=volume,source="+volume+",target=/opt/bdock/toolbox,readonly,volume-nocopy")
	if err := os.WriteFile(filepath.Join(source, ".devcontainer/devcontainer.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "forbidden")
	for _, status := range []string{"in_sync", "drift", "cannot_determine"} {
		mounted := volume
		if status == "drift" {
			mounted = "bundledock-toolbox-" + strings.Repeat("b", 64)
		}
		if status == "cannot_determine" {
			mounted = "opaque-custom-volume"
		}
		script := fmt.Sprintf(`#!/bin/sh
 test "$1" = '--host=unix://%s' || exit 98
 test -z "${GITHUB_TOKEN-}" && test -z "${DOCKER_AUTH_CONFIG-}" && test -z "${OPENAI_API_KEY-}" || exit 97
 test -d "$DOCKER_CONFIG" && test ! -e "$DOCKER_CONFIG/config.json" || exit 96
 case "$2 $3" in
 'container ls') test "$#" -eq 7 && test "$4" = --all && test "$5" = --no-trunc && test "$6" = --format && test "$7" = '{{json .}}' || exit 95;printf '%%s\n' '{"ID":"%s","Status":"fixture"}';;
 'container inspect') test "$#" -eq 6 && test "$4" = --format && test "$5" = '{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Config.Image}},"State":{{json .State.Status}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}}}' && test "$6" = '%s' || exit 94; printf '%%s\n' '{"Id":"%s","Name":"/primary","State":"running","Labels":{"devcontainer.local_folder":"%s"},"Mounts":[{"Type":"volume","Name":"%s","Destination":"/opt/bdock/toolbox","RW":false}]}';;
 *) touch '%s';exit 99;;esac
`, socket, strings.Repeat("a", 64), strings.Repeat("a", 64), strings.Repeat("a", 64), source, mounted, log)
		if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("DOCKER_HOST", "unix://"+socket)
		for _, key := range []string{"GITHUB_TOKEN", "DOCKER_AUTH_CONFIG", "OPENAI_API_KEY"} {
			t.Setenv(key, "credential-canary")
		}
		var out, diag bytes.Buffer
		code := commands.Execute(context.Background(), []string{"workspace", "drift", "--local", "--source", source, "primary", "--json"}, commands.Env{Stdout: &out, Stderr: &diag}, commands.DefaultHandlers())
		want := 0
		if status != "in_sync" {
			want = 68
		}
		if code != want {
			t.Fatalf("%s exit=%d %s %s", status, code, out.String(), diag.String())
		}
		envelope := decodeEnvelope(t, out.String())
		if err := compileSchema(t).Validate(envelope); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"status":"`+status+`"`) || strings.Contains(out.String(), "credential-canary") || strings.Contains(out.String(), "opaque-custom-volume") {
			t.Fatal(out.String())
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("runtime mutation attempted")
	}
}
