// SPDX-License-Identifier: EUPL-1.2

package runtime

import (
	"fmt"
	"slices"
)

// TrustProfile is fixed at creation (FR-003).
type TrustProfile string

// The three trust profiles of the data model.
const (
	ProfileDeveloper      TrustProfile = "developer"
	ProfileCooperativeJob TrustProfile = "cooperative_job"
	ProfileUntrustedJob   TrustProfile = "untrusted_job"
)

// Valid reports whether p is one of the three profiles.
func (p TrustProfile) Valid() bool {
	return p == ProfileDeveloper || p == ProfileCooperativeJob || p == ProfileUntrustedJob
}

// IsJob reports whether p is a job profile (cooperative or untrusted).
func (p TrustProfile) IsJob() bool {
	return p == ProfileCooperativeJob || p == ProfileUntrustedJob
}

// Status is the claim a provider makes about one capability (FR-015).
type Status string

// The three statuses. Only StatusEnforced ever satisfies a requirement.
const (
	StatusEnforced    Status = "enforced"
	StatusNotEnforced Status = "not_enforced"
	StatusUnknown     Status = "unknown"
)

// Valid reports whether s is one of the three contract statuses.
func (s Status) Valid() bool {
	return s == StatusEnforced || s == StatusNotEnforced || s == StatusUnknown
}

// FieldName names one of the capability fields of contracts/runtime-provider.md.
type FieldName string

// The eleven capability fields, in contract order.
const (
	FieldKernelIsolation     FieldName = "kernel_isolation"
	FieldFilesystemAccess    FieldName = "filesystem_access"
	FieldGitAccess           FieldName = "git_access"
	FieldGitConfigProfile    FieldName = "git_config_profile"
	FieldHomeState           FieldName = "home_state"
	FieldToolMounts          FieldName = "tool_mounts"
	FieldEgressEnforcement   FieldName = "egress_enforcement"
	FieldCredentialMediation FieldName = "credential_mediation"
	FieldHostTools           FieldName = "host_tools"
	FieldIDEAccess           FieldName = "ide_access"
	FieldCancellationCleanup FieldName = "cancellation_cleanup"
)

// Values the capability fields can hold, used by Admit and the freeze tests.
const (
	KernelSharedKernel = "shared_kernel"
	KernelMicroVM      = "microvm"

	FilesystemPrivateCopy    = "private_copy"
	FilesystemBindRW         = "bind_rw"
	FilesystemBindRO         = "bind_ro"
	FilesystemLinkedWorktree = "linked_worktree"
	FilesystemMountless      = "mountless"

	GitPrivateClone   = "private_clone"
	GitLinkedWorktree = "linked_worktree"
	GitPrimaryRW      = "primary_rw"

	GitConfigHostGlobalDirRW = "host_global_dir_rw"
	GitConfigAgentPolicyRO   = "agent_policy_ro"

	HomePersistentVolume = "persistent_volume"
	HomeScratch          = "scratch"

	ToolMountsRO = "ro_bdock"

	EgressCooperativeProxy  = "cooperative_proxy"
	EgressEnforcedAllowlist = "enforced_allowlist"
	EgressEnforcedDeny      = "enforced_deny"

	CredentialSentinelMediated = "sentinel_mediated"

	HostToolsGateway = "gateway"

	CleanupGracefulThenForce = "graceful_then_force"
	CleanupForceOnly         = "force_only"

	// ValueNone is the "nothing provided" value of several fields.
	ValueNone = "none"
)

// FieldSpec describes one field: its name and the values it may hold.
type FieldSpec struct {
	Name   FieldName
	Values []string
}

var fieldSpecs = []FieldSpec{
	{FieldKernelIsolation, []string{KernelSharedKernel, KernelMicroVM}},
	{FieldFilesystemAccess, []string{FilesystemPrivateCopy, FilesystemBindRW, FilesystemBindRO, FilesystemLinkedWorktree, FilesystemMountless}},
	{FieldGitAccess, []string{GitPrivateClone, GitLinkedWorktree, GitPrimaryRW}},
	{FieldGitConfigProfile, []string{GitConfigHostGlobalDirRW, GitConfigAgentPolicyRO}},
	{FieldHomeState, []string{HomePersistentVolume, HomeScratch, ValueNone}},
	{FieldToolMounts, []string{ToolMountsRO, ValueNone}},
	{FieldEgressEnforcement, []string{ValueNone, EgressCooperativeProxy, EgressEnforcedAllowlist, EgressEnforcedDeny}},
	{FieldCredentialMediation, []string{ValueNone, CredentialSentinelMediated}},
	{FieldHostTools, []string{ValueNone, HostToolsGateway}},
	{FieldIDEAccess, []string{"attach", ValueNone}},
	{FieldCancellationCleanup, []string{CleanupGracefulThenForce, CleanupForceOnly, ValueNone}},
}

