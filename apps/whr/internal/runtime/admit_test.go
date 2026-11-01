// SPDX-License-Identifier: EUPL-1.2

package runtime_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
)

// countingProvider stands in for a provider: Capabilities is the only
// provider call a caller makes before Admit, and Admit must add none.
type countingProvider struct{ calls int }

func (p *countingProvider) Capabilities(profile runtime.TrustProfile) runtime.Capabilities {
	p.calls++
	return fullCaps(profile)
}

type (
	codes = []runtime.RefusalCode
	edit  func(*runtime.Capabilities)
)

func req(profile runtime.TrustProfile, reqs ...runtime.Requirement) runtime.SandboxRequest {
	r := runtime.SandboxRequest{Profile: profile, Requirements: reqs}
	if profile == runtime.ProfileCooperativeJob {
		r.AcknowledgeSharedGit = true
	}
	return r
}

func need(field runtime.FieldName, value string) runtime.Requirement {
	return runtime.Requirement{Field: field, Value: value}
}

func withMounts(r runtime.SandboxRequest, m ...runtime.MountIntent) runtime.SandboxRequest {
	r.Mounts = m
	return r
}

func mount(src runtime.MountSource, target string, mode runtime.MountMode) runtime.MountIntent {
	return runtime.MountIntent{Source: src, Target: target, Mode: mode}
}

var allProfiles = []runtime.TrustProfile{runtime.ProfileDeveloper, runtime.ProfileCooperativeJob, runtime.ProfileUntrustedJob}

