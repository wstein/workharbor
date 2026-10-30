// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Resolver owns fresh configuration staging only. It never opens retained data.
// Credential paths are explicit host configuration, never repository input.
type Resolver struct {
	GitPath, StageRoot, CAFile, SSHPath, KnownHostsFile, IdentityFile string
	Parser                                                            JSONC
}
type Resolved struct {
	Root, DevcontainerPath, ProjectsPath string
	Descriptor                           Descriptor
	Source                               map[string]any
	Bases                                []string
	privateParent                        string
	frozen                               string
	sealed                               []byte
}

func (s *Resolved) Close() error {
	if s.privateParent == "" {
		return nil
	}
	e := os.RemoveAll(s.privateParent)
	if e == nil {
		s.privateParent = ""
	}
	return e
}

type treeEntry struct{ mode, hash, path string }

func (r Resolver) Resolve(ctx context.Context, d Descriptor) (result *Resolved, err error) {
	if _, e := DecodeDescriptor(d.Map()); e != nil {
		return nil, e
	}
	if !filepath.IsAbs(r.GitPath) || !filepath.IsAbs(r.StageRoot) {
		return nil, &Error{Code: "tool_unavailable"}
	}
	stageRoot, e := filepath.EvalSymlinks(r.StageRoot)
	info, statErr := os.Stat(stageRoot)
	if e != nil || statErr != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, &Error{Code: "staging_failed"}
	}
	stage, e := os.MkdirTemp(stageRoot, "config-")
	if e != nil {
		return nil, &Error{Code: "staging_failed"}
	}
	defer func() {
		if err != nil {
			os.RemoveAll(stage)
		}
	}()
	home := filepath.Join(stage, "home")
	root := filepath.Join(stage, "repository")
	if os.Mkdir(home, 0700) != nil || os.Mkdir(root, 0700) != nil {
		return nil, &Error{Code: "staging_failed"}
	}
	env := []string{"PATH=" + filepath.Dir(r.GitPath) + ":/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "LANG=C.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "SSH_ASKPASS=/bin/false", "GIT_ALLOW_PROTOCOL=https:ssh", "GIT_LFS_SKIP_SMUDGE=1"}
	flags := []string{"-c", "core.hooksPath=/dev/null", "-c", "init.templateDir=" + home, "-c", "credential.helper=", "-c", "core.attributesFile=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always"}
	if r.CAFile != "" {
		if !filepath.IsAbs(r.CAFile) {
			return nil, &Error{Code: "tool_unavailable"}
		}
		flags = append(flags, "-c", "http.sslCAInfo="+r.CAFile)
	}
	// No ambient SSH config, agent, askpass or credentials. Explicit host-approved key only.
	sshConfigured := r.SSHPath != "" || r.KnownHostsFile != "" || r.IdentityFile != ""
	if sshConfigured || !strings.HasPrefix(strings.ToLower(d.RepositoryIdentity), "https://") {
		if !filepath.IsAbs(r.SSHPath) || !filepath.IsAbs(r.KnownHostsFile) || !filepath.IsAbs(r.IdentityFile) {
			return nil, &Error{Code: "credential_unavailable"}
		}
		ssh := []string{r.SSHPath, "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "UserKnownHostsFile=" + r.KnownHostsFile, "-i", r.IdentityFile}
		for i := range ssh {
			ssh[i] = "'" + strings.ReplaceAll(ssh[i], "'", "'\\''") + "'"
		}
		flags = append(flags, "-c", "core.sshCommand="+strings.Join(ssh, " "))
	} else {
		flags = append(flags, "-c", "core.sshCommand=/bin/false")
	}
	git := func(dir string, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, r.GitPath, append(append([]string{}, flags...), args...)...)
		c.Dir = dir
		c.Env = env
		c.Stderr = io.Discard
		b, e := c.Output()
		if e != nil {
			return nil, &Error{Code: "revision_unavailable"}
		}
		return b, nil
	}
	fetch := func(dir, origin, commit string) error {
		if !strings.HasPrefix(strings.ToLower(origin), "https://") && !sshConfigured {
			return &Error{Code: "credential_unavailable"}
		}
		format := "sha1"
		if len(commit) == 64 {
			format = "sha256"
		}
		for _, args := range [][]string{{"init", "--quiet", "--object-format=" + format}, {"remote", "add", "origin", origin}, {"fetch", "--no-tags", "--no-recurse-submodules", "origin", commit}} {
			if _, e := git(dir, args...); e != nil {
				return e
			}
		}
		b, e := git(dir, "rev-parse", "--verify", commit+"^{commit}")
		if e != nil || strings.TrimSpace(string(b)) != commit {
			return &Error{Code: "revision_unavailable"}
		}
		return nil
	}
	if e := fetch(root, d.RepositoryIdentity, d.Commit); e != nil {
		return nil, e
	}
	entries, e := readTree(git, root, d.Commit)
	if e != nil {
		return nil, e
	}
	modules, e := readModules(git, root, d.Commit, entries)
	if e != nil {
		return nil, e
	}
	actual := make([]Submodule, 0, len(modules))
	for _, m := range modules {
		actual = append(actual, Submodule{m.path, m.commit})
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i].Path < actual[j].Path })
	if !reflect.DeepEqual(actual, d.Submodules) {
		return nil, invalidConfiguration()
	}
	if _, e := git(root, "checkout", "--quiet", "--detach", d.Commit); e != nil {
		return nil, e
	}
	for _, m := range modules {
		dir, e := ContainedPath(root, m.path, true)
		if e != nil {
			return nil, e
		}
		// Every submodule is initialized explicitly at its approved gitlink, never recursively.
		if e := fetch(dir, m.origin, m.commit); e != nil {
			return nil, e
		}
		child, e := readTree(git, dir, m.commit)
		if e != nil {
			return nil, e
		}
		nested, e := readModules(git, dir, m.commit, child)
		if e != nil || len(nested) != 0 {
			return nil, unsupportedConfiguration()
		}
		if _, e := git(dir, "checkout", "--quiet", "--detach", m.commit); e != nil {
			return nil, e
		}
	}
	manifestPath, e := ContainedPath(root, "workharbor.json", false)
	if e != nil {
		return nil, e
	}
	raw, e := os.ReadFile(manifestPath)
	if e != nil {
		return nil, invalidConfiguration()
	}
	dc, projects, e := decodeResolverManifest(raw)
	if e != nil {
		return nil, e
	}
	dcPath, e := ContainedPath(root, dc, false)
	if e != nil {
		return nil, e
	}
	projectsPath, e := ContainedPath(root, projects, true)
	if e != nil {
		return nil, e
	}
	raw, e = os.ReadFile(dcPath)
	if e != nil {
		return nil, invalidConfiguration()
	}
	v, e := r.Parser.Parse(ctx, raw, false)
	if e != nil {
		return nil, e
	}
	source, ok := v.(map[string]any)
	if !ok {
		return nil, invalidConfiguration()
	}
	if e := ValidateSource(source); e != nil {
		return nil, e
	}
	bases := []string{}
	if image, ok := source["image"].(string); ok {
		bases = append(bases, image)
	} else {
		build := source["build"].(map[string]any)
		file, e := relativeInput(root, filepath.Dir(dcPath), build["dockerfile"].(string), false)
		if e != nil {
			return nil, e
		}
		contextPath := "."
		if value, ok := build["context"].(string); ok {
			contextPath = value
		}
		if _, e := relativeInput(root, filepath.Dir(dcPath), contextPath, true); e != nil {
			return nil, e
		}
		raw, e := os.ReadFile(file)
		if e != nil {
			return nil, invalidConfiguration()
		}
		bases, e = ValidateDockerfile(raw)
		if e != nil {
			return nil, e
		}
	}
	digest, e := fingerprint(ctx, root)
	if e != nil {
		return nil, e
	}
	resolved := &Resolved{Root: root, DevcontainerPath: dcPath, ProjectsPath: projectsPath, Descriptor: d, Source: source, Bases: bases, privateParent: stage, frozen: digest}
	resolved.sealed = materializedFields(resolved)
	return resolved, nil
}
func relativeInput(root, base, path string, dir bool) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00\r\n\t") {
		return "", invalidConfiguration()
	}
	absolute := filepath.Clean(filepath.Join(base, path))
	rel, e := filepath.Rel(root, absolute)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", invalidConfiguration()
	}
	return ContainedPath(root, filepath.ToSlash(rel), dir)
}

