//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
	"golang.org/x/sys/unix"
)

// Ports stay internal: the production constructor always uses native endpoint,
// installed tools, and conservative read-only mount-capability admission.
type stagePreparationPorts struct {
	Endpoint       func(context.Context) (string, error)
	Tools          map[string]string
	StateRoot      string
	MountAdmission func(context.Context, stageDaemonInfo, inspection, string) error
}

type LocalStage struct {
	options                                          StageOptions
	ports                                            stagePreparationPorts
	root                                             string
	rootIdentity                                     os.FileInfo
	capture                                          *capturedStageFiles
	git                                              stageGitCapture
	runner                                           *stageRunner
	metadata                                         stageMetadata
	primary                                          inspection
	endpoint                                         stageEndpointIdentity
	config                                           map[string]any
	configRelative, workspace, home, homeDestination string
	operationID, clonePath, fingerprint              string
	homeIntent                                       CandidateHomeIntent
	invocation                                       stageCLIInvocation
	originalFacts                                    []byte
	originalFactsDigest                              string
	result                                           LifecycleResult
	applied, closed                                  bool
	store                                            *stageStore
	receipt                                          *stageReceipt
	primaryObserved                                  stageContainerDetails
	settings                                         stageSettings
	execution                                        stageExecutionBinding
	effectiveConfig                                  map[string]any
	sourceMetadata                                   map[string]any
	artifacts                                        map[string]string
}

func nativeStagePreparationPorts(c Client) (stagePreparationPorts, error) {
	stateBase := os.Getenv("XDG_STATE_HOME")
	if stateBase == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return stagePreparationPorts{}, stageConflict("private state home unavailable")
		}
		stateBase = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(stateBase) {
		return stagePreparationPorts{}, stageConflict("private state home must be absolute")
	}
	p := stagePreparationPorts{StateRoot: filepath.Join(stateBase, "workharbor", "local-stage", "v1"), Tools: map[string]string{}, Endpoint: func(ctx context.Context) (string, error) { return ResolveEndpoint(ctx, doctor.SystemPorts(nil)) }, MountAdmission: admitStageNativeMount}
	for _, name := range []string{"docker", "node", "devcontainer", "git", "sh", "bash"} {
		path := ""
		var e error
		if name == "docker" {
			path = c.Executable
		}
		if path == "" {
			path, e = exec.LookPath(name)
		}
		if e != nil {
			return p, stageConflict("installed " + name + " capability unavailable")
		}
		p.Tools[name] = path
	}
	return p, nil
}

