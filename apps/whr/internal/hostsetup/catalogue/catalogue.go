// SPDX-License-Identifier: EUPL-1.2

// Package catalogue validates host setup catalogue and template bytes. It performs
// no filesystem, process, package-manager or network operations. The production
// snapshot and measured minimum versions are deliberately not supplied here.
package catalogue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// Catalogue is a validated snapshot. Templates contains independent copies of
// only the referenced template bytes; callers never retain input buffer aliases.
type Catalogue struct {
	SchemaVersion    int               `json:"schema_version"`
	CatalogueVersion string            `json:"catalogue_version"`
	Items            []Item            `json:"items"`
	Templates        map[string][]byte `json:"-"`
}

// Item describes a curated host tool or runtime and its optional configuration.
type Item struct {
	ID                string       `json:"id"`
	Kind              string       `json:"kind"`
	Title             string       `json:"title"`
	Platforms         []string     `json:"platforms"`
	SelectedByDefault bool         `json:"selected_by_default"`
	Requires          []string     `json:"requires,omitempty"`
	Network           Network      `json:"network"`
	Privileged        []Privileged `json:"privileged,omitempty"`
	Tool              Tool         `json:"tool"`
	Config            *Config      `json:"config,omitempty"`
}

// Tool is fixed version-probe metadata and a package-manager source declaration.
type Tool struct {
	Command      string   `json:"command"`
	VersionArgs  []string `json:"version_args"`
	VersionRegex string   `json:"version_regex"`
	MinVersion   string   `json:"min_version"`
	Source       Source   `json:"source"`
}