// ContainedPath checks each actual component without following symlinks or Git entries.
// The root must be the resolver-owned stage or another trusted private directory.
func ContainedPath(root, path string, dir bool) (string, error) {
	if _, e := CanonicalPathWithDot(path, true); e != nil {
		return "", invalidConfiguration()
	}
	current := root
	parts := strings.Split(path, "/")
	if path == "." {
		parts = nil
	}
	for i, part := range parts {
		if strings.EqualFold(part, ".git") {
			return "", invalidConfiguration()
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", invalidConfiguration()
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", invalidConfiguration()
		}
	}
	info, e := os.Lstat(current)
	if e != nil || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || (!dir && !info.Mode().IsRegular()) {
		return "", invalidConfiguration()
	}
	return current, nil
}
func readTree(git func(string, ...string) ([]byte, error), dir, commit string) ([]treeEntry, error) {
	raw, e := git(dir, "ls-tree", "-rz", commit)
	if e != nil {
		return nil, e
	}
	entries := []treeEntry{}
	for _, line := range bytes.Split(raw, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		parts := bytes.SplitN(line, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return nil, invalidConfiguration()
		}
		header := strings.Fields(string(parts[0]))
		if len(header) != 3 {
			return nil, invalidConfiguration()
		}
		path := string(parts[1])
		if _, e := CanonicalPath(path); e != nil {
			return nil, invalidConfiguration()
		}
		for _, p := range strings.Split(path, "/") {
			if strings.EqualFold(p, ".git") {
				return nil, invalidConfiguration()
			}
		}
		entries = append(entries, treeEntry{header[0], header[2], path})
	}
	return entries, nil
}