func (c Client) prepareStage(parent context.Context, options StageOptions, ports stagePreparationPorts) (stage *LocalStage, err error) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if options.Source == "" || options.Target == "" {
		return nil, stageConflict("explicit source and primary required")
	}
	if ports.Endpoint == nil || ports.MountAdmission == nil || !filepath.IsAbs(ports.StateRoot) {
		return nil, stageConflict("preparation capability unavailable")
	}
	s := &LocalStage{options: options, ports: ports}
	defer func() {
		if err != nil {
			if e := s.Close(); e != nil {
				err = e
			}
		}
	}()
	s.root, err = os.MkdirTemp("", "whr-local-stage-")
	if err != nil {
		return nil, stageConflict("private capture storage unavailable")
	}
	if err = os.Chmod(s.root, 0700); err != nil {
		return nil, stageConflict("private capture storage mode")
	}
	s.rootIdentity, err = os.Lstat(s.root)
	if err != nil {
		return nil, stageConflict("private preparation identity unavailable")
	}
	phaseRoot := filepath.Join(s.root, "phase")
	if err = os.Mkdir(phaseRoot, 0700); err != nil {
		return nil, stageConflict("private phase storage unavailable")
	}
	endpoint, e := ports.Endpoint(ctx)
	if e != nil {
		return nil, e
	}
	s.endpoint, err = stageSocketIdentity(endpoint)
	if err != nil {
		return nil, err
	}
	endpoint = s.endpoint.Endpoint
	s.runner, err = newStageRunner(ctx, phaseRoot, s.endpoint.Endpoint, ports.Tools)
	if err != nil {
		return nil, err
	}
	if err = admitStageToolVersions(ctx, s.runner); err != nil {
		return nil, err
	}
	s.metadata, err = readStageMetadata(ctx, s.runner)
	if err != nil {
		return nil, err
	}
	s.endpoint.DaemonID = s.metadata.Daemon.ID
	s.primary, err = selectStagePrimary(s.metadata.Containers, options.Target)
	if err != nil {
		return nil, err
	}
	executionRoot := filepath.Join(s.root, "execution")
	if err = os.Mkdir(executionRoot, 0700); err != nil {
		return nil, stageConflict("private execution storage unavailable")
	}
	s.capture, err = captureStageFiles(ctx, options.Source, executionRoot)
	if err != nil {
		return nil, err
	}
	// Preserve localWorkspaceFolderBasename while keeping execution capture E
	// inside a separately owned private temporary root.
	capturePath := filepath.Join(executionRoot, filepath.Base(s.capture.Source))
	if err = os.Rename(s.capture.Path, capturePath); err != nil {
		return nil, stageConflict("execution capture routing unavailable")
	}
	s.capture.Path = capturePath
	s.workspace, s.home, s.homeDestination, err = bindStagePrimary(s.primary, s.capture.Source)
	if err != nil {
		return nil, err
	}
	if err = ports.MountAdmission(ctx, s.metadata.Daemon, s.primary, s.capture.Source); err != nil {
		return nil, err
	}
	for _, item := range s.metadata.Containers {
		if strings.TrimPrefix(item.Name, "/") == "whr-anchorage-staging" {
			return nil, stageConflict("candidate name already exists")
		}
	}
	configPath := s.primary.Labels["devcontainer.config_file"]
	if configPath == "" {
		configPath = filepath.Join(s.capture.Source, ".devcontainer", "devcontainer.json")
	}
	s.configRelative, err = filepath.Rel(s.capture.Source, configPath)
	if err != nil || s.configRelative == ".." || strings.HasPrefix(s.configRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(s.configRelative) {
		return nil, stageConflict("configuration outside admitted source")
	}
	configBytes, e := readStageInput(filepath.Join(s.capture.Path, s.configRelative), 256<<10)
	if e != nil {
		return nil, stageConflict("configuration input exceeds 256 KiB or is unavailable")
	}
	s.config, err = workspacecfg.DecodeDevcontainerData(configBytes)
	if err != nil {
		return nil, stageConflict("devcontainer data syntax unavailable")
	}
	if err = admitStageConfig(s.config, s.capture.Omitted); err != nil {
		return nil, err
	}
	if options.Smoke {
		if _, e = readStageInput(filepath.Join(s.capture.Path, ".devcontainer", "smoke-container.sh"), 256<<10); e != nil {
			return nil, stageConflict("smoke input exceeds 256 KiB or is unavailable")
		}
	}
	// Git construction is local and private even during preview. No persistent
	// clone or source ref is created before the explicit trust decision.
	previewRoot := filepath.Join(s.root, "preview-workspace")
	if err = os.Mkdir(previewRoot, 0700); err != nil {
		return nil, stageConflict("private preview clone storage unavailable")
	}
	previewClone := filepath.Join(previewRoot, filepath.Base(s.capture.Source))
	s.git, err = captureStageGit(ctx, s.capture.Source, s.capture.Path, previewClone)
	if err != nil {
		return nil, err
	}
	for _, repo := range s.git.Repositories {
		if err = os.Chmod(repo.ClonePath, 0700); err != nil {
			return nil, stageConflict("private clone root mode unavailable")
		}
	}
	var operation [16]byte
	if _, err = rand.Read(operation[:]); err != nil {
		return nil, stageConflict("operation entropy unavailable")
	}
	s.operationID = hex.EncodeToString(operation[:])
	s.homeIntent, err = NewCandidateHomeIntent(CandidateHomeScope{OperationID: s.operationID, DaemonID: s.endpoint.DaemonID, Endpoint: endpoint, PrimaryID: s.primary.ID, PrimaryHome: s.home, Destination: s.homeDestination})
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(s.endpoint.DaemonID + "\x00" + endpoint))
	s.clonePath = filepath.Join(ports.StateRoot, hex.EncodeToString(key[:]), s.operationID, "workspace", filepath.Base(s.capture.Source))
	factsPath := filepath.Join(s.capture.Source, ".devcontainer", "host.env")
	if _, e = os.Lstat(factsPath); e == nil {
		s.originalFacts, s.originalFactsDigest, err = readStageHostFacts(factsPath)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(e) {
		return nil, stageConflict("original host facts unavailable")
	}
	if stageAnchorageHostInitializer(s.config) && s.originalFacts == nil {
		return nil, stageConflict("Anchorage original host facts required")
	}
	if err = s.prepareCLIConfiguration(ctx); err != nil {
		return nil, err
	}
	s.fingerprint = stageDigest(map[string]any{"source": s.capture.Source, "files": s.capture.Digest, "repositories": stageTrustRepositories(s.git.Repositories), "tools": stageTrustTools(s.runner.Tools), "substitutions": stageTrustEnvironment(s.runner.Environment), "config": s.config, "original_host_facts": s.originalFactsDigest, "workspace": s.workspace, "primary_id": s.primary.ID, "endpoint": s.endpoint})
	// The generated driver is bound to allocation-specific resources separately;
	// its source-tool identities are already covered, so its random routing path
	// and bytes must never make the source trust fingerprint nondeterministic.
	s.result = LifecycleResult{Status: "preview", Phase: "planned", Fingerprint: &s.fingerprint, Primary: stageContainerResource(s.primary, s.endpoint.DaemonID), Candidate: &StageResource{Kind: "container", Name: stageString("whr-anchorage-staging"), DaemonID: s.endpoint.DaemonID}, Home: &StageResource{Kind: "volume", Name: stageString(s.homeIntent.Plan().Mount.Name), DaemonID: s.endpoint.DaemonID}, Effects: []StageEffect{}, Recovery: []StageRecoveryStep{}, Repositories: []StageRepository{}, ResultRefs: []StageResultRef{}}
	for _, repo := range s.git.Repositories {
		s.result.Repositories = append(s.result.Repositories, StageRepository{SourcePath: repo.Source, ClonePath: filepath.Join(s.clonePath, repo.RelativePath), CloneOwnerUID: os.Getuid(), CloneRootMode: 0700, ContainerWorkspacePath: filepath.Join(s.workspace, repo.RelativePath), HeadOID: repo.Head, ObjectFormat: repo.ObjectFormat, Dirty: repo.Dirty, UncommittedRetained: repo.UncommittedRetained})
	}
	s.disclose()
	return s, nil
}

