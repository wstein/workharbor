// SPDX-License-Identifier: EUPL-1.2

package runtime

import (
	"path"
	"strings"
)

// Requirement is one `field=value` protection a request insists on.
type Requirement struct {
	Field FieldName
	Value string
}

// MountSource classifies where a planned mount comes from. Admission reasons
// about kinds, never about provider-specific paths. A kind not listed below is
// refused (fail closed).
type MountSource string

// The mount source kinds Admit knows.
const (
	MountWorkspace         MountSource = "workspace"
	MountPrivateCopy       MountSource = "private_copy"
	MountScratch           MountSource = "scratch"
	MountPrimaryHome       MountSource = "primary_home"
	MountHostGitConfigDir  MountSource = "host_git_config_dir"
	MountHostGitConfigFile MountSource = "host_git_config_file"
	MountAgentGitProfile   MountSource = "agent_git_profile"
	MountToolBundle        MountSource = "tool_bundle"
	MountHostDockerSocket  MountSource = "host_docker_socket"
	MountHostControlSocket MountSource = "host_control_socket"
	MountLinkedWorktree    MountSource = "linked_worktree"
	MountGitCommonDir      MountSource = "git_common_dir"
	MountPrimaryGit        MountSource = "primary_git"
	MountGitAlternates     MountSource = "git_alternates"
)

// MountMode is the access of a planned mount. Anything but MountRO counts as
// writable, so an unset mode fails closed.
type MountMode string

// The mount modes.
const (
	MountRO MountMode = "ro"
	MountRW MountMode = "rw"
)

// MountIntent is a mount a request would plan: the minimal shape of an entry
// of the Plan (source kind, target, mode) that admission needs. Name is the
// volume or object name when there is one.
type MountIntent struct {
	Source MountSource
	Name   string
	Target string
	Mode   MountMode
}

// SandboxRequest is the input of admission. It holds no secret values: only
// the names of operator-supplied environment variables.
type SandboxRequest struct {
	Profile              TrustProfile
	Requirements         []Requirement
	Mounts               []MountIntent
	EnvNames             []string
	AcknowledgeSharedGit bool
}

// AdmissionDecision is allowed, or refused with the ordered, de-duplicated
// list of stable codes (contract order). It never has a side effect.
type AdmissionDecision struct {
	Allowed  bool
	Refusals []RefusalCode
}

// Err returns nil for an allowed decision and otherwise a Refusal carrying
// the first code.
func (d AdmissionDecision) Err() error {
	if d.Allowed || len(d.Refusals) == 0 {
		return nil
	}
	return NewRefusal(d.Refusals[0])
}

type matrixRow struct {
	field FieldName
	value string
	code  RefusalCode
}

// profileMatrix is what every profile needs from the provider report, whether
// or not the request names it (data-model trust profile table).
var profileMatrix = map[TrustProfile][]matrixRow{
	ProfileDeveloper: {
		{FieldFilesystemAccess, FilesystemBindRW, RefusedProfileUnsupported},
		{FieldGitAccess, GitPrimaryRW, RefusedProfileUnsupported},
		{FieldGitConfigProfile, GitConfigHostGlobalDirRW, RefusedGitConfigProfileUnavailable},
		{FieldHomeState, HomePersistentVolume, RefusedProfileUnsupported},
	},
	ProfileCooperativeJob: {
		{FieldFilesystemAccess, FilesystemLinkedWorktree, RefusedProfileUnsupported},
		{FieldGitAccess, GitLinkedWorktree, RefusedProfileUnsupported},
		{FieldGitConfigProfile, GitConfigAgentPolicyRO, RefusedGitConfigProfileUnavailable},
		{FieldHomeState, HomeScratch, RefusedProfileUnsupported},
	},
	ProfileUntrustedJob: {
		{FieldFilesystemAccess, FilesystemPrivateCopy, RefusedProfileUnsupported},
		{FieldGitAccess, GitPrivateClone, RefusedProfileUnsupported},
		{FieldGitConfigProfile, GitConfigAgentPolicyRO, RefusedGitConfigProfileUnavailable},
		{FieldHomeState, HomeScratch, RefusedProfileUnsupported},
	},
}

