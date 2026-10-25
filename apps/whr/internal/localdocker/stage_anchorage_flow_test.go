//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStageAnchorageFactsRemainIndependentThroughPromotion(t *testing.T) {
	c, opts, ports, path := fixturePromotionLifecycle(t)
	opts.Smoke = true
	state := readFakePromotionState(t, path)
	state.RequireFreshFacts = true
	writeFakePromotionState(t, path, state)
	original := stageFactsFixture()
	factsPath := filepath.Join(opts.Source, ".devcontainer", "host.env")
	if err := os.WriteFile(factsPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"image": "example:local", "remoteUser": "vscode", "initializeCommand": []any{"sh", "${localWorkspaceFolder}/.devcontainer/host-info.sh"}, "postCreateCommand": []any{"sh", ".devcontainer/setup.sh"}}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(opts.Source, ".devcontainer", "devcontainer.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("Anchorage stage: %+v %v", result, err)
	}
	generated, err := os.ReadFile(filepath.Join(s.clonePath, ".devcontainer", "host.env"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(generated, original) {
		t.Fatal("candidate facts did not come from fresh E producer")
	}
	if got, err := os.ReadFile(factsPath); err != nil || !bytes.Equal(got, original) {
		t.Fatal("stage changed admitted original facts")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(original, []byte("HOST_NAME='captain'"), []byte("HOST_NAME='owner-changed'"), 1)
	if err = os.WriteFile(factsPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	drift, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64)}, ports)
	if drift != nil {
		drift.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "promotion receipt-bound host facts changed") {
		t.Fatalf("facts drift: %v", err)
	}
	if err = os.WriteFile(factsPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64)}, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	result, err = p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("Anchorage promotion: %+v %v", result, err)
	}
	if got, err := os.ReadFile(factsPath); err != nil || !bytes.Equal(got, original) {
		t.Fatal("promotion overwrote original facts with E/W output")
	}
}