func TestAdmitTable(t *testing.T) {
	provider := &countingProvider{}
	capsFor := map[runtime.TrustProfile]runtime.Capabilities{}
	for _, p := range allProfiles {
		capsFor[p] = provider.Capabilities(p)
	}
	baseline := provider.calls

	microvm := func(c *runtime.Capabilities) {
		c.KernelIsolation = capability(runtime.StatusEnforced, "guest kernel", "microvm")
	}
	setEgress := func(status runtime.Status, values ...string) edit {
		return func(c *runtime.Capabilities) { c.EgressEnforcement = capability(status, "test", values...) }
	}

	type row struct {
		name    string
		profile runtime.TrustProfile
		request runtime.SandboxRequest
		edit    edit
		want    codes // nil: allowed
	}
	var rows []row
	for _, p := range allProfiles {
		n := string(p)
		rows = append(rows,
			row{n + "/no requirements", p, req(p), nil, nil},
			row{n + "/microvm on shared kernel", p, req(p, need(runtime.FieldKernelIsolation, "microvm")), nil,
				codes{runtime.RefusedKernelIsolationUnavailable}},
			row{n + "/microvm on a microvm provider", p, req(p, need(runtime.FieldKernelIsolation, "microvm")), microvm, nil},
			row{n + "/shared_kernel on shared kernel", p, req(p, need(runtime.FieldKernelIsolation, "shared_kernel")), nil, nil},
			row{n + "/allowlist egress on none", p, req(p, need(runtime.FieldEgressEnforcement, "enforced_allowlist")), nil,
				codes{runtime.RefusedEgressEnforcementUnavailable}},
			row{n + "/allowlist egress on cooperative proxy", p, req(p, need(runtime.FieldEgressEnforcement, "enforced_allowlist")),
				setEgress(runtime.StatusEnforced, "cooperative_proxy"), codes{runtime.RefusedEgressEnforcementUnavailable}},
			row{n + "/allowlist egress enforced", p, req(p, need(runtime.FieldEgressEnforcement, "enforced_allowlist")),
				setEgress(runtime.StatusEnforced, "enforced_allowlist", "enforced_deny"), nil},
			row{n + "/deny egress claimed but not enforced", p, req(p, need(runtime.FieldEgressEnforcement, "enforced_deny")),
				setEgress(runtime.StatusNotEnforced, "enforced_deny"), codes{runtime.RefusedEgressEnforcementUnavailable}},
			row{n + "/egress none needs nothing", p, req(p, need(runtime.FieldEgressEnforcement, "none")), nil, nil},
			row{n + "/credential mediation on none", p, req(p, need(runtime.FieldCredentialMediation, "sentinel_mediated")), nil,
				codes{runtime.RefusedCredentialMediationUnavailable}},
			row{n + "/host gateway on none", p, req(p, need(runtime.FieldHostTools, "gateway")), nil,
				codes{runtime.RefusedHostToolsUnavailable}},
			row{n + "/mountless filesystem unsupported", p, req(p, need(runtime.FieldFilesystemAccess, "mountless")), nil,
				codes{runtime.RefusedProfileUnsupported}},
			row{n + "/field outside the contract", p, req(p, need("interactive_shell", "pty")), nil,
				codes{runtime.RefusedProfileUnsupported}},
			row{n + "/value outside the contract", p, req(p, need(runtime.FieldHomeState, "tmpfs")), nil,
				codes{runtime.RefusedProfileUnsupported}},
			row{n + "/two refusals come in contract order", p,
				req(p, need(runtime.FieldHostTools, "gateway"), need(runtime.FieldKernelIsolation, "microvm"), need(runtime.FieldEgressEnforcement, "enforced_deny")), nil,
				codes{runtime.RefusedKernelIsolationUnavailable, runtime.RefusedEgressEnforcementUnavailable, runtime.RefusedHostToolsUnavailable}},
			row{n + "/repeated requirement refuses once", p,
				req(p, need(runtime.FieldHostTools, "gateway"), need(runtime.FieldHostTools, "gateway")), nil,
				codes{runtime.RefusedHostToolsUnavailable}},
			row{n + "/force_only cleanup on graceful provider", p, req(p, need(runtime.FieldCancellationCleanup, "force_only")), nil, nil},
			row{n + "/cooperative proxy on enforced deny", p, req(p, need(runtime.FieldEgressEnforcement, "cooperative_proxy")),
				setEgress(runtime.StatusEnforced, "enforced_deny"), nil},
			row{n + "/ide access is informational", p, req(p, need(runtime.FieldIDEAccess, "attach")), nil, nil},
		)
	}
	rows = append(rows,
		// Profile requirement matrix gaps.
		row{"developer/no host config directory", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) {
				c.GitConfigProfile = capability(runtime.StatusEnforced, "test", "agent_policy_ro")
			},
			codes{runtime.RefusedGitConfigProfileUnavailable}},
		row{"cooperative/no agent profile", runtime.ProfileCooperativeJob, req(runtime.ProfileCooperativeJob),
			func(c *runtime.Capabilities) {
				c.GitConfigProfile = capability(runtime.StatusEnforced, "test", "host_global_dir_rw")
			},
			codes{runtime.RefusedGitConfigProfileUnavailable}},
		row{"untrusted/no agent profile", runtime.ProfileUntrustedJob, req(runtime.ProfileUntrustedJob),
			func(c *runtime.Capabilities) {
				c.GitConfigProfile = capability(runtime.StatusEnforced, "test", "host_global_dir_rw")
			},
			codes{runtime.RefusedGitConfigProfileUnavailable}},
		row{"developer/no bind_rw", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) {
				c.FilesystemAccess = capability(runtime.StatusEnforced, "test", "private_copy")
			},
			codes{runtime.RefusedProfileUnsupported}},
		row{"developer/no primary_rw", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) {
				c.GitAccess = capability(runtime.StatusEnforced, "test", "private_clone")
			},
			codes{runtime.RefusedProfileUnsupported}},
		row{"developer/no persistent home", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) { c.HomeState = capability(runtime.StatusEnforced, "test", "scratch") },
			codes{runtime.RefusedProfileUnsupported}},
		row{"cooperative/no linked worktree", runtime.ProfileCooperativeJob, req(runtime.ProfileCooperativeJob),
			func(c *runtime.Capabilities) {
				c.GitAccess = capability(runtime.StatusEnforced, "test", "private_clone")
			},
			codes{runtime.RefusedProfileUnsupported}},
		row{"untrusted/no private copy", runtime.ProfileUntrustedJob, req(runtime.ProfileUntrustedJob),
			func(c *runtime.Capabilities) {
				c.FilesystemAccess = capability(runtime.StatusEnforced, "test", "bind_rw")
			},
			codes{runtime.RefusedProfileUnsupported}},
		row{"untrusted/no scratch home", runtime.ProfileUntrustedJob, req(runtime.ProfileUntrustedJob),
			func(c *runtime.Capabilities) {
				c.HomeState = capability(runtime.StatusEnforced, "test", "persistent_volume")
			},
			codes{runtime.RefusedProfileUnsupported}},
		row{"untrusted/matrix value merely claimed", runtime.ProfileUntrustedJob, req(runtime.ProfileUntrustedJob),
			func(c *runtime.Capabilities) {
				c.GitAccess = capability(runtime.StatusNotEnforced, "test", "private_clone")
			},
			codes{runtime.RefusedProfileUnsupported}},
		// The report must be for the requested profile.
		row{"report for another profile", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) { c.Profile = runtime.ProfileUntrustedJob },
			codes{runtime.RefusedProfileUnsupported}},
		row{"report without profile", runtime.ProfileDeveloper, req(runtime.ProfileDeveloper),
			func(c *runtime.Capabilities) { c.Profile = "" },
			codes{runtime.RefusedProfileUnsupported}},
		row{"unlisted profile", "root", runtime.SandboxRequest{Profile: "root"},
			func(c *runtime.Capabilities) { c.Profile = "root" },
			codes{runtime.RefusedProfileUnsupported}},
	)

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			caps := capsFor[r.profile]
			if r.profile == "root" {
				caps = fullCaps("root")
			}
			if r.edit != nil {
				r.edit(&caps)
			}
			got := runtime.Admit(r.request, caps)
			if got.Allowed != (len(r.want) == 0) {
				t.Fatalf("Allowed = %v, refusals %v", got.Allowed, got.Refusals)
			}
			if !slices.Equal(got.Refusals, r.want) && !(len(got.Refusals) == 0 && len(r.want) == 0) {
				t.Fatalf("refusals %v, want %v", got.Refusals, r.want)
			}
			if (got.Err() == nil) != got.Allowed {
				t.Fatalf("Err() = %v for Allowed = %v", got.Err(), got.Allowed)
			}
		})
	}

	// SC-001: zero provider calls. Admit takes values only: no interface,
	// function or channel parameter can reach a provider, and the counter
	// that stands in for the provider did not move.
	if provider.calls != baseline {
		t.Errorf("provider was called %d times during admission", provider.calls-baseline)
	}
	ft := reflect.TypeOf(runtime.Admit)
	for i := 0; i < ft.NumIn(); i++ {
		switch ft.In(i).Kind() {
		case reflect.Interface, reflect.Func, reflect.Chan, reflect.Pointer:
			t.Errorf("Admit parameter %d is %v: admission must stay a pure function of values", i, ft.In(i))
		}
	}
}

