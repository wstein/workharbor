// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"encoding/json"
	"strings"
	"testing"
)

func nativeContainerFixture() map[string]any {
	p := policyFixture(Resources)
	return map[string]any{"Id": strings.Repeat("a", 64), "Name": "/" + p.Scope.Name(), "Image": "sha256:" + strings.Repeat("b", 64), "Config": map[string]any{"User": "1000:1000", "Labels": p.Scope.Labels()}, "HostConfig": map[string]any{"UsernsMode": ""}, "Mounts": []any{map[string]any{"Type": "bind", "Source": p.DataPath, "Destination": "/workspace", "RW": true}}, "State": map[string]any{"Running": true}}
}
func TestNativeInspectionPreservesFullCandidateSetAndRuntimeUser(t *testing.T) {
	c := nativeContainerFixture()
	raw, _ := json.Marshal([]any{c, c})
	all, e := nativeContainers(raw)
	if e != nil || len(all) != 2 {
		t.Fatal("candidate truncation", e, all)
	}
	if all[0].User != "1000:1000" || all[0].UsernsMode != "" || !all[0].Running {
		t.Fatal("runtime owner/state omitted", all[0])
	}
	if _, e = VerifyRuntime(policyFixture(Resources).Scope, policyFixture(Resources).DataPath, all); e == nil {
		t.Fatal("duplicate adopted")
	}
}
func TestNativeInspectionRejectsAmbiguousOrPartialSecurityFields(t *testing.T) {
	for _, field := range []string{"Id", "Config", "HostConfig", "Mounts", "State"} {
		c := nativeContainerFixture()
		delete(c, field)
		raw, _ := json.Marshal([]any{c})
		if _, e := nativeContainers(raw); e == nil {
			t.Fatal("partial accepted", field)
		}
	}
	for _, raw := range []string{`[{"Id":"x","Id":"y"}]`, `[{"Config":{"User":"0:0","User":"1000:1000"}}]`, `[]garbage`} {
		if _, e := nativeContainers([]byte(raw)); e == nil {
			t.Fatal("ambiguous accepted")
		}
	}
}
func TestNativeInformationRequiresActualExplicitSecurityOptions(t *testing.T) {
	raw := []byte(`{"OSType":"linux","Architecture":"aarch64","ServerVersion":"29.8.2","SecurityOptions":[]}`)
	info, e := nativeInformation(raw)
	if e != nil || !supportedEngine(info) {
		t.Fatal("valid actual info", e, info)
	}
	for _, raw := range []string{`{"OSType":"linux","Architecture":"aarch64","ServerVersion":"29.8.2"}`, `{"OSType":"linux","Architecture":"aarch64","ServerVersion":"29.8.2","SecurityOptions":null}`, `{"OSType":"linux","Architecture":"aarch64","ServerVersion":"29.8.2","SecurityOptions":["name=userns"]}`} {
		info, e := nativeInformation([]byte(raw))
		if e == nil && supportedEngine(info) {
			t.Fatal("unproved daemon accepted")
		}
	}
}
