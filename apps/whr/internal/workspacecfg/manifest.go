// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// File names and limits (FR-055).
const (
	ManifestFile       = "workharbor.json"
	MaxManifestBytes   = 64 << 10
	MaxDevcontainerLen = 256 << 10
	MaxGitmodulesBytes = 64 << 10
)

// Project is the optional project declaration of a version 2 manifest.
type Project struct{ URL, Path string }

// Manifest holds the fields of a parsed manifest. A field that failed its check
// is left empty, so callers use only what is valid.
type Manifest struct {
	SchemaVersion    int
	DevcontainerPath string
	ProjectsPath     string
	Name             string
	StartDir         string
	HumanShell       string
	ConsoleMode      string
	Shortcuts        string
	Project          *Project
}

// ManifestResult is the outcome of CheckManifest. Manifest is nil when the
// content could not be read as a manifest object at all.
type ManifestResult struct {
	Manifest *Manifest
	Findings []Finding
}

var (
	v1Fields = map[string]bool{"schema_version": true, "devcontainer_path": true, "projects_path": true, "bundles": true}
	v2Fields = map[string]bool{
		"schema_version": true, "devcontainer_path": true, "projects_path": true, "bundles": true,
		"name": true, "start_dir": true, "human_shell": true, "console_mode": true, "shortcuts": true, "project": true,
	}
	humanShells  = map[string]bool{"fish": true, "bash": true, "zsh": true, "nu": true}
	consoleModes = map[string]bool{"herdr": true, "plain": true}
	shortcutVals = map[string]bool{"on": true, "off": true}
)