func TestAdmitMicroVMNeverFallsBack(t *testing.T) {
	want := codes{runtime.RefusedKernelIsolationUnavailable}
	reports := map[string]edit{
		"shared kernel enforced": nil,
		"shared kernel and microvm listed but unknown": func(c *runtime.Capabilities) {
			c.KernelIsolation = capability(runtime.StatusUnknown, "test", "shared_kernel", "microvm")
		},
		"microvm declared but not enforced": func(c *runtime.Capabilities) {
			c.KernelIsolation = capability(runtime.StatusNotEnforced, "test", "microvm")
		},
		"enforced with no value": func(c *runtime.Capabilities) {
			c.KernelIsolation = capability(runtime.StatusEnforced, "test")
		},
		"zero capability": func(c *runtime.Capabilities) { c.KernelIsolation = runtime.Capability{} },
		"every other field enforced": func(c *runtime.Capabilities) {
			*c = fullCaps(c.Profile)
			c.EgressEnforcement = capability(runtime.StatusEnforced, "test", "enforced_deny")
		},
	}
	for _, p := range allProfiles {
		for name, e := range reports {
			t.Run(string(p)+"/"+name, func(t *testing.T) {
				caps := fullCaps(p)
				if e != nil {
					e(&caps)
				}
				got := runtime.Admit(req(p, need(runtime.FieldKernelIsolation, "microvm")), caps)
				if got.Allowed || !slices.Equal(got.Refusals, want) {
					t.Fatalf("microvm must be refused with no fallback: %+v", got)
				}
			})
		}
		// Only a provider that declares and enforces microvm satisfies it.
		caps := fullCaps(p)
		caps.KernelIsolation = capability(runtime.StatusEnforced, "guest kernel", "microvm")
		if got := runtime.Admit(req(p, need(runtime.FieldKernelIsolation, "microvm")), caps); !got.Allowed {
			t.Errorf("%s: an enforcing microvm provider must be admitted: %+v", p, got)
		}
		// A request without a kernel requirement is not changed by the
		// provider kind: Admit never branches on it (FR-034).
		for _, k := range []string{"shared_kernel", "microvm"} {
			caps.KernelIsolation = capability(runtime.StatusEnforced, "test", k)
			if got := runtime.Admit(req(p), caps); !got.Allowed {
				t.Errorf("%s: kernel %s must not change an unrelated request: %+v", p, k, got)
			}
		}
	}
	// A near miss is a different value, not microvm.
	for _, v := range []string{"MicroVM", "micro_vm", "microvm ", ""} {
		caps := fullCaps(runtime.ProfileUntrustedJob)
		caps.KernelIsolation = capability(runtime.StatusEnforced, "test", "microvm")
		if got := runtime.Admit(req(runtime.ProfileUntrustedJob, need(runtime.FieldKernelIsolation, v)), caps); got.Allowed {
			t.Errorf("requirement %q must not be satisfied", v)
		}
	}
}