type gitModule struct{ path, origin, commit string }

func readModules(git func(string, ...string) ([]byte, error), dir, commit string, entries []treeEntry) ([]gitModule, error) {
	links := map[string]string{}
	hasManifest := false
	for _, entry := range entries {
		if entry.mode == "160000" {
			links[entry.path] = entry.hash
		}
		if entry.path == ".gitmodules" {
			if entry.mode != "100644" && entry.mode != "100755" {
				return nil, invalidConfiguration()
			}
			hasManifest = true
		}
	}
	byName := map[string]map[string]string{}
	if hasManifest {
		raw, e := git(dir, "config", "--no-includes", "--blob", commit+":.gitmodules", "--null", "--list")
		if e != nil {
			return nil, invalidConfiguration()
		}
		for _, record := range bytes.Split(raw, []byte{0}) {
			if len(record) == 0 {
				continue
			}
			pair := bytes.SplitN(record, []byte{'\n'}, 2)
			if len(pair) != 2 {
				return nil, invalidConfiguration()
			}
			key := string(pair[0])
			last := strings.LastIndexByte(key, '.')
			if !strings.HasPrefix(key, "submodule.") || last <= len("submodule.") {
				return nil, unsupportedConfiguration()
			}
			name, field := key[len("submodule."):last], key[last+1:]
			if field != "path" && field != "url" {
				return nil, unsupportedConfiguration()
			}
			if byName[name] == nil {
				byName[name] = map[string]string{}
			}
			if _, exists := byName[name][field]; exists {
				return nil, invalidConfiguration()
			}
			byName[name][field] = string(pair[1])
		}
	}
	modules := []gitModule{}
	used := map[string]bool{}
	for _, m := range byName {
		path, origin := m["path"], m["url"]
		if _, e := CanonicalPath(path); e != nil || !validOrigin(origin) || links[path] == "" || used[path] {
			return nil, invalidConfiguration()
		}
		for previous := range used {
			if strings.HasPrefix(path, previous+"/") || strings.HasPrefix(previous, path+"/") {
				return nil, invalidConfiguration()
			}
		}
		used[path] = true
		modules = append(modules, gitModule{path, origin, links[path]})
	}
	if len(modules) != len(links) {
		return nil, invalidConfiguration()
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].path < modules[j].path })
	return modules, nil
}

