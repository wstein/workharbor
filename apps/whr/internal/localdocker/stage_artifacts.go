// SPDX-License-Identifier: EUPL-1.2
package localdocker

// These are closed private artifact projections, not additional public result
// or receipt fields. execution-binding.json is bound by the receipt; its settings
// digest transitively binds every other private artifact used at promotion.
type stageSettings struct {
	ConfigRelativePath  string   `json:"config_relative_path"`
	PrimaryConfigDigest string   `json:"primary_config_digest"`
	GitSnapshotsDigest  string   `json:"git_snapshots_digest"`
	HomePlanDigest      string   `json:"home_plan_digest"`
	AnchorageFacts      bool     `json:"anchorage_facts"`
	SmokeRequested      bool     `json:"smoke_requested"`
	ExecutionPaths      []string `json:"execution_paths"`
}
type stageExecutionGitInput struct {
	RepositoryPath string `json:"repository_path"`
	Predicate      string `json:"predicate"`
	InputDigest    string `json:"input_digest"`
}
type stageExecutionBinding struct {
	SettingsDigest  string                   `json:"settings_digest"`
	CandidateDigest string                   `json:"candidate_digest"`
	OriginalDigest  string                   `json:"original_digest"`
	GitInputs       []stageExecutionGitInput `json:"git_inputs"`
}

type stageContainerDetails struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	ImageID     string            `json:"ImageID"`
	ConfigImage string            `json:"ConfigImage"`
	User        string            `json:"User"`
	Hostname    string            `json:"Hostname"`
	State       string            `json:"State"`
	Running     bool              `json:"Running"`
	Healthcheck bool              `json:"Healthcheck"`
	Health      *string           `json:"Health"`
	Labels      map[string]string `json:"Labels"`
	Mounts      []mount           `json:"Mounts"`
}
type stageImageDetails struct {
	ID       string           `json:"Id"`
	Metadata []map[string]any `json:"Metadata"`
}
