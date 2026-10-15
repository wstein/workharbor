// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
)

// Provider contains trusted host setup only. It is not a wire command or service.
// Admission carries the separately authenticated operation/approval/fence checker.
type Provider struct {
	Resolver                                                    config.Resolver
	Tools                                                       devcontainer.Tools
	Registry                                                    Registry
	Store                                                       storage.Store
	DockerPath, Socket, BuildxPath, AgentPath, PrivateDirectory string
}
type Prepared struct {
	closeOnce                      sync.Once
	closeErr                       error
	closed                         bool
	mu                             sync.Mutex
	source                         *config.Resolved
	admission                      Admission
	storage                        storage.Handle
	port                           *nativePort
	tools                          devcontainer.Tools
	agent, root                    string
	invocation                     Invocation
	metadata                       []map[string]any
	effective                      map[string]any
	resourcesStarted, hooksStarted bool
	resources                      *Target
}

func (p *Prepared) Check(ctx context.Context) error {
	if p == nil {
		return fail("forbidden")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.source == nil {
		return fail("forbidden")
	}
	if e := p.admission.Check(ctx); e != nil {
		return e
	}
	return p.source.CheckContext(ctx)
}
func (p *Prepared) Storage() storage.Handle {
	h := p.storage
	h.Binding.Initial, _ = config.DecodeDescriptor(h.Binding.Initial.Map())
	return h
}
func (p *Prepared) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		if p.port != nil {
			p.closeErr = p.port.Close()
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.source != nil {
			if e := p.source.Close(); p.closeErr == nil {
				p.closeErr = e
			}
		}
		if p.root != "" {
			if !ownedDirectory(p.root) {
				if p.closeErr == nil {
					p.closeErr = fail("provider_unavailable")
				}
				return
			}
			if e := os.RemoveAll(p.root); p.closeErr == nil {
				p.closeErr = e
			}
			p.root = ""
		}
	})
	return p.closeErr
}
func contextInput(s *config.Resolved, path string, dir bool) (string, error) {
	absolute := filepath.Join(filepath.Dir(s.DevcontainerPath), path)
	rel, e := filepath.Rel(s.Root, absolute)
	if e != nil {
		return "", fail("invalid_configuration")
	}
	return config.ContainedPath(s.Root, filepath.ToSlash(rel), dir)
}
func folderImage(workspace string) string {
	sum := sha256.Sum256([]byte(workspace))
	raw := strings.ToLower("vsc-" + filepath.Base(workspace) + "-" + hex.EncodeToString(sum[:]))
	filtered := []byte{}
	for _, b := range []byte(raw) {
		if b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-' {
			filtered = append(filtered, b)
		}
	}
	out := []byte{}
	for i := 0; i < len(filtered); {
		b := filtered[i]
		out = append(out, b)
		i++
		if b != '.' && b != '_' && b != '-' {
			continue
		}
		limit := 1
		if b == '_' {
			limit = 2
		}
		if b == '-' {
			limit = len(filtered)
		}
		kept := 1
		for i < len(filtered) && filtered[i] == b && kept < limit {
			out = append(out, b)
			i++
			kept++
		}
		for i < len(filtered) && (filtered[i] == '.' || filtered[i] == '_' || filtered[i] == '-') {
			i++
		}
	}
	return string(out)
}
func imageSnapshot(i AdmittedImage) map[string]any {
	return map[string]any{"Id": i.ID, "Architecture": i.Document["architecture"], "Os": i.Document["os"], "Config": clone(i.Document["config"]), "RootFS": clone(i.Document["rootfs"])}
}
func envValues(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, v := range m {
			out[k] = v.(string)
		}
	}
	return out
}

