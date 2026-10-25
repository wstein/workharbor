// SPDX-License-Identifier: EUPL-1.2
package localdocker

// LifecycleResult is the closed local stage/promote result. Nullable fields are
// deliberately present even on previews and refusals; no remote operation ID,
// environment, script body or child transcript is part of this model.
type LifecycleResult struct {
	Status           string              `json:"status"`
	UnverifiedReason *string             `json:"unverified_reason"`
	Phase            string              `json:"phase"`
	RecoveryID       *string             `json:"recovery_id"`
	Fingerprint      *string             `json:"fingerprint"`
	Primary          *StageResource      `json:"primary"`
	Candidate        *StageResource      `json:"candidate"`
	Home             *StageResource      `json:"home"`
	Verification     *StageVerification  `json:"verification"`
	Effects          []StageEffect       `json:"effects"`
	Recovery         []StageRecoveryStep `json:"recovery"`
	Repositories     []StageRepository   `json:"repositories"`
	ResultRefs       []StageResultRef    `json:"result_refs"`
}
type StageResource struct {
	Kind     string  `json:"kind"`
	Name     *string `json:"name"`
	ID       *string `json:"id"`
	DaemonID string  `json:"daemon_id"`
}
type StageRepository struct {
	SourcePath             string `json:"source_path"`
	ClonePath              string `json:"clone_path"`
	CloneOwnerUID          int    `json:"clone_owner_uid"`
	CloneRootMode          int    `json:"clone_root_mode"`
	ContainerWorkspacePath string `json:"container_workspace_path"`
	HeadOID                string `json:"head_oid"`
	ObjectFormat           string `json:"object_format"`
	Dirty                  bool   `json:"dirty"`
	UncommittedRetained    bool   `json:"uncommitted_retained"`
}
type StageResultRef struct {
	RepositoryPath string `json:"repository_path"`
	ExportRef      string `json:"export_ref"`
	TargetRef      string `json:"target_ref"`
	OID            string `json:"oid"`
	State          string `json:"state"`
}
type StageVerification struct {
	Policy          string  `json:"policy"`
	Status          string  `json:"status"`
	BindingMode     string  `json:"binding_mode"`
	Quiescence      string  `json:"quiescence"`
	PreInputDigest  string  `json:"pre_input_digest"`
	PostInputDigest *string `json:"post_input_digest"`
	ImageID         string  `json:"image_id"`
	InputDigest     string  `json:"input_digest"`
	ObservedAt      string  `json:"observed_at"`
	OutputDigest    *string `json:"output_digest"`
}
type StageRepositoryBinding struct {
	SourcePath      string `json:"source_path"`
	GitDir          string `json:"git_dir"`
	CommonDir       string `json:"common_dir"`
	ObjectFormat    string `json:"object_format"`
	OriginalHeadOID string `json:"original_head_oid"`
	BindingDigest   string `json:"binding_digest"`
}
type StageGitObjectsTarget struct {
	Kind                 string                 `json:"kind"`
	Repository           StageRepositoryBinding `json:"repository"`
	ExportPath           string                 `json:"export_path"`
	ObjectManifestDigest string                 `json:"object_manifest_digest"`
	ObjectCount          int                    `json:"object_count"`
}
type StageRefCreate struct {
	Name           string `json:"name"`
	OID            string `json:"oid"`
	ExpectedAbsent bool   `json:"expected_absent"`
}
type StageGitRefsTarget struct {
	Kind       string                 `json:"kind"`
	Repository StageRepositoryBinding `json:"repository"`
	Creates    []StageRefCreate       `json:"creates"`
}
type StageCloneOutputTarget struct {
	Kind                string `json:"kind"`
	ClonePath           string `json:"clone_path"`
	RelativePath        string `json:"relative_path"`
	Digest              string `json:"digest"`
	ProducerOperationID string `json:"producer_operation_id"`
}
type StageOutputEntry struct {
	Path                 string  `json:"path"`
	Origin               string  `json:"origin"`
	Size                 int64   `json:"size"`
	Digest               string  `json:"digest"`
	ProducerOperationID  *string `json:"producer_operation_id"`
	ProducerInputDigest  *string `json:"producer_input_digest"`
	AdmissionOperationID string  `json:"admission_operation_id"`
	AdmissionInputDigest string  `json:"admission_input_digest"`
}

// Target contains exactly one of the four closed target types above. The
// constructors/decoder validate its discriminator before it enters a receipt.
type StageEffect struct {
	Action string      `json:"action"`
	Target StageTarget `json:"target"`
	State  string      `json:"state"`
}
type StageRecoveryStep struct {
	Action      string       `json:"action"`
	Target      *StageTarget `json:"target"`
	Instruction string       `json:"instruction"`
}
type StageTarget struct {
	Resource *StageResource
	Objects  *StageGitObjectsTarget
	Refs     *StageGitRefsTarget
	Output   *StageCloneOutputTarget
}