// resolverManifestPaths returns only provider inputs. Session preferences are
// validated declarations; they never affect source, tools or resolved output.
func resolverManifestPaths(value any) (string, string, error) {
	manifest, ok := value.(map[string]any)
	if !ok {
		return "", "", invalidConfiguration()
	}
	required := []string{"schema_version", "devcontainer_path", "projects_path", "bundles"}
	switch manifest["schema_version"] {
	case json.Number("1"):
		if !fields(manifest, required) {
			return "", "", invalidConfiguration()
		}
	case json.Number("2"):
		if !fields(manifest, required, "name", "start_dir", "human_shell", "console_mode", "shortcuts", "project", "helper_images") {
			return "", "", invalidConfiguration()
		}
	default:
		return "", "", invalidConfiguration()
	}
	dc, e := resolverManifestPath(manifest["devcontainer_path"], false)
	if e != nil {
		return "", "", e
	}
	projects, e := resolverManifestPath(manifest["projects_path"], false)
	if e != nil {
		return "", "", e
	}
	bundles, ok := manifest["bundles"].([]any)
	if !ok {
		return "", "", invalidConfiguration()
	}
	if manifest["schema_version"] == json.Number("2") {
		if value, exists := manifest["name"]; exists {
			name, ok := value.(string)
			if !ok || !manifestWorkspaceName.MatchString(name) {
				return "", "", invalidConfiguration()
			}
		}
		if value, exists := manifest["start_dir"]; exists {
			if _, e := resolverManifestPath(value, true); e != nil {
				return "", "", e
			}
		}
		for name, allowed := range map[string][]string{"human_shell": {"fish", "bash", "zsh", "nu"}, "console_mode": {"herdr", "plain"}, "shortcuts": {"on", "off"}} {
			if value, exists := manifest[name]; exists {
				text, ok := value.(string)
				found := false
				for _, candidate := range allowed {
					found = found || text == candidate
				}
				if !ok || !found {
					return "", "", invalidConfiguration()
				}
			}
		}
		if value, exists := manifest["project"]; exists {
			if !resolverManifestProject(value, projects) {
				return "", "", invalidConfiguration()
			}
		}
		if value, exists := manifest["helper_images"]; exists && !resolverManifestHelperImages(value) {
			return "", "", invalidConfiguration()
		}
	}
	// A well-shaped nonempty list retains the existing provider capability error;
	// invalid manifests cannot mask their schema failure behind that capability.
	if len(bundles) != 0 {
		return "", "", unsupportedConfiguration()
	}
	return dc, projects, nil
}

