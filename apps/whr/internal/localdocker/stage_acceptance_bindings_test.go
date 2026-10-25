// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStageRejectsUnresolvedLabel(t *testing.T) {
	source := t.TempDir()
	primary := inspection{Labels: map[string]string{"devcontainer.local_folder": filepath.Join(source, "missing")}, Mounts: []mount{{Type: "bind", Source: source, Destination: "/workspaces/project", RW: true}, {Type: "volume", Name: "anchorage-home-owner", Destination: "/home/vscode", RW: true}}}
	_, _, _, err := bindStagePrimary(primary, source)
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || err.Error() != "local stage primary physical source binding mismatch" {
		t.Fatalf("unresolved source label refusal: %v", err)
	}
}

func TestLocalStageRejectsChangedInputs(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	stage, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	if err = os.WriteFile(filepath.Join(opts.Source, ".devcontainer", "setup.sh"), []byte("printf changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "source inputs changed since capture") || result.Status != "refused" {
		t.Fatalf("source drift refusal: %+v %v", result, err)
	}
	state := readFakeStageState(t, path)
	if state.Candidate != nil || state.Volume != nil {
		t.Fatal("source drift allocated resources")
	}
	if _, err = os.Stat(ports.StateRoot); !os.IsNotExist(err) {
		t.Fatalf("source drift wrote state: %v", err)
	}
}

func TestLocalStageHealthAndSmoke(t *testing.T) {
	for _, smoke := range []bool{false, true} {
		name := "health"
		if smoke {
			name = "health_and_smoke"
		}
		t.Run(name, func(t *testing.T) {
			c, opts, ports, path := fixtureStageLifecycle(t, true)
			opts.Smoke = smoke
			stage, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if err != nil || result.Status != "staged_verified" || result.Verification == nil || result.Verification.Status != "passed" || result.Verification.Policy != name {
				t.Fatalf("verification policy: %+v %v", result, err)
			}
			state := readFakeStageState(t, path)
			if (state.SmokeStdin != "") != smoke {
				t.Fatalf("smoke execution differs from explicit option: %q", state.SmokeStdin)
			}
			if state.Primary["Running"] != true {
				t.Fatal("verification stopped original primary")
			}
		})
	}
}