func TestAdmitUnknownNeverSatisfies(t *testing.T) {
	cases := []struct {
		field runtime.FieldName
		value string
		code  runtime.RefusalCode
	}{
		{runtime.FieldKernelIsolation, "shared_kernel", runtime.RefusedKernelIsolationUnavailable},
		{runtime.FieldKernelIsolation, "microvm", runtime.RefusedKernelIsolationUnavailable},
		{runtime.FieldFilesystemAccess, "private_copy", runtime.RefusedProfileUnsupported},
		{runtime.FieldGitConfigProfile, "agent_policy_ro", runtime.RefusedGitConfigProfileUnavailable},
		{runtime.FieldHomeState, "scratch", runtime.RefusedProfileUnsupported},
		{runtime.FieldToolMounts, "ro_bdock", runtime.RefusedProfileUnsupported},
		{runtime.FieldEgressEnforcement, "enforced_allowlist", runtime.RefusedEgressEnforcementUnavailable},
		{runtime.FieldEgressEnforcement, "cooperative_proxy", runtime.RefusedEgressEnforcementUnavailable},
		{runtime.FieldCredentialMediation, "sentinel_mediated", runtime.RefusedCredentialMediationUnavailable},
		{runtime.FieldHostTools, "gateway", runtime.RefusedHostToolsUnavailable},
		{runtime.FieldCancellationCleanup, "graceful_then_force", runtime.RefusedCleanupCapabilityInsufficient},
	}
	for _, p := range allProfiles {
		for _, c := range cases {
			// Control: the same requirement is satisfied when enforced.
			caps := fullCaps(p)
			setField(&caps, c.field, capability(runtime.StatusEnforced, "test", c.value))
			if c.field == runtime.FieldFilesystemAccess || c.field == runtime.FieldHomeState || c.field == runtime.FieldGitConfigProfile {
				// keep the profile matrix satisfied around the field under test
				caps = fullCaps(p)
				extend(&caps, c.field, c.value)
			}
			if got := runtime.Admit(req(p, need(c.field, c.value)), caps); !got.Allowed {
				t.Fatalf("%s/%s=%s: control must be admitted: %+v", p, c.field, c.value, got)
			}
			// Unknown, with the value listed, never satisfies it.
			caps = fullCaps(p)
			setField(&caps, c.field, capability(runtime.StatusUnknown, "test", c.value))
			code := c.code
			if c.field == runtime.FieldCancellationCleanup && p == runtime.ProfileDeveloper {
				code = runtime.RefusedProfileUnsupported // the cleanup code is job-only
			}
			got := runtime.Admit(req(p, need(c.field, c.value)), caps)
			if got.Allowed || !slices.Contains(got.Refusals, code) {
				t.Errorf("%s/%s=%s unknown: want refusal %s, got %+v", p, c.field, c.value, c.code, got)
			}
			// The zero report (everything unknown) refuses too.
			zero := runtime.Capabilities{Profile: p}
			got = runtime.Admit(req(p, need(c.field, c.value)), zero)
			if got.Allowed || !slices.Contains(got.Refusals, code) {
				t.Errorf("%s/%s=%s zero report: want refusal %s, got %+v", p, c.field, c.value, c.code, got)
			}
		}
		// The zero report cannot even satisfy the profile's own matrix.
		if got := runtime.Admit(req(p), runtime.Capabilities{Profile: p}); got.Allowed {
			t.Errorf("%s: an all-unknown report was admitted", p)
		}
	}
}

func setField(c *runtime.Capabilities, name runtime.FieldName, v runtime.Capability) {
	switch name {
	case runtime.FieldKernelIsolation:
		c.KernelIsolation = v
	case runtime.FieldFilesystemAccess:
		c.FilesystemAccess = v
	case runtime.FieldGitAccess:
		c.GitAccess = v
	case runtime.FieldGitConfigProfile:
		c.GitConfigProfile = v
	case runtime.FieldHomeState:
		c.HomeState = v
	case runtime.FieldToolMounts:
		c.ToolMounts = v
	case runtime.FieldEgressEnforcement:
		c.EgressEnforcement = v
	case runtime.FieldCredentialMediation:
		c.CredentialMediation = v
	case runtime.FieldHostTools:
		c.HostTools = v
	case runtime.FieldIDEAccess:
		c.IDEAccess = v
	case runtime.FieldCancellationCleanup:
		c.CancellationCleanup.Capability = v
	}
}

// extend adds value to an already enforced field of the standard report.
func extend(c *runtime.Capabilities, name runtime.FieldName, value string) {
	f, _ := c.Field(name)
	if !f.Provides(value) {
		f.Values = append(slices.Clone(f.Values), value)
	}
	setField(c, name, f)
}

