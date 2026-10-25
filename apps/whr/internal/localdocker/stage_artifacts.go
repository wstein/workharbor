// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// These are closed private artifact projections, not additional public result
// or receipt fields. execution-binding.json is bound by the receipt; its settings
// digest transitively binds every other private artifact used at promotion.
type stageSettings struct {
	ConfigRelativePath         string   `json:"config_relative_path"`
	PrimaryConfigDigest        string   `json:"primary_config_digest"`
	GitSnapshotsDigest         string   `json:"git_snapshots_digest"`
	HomePlanDigest             string   `json:"home_plan_digest"`
	HomeObservationDigest      string   `json:"home_observation_digest"`
	PrimaryObservationDigest   string   `json:"primary_observation_digest"`
	CandidateObservationDigest string   `json:"candidate_observation_digest"`
	AnchorageFacts             bool     `json:"anchorage_facts"`
	SmokeRequested             bool     `json:"smoke_requested"`
	ExecutionPaths             []string `json:"execution_paths"`
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
	Environment []string          `json:"Environment"`
	Entrypoint  []string          `json:"Entrypoint"`
	Command     []string          `json:"Command"`
}
type stageImageDetails struct {
	ID       string           `json:"Id"`
	Metadata []map[string]any `json:"Metadata"`
}

type stageVolumeDetails struct {
	Name      string            `json:"Name"`
	Driver    string            `json:"Driver"`
	CreatedAt string            `json:"CreatedAt"`
	Labels    map[string]string `json:"Labels"`
}

func stageArtifactBytes(value any) ([]byte, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	var canonical any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e = d.Decode(&canonical); e != nil {
		return nil, e
	}
	b, e = json.Marshal(canonical)
	if e != nil {
		return nil, e
	}
	return append(b, '\n'), nil
}
func stageDigest(value any) string {
	b, e := stageArtifactBytes(value)
	if e != nil {
		return ""
	}
	return stageBytesDigest(b)
}
func stageBytesDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
