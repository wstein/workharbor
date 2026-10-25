// SPDX-License-Identifier: EUPL-1.2
package localdocker

// All receipt data is derived from admitted inputs or observations. Executable
// configuration and byte inventories live in separate private digest-bound
// artifacts; no environment, script or child transcript is embedded here.
type stageEndpointIdentity struct {
	Endpoint     string `json:"endpoint"`
	DaemonID     string `json:"daemon_id"`
	SocketDevice uint64 `json:"socket_device"`
	SocketInode  uint64 `json:"socket_inode"`
}
type stageConsentFlags struct {
	Trust        bool `json:"trust"`
	Confirmation bool `json:"confirmation"`
	Volumes      bool `json:"volumes"`
	Force        bool `json:"force"`
	Quiescence   bool `json:"quiescence"`
}
type stageCandidateGitVersion struct {
	RepositoryPath string            `json:"repository_path"`
	HeadOID        string            `json:"head_oid"`
	Refs           map[string]string `json:"refs"`
	WorkingDigest  string            `json:"working_digest"`
}
type stageReceipt struct {
	Version                 int                          `json:"version"`
	OperationID             string                       `json:"operation_id"`
	Sequence                int                          `json:"sequence"`
	PreviousHash            string                       `json:"previous_hash"`
	State                   string                       `json:"state"`
	EndpointIdentity        stageEndpointIdentity        `json:"endpoint_identity"`
	OriginalSource          string                       `json:"original_source"`
	InputManifestDigest     string                       `json:"input_manifest_digest"`
	ToolManifest            map[string]stageToolIdentity `json:"tool_manifest"`
	ConfigDigest            string                       `json:"config_digest"`
	ImageMetadataDigest     string                       `json:"image_metadata_digest"`
	EffectiveConfigDigest   string                       `json:"effective_config_digest"`
	OutputInventory         []StageOutputEntry           `json:"output_inventory"`
	OriginBinding           []StageRepositoryBinding     `json:"origin_binding"`
	CandidateGitVersion     []stageCandidateGitVersion   `json:"candidate_git_version"`
	ExecutionManifestDigest string                       `json:"execution_manifest_digest"`
	PhaseBindingMode        string                       `json:"phase_binding_mode"`
	CloneRepositories       []StageRepository            `json:"clone_repositories"`
	ExportedRefs            []StageResultRef             `json:"exported_refs"`
	ImportedObjects         []StageGitObjectsTarget      `json:"imported_objects"`
	ResultRefTransactions   []StageGitRefsTarget         `json:"result_ref_transactions"`
	Primary                 *StageResource               `json:"primary"`
	Candidate               *StageResource               `json:"candidate"`
	Home                    *StageResource               `json:"home"`
	TrustDigest             string                       `json:"trust_digest"`
	ConsentFlags            stageConsentFlags            `json:"consent_flags"`
	Verification            *StageVerification           `json:"verification"`
	UnverifiedReason        *string                      `json:"unverified_reason"`
	ActionInventory         []StageEffect                `json:"action_inventory"`
}
