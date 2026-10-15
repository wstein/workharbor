// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNativeLiveCIDEnablesExactOfficialDiscoveryAndInspection(t *testing.T) {
	p, run, ctx := nativeExecutionFixture(t)
	a, e := reserveAllocation(ctx, p.lifetime.root, p.scope, p.policy.DataPath, run.Run.ImageID, p.check)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if e = a.Begin(ctx); e != nil {
		t.Fatal(e)
	}
	p.allocation = a
	id := strings.Repeat("a", 64)
	if e = os.WriteFile(a.cid, []byte(id), 0644); e != nil {
		t.Fatal(e)
	}
	c := nativeContainerFixture()
	c["Config"].(map[string]any)["User"] = p.policy.ContainerUser
	c["Config"].(map[string]any)["Labels"] = run.Run.Labels
	c["Mounts"] = []any{map[string]any{"Type": "bind", "Source": p.policy.DataPath, "Destination": "/workspace", "RW": true}}
	raw, _ := json.Marshal([]any{c})
	old := p.query
	p.query = func(ctx context.Context, args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte(id + "\n"), nil
		}
		if args[0] == "inspect" && args[2] == "container" {
			return raw, nil
		}
		return old(ctx, args)
	}
	gateway := &Gateway{Parser: p.parser, Policy: copyPolicy(p.policy), Port: p, Authorize: p.check, Images: copyImages(p.images)}
	discovery := []string{"ps", "-q", "-a"}
	for _, l := range p.scope.LabelList() {
		discovery = append(discovery, "--filter", "label="+l)
	}
	out, e := gateway.Forward(ctx, discovery)
	if e != nil || string(out) != id+"\n" {
		t.Fatal("live allocation undiscoverable", string(out), e)
	}
	out, e = gateway.Forward(ctx, []string{"inspect", "--type", "container", id})
	if e != nil || len(out) == 0 {
		t.Fatal("exact live inspection unavailable", e)
	}
	a.End()
	if gateway.Target == nil || gateway.Target.ContainerID != id {
		t.Fatal("target not frozen")
	}
	// Once bound, a foreign label/name collision must refuse instead of returning a first match.
	p.query = func(ctx context.Context, args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte(id + "\n" + strings.Repeat("c", 64) + "\n"), nil
		}
		return old(ctx, args)
	}
	if _, e = gateway.Forward(ctx, discovery); e == nil {
		t.Fatal("ambiguous discovery accepted")
	}
}
func TestFrozenPolicyDoesNotShareMutableAdmissionMaps(t *testing.T) {
	original := policyFixture(Resources)
	frozen := copyPolicy(original)
	original.KnownIDs[strings.Repeat("d", 64)] = true
	original.Build.Args["BUILDKIT_SYNTAX"] = "evil"
	original.ContainerEnv["LEAK"] = "secret"
	if len(frozen.KnownIDs) != 1 || frozen.Build.Args["BUILDKIT_SYNTAX"] != "" || frozen.ContainerEnv["LEAK"] != "" {
		t.Fatal("mutable caller retargeted frozen policy")
	}
}