// manifestHelperImage mirrors the whr digest-pinned helper reference: a
// lowercase repository path, an optional tag and a mandatory sha256 digest.
var manifestHelperImage = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?@sha256:[0-9a-f]{64}$`)

// resolverManifestHelperImages validates the closed helper_images object. It is
// a client-side declaration for whr; the provider never reads it.
func resolverManifestHelperImages(value any) bool {
	helpers, ok := value.(map[string]any)
	if !ok || len(helpers) > 1 {
		return false
	}
	if base, exists := helpers["base"]; exists {
		text, ok := base.(string)
		return ok && len(text) <= 512 && manifestHelperImage.MatchString(text)
	}
	return len(helpers) == 0
}

var manifestWorkspaceName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
var manifestProjectURL = regexp.MustCompile(`^(https://[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+|ssh://(?:[A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+)$`)
var manifestProjectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func resolverManifestProject(value any, projects string) bool {
	project, ok := value.(map[string]any)
	if !ok || !fields(project, []string{"url", "path"}) {
		return false
	}
	url, ok := project["url"].(string)
	if !ok || len(url) > 2048 || !manifestProjectURL.MatchString(url) || !validOrigin(url) {
		return false
	}
	// validOrigin is intentionally broader than manifest URLs. The manifest adds
	// an exact lower-case scheme, no credentials, strict port/path and name rules.
	tail := url[strings.Index(url, "://")+3:]
	slash := strings.IndexByte(tail, '/')
	authority, rest := tail[:slash], tail[slash:]
	if strings.Contains(rest, "//") {
		return false
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		if !strings.HasPrefix(url, "ssh://") || at == 0 || strings.HasPrefix(authority, "-") {
			return false
		}
		authority = authority[at+1:]
	}
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		port, e := strconv.Atoi(authority[colon+1:])
		if e != nil || port < 1 || port > 65535 {
			return false
		}
		authority = authority[:colon]
	}
	if !hostName.MatchString(authority) {
		return false
	}
	name := strings.TrimSuffix(rest[strings.LastIndexByte(rest, '/')+1:], ".git")
	if !manifestProjectName.MatchString(name) {
		return false
	}
	path, e := resolverManifestPath(project["path"], false)
	return e == nil && utf8.RuneCountInString(path) >= 3 && path == projects+"/"+name
}
func resolverManifestPath(value any, allowDot bool) (string, error) {
	path, e := CanonicalPathWithDot(value, allowDot)
	if e != nil {
		return "", invalidConfiguration()
	}
	if allowDot && path == "." {
		return path, nil
	}
	for _, part := range strings.Split(path, "/") {
		// Same Windows aliases as the whr/Python manifest contract, without changing
		// CanonicalPath semantics for the provider's other accepted inputs.
		folded := strings.Map(func(r rune) rune {
			switch {
			case r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
				return -1
			case r == 0xff0e:
				return '.'
			case r == 0xa0 || r == 0x3000:
				return ' '
			}
			return r
		}, part)
		if strings.Trim(folded, ". ") == "" {
			return "", invalidConfiguration()
		}
		base, _, _ := strings.Cut(folded, ":")
		base = strings.TrimRight(base, ". ")
		if strings.EqualFold(base, ".git") || strings.EqualFold(base, "git~1") {
			return "", invalidConfiguration()
		}
	}
	return path, nil
}

// decodeResolverManifest preserves exact JSON number lexemes and validates the
// shared manifest byte/depth limits before any external parser or runtime tool.
func decodeResolverManifest(raw []byte) (string, string, error) {
	if len(raw) > 65536 || !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) || bytes.IndexByte(raw, 0) >= 0 || !resolverManifestScalars(raw) {
		return "", "", invalidConfiguration()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, e := resolverManifestJSON(decoder, 0)
	if e != nil {
		return "", "", e
	}
	if _, e = decoder.Token(); e != io.EOF {
		return "", "", invalidConfiguration()
	}
	return resolverManifestPaths(value)
}
func resolverManifestJSON(decoder *json.Decoder, depth int) (any, error) {
	token, e := decoder.Token()
	if e != nil {
		return nil, invalidConfiguration()
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	if depth >= 128 {
		return nil, invalidConfiguration()
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			key, e := decoder.Token()
			if e != nil {
				return nil, invalidConfiguration()
			}
			name, ok := key.(string)
			if !ok {
				return nil, invalidConfiguration()
			}
			if _, duplicate := object[name]; duplicate {
				return nil, invalidConfiguration()
			}
			value, e := resolverManifestJSON(decoder, depth+1)
			if e != nil {
				return nil, e
			}
			object[name] = value
		}
		end, e := decoder.Token()
		if e != nil || end != json.Delim('}') {
			return nil, invalidConfiguration()
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			value, e := resolverManifestJSON(decoder, depth+1)
			if e != nil {
				return nil, e
			}
			array = append(array, value)
		}
		end, e := decoder.Token()
		if e != nil || end != json.Delim(']') {
			return nil, invalidConfiguration()
		}
		return array, nil
	}
	return nil, invalidConfiguration()
}

// encoding/json replaces lone escaped UTF-16 surrogates. Reject them in the
// original strings instead; a literal U+FFFD and a valid surrogate pair remain valid.
func resolverManifestScalars(raw []byte) bool {
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if e != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