func stageString(s string) *string { return &s }
func stageContainerResource(item inspection, daemon string) *StageResource {
	name := strings.TrimPrefix(item.Name, "/")
	return &StageResource{Kind: "container", Name: &name, ID: stageString(item.ID), DaemonID: daemon}
}
func stageTrustRepositories(repos []stageGitSnapshot) []map[string]any {
	out := []map[string]any{}
	for _, r := range repos {
		out = append(out, map[string]any{"source": r.Source, "binding_digest": r.BindingDigest, "head": r.Head, "object_manifest_digest": r.ObjectManifestDigest, "config_digest": r.ConfigDigest})
	}
	return out
}
func stageTrustEnvironment(env []string) map[string]string {
	out := map[string]string{}
	for _, v := range env {
		key, value, _ := strings.Cut(v, "=")
		if key == "PATH" {
			parts := strings.Split(value, string(os.PathListSeparator))
			if len(parts) > 0 && strings.HasSuffix(parts[0], "/driver") {
				value = strings.Join(parts[1:], string(os.PathListSeparator))
			}
		}
		switch key {
		case "HOME", "TMPDIR", "XDG_CACHE_HOME", "DOCKER_CONFIG":
			continue
		}
		out[key] = value
	}
	return out
}

func (s *LocalStage) Preview() LifecycleResult {
	b, _ := json.Marshal(s.result)
	var out LifecycleResult
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *LocalStage) Close() error {
	if s == nil || s.closed {
		return nil
	}
	if s.store != nil {
		defer func() { s.store.Close(); s.store = nil }()
	}
	if s.root != "" {
		info, e := os.Lstat(s.root)
		if e != nil || s.rootIdentity == nil || !os.SameFile(info, s.rootIdentity) || info.Mode()&os.ModeSymlink != 0 {
			return stageConflict("temporary preparation identity changed; retained for inspection")
		}
	}
	if s.capture != nil {
		if e := s.capture.Close(); e != nil {
			return e
		}
	}
	if s.root != "" {
		if e := removeStageTemporaryRoot(s.root); e != nil {
			return stageConflict("temporary preparation retained after cleanup failure")
		}
	}
	s.closed = true
	return nil
}
func removeStageTemporaryRoot(path string) error {
	root, e := os.OpenRoot(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	e = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		f, e := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if e != nil {
			return e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return e
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Getuid() {
			return stageConflict("temporary directory owner changed")
		}
		return f.Chmod(st.Mode().Perm() | 0700)
	})
	root.Close()
	if e != nil {
		return e
	}
	return os.RemoveAll(path)
}

func (s *LocalStage) disclose() {
	if s.options.Diagnostics == nil {
		return
	}
	fmt.Fprintln(s.options.Diagnostics, "Pause input writers for capture and every execution phase; hashes are cooperative drift checks. Trusted container hooks have read-write access to their previewed workspace.")
	fmt.Fprintf(s.options.Diagnostics, "verification policy: healthcheck when present; trusted stdin smoke requested=%t; otherwise staged_unverified.\n", s.options.Smoke)
	if _, ok := s.config["initializeCommand"]; ok {
		fmt.Fprintln(s.options.Diagnostics, "host initialization: captured source contribution; effects and generated host facts are part of this trust decision.")
	} else {
		fmt.Fprintln(s.options.Diagnostics, "host initialization: no source contribution.")
	}
	if _, ok := s.config["build"]; ok {
		fmt.Fprintln(s.options.Diagnostics, "build intent: captured Dockerfile/context and trusted runtime/network dependencies.")
	} else {
		fmt.Fprintln(s.options.Diagnostics, "build intent: configured image and official UID behavior.")
	}
	for _, key := range stageLifecycleHooks {
		if _, ok := s.config[key]; ok {
			fmt.Fprintf(s.options.Diagnostics, "container hook phase %s: source origin retained once; script contents remain private.\n", key)
		}
	}
	for _, path := range s.capture.Omitted {
		fmt.Fprintf(s.options.Diagnostics, "omitted optional data: %s\n", terminal.Escape(path))
	}
}

func readStageInput(path string, limit int64) ([]byte, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, stageConflict("required captured input unavailable")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, stageConflict("required captured input type or quota")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, stageConflict("required captured input read quota")
	}
	return b, nil
}
