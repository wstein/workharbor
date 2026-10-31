// SPDX-License-Identifier: EUPL-1.2

// Package runtime holds the provider-neutral sandbox contract of Feature 008:
// capability types, stable refusal codes and the pure admission function. It
// never names a provider kind (FR-034) and performs no provider side effect.
package runtime

import "errors"

// RefusalCode is a stable refusal identifier. The codes are frozen against
// specs/008-workspace-sandbox/contracts/refusal-codes.json: never reused or
// renamed, new codes are appended. They all map to exit 68 (ADR 0013); the
// code itself travels in a separate field, not as the exit status.
type RefusalCode string

// The stable refusal codes, in contract order.
const (
	RefusedProfileUnsupported             RefusalCode = "refused.profile_unsupported"
	RefusedKernelIsolationUnavailable     RefusalCode = "refused.kernel_isolation_unavailable"
	RefusedEgressEnforcementUnavailable   RefusalCode = "refused.egress_enforcement_unavailable"
	RefusedCredentialMediationUnavailable RefusalCode = "refused.credential_mediation_unavailable"
	RefusedHostToolsUnavailable           RefusalCode = "refused.host_tools_unavailable"
	RefusedGitConfigProfileUnavailable    RefusalCode = "refused.git_config_profile_unavailable"
	RefusedGitConfigSingleFileMount       RefusalCode = "refused.git_config_single_file_mount"
	RefusedHostGitConfigForAgent          RefusalCode = "refused.host_git_config_for_agent"
	RefusedSharedGitMetadataUntrusted     RefusalCode = "refused.shared_git_metadata_untrusted"
	RefusedHostSocketMount                RefusalCode = "refused.host_socket_mount"
	RefusedToolMountWritable              RefusalCode = "refused.tool_mount_writable"
	RefusedTrustAcknowledgementMissing    RefusalCode = "refused.trust_acknowledgement_missing"
	RefusedCopyPolicySecretPath           RefusalCode = "refused.copy_policy_secret_path"
	RefusedCopyPolicyDirtySubmodule       RefusalCode = "refused.copy_policy_dirty_submodule"
	RefusedForeignObject                  RefusalCode = "refused.foreign_object"
	RefusedPrimaryTarget                  RefusalCode = "refused.primary_target"
	RefusedUnexportedResults              RefusalCode = "refused.unexported_results"
	RefusedInvalidState                   RefusalCode = "refused.invalid_state"
	RefusedGitEnvOverride                 RefusalCode = "refused.git_env_override"
	RefusedCleanupCapabilityInsufficient  RefusalCode = "refused.cleanup_capability_insufficient"
	RefusedGitConfigNotReady              RefusalCode = "refused.git_config_not_ready"
)

var refusalCodes = []RefusalCode{
	RefusedProfileUnsupported,
	RefusedKernelIsolationUnavailable,
	RefusedEgressEnforcementUnavailable,
	RefusedCredentialMediationUnavailable,
	RefusedHostToolsUnavailable,
	RefusedGitConfigProfileUnavailable,
	RefusedGitConfigSingleFileMount,
	RefusedHostGitConfigForAgent,
	RefusedSharedGitMetadataUntrusted,
	RefusedHostSocketMount,
	RefusedToolMountWritable,
	RefusedTrustAcknowledgementMissing,
	RefusedCopyPolicySecretPath,
	RefusedCopyPolicyDirtySubmodule,
	RefusedForeignObject,
	RefusedPrimaryTarget,
	RefusedUnexportedResults,
	RefusedInvalidState,
	RefusedGitEnvOverride,
	RefusedCleanupCapabilityInsufficient,
	RefusedGitConfigNotReady,
}

// RefusalCodes returns a copy of every stable code in contract order.
func RefusalCodes() []RefusalCode {
	return append([]RefusalCode(nil), refusalCodes...)
}

// Valid reports whether the code is one of the stable codes.
func (c RefusalCode) Valid() bool {
	for _, known := range refusalCodes {
		if c == known {
			return true
		}
	}
	return false
}

// Refusal is the error form of a refusal. It carries only the stable code so
// that nothing sensitive can leak through it; the exit status is chosen
// separately (sandbox.ExitFor, task T004).
type Refusal struct {
	Code RefusalCode
}

// NewRefusal returns the error for one stable code.
func NewRefusal(code RefusalCode) Refusal { return Refusal{Code: code} }

// Error returns the stable code.
func (r Refusal) Error() string { return string(r.Code) }

// RefusalCodeOf extracts a refusal code from err or anything it wraps.
func RefusalCodeOf(err error) (RefusalCode, bool) {
	var r Refusal
	if errors.As(err, &r) {
		return r.Code, true
	}
	return "", false
}
