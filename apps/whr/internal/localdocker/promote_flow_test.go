// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureVerifiedPromotion(t *testing.T) (*LocalPromotion, string, stagePreparationPorts) {
	t.Helper()
	c, opts, ports, path := fixturePromotionLifecycle(t)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("stage producer: %+v %v", result, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	state := readFakePromotionState(t, path)
	state.Promotion = true
	if err = os.WriteFile(path+".promotion", []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	writeFakePromotionState(t, path, state)
	var diagnostic bytes.Buffer
	p, err := c.preparePromotion(context.Background(), PromoteOptions{Target: strings.Repeat("a", 64), Diagnostics: &diagnostic}, ports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p, path, ports
}
func TestLocalPromoteUsesVerifiedImage(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	before := readFakePromotionState(t, path)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("promote: %+v %v; repeated boundary: %v", result, err, p.checkBeforePhase(context.Background(), false))
	}
	state := readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["ImageID"] != "sha256:"+strings.Repeat("d", 64) || state.Primary["ConfigImage"] != "sha256:"+strings.Repeat("d", 64) || state.Primary["Id"] == before.Primary["Id"] || state.Candidate != nil || state.Volume != nil {
		t.Fatalf("native promotion resources: %+v", state)
	}
	for _, phase := range state.Phases {
		var c, o string
		for i, v := range phase {
			if i+1 < len(phase) {
				if v == "--config" {
					c = phase[i+1]
				}
				if v == "--override-config" {
					o = phase[i+1]
				}
			}
		}
		if c != o || filepath.Base(c) != "devcontainer.json" {
			t.Fatalf("C=P=O: %q", phase)
		}
	}
	if _, err = os.Stat(p.snapshots[0].ClonePath); err != nil {
		t.Fatalf("retained clone: %v", err)
	}
}