func TestAdmitHostConfigMountOnlyForDeveloperProfile(t *testing.T) {
	for _, src := range []runtime.MountSource{runtime.MountHostGitConfigDir, runtime.MountHostGitConfigFile} {
		for _, mode := range []runtime.MountMode{runtime.MountRW, runtime.MountRO} {
			for _, p := range allProfiles {
				r := withMounts(req(p), mount(src, "/home/captain/.config/git", mode))
				got := runtime.Admit(r, fullCaps(p))
				refused := got.Refusals
				hasAgent := slices.Contains(refused, runtime.RefusedHostGitConfigForAgent)
				if p == runtime.ProfileDeveloper && hasAgent {
					t.Errorf("%s %s %s: developer may mount the host configuration: %v", p, src, mode, refused)
				}
				if p != runtime.ProfileDeveloper && (!hasAgent || got.Allowed) {
					t.Errorf("%s %s %s: must be refused with host_git_config_for_agent: %+v", p, src, mode, got)
				}
			}
		}
	}
	// The developer directory mount, read/write, is the admitted plan.
	dev := withMounts(req(runtime.ProfileDeveloper), mount(runtime.MountHostGitConfigDir, "/home/captain/.config/git", runtime.MountRW))
	if got := runtime.Admit(dev, fullCaps(runtime.ProfileDeveloper)); !got.Allowed {
		t.Errorf("developer directory mount refused: %+v", got)
	}
	// The workspace container of ADR 0009 is not a profile; its plan is
	// described by the profile requested, so asking for one with a host
	// mount and a job profile is refused the same way.
	agentish := withMounts(req(runtime.ProfileUntrustedJob), mount(runtime.MountHostGitConfigDir, "/root/.config/git", runtime.MountRW))
	if got := runtime.Admit(agentish, fullCaps(runtime.ProfileUntrustedJob)); got.Allowed {
		t.Error("a job profile must never plan the host global configuration")
	}
	// The read-only agent profile file is not the host configuration.
	ok := withMounts(req(runtime.ProfileUntrustedJob), mount(runtime.MountAgentGitProfile, "/etc/workharbor/gitconfig", runtime.MountRO))
	if got := runtime.Admit(ok, fullCaps(runtime.ProfileUntrustedJob)); !got.Allowed {
		t.Errorf("read-only agent profile refused: %+v", got)
	}
}

func TestAdmitRefusesWritableSingleFileGitConfig(t *testing.T) {
	devFile := withMounts(req(runtime.ProfileDeveloper), mount(runtime.MountHostGitConfigFile, "/home/captain/.gitconfig", runtime.MountRW))
	got := runtime.Admit(devFile, fullCaps(runtime.ProfileDeveloper))
	if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedGitConfigSingleFileMount}) {
		t.Errorf("developer single file rw: %+v", got)
	}
	rwAgent := withMounts(req(runtime.ProfileUntrustedJob), mount(runtime.MountAgentGitProfile, "/etc/workharbor/gitconfig", runtime.MountRW))
	got = runtime.Admit(rwAgent, fullCaps(runtime.ProfileUntrustedJob))
	if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedGitConfigSingleFileMount}) {
		t.Errorf("agent profile rw: %+v", got)
	}
	both := withMounts(req(runtime.ProfileCooperativeJob), mount(runtime.MountHostGitConfigFile, "/root/.gitconfig", runtime.MountRW))
	got = runtime.Admit(both, fullCaps(runtime.ProfileCooperativeJob))
	want := codes{runtime.RefusedGitConfigSingleFileMount, runtime.RefusedHostGitConfigForAgent}
	if !slices.Equal(got.Refusals, want) {
		t.Errorf("refusals %v, want %v", got.Refusals, want)
	}
}

func TestAdmitRefusesJobWithoutCleanupCapability(t *testing.T) {
	levels := []struct {
		name string
		c    runtime.CleanupCapability
		job  bool // admitted for a job profile
	}{
		{"graceful then force", runtime.CleanupCapability{Capability: capability(runtime.StatusEnforced, "t", "graceful_then_force")}, true},
		{"force only", runtime.CleanupCapability{Capability: capability(runtime.StatusEnforced, "t", "force_only")}, true},
		{"none", runtime.CleanupCapability{Capability: capability(runtime.StatusEnforced, "t", "none")}, false},
		{"force only but not enforced", runtime.CleanupCapability{Capability: capability(runtime.StatusNotEnforced, "t", "force_only")}, false},
		{"force only but unknown", runtime.CleanupCapability{Capability: capability(runtime.StatusUnknown, "t", "force_only")}, false},
		{"zero", runtime.CleanupCapability{}, false},
	}
	for _, l := range levels {
		for _, p := range allProfiles {
			caps := fullCaps(p)
			caps.CancellationCleanup = l.c
			got := runtime.Admit(req(p), caps)
			wantAllowed := l.job || p == runtime.ProfileDeveloper
			if got.Allowed != wantAllowed {
				t.Errorf("%s/%s: Allowed = %v, want %v (%v)", p, l.name, got.Allowed, wantAllowed, got.Refusals)
			}
			if !wantAllowed && !slices.Equal(got.Refusals, codes{runtime.RefusedCleanupCapabilityInsufficient}) {
				t.Errorf("%s/%s: refusals %v", p, l.name, got.Refusals)
			}
		}
	}
}

