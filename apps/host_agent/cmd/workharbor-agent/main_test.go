// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestScaffoldRefusesProductCommands(t *testing.T) {
	for _, args := range [][]string{{"setup"}, {"ws", "create"}, {"--version", "setup"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code == 0 {
			t.Fatalf("run(%q) succeeded before product commands exist", args)
		}
		if out.Len() != 0 || !strings.Contains(errOut.String(), "not implemented") {
			t.Fatalf("run(%q): stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}

func TestRunRefusesMissingIdentityWithFixedDiagnostic(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--identity-dir", "/missing/identity"}, &out, &errOut); code != 1 || out.Len() != 0 || errOut.String() != "workharbor-agent: host_transport_unavailable\n" {
		t.Fatalf("unexpected run refusal: %d %q %q", code, out.String(), errOut.String())
	}
}

func TestPrivateDockerProxyModeRejectsMalformedLifetimeAndSocket(t *testing.T) {
	for _, args := range [][]string{{"__devcontainer-docker"}, {"__devcontainer-docker", "/missing/socket", "0", "-v"}, {"__devcontainer-docker", "/missing/socket", "not-time", "exec"}} {
		var out, diagnostic bytes.Buffer
		code := run(args, &out, &diagnostic)
		if code != 1 || out.Len() != 0 || diagnostic.String() != "workharbor-agent: provider_driver_unavailable\n" {
			t.Fatal("private helper accepted or disclosed args", code, out.String(), diagnostic.String())
		}
	}
}
