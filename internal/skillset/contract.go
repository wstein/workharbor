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