func TestAdmitRefusesGitConfigEnvForDeveloper(t *testing.T) {
	refused := []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_COUNT",
		"GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS", "git_config_global"}
	for _, name := range refused {
		r := req(runtime.ProfileDeveloper)
		r.EnvNames = []string{"GIT_AUTHOR_NAME", name}
		got := runtime.Admit(r, fullCaps(runtime.ProfileDeveloper))
		if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedGitEnvOverride}) {
			t.Errorf("developer with %s: %+v", name, got)
		}
		// Jobs scrub the variable at exec time (FR-023); admission allows it.
		for _, p := range []runtime.TrustProfile{runtime.ProfileCooperativeJob, runtime.ProfileUntrustedJob} {
			j := req(p)
			j.EnvNames = []string{name}
			if got := runtime.Admit(j, fullCaps(p)); !got.Allowed {
				t.Errorf("%s with %s is scrubbed, not refused: %+v", p, name, got)
			}
		}
	}
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_CONFIGURATION", "MY_GIT_CONFIG_X", "HOME"} {
		r := req(runtime.ProfileDeveloper)
		r.EnvNames = []string{name}
		if got := runtime.Admit(r, fullCaps(runtime.ProfileDeveloper)); !got.Allowed {
			t.Errorf("developer with %s must be admitted: %+v", name, got)
		}
	}
}

func TestAdmitRefusesUntrustedWorktree(t *testing.T) {
	shared := []runtime.MountSource{runtime.MountLinkedWorktree, runtime.MountGitCommonDir, runtime.MountPrimaryGit, runtime.MountGitAlternates}
	want := codes{runtime.RefusedSharedGitMetadataUntrusted}
	for _, src := range shared {
		for _, mode := range []runtime.MountMode{runtime.MountRW, runtime.MountRO} {
			r := withMounts(req(runtime.ProfileUntrustedJob), mount(src, "/work/repo", mode))
			if got := runtime.Admit(r, fullCaps(runtime.ProfileUntrustedJob)); !slices.Equal(got.Refusals, want) {
				t.Errorf("untrusted %s %s: %+v", src, mode, got)
			}
		}
	}
	// The workspace bind mount carries the primary .git (config, hooks,
	// refs, object store): never for an untrusted job (FR-010, FR-001).
	for _, mode := range []runtime.MountMode{runtime.MountRW, runtime.MountRO, ""} {
		r := withMounts(req(runtime.ProfileUntrustedJob), mount(runtime.MountWorkspace, "/work/repo", mode))
		if got := runtime.Admit(r, fullCaps(runtime.ProfileUntrustedJob)); got.Allowed || !slices.Equal(got.Refusals, want) {
			t.Errorf("untrusted workspace mount %q: %+v", mode, got)
		}
	}
	for _, v := range []string{"bind_rw", "bind_ro", "linked_worktree"} {
		r := req(runtime.ProfileUntrustedJob, need(runtime.FieldFilesystemAccess, v))
		if got := runtime.Admit(r, fullCaps(runtime.ProfileUntrustedJob)); got.Allowed || !slices.Equal(got.Refusals, want) {
			t.Errorf("untrusted filesystem_access=%s: %+v", v, got)
		}
	}
	if got := runtime.Admit(req(runtime.ProfileUntrustedJob, need(runtime.FieldFilesystemAccess, "private_copy")), fullCaps(runtime.ProfileUntrustedJob)); !got.Allowed {
		t.Errorf("private copy must be admitted: %+v", got)
	}
	dev := withMounts(req(runtime.ProfileDeveloper), mount(runtime.MountWorkspace, "/work/repo", runtime.MountRW))
	if got := runtime.Admit(dev, fullCaps(runtime.ProfileDeveloper)); !got.Allowed {
		t.Errorf("developer workspace mount must be admitted: %+v", got)
	}
	for _, v := range []string{"linked_worktree", "primary_rw"} {
		r := req(runtime.ProfileUntrustedJob, need(runtime.FieldGitAccess, v))
		if got := runtime.Admit(r, fullCaps(runtime.ProfileUntrustedJob)); !slices.Equal(got.Refusals, want) {
			t.Errorf("untrusted git_access=%s: %+v", v, got)
		}
	}
	if got := runtime.Admit(req(runtime.ProfileUntrustedJob, need(runtime.FieldGitAccess, "private_clone")), fullCaps(runtime.ProfileUntrustedJob)); !got.Allowed {
		t.Errorf("private clone must be admitted: %+v", got)
	}
	// The same metadata is allowed where the profile permits it.
	for _, p := range []runtime.TrustProfile{runtime.ProfileCooperativeJob, runtime.ProfileDeveloper} {
		r := withMounts(req(p), mount(runtime.MountLinkedWorktree, "/host/wt", runtime.MountRW), mount(runtime.MountGitCommonDir, "/host/repo/.git", runtime.MountRW))
		if got := runtime.Admit(r, fullCaps(p)); !got.Allowed {
			t.Errorf("%s: shared metadata must be admitted: %+v", p, got)
		}
	}
	// A cooperative job needs the explicit acknowledgement (FR-011).
	noAck := req(runtime.ProfileCooperativeJob)
	noAck.AcknowledgeSharedGit = false
	got := runtime.Admit(noAck, fullCaps(runtime.ProfileCooperativeJob))
	if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedTrustAcknowledgementMissing}) {
		t.Errorf("cooperative without acknowledgement: %+v", got)
	}
	// The acknowledgement is not needed, and not accepted as a substitute,
	// for the other profiles.
	for _, p := range []runtime.TrustProfile{runtime.ProfileDeveloper, runtime.ProfileUntrustedJob} {
		if got := runtime.Admit(req(p), fullCaps(p)); !got.Allowed {
			t.Errorf("%s: %+v", p, got)
		}
	}
	ack := withMounts(req(runtime.ProfileUntrustedJob), mount(runtime.MountLinkedWorktree, "/host/wt", runtime.MountRW))
	ack.AcknowledgeSharedGit = true
	if got := runtime.Admit(ack, fullCaps(runtime.ProfileUntrustedJob)); got.Allowed {
		t.Error("an acknowledgement must not unlock a worktree for an untrusted job")
	}
}

