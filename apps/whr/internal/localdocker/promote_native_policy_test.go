//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalPromoteProjectsNativeRetentionPolicy(t *testing.T) {
	tmpl, err := template.New("inspect").Funcs(template.FuncMap{"json": func(v any) string { b, _ := json.Marshal(v); return string(b) }}).Parse(stageContainerProjection)
	if err != nil {
		t.Fatal(err)
	}
	native := map[string]any{"HostConfig": map[string]any{"AutoRemove": true, "RestartPolicy": map[string]any{"Name": "always", "MaximumRetryCount": 7}}}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, native); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	policy, ok := fields["RestartPolicy"].(map[string]any)
	if fields["AutoRemove"] != true || !ok || policy["Name"] != "always" || policy["MaximumRetryCount"] != float64(7) {
		t.Fatalf("native HostConfig not selected: %s", output.Bytes())
	}
}

func TestLocalPromoteRefusesUnsafeOriginalNativePolicy(t *testing.T) {
	p, path, ports := fixtureVerifiedPromotion(t)
	storePath := p.store.path
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		auto            bool
		restart, reason string
	}{
		{"auto_remove", true, "no", "promotion original primary AutoRemove must be disabled"},
		{"always", false, "always", "promotion original primary restart policy must be disabled"},
		{"unless_stopped", false, "unless-stopped", "promotion original primary restart policy must be disabled"},
		{"on_failure", false, "on-failure", "promotion original primary restart policy must be disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := readFakePromotionState(t, path)
			state.Primary["AutoRemove"] = tc.auto
			state.Primary["RestartPolicy"] = map[string]any{"Name": tc.restart, "MaximumRetryCount": 0}
			writeFakePromotionState(t, path, state)
			journal, err := os.ReadFile(filepath.Join(storePath, "journal.json"))
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := (Client{}).preparePromotion(context.Background(), PromoteOptions{Target: p.primaryObservation.ID}, ports)
			if fresh != nil {
				fresh.Close()
			}
			if err == nil || exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("policy refusal: %v", err)
			}
			after := readFakePromotionState(t, path)
			assertNativePolicyNoEffects(t, state, after)
			current, err := os.ReadFile(filepath.Join(storePath, "journal.json"))
			if err != nil || !bytes.Equal(journal, current) {
				t.Fatal("policy refusal changed Git/Docker effect journal")
			}
		})
	}
}

func assertNativePolicyNoEffects(t *testing.T, before, after fakePromotionState) {
	t.Helper()
	for _, call := range after.Calls[len(before.Calls):] {
		if len(call) > 1 && call[0] == "container" && (call[1] == "stop" || call[1] == "rename" || call[1] == "rm" || call[1] == "start") {
			t.Fatalf("unsafe native effect: %v", call)
		}
	}
	if len(after.Phases) != len(before.Phases) {
		t.Fatal("unsafe policy launched official CLI phase")
	}
}

func TestLocalPromoteRechecksNativePolicyBeforeEffects(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	state.Primary["AutoRemove"] = true
	writeFakePromotionState(t, path, state)
	assertPromotionRefusal(t, p, path, LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}, exitcode.Conflict, "promotion original primary AutoRemove must be disabled")
	assertNativePolicyNoEffects(t, state, readFakePromotionState(t, path))
}

func TestLocalPromoteAllowsDisabledNativePolicy(t *testing.T) {
	p, path, ports := fixtureVerifiedPromotion(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "no"} {
		t.Run(name, func(t *testing.T) {
			state := readFakePromotionState(t, path)
			state.Primary["AutoRemove"] = false
			state.Primary["RestartPolicy"] = map[string]any{"Name": name, "MaximumRetryCount": 0}
			writeFakePromotionState(t, path, state)
			fresh, err := (Client{}).preparePromotion(context.Background(), PromoteOptions{Target: p.primaryObservation.ID}, ports)
			if err != nil {
				t.Fatalf("disabled policy refused: %v", err)
			}
			fresh.Close()
		})
	}
}
