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

// Exercise DefaultHandlers through a fixture-only Unix listener and Docker shim;
// no actual Docker daemon, binary build or installation participates.
func TestLocalRmDefaultHandlerUsesGuardedBackend(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "whr-rm-")
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
	id := strings.Repeat("a", 64)
	log := filepath.Join(dir, "mutations")
	script := fmt.Sprintf(`#!/bin/sh
 test "$1" = '--host=unix://%s' || exit 98
 test -z "${GITHUB_TOKEN-}" && test -z "${DOCKER_AUTH_CONFIG-}" && test -z "${ANTHROPIC_API_KEY-}" || exit 97
 case "$2 $3" in
 'container ls') printf '%%s\n' '{"ID":"%s","Status":"fixture"}';;
 'container inspect') printf '%%s\n' '{"Id":"%s","Name":"/sample","Image":"fixture","State":"running","Labels":{"devcontainer.local_folder":"/fixture"},"Mounts":[{"Type":"volume","Name":"home","Destination":"/home/captain","RW":true}]}';;
 'rm -f') test "$#" -eq 5 && test "$4" = -- && test "$5" = '%s' || exit 96; printf '%%s\n' "$5" >> '%s';printf '%%s\n' "$5";;
 *) exit 95;;
 esac
`, socket, id, id, id, log)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_HOST", "unix://"+socket)
	for _, k := range []string{"GITHUB_TOKEN", "DOCKER_AUTH_CONFIG", "ANTHROPIC_API_KEY"} {
		t.Setenv(k, "fixture-canary")
	}
	for _, dry := range []bool{true, false} {
		args := []string{"rm", "--local", "sample", "--yes", "--json"}
		if dry {
			args = append(args, "--dry-run")
		}
		var out, diag bytes.Buffer
		code := commands.Execute(context.Background(), args, commands.Env{Stdout: &out, Stderr: &diag}, commands.DefaultHandlers())
		if code != 0 || diag.Len() != 0 {
			t.Fatalf("dry=%t code=%d stdout=%s stderr=%s", dry, code, out.String(), diag.String())
		}
		envelope := decodeEnvelope(t, out.String())
		if err := compileSchema(t).Validate(envelope); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"preserve_home_volume":true`) || !strings.Contains(out.String(), `"preserved_home_volumes":["home"]`) {
			t.Fatal(out.String())
		}
		raw, e := os.ReadFile(log)
		if dry {
			if !os.IsNotExist(e) {
				t.Fatalf("dry run mutated: %q %v", raw, e)
			}
		} else if e != nil || string(raw) != id+"\n" {
			t.Fatalf("mutations:%q error:%v", raw, e)
		}
	}
}