func TestAdmitRefusesHostSocketAndWritableToolMount(t *testing.T) {
	for _, p := range allProfiles {
		for _, src := range []runtime.MountSource{runtime.MountHostDockerSocket, runtime.MountHostControlSocket} {
			for _, mode := range []runtime.MountMode{runtime.MountRO, runtime.MountRW} {
				r := withMounts(req(p), mount(src, "/var/run/docker.sock", mode))
				got := runtime.Admit(r, fullCaps(p))
				if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedHostSocketMount}) {
					t.Errorf("%s %s %s: %+v", p, src, mode, got)
				}
			}
		}
		writable := []runtime.MountIntent{
			mount(runtime.MountToolBundle, "/opt/bdock/base", runtime.MountRW),
			{Source: runtime.MountScratch, Name: "bundledock-base", Target: "/data", Mode: runtime.MountRW},
			{Source: runtime.MountScratch, Name: "x", Target: "/opt/bdock/base/bin", Mode: runtime.MountRW},
			{Source: runtime.MountToolBundle, Target: "/opt/bdock/base", Mode: ""},
		}
		for _, m := range writable {
			got := runtime.Admit(withMounts(req(p), m), fullCaps(p))
			if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedToolMountWritable}) {
				t.Errorf("%s writable tool mount %+v: %+v", p, m, got)
			}
		}
		readOnly := withMounts(req(p),
			mount(runtime.MountToolBundle, "/opt/bdock/base", runtime.MountRO),
			runtime.MountIntent{Source: runtime.MountScratch, Name: "bundledock-base", Target: "/opt/bdock/base", Mode: runtime.MountRO})
		if got := runtime.Admit(readOnly, fullCaps(p)); !got.Allowed {
			t.Errorf("%s: read-only tool mounts must be admitted: %+v", p, got)
		}
	}
	// A path that merely resembles the tool root is not one.
	r := withMounts(req(runtime.ProfileDeveloper), mount(runtime.MountScratch, "/opt/bdock-extra/x", runtime.MountRW))
	if got := runtime.Admit(r, fullCaps(runtime.ProfileDeveloper)); !got.Allowed {
		t.Errorf("/opt/bdock-extra is not a tool root: %+v", got)
	}
}

func TestAdmitOrderedRequirements(t *testing.T) {
	cleanup := func(status runtime.Status, values ...string) edit {
		return func(c *runtime.Capabilities) {
			c.CancellationCleanup.Capability = capability(status, "test", values...)
		}
	}
	egress := func(status runtime.Status, values ...string) edit {
		return func(c *runtime.Capabilities) { c.EgressEnforcement = capability(status, "test", values...) }
	}
	type row struct {
		name  string
		field runtime.FieldName
		value string
		edit  edit
		// job and dev are the expected refusals for job profiles and developer.
		job, dev codes
	}
	jobCleanup := codes{runtime.RefusedCleanupCapabilityInsufficient}
	unsupported := codes{runtime.RefusedProfileUnsupported}
	egressNo := codes{runtime.RefusedEgressEnforcementUnavailable}
	E := runtime.StatusEnforced
	rows := []row{
		{"force_only asked, graceful enforced", runtime.FieldCancellationCleanup, "force_only", cleanup(E, "graceful_then_force"), nil, nil},
		{"force_only asked, force_only enforced", runtime.FieldCancellationCleanup, "force_only", cleanup(E, "force_only"), nil, nil},
		{"graceful asked, force_only enforced", runtime.FieldCancellationCleanup, "graceful_then_force", cleanup(E, "force_only"), jobCleanup, unsupported},
		{"none asked, nothing enforced", runtime.FieldCancellationCleanup, "none", cleanup(E, "none"), jobCleanup, nil},
		{"force_only asked, graceful unknown", runtime.FieldCancellationCleanup, "force_only", cleanup(runtime.StatusUnknown, "graceful_then_force"), jobCleanup, unsupported},
		{"force_only asked, graceful not enforced", runtime.FieldCancellationCleanup, "force_only", cleanup(runtime.StatusNotEnforced, "graceful_then_force"), jobCleanup, unsupported},
		{"cooperative_proxy asked, allowlist enforced", runtime.FieldEgressEnforcement, "cooperative_proxy", egress(E, "enforced_allowlist"), nil, nil},
		{"cooperative_proxy asked, deny enforced", runtime.FieldEgressEnforcement, "cooperative_proxy", egress(E, "enforced_deny"), nil, nil},
		{"cooperative_proxy asked, deny unknown", runtime.FieldEgressEnforcement, "cooperative_proxy", egress(runtime.StatusUnknown, "enforced_deny"), egressNo, egressNo},
		{"cooperative_proxy asked, none", runtime.FieldEgressEnforcement, "cooperative_proxy", egress(E, "none"), egressNo, egressNo},
		{"allowlist asked, only proxy", runtime.FieldEgressEnforcement, "enforced_allowlist", egress(E, "cooperative_proxy"), egressNo, egressNo},
		{"allowlist asked, deny is a different policy", runtime.FieldEgressEnforcement, "enforced_allowlist", egress(E, "enforced_deny"), egressNo, egressNo},
		{"deny asked, allowlist is a different policy", runtime.FieldEgressEnforcement, "enforced_deny", egress(E, "enforced_allowlist"), egressNo, egressNo},
	}
	for _, r := range rows {
		for _, p := range allProfiles {
			want := r.job
			if p == runtime.ProfileDeveloper {
				want = r.dev
			}
			caps := fullCaps(p)
			r.edit(&caps)
			// A matrix gap from the edited cleanup field is expected for jobs
			// whose provider is below force_only; the requirement adds no code of its own.
			got := runtime.Admit(req(p, need(r.field, r.value)), caps)
			if !slices.Equal(got.Refusals, want) && !(len(got.Refusals) == 0 && len(want) == 0) {
				t.Errorf("%s/%s: refusals %v, want %v", p, r.name, got.Refusals, want)
			}
			if p == runtime.ProfileDeveloper && slices.Contains(got.Refusals, runtime.RefusedCleanupCapabilityInsufficient) {
				t.Errorf("%s/%s: the cleanup code is job-only", p, r.name)
			}
		}
	}
}