// Prepare resolves approved immutable inputs, completes all capability/trust gates,
// then reconciles/publishes only initial project storage. No Docker mutation or
// hook is issued. A valid Inspect observation ALWAYS passes Seed's fsync path.
func (host Provider) Prepare(ctx context.Context, a Admission) (prepared *Prepared, err error) {
	if e := a.Check(ctx); e != nil {
		return nil, e
	}
	if _, ok := ctx.Deadline(); !ok || !ownedDirectory(host.PrivateDirectory) || !physicalExecutable(host.AgentPath) {
		return nil, fail("provider_unavailable")
	}
	if e := host.Tools.Verify(ctx, true); e != nil {
		return nil, e
	}
	source, e := host.Resolver.Resolve(ctx, a.Descriptor())
	if e != nil {
		return nil, e
	}
	p := &Prepared{source: source, admission: a, tools: host.Tools, agent: host.AgentPath}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	p.root, e = registryPrivate(host.PrivateDirectory)
	if e != nil {
		return nil, e
	}
	for _, name := range []string{"tmp", "cli"} {
		if os.Mkdir(filepath.Join(p.root, name), 0700) != nil {
			return nil, fail("staging_failed")
		}
	}
	if e = p.Check(ctx); e != nil {
		return nil, e
	}
	images := map[string]AdmittedImage{}
	registry := host.Registry
	registry.Parser = host.Resolver.Parser
	for _, ref := range source.Bases {
		image, e := registry.Inspect(ctx, ref)
		if e != nil {
			return nil, e
		}
		images[ref] = image
	}
	selected := ""
	if ref, ok := source.Source["image"].(string); ok {
		selected = ref
	}
	var original []byte
	var plan config.DockerfilePlan
	var build *BuildPolicy
	target := ""
	if selected == "" {
		b := source.Source["build"].(map[string]any)
		file, e := contextInput(source, b["dockerfile"].(string), false)
		if e != nil {
			return nil, e
		}
		original, e = os.ReadFile(file)
		if e != nil {
			return nil, e
		}
		target, _ = b["target"].(string)
		plan, e = config.AnalyzeDockerfile(original, target)
		if e != nil {
			return nil, e
		}
		selected = plan.SelectedBase
		contextPath := "."
		if value, ok := b["context"].(string); ok {
			contextPath = value
		}
		directory, e := contextInput(source, contextPath, true)
		if e != nil {
			return nil, e
		}
		args := envValues(b["args"])
		args["BUILDKIT_INLINE_CACHE"] = "1"
		args["_DEV_CONTAINERS_BASE_IMAGE"] = plan.Stage
		if target != "" {
			args["_DEV_CONTAINERS_BASE_IMAGE"] = target
		}
		build = &BuildPolicy{Context: directory, Scratch: filepath.Join(p.root, "tmp"), Tag: folderImage(source.ProjectsPath), Target: "dev_containers_target_stage", Args: args}
	}
	base, ok := images[selected]
	if !ok {
		return nil, fail("configuration_changed")
	}
	inherited, e := ValidateImage(ctx, host.Resolver.Parser, base.Config)
	if e != nil {
		return nil, e
	}
	runtimeMetadata, metadata, e := ExpectedMetadata(source.Source, inherited)
	if e != nil {
		return nil, e
	}
	p.metadata = inherited
	binding := a.Binding()
	existing, found, e := host.Store.Inspect(ctx, binding, p.Check)
	if e != nil {
		return nil, e
	}
	data := filepath.Join(host.Store.Root, binding.StorageID, "data")
	policy := DriverPolicy{Phase: Resources, Scope: a.Scope(), DataPath: data, KnownIDs: map[string]bool{}, ImageRefs: map[string]bool{}, RuntimeMetadata: runtimeMetadata, StartupSHA256: "1a63da31389392b4e5ae4276c29a2c9a81b466739351488bef177c13fbcd7813", Build: build}
	for ref := range images {
		policy.ImageRefs[ref] = true
	}
	checkBuild := func(ctx context.Context, call BuildCall) error {
		if e := p.Check(ctx); e != nil {
			return e
		}
		raw, e := os.ReadFile(call.Dockerfile)
		if e != nil {
			return fail("configuration_changed")
		}
		return ValidateGeneratedDockerfile(ctx, host.Resolver.Parser, original, raw, target, metadata)
	}
	p.port, e = openNativePort(ctx, nativeOptions{Scope: a.Scope(), Policy: policy, Images: images, Registry: &registry, Parser: host.Resolver.Parser, CheckBuild: checkBuild, Check: p.Check, DockerPath: host.DockerPath, Socket: host.Socket, BuildxPath: host.BuildxPath, PrivateDirectory: p.root})
	if e != nil {
		return nil, e
	}
	info, e := p.port.Information(ctx)
	if e != nil {
		return nil, e
	}
	all, e := p.port.Candidates(ctx, a.Scope())
	if e != nil {
		return nil, e
	}
	if len(all) != 0 {
		return nil, fail("runtime_conflict")
	}
	if !supportedEngine(info) {
		return nil, fail("unsupported_configuration")
	}
	// Validate the explicit source pairs before any seed, then compare their
	// effective CLI values and immutable INITIAL data owner below.
	var initial *User
	if found {
		initial = &User{UID: existing.Binding.DataUID, GID: existing.Binding.DataGID}
	}
	user, e := AdmitUsers(source.Source, source.Source, info, initial)
	if e != nil {
		return nil, e
	}
	if e = storage.CheckInitialOwner(user.UID, user.GID); e != nil {
		return nil, e
	}
	p.invocation = Invocation{Phase: Preflight, Scope: a.Scope(), Workspace: source.ProjectsPath, Config: source.DevcontainerPath, Override: filepath.Join(p.root, "override.json"), UserData: filepath.Join(p.root, "cli"), DataPath: data}
	override, e := FullOverride(source.Source, a.Scope(), data)
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(override)
	if e = os.WriteFile(p.invocation.Override, raw, 0600); e != nil {
		return nil, e
	}
	snapshot := PreflightDriver{Scope: a.Scope(), EngineVersion: info.ServerVersion, BuildxVersion: "v0.38.0", CandidateAbsent: true, Images: map[string]map[string]any{}}
	for ref, image := range images {
		snapshot.Images[ref] = imageSnapshot(image)
	}
	driver, e := startDriver(ctx, p.root, p.Check, func(ctx context.Context, args []string, _ io.Reader, out io.Writer) error {
		raw, e := snapshot.Reply(args)
		if e != nil {
			return e
		}
		_, e = out.Write(raw)
		return e
	})
	if e != nil {
		return nil, e
	}
	p.invocation.Driver, e = driver.Executable(ctx, host.AgentPath)
	if e != nil {
		driver.Close()
		return nil, e
	}
	effective, e := readConfiguration(ctx, host.Tools, p.invocation, p.Check)
	closeError := driver.Close()
	if e != nil {
		return nil, e
	}
	if closeError != nil {
		return nil, closeError
	}
	if e = ValidateEffective(effective, source.Source, inherited, p.invocation); e != nil {
		return nil, e
	}
	p.effective = effective
	merged := effective["mergedConfiguration"].(map[string]any)
	user, e = AdmitUsers(source.Source, merged, info, initial)
	if e != nil {
		return nil, e
	}
	policy.ContainerUser = user.Container
	policy.RemoteUser = user.Remote
	policy.ContainerEnv = envValues(merged["containerEnv"])
	policy.RemoteEnv = envValues(merged["remoteEnv"])
	policy.OverrideCommand = source.Source["image"] != nil
	if override, ok := merged["overrideCommand"].(bool); ok {
		policy.OverrideCommand = override
	}
	if !policy.OverrideCommand && build == nil {
		policy.StartupTail = imageCommand(base)
	}

	p.port.policy = copyPolicy(policy)
	binding.DataUID = user.UID
	binding.DataGID = user.GID
	var seedSource *config.Resolved
	if !found {
		if !reflect.DeepEqual(binding.Initial.Map(), source.Descriptor.Map()) {
			return nil, fail("seed_mismatch")
		}
		seedSource = source
	}
	p.storage, e = host.Store.Seed(ctx, binding, seedSource, p.Check)
	if e != nil {
		return nil, e
	}
	if e = p.Check(ctx); e != nil {
		return nil, e
	}
	return p, nil
}
