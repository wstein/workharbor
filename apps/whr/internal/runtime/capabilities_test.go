// SPDX-License-Identifier: EUPL-1.2

package runtime_test

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
)

// contractFields parses the capability table of contracts/runtime-provider.md:
// field name to the set of allowed values.
func contractFields(t *testing.T) (order []string, values map[string][]string) {
	t.Helper()
	text := repoFile(t, "contracts", "runtime-provider.md")
	start := strings.Index(text, "## Capability report")
	end := strings.Index(text, "## Go interface")
	if start < 0 || end < start {
		t.Fatal("capability section not found in runtime-provider.md")
	}
	row := regexp.MustCompile("^\\| `([a-z_]+)` \\| ([^|]*) \\|")
	token := regexp.MustCompile("`([a-z_]+)`")
	values = map[string][]string{}
	for _, line := range strings.Split(text[start:end], "\n") {
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var vs []string
		for _, tm := range token.FindAllStringSubmatch(m[2], -1) {
			if tm[1] == "leftover_report" { // a bool beside the value, not a value
				continue
			}
			vs = append(vs, tm[1])
		}
		order = append(order, m[1])
		values[m[1]] = vs
	}
	return order, values
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestCapabilityFieldsCompleteAndTyped(t *testing.T) {
	order, want := contractFields(t)
	if len(order) != 11 {
		t.Fatalf("contract lists %d fields, the data model says eleven: %v", len(order), order)
	}

	specs := runtime.FieldSpecs()
	if len(specs) != len(order) {
		t.Fatalf("code has %d fields, contract %d", len(specs), len(order))
	}
	for i, spec := range specs {
		if string(spec.Name) != order[i] {
			t.Errorf("field %d: %q, contract %q", i, spec.Name, order[i])
		}
		if !reflect.DeepEqual(sorted(spec.Values), sorted(want[order[i]])) {
			t.Errorf("field %q values %v, contract %v", spec.Name, spec.Values, want[order[i]])
		}
	}

	// Every field of Capabilities is a typed Capability (or the cleanup
	// variant) with a json tag equal to a contract name, and nothing else.
	typ := reflect.TypeOf(runtime.Capabilities{})
	capType := reflect.TypeOf(runtime.Capability{})
	cleanType := reflect.TypeOf(runtime.CleanupCapability{})
	tags := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "profile" {
			if f.Type != reflect.TypeOf(runtime.TrustProfile("")) {
				t.Errorf("profile has type %v", f.Type)
			}
			continue
		}
		if f.Type != capType && f.Type != cleanType {
			t.Errorf("field %s has type %v, want Capability", f.Name, f.Type)
		}
		tags[tag] = true
	}
	if len(tags) != len(order) {
		t.Errorf("Capabilities has %d capability fields, contract %d", len(tags), len(order))
	}
	for _, name := range order {
		if !tags[name] {
			t.Errorf("Capabilities lacks field %q", name)
		}
	}
	if cleanType.NumField() != 2 || cleanType.Field(1).Name != "LeftoverReport" || cleanType.Field(1).Type.Kind() != reflect.Bool {
		t.Error("cancellation_cleanup must carry the leftover_report bool")
	}

	// Field lookup agrees with the specs and every status is typed.
	var caps runtime.Capabilities
	for _, spec := range specs {
		if _, ok := caps.Field(spec.Name); !ok {
			t.Errorf("Field(%q) not found", spec.Name)
		}
	}
	for _, s := range []runtime.Status{runtime.StatusEnforced, runtime.StatusNotEnforced, runtime.StatusUnknown} {
		if !s.Valid() {
			t.Errorf("status %q must be valid", s)
		}
	}
	if runtime.Status("maybe").Valid() || runtime.Status("").Valid() {
		t.Error("only the three contract statuses are valid")
	}

	// Validate: complete, typed, scoped.
	complete := fullCaps(runtime.ProfileDeveloper)
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete report rejected: %v", err)
	}
	bad := fullCaps(runtime.ProfileDeveloper)
	bad.HomeState.Values = []string{"persistent_volume", "tmpfs"}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "home_state") {
		t.Errorf("untyped value accepted: %v", err)
	}
	bad = fullCaps(runtime.ProfileDeveloper)
	bad.EgressEnforcement.Scope = ""
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "egress_enforcement") {
		t.Errorf("empty scope accepted: %v", err)
	}
	bad = fullCaps(runtime.ProfileDeveloper)
	bad.IDEAccess.Status = "maybe"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "ide_access") {
		t.Errorf("invalid status accepted: %v", err)
	}
	bad = fullCaps(runtime.TrustProfile("root"))
	if err := bad.Validate(); err == nil {
		t.Error("invalid profile accepted")
	}
	if err := (runtime.Capabilities{}).Validate(); err == nil {
		t.Error("the zero report is not complete")
	}
}

