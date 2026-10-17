// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func TestExecRunnerEndsWithinItsTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{time.Second, 3 * time.Second} {
		p := shim(t, "sleep 300 &\nwait")
		start := time.Now()
		_, err := workspacecfg.ExecRunner{}.Run(context.Background(), workspacecfg.GitCmd{Path: p, Env: []string{"PATH=" + os.Getenv("PATH")}, Dir: "/", MaxOutput: 4096, Timeout: timeout})
		if err == nil {
			t.Error("no error on timeout")
		}
		if elapsed := time.Since(start); elapsed >= timeout {
			t.Errorf("timeout %v: the run lasted %v, longer than its budget", timeout, elapsed)
		}
	}
}

func TestManifestDepthCountsObjects(t *testing.T) {
	nest := func(n int) []byte {
		return []byte(strings.Repeat(`{"a":`, n) + "1" + strings.Repeat("}", n))
	}
	if got := codesOf(workspacecfg.CheckManifest(nest(129)).Findings); got != "manifest.syntax" {
		t.Errorf("129 nested objects: %q", got)
	}
	if got := codesOf(workspacecfg.CheckManifest(nest(128)).Findings); got == "manifest.syntax" {
		t.Errorf("128 nested objects are within the bound: %q", got)
	}
}

func TestManifestProjectURLHyphenAuthority(t *testing.T) {
	for _, u := range []string{"https://-h/o/a", "ssh://u@-h/o/a", "ssh://-u@h/o/a"} {
		doc := fmt.Sprintf(`{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"project":{"url":%q,"path":"projects/a"}}`, u)
		if got := codesOf(workspacecfg.CheckManifest([]byte(doc)).Findings); got != "manifest.schema" {
			t.Errorf("%s: %q", u, got)
		}
	}
}