// fieldCode is the refusal for an unmet requirement on a field.
var fieldCode = map[FieldName]RefusalCode{
	FieldKernelIsolation:     RefusedKernelIsolationUnavailable,
	FieldFilesystemAccess:    RefusedProfileUnsupported,
	FieldGitAccess:           RefusedProfileUnsupported,
	FieldGitConfigProfile:    RefusedGitConfigProfileUnavailable,
	FieldHomeState:           RefusedProfileUnsupported,
	FieldToolMounts:          RefusedProfileUnsupported,
	FieldEgressEnforcement:   RefusedEgressEnforcementUnavailable,
	FieldCredentialMediation: RefusedCredentialMediationUnavailable,
	FieldHostTools:           RefusedHostToolsUnavailable,
	FieldCancellationCleanup: RefusedCleanupCapabilityInsufficient,
}

// Admit decides a request against the capability report for its profile. It
// is a pure function of its two values: it calls no provider, reads no host
// state and has no side effect (FR-013). A requirement is satisfied only by a
// capability that is enforced and lists the exact value; unknown, not
// enforced, absent or unrecognized never satisfy (FR-015), and there is no
// downgrade or substitution, in particular none for microvm (FR-014). It never
// branches on a provider kind (FR-034).
func Admit(req SandboxRequest, caps Capabilities) AdmissionDecision {
	found := map[RefusalCode]bool{}
	finish := func() AdmissionDecision {
		var out []RefusalCode
		for _, code := range refusalCodes {
			if found[code] {
				out = append(out, code)
			}
		}
		return AdmissionDecision{Allowed: len(out) == 0, Refusals: out}
	}

	// The report must be the one for the requested profile.
	if !req.Profile.Valid() || caps.Profile != req.Profile {
		found[RefusedProfileUnsupported] = true
		return finish()
	}

	for _, row := range profileMatrix[req.Profile] {
		field, _ := caps.Field(row.field)
		if !field.Provides(row.value) {
			found[row.code] = true
		}
	}
	if req.Profile.IsJob() && !caps.CancellationCleanup.ProvidesAny(CleanupGracefulThenForce, CleanupForceOnly) {
		found[RefusedCleanupCapabilityInsufficient] = true
	}

	for _, r := range req.Requirements {
		admitRequirement(req.Profile, r, caps, found)
	}
	for _, m := range req.Mounts {
		admitMount(req.Profile, m, found)
	}

	if req.Profile == ProfileDeveloper {
		for _, name := range req.EnvNames {
			if len(name) >= len("GIT_CONFIG_") && strings.EqualFold(name[:len("GIT_CONFIG_")], "GIT_CONFIG_") {
				found[RefusedGitEnvOverride] = true
			}
		}
	}
	if req.Profile == ProfileCooperativeJob && !req.AcknowledgeSharedGit {
		found[RefusedTrustAcknowledgementMissing] = true
	}
	return finish()
}

func admitRequirement(profile TrustProfile, r Requirement, caps Capabilities, found map[RefusalCode]bool) {
	spec, ok := specOf(r.Field)
	if !ok {
		// A field this version does not know cannot be shown enforced.
		found[RefusedProfileUnsupported] = true
		return
	}
	if r.Field == FieldIDEAccess {
		return // informational, never a refusal
	}
	known := false
	for _, v := range spec.Values {
		known = known || v == r.Value
	}
	if !known {
		found[RefusedProfileUnsupported] = true
		return
	}
	if profile != ProfileDeveloper && r.Field == FieldGitConfigProfile && r.Value == GitConfigHostGlobalDirRW {
		// The host global git configuration is developer-only (FR-024), as
		// for the mount path.
		found[RefusedHostGitConfigForAgent] = true
		return
	}
	if profile == ProfileUntrustedJob && untrustedSharedRequirement(r) {
		found[RefusedSharedGitMetadataUntrusted] = true
		return
	}
	switch r.Field {
	case FieldEgressEnforcement, FieldCredentialMediation, FieldHostTools, FieldCancellationCleanup:
		if r.Value == ValueNone {
			return // no protection is asked for
		}
	}
	field, _ := caps.Field(r.Field)
	if satisfies(r, field) {
		return
	}
	code := fieldCode[r.Field]
	if r.Field == FieldCancellationCleanup && !profile.IsJob() {
		// The cleanup code is for job profiles only (contract).
		code = RefusedProfileUnsupported
	}
	found[code] = true
}

