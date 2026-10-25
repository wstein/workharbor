//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func setStageFixtureHook(t *testing.T, source string, hook any) {
	t.Helper()
	path := filepath.Join(source, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["postCreateCommand"] = hook
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLocalStageNamedHookDependencyDriftRefusesPromotion(t *testing.T) {
	for _, side := range []string{"candidate", "original"} {
		t.Run(side, func(t *testing.T) {
			c, opts, ports, path := fixturePromotionLifecycle(t)
			opts.Smoke = true
			if err := os.Mkdir(filepath.Join(opts.Source, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opts.Source, "scripts", "setup.sh"), []byte("printf admitted-hook\\n\n"), 0600); err != nil {
				t.Fatal(err)
			}
			setStageFixtureHook(t, opts.Source, "sh scripts/setup.sh")
			s, err := c.prepareStage(context.Background(), opts, ports)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if err != nil || result.Status != "staged_verified" {
				t.Fatalf("stage: %+v %v", result, err)
			}
			root := s.clonePath
			reason := "promotion candidate execution inputs changed"
			if side == "original" {
				root = opts.Source
				reason = "promotion original execution inputs changed"
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "scripts", "setup.sh"), []byte("printf changed-hook\\n\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
				t.Fatal(err)
			}
			before := readFakePromotionState(t, path)
			p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64)}, ports)
			if p != nil {
				p.Close()
			}
			if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("%s named dependency drift: %v", side, err)
			}
			after := readFakePromotionState(t, path)
			if stageDigest(before.Primary) != stageDigest(after.Primary) || stageDigest(before.Candidate) != stageDigest(after.Candidate) || stageDigest(before.Volume) != stageDigest(after.Volume) {
				t.Fatal("dependency drift caused native lifecycle effect")
			}
		})
	}
}
func TestLocalStageUnknownGitExecutionIdentityRefusesBeforeEffects(t *testing.T) {
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	setStageFixtureHook(t, opts.Source, "git rev-parse HEAD")
	s, err := c.prepareStage(context.Background(), opts, ports)
	if s != nil {
		s.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "Git execution identity capability unsupported") {
		t.Fatalf("unbound actual Git consumption: %v", err)
	}
	state := readFakeStageState(t, path)
	if state.Candidate != nil || state.Volume != nil {
		t.Fatal("unsupported Git identity caused allocation")
	}
	if _, err = os.Lstat(ports.StateRoot); !os.IsNotExist(err) {
		t.Fatal("unsupported Git identity created durable operation")
	}
}