func TestCapabilityUnknownIsNotEnforced(t *testing.T) {
	for _, status := range []runtime.Status{"", runtime.StatusUnknown, runtime.StatusNotEnforced, "ENFORCED", "enforced ", "maybe"} {
		c := runtime.Capability{Status: status, Values: []string{"microvm"}, Scope: "test"}
		if c.Enforced() {
			t.Errorf("status %q must not be enforced", status)
		}
		if c.Provides("microvm") {
			t.Errorf("status %q must not provide a value", status)
		}
	}
	enforced := runtime.Capability{Status: runtime.StatusEnforced, Values: []string{"shared_kernel"}, Scope: "test"}
	if !enforced.Enforced() || !enforced.Provides("shared_kernel") {
		t.Error("enforced value must be provided")
	}
	if enforced.Provides("microvm") || enforced.Provides("") {
		t.Error("an enforced capability provides only its own values")
	}
	if (runtime.Capability{Status: runtime.StatusEnforced}).Provides("") {
		t.Error("enforced with no value provides nothing")
	}

	// The zero Capabilities and an unknown field name enforce nothing.
	var zero runtime.Capabilities
	for _, spec := range runtime.FieldSpecs() {
		c, ok := zero.Field(spec.Name)
		if !ok || c.Enforced() {
			t.Errorf("zero %q: ok=%v enforced=%v", spec.Name, ok, c.Enforced())
		}
	}
	c, ok := fullCaps(runtime.ProfileDeveloper).Field("interactive_shell")
	if ok || c.Enforced() || c.Status != runtime.StatusUnknown {
		t.Errorf("an unlisted field must read as unknown and not enforced: %+v ok=%v", c, ok)
	}
}

func capability(status runtime.Status, scope string, values ...string) runtime.Capability {
	return runtime.Capability{Status: status, Values: values, Scope: scope}
}

// fullCaps is the report of a standard shared-kernel container provider for
// one profile, as contracts/runtime-provider.md describes the Stage 2 values.
func fullCaps(profile runtime.TrustProfile) runtime.Capabilities {
	const enforced = runtime.StatusEnforced
	return runtime.Capabilities{
		Profile:             profile,
		KernelIsolation:     capability(enforced, "container shares the Docker VM kernel", "shared_kernel"),
		FilesystemAccess:    capability(enforced, "mounts planned by whr", "private_copy", "bind_rw", "bind_ro", "linked_worktree"),
		GitAccess:           capability(enforced, "mounts planned by whr", "private_clone", "linked_worktree", "primary_rw"),
		GitConfigProfile:    capability(enforced, "preflight and canary tests only", "host_global_dir_rw", "agent_policy_ro"),
		HomeState:           capability(enforced, "volume or scratch of the instance", "persistent_volume", "scratch"),
		ToolMounts:          capability(enforced, "tool files only", "ro_bdock"),
		EgressEnforcement:   capability(runtime.StatusNotEnforced, "proxy variables enforce nothing", "none"),
		CredentialMediation: capability(runtime.StatusNotEnforced, "none provided", "none"),
		HostTools:           capability(runtime.StatusNotEnforced, "none provided", "none"),
		IDEAccess:           capability(runtime.StatusUnknown, "unverified"),
		CancellationCleanup: runtime.CleanupCapability{
			Capability:     capability(enforced, "labelled objects of the instance", "graceful_then_force"),
			LeftoverReport: true,
		},
	}
}