// FieldSpecs returns the eleven field specifications in contract order.
func FieldSpecs() []FieldSpec {
	out := make([]FieldSpec, len(fieldSpecs))
	for i, s := range fieldSpecs {
		out[i] = FieldSpec{Name: s.Name, Values: slices.Clone(s.Values)}
	}
	return out
}

func specOf(name FieldName) (FieldSpec, bool) {
	for _, s := range fieldSpecs {
		if s.Name == name {
			return s, true
		}
	}
	return FieldSpec{}, false
}

// Capability is one field of a report: a status, the values the status covers
// and a scope stating what the claim covers (FR-012). Some fields hold a set
// of supported modes, hence Values rather than one value.
type Capability struct {
	Status Status   `json:"status"`
	Values []string `json:"values"`
	Scope  string   `json:"scope"`
}

// Enforced reports whether the status is exactly "enforced". The zero value,
// "unknown", "not_enforced" and any unrecognized status are not enforced.
func (c Capability) Enforced() bool { return c.Status == StatusEnforced }

// Provides reports whether the capability is enforced and covers value. This
// is the only way a requirement is satisfied; unknown never satisfies it.
func (c Capability) Provides(value string) bool {
	return c.Enforced() && value != "" && slices.Contains(c.Values, value)
}

// ProvidesAny is Provides for any of the values.
func (c Capability) ProvidesAny(values ...string) bool {
	for _, v := range values {
		if c.Provides(v) {
			return true
		}
	}
	return false
}

// CleanupCapability is cancellation_cleanup: a capability plus the
// leftover_report bool.
type CleanupCapability struct {
	Capability
	LeftoverReport bool `json:"leftover_report"`
}

// Capabilities is the report of one provider for one trust profile. Its zero
// value claims nothing: every status is unknown.
type Capabilities struct {
	Profile             TrustProfile      `json:"profile"`
	KernelIsolation     Capability        `json:"kernel_isolation"`
	FilesystemAccess    Capability        `json:"filesystem_access"`
	GitAccess           Capability        `json:"git_access"`
	GitConfigProfile    Capability        `json:"git_config_profile"`
	HomeState           Capability        `json:"home_state"`
	ToolMounts          Capability        `json:"tool_mounts"`
	EgressEnforcement   Capability        `json:"egress_enforcement"`
	CredentialMediation Capability        `json:"credential_mediation"`
	HostTools           Capability        `json:"host_tools"`
	IDEAccess           Capability        `json:"ide_access"`
	CancellationCleanup CleanupCapability `json:"cancellation_cleanup"`
}

// Field returns the named capability. A name outside the contract list is
// reported as not found with an unknown, non-enforced capability, so that a
// field this version does not know can never satisfy a requirement.
func (c Capabilities) Field(name FieldName) (Capability, bool) {
	switch name {
	case FieldKernelIsolation:
		return c.KernelIsolation, true
	case FieldFilesystemAccess:
		return c.FilesystemAccess, true
	case FieldGitAccess:
		return c.GitAccess, true
	case FieldGitConfigProfile:
		return c.GitConfigProfile, true
	case FieldHomeState:
		return c.HomeState, true
	case FieldToolMounts:
		return c.ToolMounts, true
	case FieldEgressEnforcement:
		return c.EgressEnforcement, true
	case FieldCredentialMediation:
		return c.CredentialMediation, true
	case FieldHostTools:
		return c.HostTools, true
	case FieldIDEAccess:
		return c.IDEAccess, true
	case FieldCancellationCleanup:
		return c.CancellationCleanup.Capability, true
	}
	return Capability{Status: StatusUnknown}, false
}

// Validate checks that the report is complete and typed: a valid profile,
// every field with a valid status, only values the contract allows, and a
// non-empty scope (conformance case 1).
func (c Capabilities) Validate() error {
	if !c.Profile.Valid() {
		return fmt.Errorf("capabilities: invalid profile %q", c.Profile)
	}
	for _, spec := range fieldSpecs {
		field, _ := c.Field(spec.Name)
		if !field.Status.Valid() {
			return fmt.Errorf("capabilities: %s: invalid status %q", spec.Name, field.Status)
		}
		if field.Scope == "" {
			return fmt.Errorf("capabilities: %s: empty scope", spec.Name)
		}
		for _, v := range field.Values {
			if !slices.Contains(spec.Values, v) {
				return fmt.Errorf("capabilities: %s: value %q is not in the contract", spec.Name, v)
			}
		}
	}
	return nil
}