// CheckManifest validates the raw bytes of workharbor.json in memory: size,
// encoding, strict JSON (duplicate keys, depth, exact numbers) and the version 1
// and 2 schema with the path and URL rules. It reads no file.
func CheckManifest(raw []byte) ManifestResult {
	var c collector
	fail := func(code, msg string) ManifestResult {
		c.add(code, ManifestFile, msg, "")
		return ManifestResult{Findings: c.list}
	}
	if len(raw) > MaxManifestBytes {
		return fail("manifest.too_large", fmt.Sprintf("%s is larger than %d bytes", ManifestFile, MaxManifestBytes))
	}
	switch {
	case bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")):
		return fail("manifest.encoding", "the manifest begins with a byte-order mark")
	case bytes.IndexByte(raw, 0) >= 0:
		return fail("manifest.encoding", "the manifest contains a NUL byte")
	case !utf8.Valid(raw):
		return fail("manifest.encoding", "the manifest is not valid UTF-8")
	}
	doc, serr := parseStrict(raw, false)
	if serr != nil {
		code := "manifest.syntax"
		if serr.duplicate {
			code = "manifest.duplicate_key"
		} else if serr.encoding {
			code = "manifest.encoding"
		}
		return fail(code, serr.msg)
	}
	m := &Manifest{}
	res := ManifestResult{Manifest: m}
	schema := func(format string, args ...any) {
		c.add("manifest.schema", ManifestFile, fmt.Sprintf(format, args...), "")
	}
	if doc.kind != jObject {
		schema("the manifest must be a JSON object")
		res.Manifest = nil
		res.Findings = c.list
		return res
	}

	version := 0
	if v, ok := doc.get("schema_version"); !ok {
		schema("missing field schema_version")
	} else if v.kind == jNumber && (v.str == "1" || v.str == "2") {
		version = int(v.str[0] - '0')
		m.SchemaVersion = version
	} else {
		schema("schema_version must be the integer 1 or 2")
	}
	allowed := v2Fields
	switch version {
	case 1:
		allowed = v1Fields
	case 0:
		allowed = nil // an unusable version makes every other field name moot
	}
	for _, member := range doc.obj {
		if allowed == nil || allowed[member.key] {
			continue
		}
		if v2Fields[member.key] {
			schema("field %s needs schema_version 2", quoteKey(member.key))
		} else {
			schema("unknown field %s", quoteKey(member.key))
		}
	}
	for _, f := range []string{"devcontainer_path", "projects_path", "bundles"} {
		if _, ok := doc.get(f); !ok {
			schema("missing field %s", f)
		}
	}
	path := func(name string, allowDot bool) string {
		v, ok := doc.get(name)
		if !ok {
			return ""
		}
		if v.kind != jString {
			schema("%s must be a string", name)
			return ""
		}
		if allowDot && v.str == "." {
			return v.str
		}
		if why := canonicalPathProblem(v.str); why != "" {
			c.add("manifest.path_invalid", ManifestFile, fmt.Sprintf("%s %s", name, why), "")
			return ""
		}
		return v.str
	}
	m.DevcontainerPath = path("devcontainer_path", false)
	m.ProjectsPath = path("projects_path", false)
	if b, ok := doc.get("bundles"); ok {
		switch {
		case b.kind != jArray:
			schema("bundles must be a list")
		case len(b.arr) > 0:
			c.add("manifest.bundles_deferred", ManifestFile, "bundles must be empty; attachments are deferred", "")
		}
	}
	if version != 2 {
		res.Findings = c.list
		return res
	}
	m.StartDir = path("start_dir", true)
	str := func(name string) (string, bool) {
		v, ok := doc.get(name)
		if !ok {
			return "", false
		}
		if v.kind != jString {
			schema("%s must be a string", name)
			return "", false
		}
		return v.str, true
	}
	enum := func(name string, set map[string]bool) string {
		s, ok := str(name)
		if ok && !set[s] {
			schema("%s is not one of the allowed values", name)
			return ""
		}
		return s
	}
	if s, ok := str("name"); ok {
		if workspaceName.MatchString(s) {
			m.Name = s
		} else {
			schema("name must be 1-64 lowercase letters, digits or hyphens, without a leading or trailing hyphen")
		}
	}
	m.HumanShell = enum("human_shell", humanShells)
	m.ConsoleMode = enum("console_mode", consoleModes)
	m.Shortcuts = enum("shortcuts", shortcutVals)
	if p, ok := doc.get("project"); ok {
		m.Project = checkProject(&c, p, m.ProjectsPath)
	}
	res.Findings = c.list
	return res
}

// checkProject validates the project declaration; it returns it when valid.
func checkProject(c *collector, p jvalue, projectsPath string) *Project {
	schema := func(format string, args ...any) {
		c.add("manifest.schema", ManifestFile, fmt.Sprintf(format, args...), "")
	}
	if p.kind != jObject {
		schema("project must be an object")
		return nil
	}
	ok := true
	var url, path string
	for _, k := range []string{"url", "path"} {
		v, found := p.get(k)
		switch {
		case !found:
			schema("project is missing %s", k)
			ok = false
		case v.kind != jString:
			schema("project.%s must be a string", k)
			ok = false
		case k == "url":
			url = v.str
		default:
			path = v.str
		}
	}
	for _, m := range p.obj {
		if m.key != "url" && m.key != "path" {
			schema("unknown project field %s", quoteKey(m.key))
			ok = false
		}
	}
	if !ok {
		return nil
	}
	if urlHasCredentials(url) {
		c.add("manifest.url_credentials", ManifestFile, "project.url carries credentials", "")
		return nil
	}
	if why := projectURLProblem(url); why != "" {
		schema("project.url %s", why)
		return nil
	}
	name, nameOK := projectURLName(url)
	if !nameOK {
		schema("project.url does not end in a usable project name")
		return nil
	}
	if len(path) < 3 {
		schema("project.path is too short")
		return nil
	}
	if projectsPath != "" && path != projectsPath+"/"+name {
		schema("project.path must be projects_path/<name> with the name derived from the URL")
		return nil
	}
	return &Project{URL: url, Path: path}
}