func TestAdmitUnknownMountSourceFailsClosed(t *testing.T) {
	for _, src := range []runtime.MountSource{"", "mystery", "Workspace", "tmpfs"} {
		for _, mode := range []runtime.MountMode{runtime.MountRW, runtime.MountRO, ""} {
			for _, p := range allProfiles {
				got := runtime.Admit(withMounts(req(p), mount(src, "/x", mode)), fullCaps(p))
				if got.Allowed || !slices.Equal(got.Refusals, codes{runtime.RefusedProfileUnsupported}) {
					t.Errorf("%s source %q mode %q: %+v", p, src, mode, got)
				}
			}
		}
	}
	// Every declared source kind is known.
	for _, src := range []runtime.MountSource{runtime.MountWorkspace, runtime.MountPrivateCopy, runtime.MountScratch,
		runtime.MountPrimaryHome, runtime.MountAgentGitProfile} {
		got := runtime.Admit(withMounts(req(runtime.ProfileDeveloper), mount(src, "/x", runtime.MountRO)), fullCaps(runtime.ProfileDeveloper))
		if slices.Contains(got.Refusals, runtime.RefusedProfileUnsupported) {
			t.Errorf("declared source %q treated as unknown", src)
		}
	}
}

func TestAdmitRefusesHostGlobalGitConfigRequirementForJobs(t *testing.T) {
	want := codes{runtime.RefusedHostGitConfigForAgent}
	for _, p := range []runtime.TrustProfile{runtime.ProfileCooperativeJob, runtime.ProfileUntrustedJob} {
		r := req(p, need(runtime.FieldGitConfigProfile, "host_global_dir_rw"))
		if got := runtime.Admit(r, fullCaps(p)); got.Allowed || !slices.Equal(got.Refusals, want) {
			t.Errorf("%s: host_global_dir_rw requirement: %+v", p, got)
		}
		// Policy comes first: a report that does not even claim it gives the same code.
		caps := fullCaps(p)
		caps.GitConfigProfile = capability(runtime.StatusUnknown, "t", "host_global_dir_rw", "agent_policy_ro")
		got := runtime.Admit(r, caps)
		if got.Allowed || !slices.Contains(got.Refusals, runtime.RefusedHostGitConfigForAgent) {
			t.Errorf("%s: unknown report: %+v", p, got)
		}
		// The agent profile itself is the admitted requirement.
		if got := runtime.Admit(req(p, need(runtime.FieldGitConfigProfile, "agent_policy_ro")), fullCaps(p)); !got.Allowed {
			t.Errorf("%s: agent_policy_ro must be admitted: %+v", p, got)
		}
	}
	dev := req(runtime.ProfileDeveloper, need(runtime.FieldGitConfigProfile, "host_global_dir_rw"))
	if got := runtime.Admit(dev, fullCaps(runtime.ProfileDeveloper)); !got.Allowed {
		t.Errorf("developer control: %+v", got)
	}
}
