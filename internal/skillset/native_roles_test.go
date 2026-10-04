package skillset

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func nativeFixture(t *testing.T) ([]byte, Pin) {
	t.Helper()
	data := []byte(`{"contract_version":2,"identity":"alternative","entrypoint":"SKILL.md","required_project_inputs":[],"clients":[{"name":"codex","version":"1.2.3"}],"roles":[{"name":"author","description":"Author","instructions":"roles/author.md","bindings":[{"client":"codex","model":"sol6.1","effort":"low"}],"tools":[]},{"name":"reviewer","description":"Reviewer","instructions":"roles/reviewer.md","bindings":[{"client":"codex","model":"sol6.1","effort":"medium"}],"tools":["read"]}],"files":[{"path":"SKILL.md","sha256":"` + Digest([]byte("workflow")) + `"},{"path":"roles/author.md","sha256":"` + Digest([]byte("author")) + `"},{"path":"roles/reviewer.md","sha256":"` + Digest([]byte("reviewer")) + `"}]}`)
	var inventory struct {
		Files []File `json:"files"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	digest, err := InventoryDigest(inventory.Files)
	if err != nil {
		t.Fatal(err)
	}
	return data, Pin{Identity: "alternative", Source: "https://example.invalid/skills", Commit: strings.Repeat("a", 40), ManifestSHA256: Digest(data), InventorySHA256: digest, ContractVersion: 2}
}

func TestNativeManifestArrayLimitsAndOrdering(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NativeManifest)
	}{
		{"clients over limit", func(manifest *NativeManifest) {
			manifest.Clients = nil
			for index := range 33 {
				manifest.Clients = append(manifest.Clients, NativeClient{Name: fmt.Sprintf("client%02d", index), Version: "1"})
			}
		}},
		{"clients unsorted", func(manifest *NativeManifest) {
			manifest.Clients = append(manifest.Clients, NativeClient{Name: "alpha", Version: "1"})
		}},
		{"roles empty", func(manifest *NativeManifest) { manifest.Roles = []NativeRole{} }},
		{"roles over limit", func(manifest *NativeManifest) {
			role := manifest.Roles[0]
			manifest.Roles = nil
			for index := range 65 {
				role.Name = fmt.Sprintf("role%02d", index)
				manifest.Roles = append(manifest.Roles, role)
			}
		}},
		{"bindings duplicate", func(manifest *NativeManifest) {
			manifest.Roles[0].Bindings = append(manifest.Roles[0].Bindings, manifest.Roles[0].Bindings[0])
		}},
		{"bindings unsorted", func(manifest *NativeManifest) {
			manifest.Clients = append([]NativeClient{{Name: "alpha", Version: "1"}}, manifest.Clients...)
			manifest.Roles[0].Bindings = append(manifest.Roles[0].Bindings, RoleBinding{Client: "alpha", Model: "explicit", Effort: "low"})
		}},
		{"bindings over limit", func(manifest *NativeManifest) {
			manifest.Roles[0].Bindings = make([]RoleBinding, 33)
		}},
		{"tools over limit", func(manifest *NativeManifest) {
			manifest.Roles[0].Tools = nil
			for index := range 65 {
				manifest.Roles[0].Tools = append(manifest.Roles[0].Tools, fmt.Sprintf("tool%02d", index))
			}
		}},
		{"inputs over limit", func(manifest *NativeManifest) {
			for index := range 33 {
				manifest.RequiredProjectInputs = append(manifest.RequiredProjectInputs, fmt.Sprintf("input%02d", index))
			}
		}},
		{"inputs duplicate", func(manifest *NativeManifest) { manifest.RequiredProjectInputs = []string{"policy", "policy"} }},
		{"version over limit", func(manifest *NativeManifest) { manifest.Clients[0].Version = strings.Repeat("a", 129) }},
		{"model over limit", func(manifest *NativeManifest) { manifest.Roles[0].Bindings[0].Model = strings.Repeat("a", 129) }},
		{"effort over limit", func(manifest *NativeManifest) { manifest.Roles[0].Bindings[0].Effort = strings.Repeat("a", 33) }},
		{"inventory case collision", func(manifest *NativeManifest) {
			manifest.Files = append(manifest.Files, File{Path: "skill.md", SHA256: Digest([]byte("collision"))})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, pin := nativeFixture(t)
			manifest, err := DecodeNativeManifest(data, pin)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&manifest)
			changed, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			pin.ManifestSHA256 = Digest(changed)
			if _, err := DecodeNativeManifest(changed, pin); err == nil {
				t.Fatal("accepted invalid limits/order")
			}
		})
	}
}

func TestNativeManifestExactBoundaries(t *testing.T) {
	data, pin := nativeFixture(t)
	manifest, err := DecodeNativeManifest(data, pin)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Clients = nil
	bindings := []RoleBinding{}
	for index := range 32 {
		name := fmt.Sprintf("client%02d", index)
		manifest.Clients = append(manifest.Clients, NativeClient{Name: name, Version: strings.Repeat("v", 128)})
		bindings = append(bindings, RoleBinding{Client: name, Model: strings.Repeat("m", 128), Effort: strings.Repeat("e", 32)})
		manifest.RequiredProjectInputs = append(manifest.RequiredProjectInputs, fmt.Sprintf("input%02d", index))
	}
	tools := []string{}
	for index := range 64 {
		tools = append(tools, fmt.Sprintf("tool%02d", index))
	}
	role := manifest.Roles[0]
	manifest.Roles = nil
	for index := range 64 {
		role.Name = fmt.Sprintf("role%02d", index) + strings.Repeat("a", 58)
		role.Description = strings.Repeat("é", 2048)
		role.Bindings = bindings
		role.Tools = tools
		manifest.Roles = append(manifest.Roles, role)
	}
	if err := manifest.Validate(pin); err != nil {
		t.Fatalf("exact declared boundaries refused: %v", err)
	}
}

func TestNativeDecoderPreservesV1(t *testing.T) {
	data, pin := nativeFixture(t)
	pin.ContractVersion = 1
	manifest := Manifest{ContractVersion: 1, Identity: pin.Identity, Entrypoint: "SKILL.md", RequiredProjectInputs: []string{}, Adapters: []Binding{{Name: "codex", Version: "1.2.3", Model: "sol6.1", Effort: "low"}}}
	var inventory struct {
		Files []File `json:"files"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	manifest.Files = inventory.Files
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	pin.ManifestSHA256 = Digest(encoded)
	var decoded Manifest
	if err := decode(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(pin); err != nil {
		t.Fatal(err)
	}
	if err := pin.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeNativeManifest(encoded, pin); err == nil {
		t.Fatal("v1 must not be reinterpreted as native roles")
	}
}

func TestDecodeNativeManifest(t *testing.T) {
	data, pin := nativeFixture(t)
	manifest, err := DecodeNativeManifest(data, pin)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Roles[0].Bindings[0].Effort != "low" || manifest.Roles[1].Bindings[0].Effort != "medium" || manifest.Roles[0].Tools == nil {
		t.Fatalf("lost explicit role requests: %+v", manifest)
	}
	if err := pin.Validate(); err == nil {
		t.Fatal("offline v2 decoder must not enable the v1 store")
	}
}

func TestNativeClientVersionSelection(t *testing.T) {
	for _, test := range []struct {
		version string
		valid   bool
	}{
		{"*", false},
		{">=1.2.3", false},
		{"^1.2.3", false},
		{"1.2.x", false},
		{"1.2.3 || 2.0.0", false},
		{"~1.2.3", false},
		{"<2.0.0", false},
		{"1.2.X", false},
		{"1.2.3 - 2.0.0", false},
		{"1.2.3,2.0.0", false},
		{"1.2.3", true},
		{"v1.2.3", true},
		{"1.2.3-rc.1+build7", true},
		{strings.Repeat("v", 128), true},
	} {
		t.Run(test.version, func(t *testing.T) {
			data, pin := nativeFixture(t)
			version, err := json.Marshal(test.version)
			if err != nil {
				t.Fatal(err)
			}
			changed := []byte(strings.Replace(string(data), `"version":"1.2.3"`, `"version":`+string(version), 1))
			pin.ManifestSHA256 = Digest(changed)
			_, err = DecodeNativeManifest(changed, pin)
			if (err == nil) != test.valid {
				t.Fatalf("version %q: error = %v, want valid = %v", test.version, err, test.valid)
			}
		})
	}
}

func TestDecodeNativeManifestRefuses(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{"unknown version", `"contract_version":2`, `"contract_version":3`},
		{"v1", `"contract_version":2`, `"contract_version":1`},
		{"adapters", `"clients":`, `"adapters":`},
		{"case alias", `"identity":`, `"Identity":`},
		{"duplicate key", `"identity":"alternative"`, `"identity":"alternative","identity":"alternative"`},
		{"unknown client field", `"version":"1.2.3"`, `"version":"1.2.3","provider":"vendor"`},
		{"null clients", `"clients":[{"name":"codex","version":"1.2.3"}]`, `"clients":null`},
		{"empty clients", `"clients":[{"name":"codex","version":"1.2.3"}]`, `"clients":[]`},
		{"duplicate client", `"clients":[{"name":"codex","version":"1.2.3"}]`, `"clients":[{"name":"codex","version":"1.2.3"},{"name":"codex","version":"1.2.3"}]`},
		{"missing tools", `,"tools":[]`, ``},
		{"null tools", `"tools":[]`, `"tools":null`},
		{"duplicate tools", `"tools":["read"]`, `"tools":["read","read"]`},
		{"unsorted tools", `"tools":["read"]`, `"tools":["write","read"]`},
		{"invalid tool", `"tools":["read"]`, `"tools":["*"]`},
		{"unbound client", `"client":"codex"`, `"client":"other"`},
		{"null bindings", `"bindings":[{"client":"codex","model":"sol6.1","effort":"low"}]`, `"bindings":null`},
		{"empty bindings", `"bindings":[{"client":"codex","model":"sol6.1","effort":"low"}]`, `"bindings":[]`},
		{"missing effort", `,"effort":"low"`, ``},
		{"control model", `"model":"sol6.1"`, `"model":"sol\t6.1"`},
		{"control version", `"version":"1.2.3"`, `"version":"1.2\u007f3"`},
		{"empty description", `"description":"Author"`, `"description":""`},
		{"oversized description", `"description":"Author"`, `"description":"` + strings.Repeat("a", 4097) + `"`},
		{"unlisted instructions", `"instructions":"roles/author.md"`, `"instructions":"roles/missing.md"`},
		{"unsafe instructions", `"instructions":"roles/author.md"`, `"instructions":"../author.md"`},
		{"duplicate role", `"name":"reviewer"`, `"name":"author"`},
		{"unsorted role", `"name":"author"`, `"name":"worker"`},
		{"null inputs", `"required_project_inputs":[]`, `"required_project_inputs":null`},
		{"unsorted inputs", `"required_project_inputs":[]`, `"required_project_inputs":["z","a"]`},
		{"null files", `"files":[`, `"files":null,"ignored":[`},
		{"trailing JSON", `}]}`, `}]} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, pin := nativeFixture(t)
			changed := []byte(strings.Replace(string(data), test.old, test.new, 1))
			if string(changed) == string(data) {
				t.Fatal("fixture mutation did not apply")
			}
			pin.ManifestSHA256 = Digest(changed)
			if _, err := DecodeNativeManifest(changed, pin); err == nil {
				t.Fatal("accepted invalid native declaration")
			}
		})
	}
}

func TestNativeManifestPinAndByteLimits(t *testing.T) {
	for _, field := range []string{"manifest", "inventory", "identity", "commit", "source", "version", "bytes", "utf8"} {
		t.Run(field, func(t *testing.T) {
			data, pin := nativeFixture(t)
			switch field {
			case "manifest":
				pin.ManifestSHA256 = strings.Repeat("0", 64)
			case "inventory":
				pin.InventorySHA256 = strings.Repeat("0", 64)
			case "identity":
				pin.Identity = "different"
			case "commit":
				pin.Commit = "main"
			case "source":
				pin.Source = ""
			case "version":
				pin.ContractVersion = 1
			case "bytes":
				data = append(data, []byte(strings.Repeat(" ", MaxManifestBytes))...)
				pin.ManifestSHA256 = Digest(data)
			case "utf8":
				data = []byte(strings.Replace(string(data), "Author", string([]byte{0xff}), 1))
				pin.ManifestSHA256 = Digest(data)
			}
			if _, err := DecodeNativeManifest(data, pin); err == nil {
				t.Fatal("accepted invalid native manifest/pin")
			}
		})
	}
}