// Source identifies a Homebrew formula or cask; it never causes installation.
type Source struct {
	Manager    string `json:"manager"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Tap        string `json:"tap"`
	MayElevate bool   `json:"may_elevate"`
}

// Network declares permitted lookup destinations and their displayed purpose.
type Network struct {
	Hosts      []string `json:"hosts"`
	Purpose    string   `json:"purpose"`
	LookupURLs []string `json:"lookup_urls,omitempty"`
}

// Privileged is a catalogue-reviewed command declaration, never executed here.
type Privileged struct {
	Purpose string   `json:"purpose"`
	Argv    []string `json:"argv"`
}

// Config binds a home-relative destination to exact snapshot template bytes.
type Config struct {
	Marker      string `json:"marker"`
	Target      string `json:"target"`
	ContentFile string `json:"content_file"`
	SHA256      string `json:"sha256"`
	Mode        string `json:"mode"`
}

// Load enforces schema_version 1, the catalogue schema and its x-loader-rules.
// The schema is mirrored below with standard-library checks; tests compare the
// fixture corpus with the authoritative JSON Schema using its existing test-only
// validator. Invalid input uses the existing CLI exit table's internal (74).
func Load(raw []byte, templates map[string][]byte) (*Catalogue, error) {
	invalid := func() (*Catalogue, error) {
		return nil, &exitcode.Error{Path: exitcode.PathUnclassified, Message: "Host setup catalogue is invalid.", Result: map[string]string{"reason": "host_setup.catalogue_invalid"}}
	}
	if !utf8.Valid(raw) {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readValue(decoder)
	if err != nil {
		return invalid()
	}
	if _, err = decoder.Token(); err != io.EOF {
		return invalid()
	}
	if !validCatalogue(value) {
		return invalid()
	}
	var result Catalogue
	if err = json.Unmarshal(raw, &result); err != nil {
		return invalid()
	}
	ids := make(map[string]int, len(result.Items))
	for i, item := range result.Items {
		if _, exists := ids[item.ID]; exists {
			return invalid()
		}
		ids[item.ID] = i
		if agentIdentity(item.ID) || agentIdentity(item.Tool.Command) || agentIdentity(item.Tool.Source.Name) {
			return invalid()
		}
		if (item.Tool.Source.Type == "formula" && item.Tool.Source.Tap != "homebrew/core") || (item.Tool.Source.Type == "cask" && item.Tool.Source.Tap != "homebrew/cask") {
			return invalid()
		}
		if _, err = regexp.Compile(item.Tool.VersionRegex); err != nil {
			return invalid()
		}
		hosts := make(map[string]bool, len(item.Network.Hosts))
		for _, host := range item.Network.Hosts {
			hosts[host] = true
		}
		for _, lookup := range item.Network.LookupURLs {
			u, e := url.Parse(lookup)
			if e != nil || u.Scheme != "https" || !hosts[u.Host] {
				return invalid()
			}
		}
		if item.Config != nil {
			config := item.Config
			if strings.Contains(config.Target, "..") || strings.Contains(config.Target, "//") {
				return invalid()
			}
			// A filename matching the schema can still be the special path . or .. .
			if config.ContentFile == "." || config.ContentFile == ".." {
				return invalid()
			}
			content, exists := templates[config.ContentFile]
			if !exists {
				return invalid()
			}
			digest := sha256.Sum256(content)
			if hex.EncodeToString(digest[:]) != config.SHA256 {
				return invalid()
			}
		}
	}
	state := make([]uint8, len(result.Items))
	var visit func(int) bool
	visit = func(i int) bool {
		if state[i] == 1 {
			return false
		}
		if state[i] == 2 {
			return true
		}
		state[i] = 1
		for _, dependency := range result.Items[i].Requires {
			j, exists := ids[dependency]
			if !exists || !visit(j) {
				return false
			}
		}
		state[i] = 2
		return true
	}
	for i := range result.Items {
		if !visit(i) {
			return invalid()
		}
	}
	result.Templates = make(map[string][]byte)
	for _, item := range result.Items {
		if item.Config != nil {
			name := item.Config.ContentFile
			result.Templates[name] = bytes.Clone(templates[name])
		}
	}
	return &result, nil
}

// readValue retains strict JSON types and rejects duplicate keys at every object
// depth before encoding/json can silently replace an earlier value.
func readValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for d.More() {
			keyToken, e := d.Token()
			if e != nil {
				return nil, e
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, io.ErrUnexpectedEOF
			}
			if _, exists := object[key]; exists {
				return nil, io.ErrUnexpectedEOF
			}
			value, e := readValue(d)
			if e != nil {
				return nil, e
			}
			object[key] = value
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, io.ErrUnexpectedEOF
		}
		return object, nil
	case '[':
		values := []any{}
		for d.More() {
			value, e := readValue(d)
			if e != nil {
				return nil, e
			}
			values = append(values, value)
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return nil, io.ErrUnexpectedEOF
		}
		return values, nil
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

func object(value any, required, optional string) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	allowed := make(map[string]bool)
	for _, key := range strings.Fields(required) {
		if _, exists := result[key]; !exists {
			return nil, false
		}
		allowed[key] = true
	}
	for _, key := range strings.Fields(optional) {
		allowed[key] = true
	}
	for key := range result {
		if !allowed[key] {
			return nil, false
		}
	}
	return result, true
}

func stringValue(value any, pattern string, max int) bool {
	s, ok := value.(string)
	return ok && (max == 0 || utf8.RuneCountInString(s) <= max) && (pattern == "" || regexp.MustCompile(pattern).MatchString(s))
}
func oneOf(value any, options ...string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, option := range options {
		if s == option {
			return true
		}
	}
	return false
}
func boolean(value any) bool { _, ok := value.(bool); return ok }
func stringsValue(value any, min, max int, unique bool, valid func(any) bool) bool {
	values, ok := value.([]any)
	if !ok || len(values) < min || (max > 0 && len(values) > max) {
		return false
	}
	seen := make(map[string]bool)
	for _, v := range values {
		s, ok := v.(string)
		if !ok || !valid(s) || (unique && seen[s]) {
			return false
		}
		seen[s] = true
	}
	return true
}

const idPattern = `^[a-z][a-z0-9-]{1,31}$`

func validID(v any) bool { return stringValue(v, idPattern, 0) }

func validCatalogue(value any) bool {
	root, ok := object(value, "schema_version catalogue_version items", "")
	if !ok {
		return false
	}
	version, ok := root["schema_version"].(json.Number)
	if !ok || version != "1" {
		return false
	}
	if !stringValue(root["catalogue_version"], `^[0-9]{4}\.[0-9]{2}\.[0-9]+$`, 0) {
		return false
	}
	items, ok := root["items"].([]any)
	if !ok || len(items) < 1 || len(items) > 64 {
		return false
	}
	for _, item := range items {
		if !validItem(item) {
			return false
		}
	}
	return true
}

func validItem(value any) bool {
	item, ok := object(value, "id kind title platforms selected_by_default network tool", "requires privileged config")
	if !ok {
		return false
	}
	if !validID(item["id"]) || !oneOf(item["kind"], "runtime", "tool") || !stringValue(item["title"], "", 80) || !boolean(item["selected_by_default"]) {
		return false
	}
	if !stringsValue(item["platforms"], 1, 0, true, func(v any) bool { return oneOf(v, "darwin-arm64", "darwin-amd64") }) {
		return false
	}
	if v, exists := item["requires"]; exists && !stringsValue(v, 0, 0, true, validID) {
		return false
	}
	if !validNetwork(item["network"]) || !validTool(item["tool"]) {
		return false
	}
	if v, exists := item["config"]; exists && !validConfig(v) {
		return false
	}
	if v, exists := item["privileged"]; exists {
		steps, ok := v.([]any)
		if !ok || len(steps) > 4 {
			return false
		}
		for _, step := range steps {
			p, ok := object(step, "purpose argv", "")
			if !ok || !stringValue(p["purpose"], "", 120) || !stringsValue(p["argv"], 1, 8, false, func(v any) bool { return stringValue(v, `^[A-Za-z0-9._/=:+-]{1,128}$`, 0) }) {
				return false
			}
		}
	}
	return true
}

func validNetwork(value any) bool {
	network, ok := object(value, "hosts purpose", "lookup_urls")
	if !ok {
		return false
	}
	if !stringValue(network["purpose"], "", 120) || !stringsValue(network["hosts"], 1, 8, false, func(v any) bool { return stringValue(v, `^[a-z0-9][a-z0-9.-]{0,252}$`, 0) }) {
		return false
	}
	if v, exists := network["lookup_urls"]; exists && !stringsValue(v, 0, 8, false, func(v any) bool {
		return stringValue(v, `^https://[a-z0-9][a-z0-9.-]{0,252}/[A-Za-z0-9._~/%+-]{0,256}$`, 0)
	}) {
		return false
	}
	return true
}
func validTool(value any) bool {
	tool, ok := object(value, "command version_args version_regex min_version source", "")
	if !ok {
		return false
	}
	if !stringValue(tool["command"], `^[A-Za-z0-9._+-]{1,64}$`, 0) || !stringValue(tool["version_regex"], "", 128) || !stringValue(tool["min_version"], `^[0-9]+(\.[0-9]+){0,3}$`, 0) || !stringsValue(tool["version_args"], 1, 4, false, func(v any) bool { return stringValue(v, `^[A-Za-z0-9._=-]{1,32}$`, 0) }) {
		return false
	}
	source, ok := object(tool["source"], "manager type name tap may_elevate", "")
	return ok && oneOf(source["manager"], "homebrew") && oneOf(source["type"], "formula", "cask") && stringValue(source["name"], `^[a-z0-9][a-z0-9@._+-]{0,63}$`, 0) && oneOf(source["tap"], "homebrew/core", "homebrew/cask") && boolean(source["may_elevate"])
}
func validConfig(value any) bool {
	config, ok := object(value, "marker target content_file sha256 mode", "")
	return ok && oneOf(config["marker"], "dotfile", "shell_hook") && stringValue(config["target"], `^\.config/[A-Za-z0-9._/-]{1,120}$`, 0) && stringValue(config["content_file"], `^[A-Za-z0-9._-]{1,64}$`, 0) && stringValue(config["sha256"], `^[0-9a-f]{64}$`, 0) && oneOf(config["mode"], "0644", "0600")
}

// This recognizes known agent program/package identities, including common
// formula-version suffixes. It is not a semantic audit of arbitrary template
// content or a substitute for the owner review required for every new entry.
func agentIdentity(value string) bool {
	value = strings.ToLower(value)
	value = strings.SplitN(value, "@", 2)[0]
	switch value {
	case "codex", "claude", "claude-code", "agy", "aider", "aider-chat", "gemini", "gemini-cli", "opencode", "cursor", "cursor-agent", "cline", "goose":
		return true
	default:
		return false
	}
}
