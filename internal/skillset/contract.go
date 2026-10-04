// Package skillset validates pinned external text packages independently of
// repository content and client discovery (D52).
package skillset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ManifestName = "workharbor.json"

const (
	MaxManifestBytes = 256 << 10
	MaxFiles         = 1024
	MaxFileBytes     = 1 << 20
	MaxTotalBytes    = 16 << 20
)

var (
	hashRE   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Binding struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

type Manifest struct {
	ContractVersion       int       `json:"contract_version"`
	Identity              string    `json:"identity"`
	Entrypoint            string    `json:"entrypoint"`
	RequiredProjectInputs []string  `json:"required_project_inputs"`
	Adapters              []Binding `json:"adapters"`
	Files                 []File    `json:"files"`
}

type NativeClient struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RoleBinding struct {
	Client string `json:"client"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type NativeRole struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Instructions string        `json:"instructions"`
	Bindings     []RoleBinding `json:"bindings"`
	Tools        []string      `json:"tools"`
}

type NativeManifest struct {
	ContractVersion       int            `json:"contract_version"`
	Identity              string         `json:"identity"`
	Entrypoint            string         `json:"entrypoint"`
	RequiredProjectInputs []string       `json:"required_project_inputs"`
	Clients               []NativeClient `json:"clients"`
	Roles                 []NativeRole   `json:"roles"`
	Files                 []File         `json:"files"`
}

func DecodeNativeManifest(data []byte, pin Pin) (NativeManifest, error) {
	var manifest NativeManifest
	if len(data) > MaxManifestBytes || !utf8.Valid(data) || pin.ContractVersion != 2 || Digest(data) != pin.ManifestSHA256 {
		return manifest, errors.New("native skill manifest needs bounded UTF-8 and an exact trusted v2 pin")
	}
	v1Pin := pin
	v1Pin.ContractVersion = 1
	if err := v1Pin.Validate(); err != nil {
		return manifest, err
	}
	if err := decode(data, &manifest); err != nil {
		return NativeManifest{}, err
	}
	if err := nativeShape(data, "manifest"); err != nil {
		return NativeManifest{}, err
	}
	if err := manifest.Validate(pin); err != nil {
		return NativeManifest{}, err
	}
	return manifest, nil
}

func nativeShape(data []byte, kind string) error {
	fields := map[string][]string{
		"manifest": {"contract_version", "identity", "entrypoint", "required_project_inputs", "clients", "roles", "files"},
		"clients":  {"name", "version"},
		"roles":    {"name", "description", "instructions", "bindings", "tools"},
		"bindings": {"client", "model", "effort"},
		"files":    {"path", "sha256"},
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if len(object) != len(fields[kind]) {
		return errors.New("native skill object has missing or unknown fields")
	}
	for _, field := range fields[kind] {
		value, found := object[field]
		if !found || string(value) == "null" {
			return errors.New("native skill fields must use exact names and nonnull values")
		}
		if field == "clients" || field == "roles" || field == "bindings" || field == "files" {
			var entries []json.RawMessage
			if err := json.Unmarshal(value, &entries); err != nil {
				return err
			}
			for _, entry := range entries {
				if err := nativeShape(entry, field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func nativeValue(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func nativeClientVersion(version string) bool {
	if !nativeValue(version, 128) || strings.ContainsAny(version, "*<>=^~|,") || strings.ContainsFunc(version, unicode.IsSpace) {
		return false
	}
	for _, component := range strings.Split(version, ".") {
		if strings.EqualFold(component, "x") {
			return false
		}
	}
	return true
}

func nativeNames(names []string, limit int) bool {
	if names == nil || len(names) > limit {
		return false
	}
	previous := ""
	for _, name := range names {
		if !nameRE.MatchString(name) || name <= previous {
			return false
		}
		previous = name
	}
	return true
}

func (m NativeManifest) Validate(pin Pin) error {
	digest, err := InventoryDigest(m.Files)
	if err != nil {
		return err
	}
	if m.ContractVersion != 2 || pin.ContractVersion != 2 || !nameRE.MatchString(m.Identity) || m.Identity != pin.Identity || digest != pin.InventorySHA256 || !validPath(m.Entrypoint) {
		return errors.New("native skill manifest does not match the trusted v2 pin")
	}
	inventory := map[string]bool{}
	for _, file := range m.Files {
		inventory[file.Path] = true
	}
	if !inventory[m.Entrypoint] || !nativeNames(m.RequiredProjectInputs, 32) || len(m.Clients) == 0 || len(m.Clients) > 32 || len(m.Roles) == 0 || len(m.Roles) > 64 {
		return errors.New("native skill manifest needs bounded clients, roles, inputs and an inventoried entrypoint")
	}
	clients := map[string]bool{}
	previous := ""
	for _, client := range m.Clients {
		if !nameRE.MatchString(client.Name) || client.Name <= previous || !nativeClientVersion(client.Version) {
			return errors.New("invalid, duplicate or unsorted native skill client")
		}
		clients[client.Name] = true
		previous = client.Name
	}
	previous = ""
	for _, role := range m.Roles {
		if !nameRE.MatchString(role.Name) || role.Name <= previous || role.Description == "" || len(role.Description) > 4096 || !utf8.ValidString(role.Description) || !validPath(role.Instructions) || !inventory[role.Instructions] || !nativeNames(role.Tools, 64) || len(role.Bindings) == 0 || len(role.Bindings) > 32 {
			return errors.New("invalid, duplicate, unsorted or unbounded native skill role")
		}
		previous = role.Name
		previousClient := ""
		for _, binding := range role.Bindings {
			if !clients[binding.Client] || binding.Client <= previousClient || !nativeValue(binding.Model, 128) || !nativeValue(binding.Effort, 32) {
				return errors.New("invalid, duplicate, unsorted or undeclared native role binding")
			}
			previousClient = binding.Client
		}
	}
	return nil
}

type Pin struct {
	Identity        string `json:"identity"`
	Source          string `json:"source"`
	Commit          string `json:"commit"`
	ManifestSHA256  string `json:"manifest_sha256"`
	InventorySHA256 string `json:"inventory_sha256"`
	ContractVersion int    `json:"contract_version"`
}

type Config struct {
	Store     string `json:"store,omitempty"`
	Selection string `json:"selection,omitempty"`
	Default   *Pin   `json:"default,omitempty"`
	Package   *Pin   `json:"package,omitempty"`
}

func (c Config) Resolve() (*Pin, error) {
	switch c.Selection {
	case "none":
		if c.Package != nil {
			return nil, errors.New("skill_set none cannot select a package")
		}
		return nil, nil
	case "", "default":
		if c.Package != nil {
			return nil, errors.New("skill_set default cannot select an alternative package")
		}
		if c.Default == nil {
			return nil, errors.New("crewbook default is unconfigured: install a reviewed compatible pin or select none explicitly")
		}
		if c.Default.Identity != "crewbook" {
			return nil, errors.New("the default skill set must identify crewbook")
		}
		if err := c.Default.Validate(); err != nil {
			return nil, err
		}
		return c.Default, nil
	case "package":
		if c.Package == nil {
			return nil, errors.New("skill_set package needs an external pin")
		}
		if err := c.Package.Validate(); err != nil {
			return nil, err
		}
		return c.Package, nil
	default:
		return nil, errors.New("unknown skill_set selection")
	}
}

func (p Pin) Validate() error {
	if p.ContractVersion != 1 || !nameRE.MatchString(p.Identity) || !commitRE.MatchString(p.Commit) || !hashRE.MatchString(p.ManifestSHA256) || !hashRE.MatchString(p.InventorySHA256) || p.Source == "" || len(p.Source) > 2048 || strings.ContainsAny(p.Source, "\r\n\x00") {
		return errors.New("skill pin needs contract v1, identity, source, full commit and externally trusted SHA256 digests")
	}
	return nil
}

func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func validPath(value string) bool {
	if value == "" || len(value) > 240 || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "." || component == ".." || strings.EqualFold(component, ".git") || strings.TrimRight(component, ". ") != component {
			return false
		}
	}
	for _, char := range value {
		allowed := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._-/", char)
		if !allowed {
			return false
		}
	}
	return true
}

func InventoryDigest(files []File) (string, error) {
	if len(files) == 0 || len(files) > MaxFiles {
		return "", errors.New("skill inventory file count exceeds limits")
	}
	var encoded strings.Builder
	seen := map[string]bool{}
	previous := ""
	for _, file := range files {
		if !validPath(file.Path) || file.Path == ManifestName || file.Path <= previous || !hashRE.MatchString(file.SHA256) {
			return "", fmt.Errorf("invalid or unsorted skill inventory path %q", file.Path)
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return "", fmt.Errorf("colliding skill path %q", file.Path)
		}
		seen[key] = true
		previous = file.Path
		encoded.WriteString(file.SHA256 + "  " + file.Path + "\n")
	}
	return Digest([]byte(encoded.String())), nil
}

func decode(data []byte, destination any) error {
	if !json.Valid(data) {
		return errors.New("invalid skill JSON")
	}
	if err := uniqueKeys(json.NewDecoder(strings.NewReader(string(data)))); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func uniqueKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate skill JSON field")
			}
			seen[name] = true
		}
		if err := uniqueKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (m Manifest) Validate(pin Pin) error {
	digest, err := InventoryDigest(m.Files)
	if err != nil {
		return err
	}
	if m.ContractVersion != 1 || m.Identity != pin.Identity || digest != pin.InventorySHA256 || !validPath(m.Entrypoint) {
		return errors.New("skill manifest does not match the trusted pin")
	}
	found := false
	for _, file := range m.Files {
		found = found || file.Path == m.Entrypoint
	}
	if !found || len(m.Adapters) == 0 || len(m.Adapters) > 32 || m.RequiredProjectInputs == nil || len(m.RequiredProjectInputs) > 32 {
		return errors.New("skill manifest needs an inventoried entrypoint and bounded adapters/project inputs")
	}
	seen := map[string]bool{}
	for _, binding := range m.Adapters {
		if !nameRE.MatchString(binding.Name) || binding.Version == "" || len(binding.Version) > 128 || binding.Model == "" || len(binding.Model) > 128 || binding.Effort == "" || len(binding.Effort) > 32 || seen[binding.Name] || strings.ContainsAny(binding.Version+binding.Model+binding.Effort, "\r\n\x00") {
			return errors.New("invalid or duplicate explicit skill adapter binding")
		}
		seen[binding.Name] = true
	}
	seen = map[string]bool{}
	for _, input := range m.RequiredProjectInputs {
		if !nameRE.MatchString(input) || seen[input] {
			return errors.New("invalid or duplicate required project input")
		}
		seen[input] = true
	}
	return nil
}
