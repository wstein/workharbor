// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"strings"
	"testing"
)

func TestNativeRunUsesVerifiedImageIDAndPrivateCIDOnly(t *testing.T) {
	p := policyFixture(Resources)
	c, e := Classify(p, runFixture(p))
	if e != nil {
		t.Fatal(e)
	}
	c.Scope = p.Scope
	c.Run.ImageID = "sha256:" + strings.Repeat("b", 64)
	cid := "/private/operation/" + p.Scope.RuntimeID + ".cid"
	args, e := nativeArguments(c, cid)
	if e != nil {
		t.Fatal(e)
	}
	if !containsPair(args, "--cidfile", cid) || !containsPair(args, "--pull", "never") || !containsPair(args, "--name", p.Scope.Name()) {
		t.Fatal(args)
	}
	foundID := false
	for _, arg := range args {
		if arg == c.Run.Image {
			t.Fatal("mutable/reference run image forwarded")
		}
		if arg == c.Run.ImageID {
			foundID = true
		}
	}
	if !foundID {
		t.Fatal("verified image ID absent")
	}
	if _, e = nativeArguments(c, "/foreign/cid"); e == nil {
		t.Fatal("unbound CID path accepted")
	}
}
func containsPair(args []string, k, v string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == k && args[i+1] == v {
			return true
		}
	}
	return false
}
func TestNativeEventsAlwaysCarryCompleteReservedScopeFilters(t *testing.T) {
	s := scopeFixture()
	args, e := nativeArguments(Call{Kind: "events", Scope: s}, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, label := range s.LabelList() {
		if !containsPair(args, "--filter", "label="+label) {
			t.Fatal("event escaped complete scope", args)
		}
	}
	if _, e = nativeArguments(Call{Kind: "events"}, ""); e == nil {
		t.Fatal("unscoped events accepted")
	}
}