// untrustedSharedRequirement reports whether the requirement asks an
// untrusted job for the primary checkout or shared git metadata: bind modes
// expose the primary .git, a linked worktree or primary_rw share it (FR-010).
func untrustedSharedRequirement(r Requirement) bool {
	switch r.Field {
	case FieldGitAccess:
		return r.Value == GitLinkedWorktree || r.Value == GitPrimaryRW
	case FieldFilesystemAccess:
		return r.Value == FilesystemBindRW || r.Value == FilesystemBindRO || r.Value == FilesystemLinkedWorktree
	}
	return false
}

// cleanupRank orders cancellation_cleanup ("below force_only" in the
// contract). Values outside the map rank 0 and never satisfy (fail closed).
var cleanupRank = map[string]int{ValueNone: 0, CleanupForceOnly: 1, CleanupGracefulThenForce: 2}

// satisfies applies the comparison of the field to an enforced capability.
// cancellation_cleanup is ordered (graceful_then_force covers force_only).
// egress_enforcement: cooperative_proxy is covered by any enforcement;
// enforced_allowlist and enforced_deny are different policies, matched
// exactly. All other fields match the exact value.
func satisfies(r Requirement, field Capability) bool {
	if !field.Enforced() {
		return false
	}
	switch r.Field {
	case FieldCancellationCleanup:
		if r.Value == ValueNone {
			return true
		}
		need := cleanupRank[r.Value]
		for _, v := range field.Values {
			if need > 0 && cleanupRank[v] >= need {
				return true
			}
		}
		return false
	case FieldEgressEnforcement:
		if r.Value == EgressCooperativeProxy {
			return field.ProvidesAny(EgressCooperativeProxy, EgressEnforcedAllowlist, EgressEnforcedDeny)
		}
	}
	return field.Provides(r.Value)
}

func admitMount(profile TrustProfile, m MountIntent, found map[RefusalCode]bool) {
	writable := m.Mode != MountRO
	switch m.Source {
	case MountWorkspace:
		// The workspace bind mount carries the primary .git (FR-010).
		if profile == ProfileUntrustedJob {
			found[RefusedSharedGitMetadataUntrusted] = true
		}
	case MountPrivateCopy, MountScratch:
		// Always allowed by kind; the tool-root and socket rules below still apply.
	case MountPrimaryHome:
		// "Primary home volume in a job" is forbidden by runtime-provider.md
		// but has no stable refusal code; Plan validation (T006/T008) owns it.
	case MountHostDockerSocket, MountHostControlSocket:
		found[RefusedHostSocketMount] = true
	case MountHostGitConfigDir:
		if profile != ProfileDeveloper {
			found[RefusedHostGitConfigForAgent] = true
		}
	case MountHostGitConfigFile:
		if profile != ProfileDeveloper {
			found[RefusedHostGitConfigForAgent] = true
		}
		if writable {
			found[RefusedGitConfigSingleFileMount] = true
		}
	case MountAgentGitProfile:
		if writable {
			found[RefusedGitConfigSingleFileMount] = true
		}
	case MountToolBundle:
		if writable {
			found[RefusedToolMountWritable] = true
		}
	case MountLinkedWorktree, MountGitCommonDir, MountPrimaryGit, MountGitAlternates:
		if profile == ProfileUntrustedJob {
			found[RefusedSharedGitMetadataUntrusted] = true
		}
	default:
		// An empty or unrecognized kind cannot be judged: refuse, whatever its mode.
		found[RefusedProfileUnsupported] = true
	}
	if path.Base(m.Target) == "docker.sock" || path.Base(m.Name) == "docker.sock" {
		found[RefusedHostSocketMount] = true
	}
	if writable && (strings.HasPrefix(m.Name, "bundledock-") || isToolRoot(m.Target)) {
		found[RefusedToolMountWritable] = true
	}
}

// isToolRoot reports whether target is /opt/bdock or below it.
func isToolRoot(target string) bool {
	clean := path.Clean(target)
	return clean == "/opt/bdock" || strings.HasPrefix(clean, "/opt/bdock/")
}
