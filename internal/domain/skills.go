package domain

type SkillSelection struct {
	Mode              string `json:"mode"`
	Identity          string `json:"identity,omitempty"`
	Source            string `json:"source,omitempty"`
	Commit            string `json:"commit,omitempty"`
	ManifestSHA256    string `json:"manifest_sha256,omitempty"`
	InventorySHA256   string `json:"inventory_sha256,omitempty"`
	ContractVersion   int    `json:"contract_version,omitempty"`
	Entrypoint        string `json:"entrypoint,omitempty"`
	Adapter           string `json:"adapter,omitempty"`
	AdapterVersion    string `json:"adapter_version,omitempty"`
	Model             string `json:"model,omitempty"`
	Effort            string `json:"effort,omitempty"`
	ProjectRevision   string `json:"project_revision,omitempty"`
	ProjectSHA256     string `json:"project_sha256,omitempty"`
	InstructionSHA256 string `json:"instruction_sha256,omitempty"`
}
