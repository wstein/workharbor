// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const defaultDevcontainer = ".devcontainer/devcontainer.json"

type manifest struct {
	SchemaVersion    int      `json:"schema_version"`
	DevcontainerPath string   `json:"devcontainer_path"`
	ProjectsPath     string   `json:"projects_path"`
	Bundles          []string `json:"bundles"`
	Name             string   `json:"name,omitempty"`
	StartDir         string   `json:"start_dir"`
	HumanShell       string   `json:"human_shell"`
	ConsoleMode      string   `json:"console_mode"`
	Shortcuts        string   `json:"shortcuts"`
	Project          *project `json:"project,omitempty"`
	HelperImages     *helpers `json:"helper_images,omitempty"`
}
type helpers struct {
	Base string `json:"base"`
}
type project struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}
type Setting struct {
	Value  any    `json:"value"`
	Origin string `json:"origin"`
}

// Fingerprint binds the proposed action to its observed input, not to a later
// path lookup. The apply stage must recheck identity AND bytes before mutation.
type Fingerprint struct {
	Exists       bool
	Kind, SHA256 string
	Mode         fs.FileMode
	Info         fs.FileInfo
}
type Proposal struct {
	files   map[string][]byte
	inputs  map[string]Fingerprint
	preview Preview
}

func relativePath(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "devcontainer_path": value, "projects_path": "projects", "bundles": []string{}})
	return len(workspacecfg.CheckManifest(raw).Findings) == 0
}
func digest(data []byte) string    { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func copyBytes(data []byte) []byte { return append([]byte(nil), data...) }
func snapshotCopy(s Snapshot) Snapshot {
	out := s
	out.Entries = map[string]Entry{}
	for k, e := range s.Entries {
		e.Data = copyBytes(e.Data)
		out.Entries[k] = e
	}
	return out
}
func (p *Proposal) Files() map[string][]byte {
	out := map[string][]byte{}
	for k, v := range p.files {
		out[k] = copyBytes(v)
	}
	return out
}
func (p *Proposal) Fingerprints() map[string]Fingerprint {
	out := map[string]Fingerprint{}
	for k, v := range p.inputs {
		out[k] = v
	}
	return out
}

// Build constructs a complete, validated proposal. It performs no filesystem,
// process, Git or network operations and never applies the proposal.
func Build(snapshot Snapshot, o Options) (*Proposal, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	s := snapshotCopy(snapshot)
	for name := range s.Entries {
		if strings.HasPrefix(path.Base(name), ".whr-init-tmp-") {
			return nil, exitcode.Errorf(exitcode.PathConflict, "leftover .whr-init-tmp- file must be recovered before init")
		}
	}
	p := &Proposal{files: map[string][]byte{}, inputs: map[string]Fingerprint{}}
	p.preview = Preview{AgentsBlockWarnings: []AgentsBlockWarning{}, Directory: s.Directory, DryRun: o.DryRun, Actions: []Action{}, RuntimeEnforcement: "not_in_this_release", GitChecks: "not_run", BundleSelectionUnavailable: true, Settings: map[string]Setting{}, NextSteps: []string{"Run git init -b main, pin declared projects as submodules, and commit configuration before requesting creation trust.", "Run whr validate after Git initialization; Git checks have not run."}}
	p.preview.NextSteps = append(p.preview.NextSteps, UnsupportedStarters())
	p.inputs["."] = Fingerprint{Exists: true, Kind: "directory", Info: s.RootInfo}
	if s.RootInfo != nil {
		root := p.inputs["."]
		root.Mode = s.RootInfo.Mode()
		p.inputs["."] = root
	}
	doc := manifest{SchemaVersion: 2, DevcontainerPath: defaultDevcontainer, ProjectsPath: "projects", Bundles: []string{}, StartDir: ".", HumanShell: "fish", ConsoleMode: workspacecfg.DefaultConsoleMode, Shortcuts: "on"}
	p.preview.Settings["console_mode"] = Setting{doc.ConsoleMode, "default"}
	old, exists := s.Entries["workharbor.json"]
	if exists {
		if old.Kind != "file" {
			return nil, exitcode.Errorf(exitcode.PathConflict, "workharbor.json must be a regular file")
		}
		checked := workspacecfg.CheckManifest(old.Data)
		if checked.Manifest == nil || len(checked.Findings) != 0 {
			return nil, exitcode.Errorf(exitcode.PathConflict, "existing workharbor.json fails current validation")
		}
		m := checked.Manifest
		doc.DevcontainerPath = m.DevcontainerPath
		doc.ProjectsPath = m.ProjectsPath
		for key, value := range map[string]string{"name": m.Name, "start_dir": m.StartDir, "human_shell": m.HumanShell, "console_mode": m.ConsoleMode, "shortcuts": m.Shortcuts} {
			if value != "" {
				p.preview.Settings[key] = Setting{value, "existing"}
			}
		}
		if m.Name != "" {
			doc.Name = m.Name
		}
		if m.StartDir != "" {
			doc.StartDir = m.StartDir
		}
		if m.HumanShell != "" {
			doc.HumanShell = m.HumanShell
		}
		doc.ConsoleMode = m.EffectiveConsoleMode()
		if m.Shortcuts != "" {
			doc.Shortcuts = m.Shortcuts
		}
		if m.Project != nil {
			doc.Project = &project{m.Project.URL, m.Project.Path}
			p.preview.Settings["project"] = Setting{*doc.Project, "existing"}
		}
		if m.HelperBaseImage != "" {
			doc.HelperImages = &helpers{m.HelperBaseImage}
			p.preview.Settings["helper_images"] = Setting{map[string]string{"base": m.HelperBaseImage}, "existing"}
		}
	}
	selectionChanged := o.Devcontainer != nil && *o.Devcontainer != doc.DevcontainerPath
	if o.Devcontainer != nil {
		doc.DevcontainerPath = *o.Devcontainer
	}
	flags := map[string]*string{"name": o.Name, "start_dir": o.StartDir, "human_shell": o.HumanShell, "console_mode": o.ConsoleMode, "shortcuts": o.Shortcuts}
	values := map[string]*string{"name": &doc.Name, "start_dir": &doc.StartDir, "human_shell": &doc.HumanShell, "console_mode": &doc.ConsoleMode, "shortcuts": &doc.Shortcuts}
	for key, flag := range flags {
		if flag != nil {
			*values[key] = *flag
			p.preview.Settings[key] = Setting{*flag, "flag"}
		}
	}
	if o.ProjectURL != nil {
		doc.Project = &project{*o.ProjectURL, doc.ProjectsPath + "/" + projectName(*o.ProjectURL)}
		p.preview.Settings["project"] = Setting{*doc.Project, "flag"}
	}
	if o.HelperBaseImage != nil {
		doc.HelperImages = &helpers{*o.HelperBaseImage}
		p.preview.Settings["helper_images"] = Setting{map[string]string{"base": *o.HelperBaseImage}, "flag"}
		p.preview.NextSteps = append(p.preview.NextSteps, "Helper base image "+*o.HelperBaseImage+" is pinned for whr workspace home clone and backup; init does not pull or run it.")
	}
	fileRoles := map[string]bool{}
	for _, name := range []string{"workharbor.json", doc.DevcontainerPath, doc.ProjectsPath + "/.gitkeep", claudeSettingsPath} {
		if fileRoles[name] {
			return nil, exitcode.Errorf(exitcode.PathConflict, "init configuration has conflicting file roles")
		}
		fileRoles[name] = true
	}
	if !o.NoAgentsBlock {
		if fileRoles["AGENTS.md"] {
			return nil, exitcode.Errorf(exitcode.PathConflict, "AGENTS.md has conflicting init roles")
		}
		fileRoles["AGENTS.md"] = true
	}
	for name := range fileRoles {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if fileRoles[dir] {
				return nil, exitcode.Errorf(exitcode.PathConflict, "init configuration has conflicting file and directory roles")
			}
		}
	}
	var chosen []byte
	if exists && !o.requestsEdit() && !selectionChanged {
		chosen = old.Data
	} else {
		chosen, _ = json.MarshalIndent(doc, "", "  ")
		chosen = append(chosen, '\n')
		if checked := workspacecfg.CheckManifest(chosen); len(checked.Findings) != 0 {
			if !o.requestsEdit() && !selectionChanged {
				return nil, exitcode.Errorf(exitcode.PathUnclassified, "generated proposal failed self-validation")
			}
			return nil, exitcode.Errorf(exitcode.PathBadArguments, "init settings fail current manifest validation; check --name, --project-url, --start-dir, --human-shell, --console-mode, --shortcuts and --helper-base-image")
		}
		for key, value := range values {
			if *value != "" {
				if _, ok := p.preview.Settings[key]; !ok {
					p.preview.Settings[key] = Setting{*value, "default"}
				}
			}
		}
	}
	dc, ok := s.Entries[doc.DevcontainerPath]
	if !ok {
		return nil, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "init requires the existing devcontainer file %s", doc.DevcontainerPath)
	}
	if dc.Kind != "file" {
		return nil, exitcode.Errorf(exitcode.PathConflict, "the selected devcontainer must be a regular file")
	}
	if err := p.directories(s, path.Dir(doc.DevcontainerPath), true); err != nil {
		return nil, err
	}
	p.files[doc.DevcontainerPath] = copyBytes(dc.Data)
	p.observe(s, doc.DevcontainerPath)
	p.preview.Actions = append(p.preview.Actions, Action{Action: "reuse", Kind: "file", Path: doc.DevcontainerPath})
	if err := p.directories(s, doc.ProjectsPath, false); err != nil {
		return nil, err
	}
	if err := p.file(s, o, "workharbor.json", chosen); err != nil {
		return nil, err
	}
	if err := p.file(s, o, doc.ProjectsPath+"/.gitkeep", []byte{}); err != nil {
		return nil, err
	}
	if !o.NoAgentsBlock {
		if err := p.agentsFile(s, o); err != nil {
			return nil, err
		}
	}
	if err := p.claudeSettingsFile(s, o); err != nil {
		return nil, err
	}
	p.preview.Validation = workspacecfg.ValidateMemory(s.Directory, workspacecfg.MemoryInput{Manifest: p.files["workharbor.json"], Devcontainer: p.files[doc.DevcontainerPath], DevcontainerPresent: true, ProjectsPresent: true})
	if !p.preview.Validation.Valid {
		return nil, exitcode.Errorf(exitcode.PathConflict, "preserved configuration fails Git-free validation")
	}
	if workspacecfg.CheckManifest(chosen).Manifest.SchemaVersion == 2 {
		p.preview.NextSteps = append(p.preview.NextSteps, "schema_version 2 is not creation-ready until the separately reviewed resolver task T035 is implemented.")
	}
	if len(p.preview.Actions) > 32 {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "init proposal exceeds the current 32-action contract")
	}
	return p, nil
}
func (p *Proposal) observe(s Snapshot, name string) {
	e, ok := s.Entries[name]
	sum := e.Digest
	if sum == "" {
		sum = digest(e.Data)
	}
	p.inputs[name] = Fingerprint{Exists: ok, Kind: e.Kind, SHA256: sum, Mode: e.Mode, Info: e.Info}
}
func (p *Proposal) directories(s Snapshot, name string, requireExisting bool) error {
	if name == "." {
		return nil
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		rel := strings.Join(parts[:i+1], "/")
		if _, done := p.inputs[rel]; done {
			continue
		}
		e, ok := s.Entries[rel]
		if ok && e.Kind != "directory" {
			return exitcode.Errorf(exitcode.PathConflict, "init parent %s is not a real directory", rel)
		}
		if !ok && requireExisting {
			return exitcode.Errorf(exitcode.PathConflict, "the devcontainer parent directory is absent from the snapshot")
		}
		action := "create"
		if ok {
			action = "reuse"
		}
		p.observe(s, rel)
		p.preview.Actions = append(p.preview.Actions, Action{Action: action, Kind: "directory", Path: rel})
	}
	return nil
}
func (p *Proposal) file(s Snapshot, o Options, name string, data []byte) error {
	if !relativePath(name) {
		return exitcode.Errorf(exitcode.PathBadArguments, "unsafe init proposal path")
	}
	e, ok := s.Entries[name]
	action := "create"
	if ok {
		if e.Kind != "file" {
			return exitcode.Errorf(exitcode.PathConflict, "init target %s is not a regular file", name)
		}
		action = "skip_existing"
		if !bytes.Equal(e.Data, data) {
			if !o.permits(name) {
				return exitcode.Errorf(exitcode.PathHumanDecisionMissing, "editing %s requires --overwrite %s", name, name)
			}
			action = "overwrite"
		}
	}
	p.files[name] = copyBytes(data)
	p.observe(s, name)
	diff := string(data)
	oldPrefix := false
	if action == "overwrite" {
		diff = wholeDiff(name, e.Data, data)
		// Only a bounded prefix of a large old file is read for the preview.
		oldPrefix = e.Info != nil && e.Info.Size() > int64(len(e.Data))
	}
	if action == "skip_existing" {
		diff = ""
	}
	shown, truncated := boundedDiff(diff)
	p.preview.Actions = append(p.preview.Actions, Action{Action: action, Kind: "file", Path: name, Diff: shown, DiffTruncated: truncated || oldPrefix})
	return nil
}
