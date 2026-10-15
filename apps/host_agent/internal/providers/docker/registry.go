// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
)

//go:embed tool-pins.json
var toolPins []byte

type registrySelection struct {
	Reference, ConfigID string
	ConfigSize          int64
}

func registryRef(ref string) (string, string, bool) {
	parts := strings.Split(ref, "@sha256:")
	if len(parts) != 2 || !hexID.MatchString(parts[1]) {
		return "", "", false
	}
	if config.ValidateSource(map[string]any{"image": ref}) != nil {
		return "", "", false
	}
	return parts[0], "sha256:" + parts[1], true
}
func registryObject(ctx context.Context, p config.JSONC, raw []byte) (map[string]any, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	// The strict companion preserves raw number lexemes into Go UseNumber;
	// descriptor integers must never pass through JavaScript numeric conversion.
	v, e := p.Parse(ctx, raw, true)
	if e != nil {
		return nil, fail("metadata_unavailable")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail("metadata_unavailable")
	}
	return m, nil
}
func selectRegistryManifest(ctx context.Context, p config.JSONC, ref, arch string, raw []byte) (registrySelection, error) {
	if len(raw) > metadataLimit {
		return registrySelection{}, fail("metadata_unavailable")
	}
	name, digest, ok := registryRef(ref)
	if !ok || (arch != "arm64" && arch != "amd64") {
		return registrySelection{}, fail("metadata_unavailable")
	}
	sum := sha256.Sum256(raw)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return registrySelection{}, fail("metadata_unavailable")
	}
	m, e := registryObject(ctx, p, raw)
	if e != nil {
		return registrySelection{}, e
	}
	if m["schemaVersion"] != json.Number("2") {
		return registrySelection{}, fail("metadata_unavailable")
	}
	switch m["mediaType"] {
	case "application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json":
		cfg, ok := m["config"].(map[string]any)
		if !ok {
			return registrySelection{}, fail("metadata_unavailable")
		}
		id, ok := cfg["digest"].(string)
		if !ok || !strings.HasPrefix(id, "sha256:") || !hexID.MatchString(strings.TrimPrefix(id, "sha256:")) {
			return registrySelection{}, fail("metadata_unavailable")
		}
		if cfg["mediaType"] != "application/vnd.oci.image.config.v1+json" && cfg["mediaType"] != "application/vnd.docker.container.image.v1+json" {
			return registrySelection{}, fail("metadata_unavailable")
		}
		size, ok := cfg["size"].(json.Number)
		if !ok {
			return registrySelection{}, fail("metadata_unavailable")
		}
		count, sizeErr := strconv.ParseInt(string(size), 10, 64)
		if sizeErr != nil || count <= 0 || count > metadataLimit {
			return registrySelection{}, fail("metadata_unavailable")
		}
		return registrySelection{Reference: ref, ConfigID: id, ConfigSize: count}, nil
	case "application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json":
		entries, ok := m["manifests"].([]any)
		if !ok {
			return registrySelection{}, fail("metadata_unavailable")
		}
		selected := ""
		for _, entry := range entries {
			item, ok := entry.(map[string]any)
			if !ok {
				return registrySelection{}, fail("metadata_unavailable")
			}
			platform, ok := item["platform"].(map[string]any)
			if !ok {
				continue
			}
			if platform["os"] != "linux" || platform["architecture"] != arch {
				continue
			}
			if variant, exists := platform["variant"]; exists && variant != "" && (arch != "arm64" || variant != "v8") {
				return registrySelection{}, fail("metadata_unavailable")
			}
			id, ok := item["digest"].(string)
			if !ok || !strings.HasPrefix(id, "sha256:") || !hexID.MatchString(strings.TrimPrefix(id, "sha256:")) || selected != "" {
				return registrySelection{}, fail("metadata_unavailable")
			}
			selected = name + "@" + id
		}
		if selected == "" {
			return registrySelection{}, fail("metadata_unavailable")
		}
		return registrySelection{Reference: selected}, nil
	default:
		return registrySelection{}, fail("metadata_unavailable")
	}
}
func canonicalDockerKeys(m map[string]any, known []string) bool {
	seen := map[string]bool{}
	for key := range m {
		fold := strings.ToLower(key)
		if seen[fold] {
			return false
		}
		seen[fold] = true
		for _, canonical := range known {
			if strings.EqualFold(key, canonical) && key != canonical {
				return false
			}
		}
	}
	return true
}
func decodeRegistryImage(ctx context.Context, p config.JSONC, selected registrySelection, arch string, raw []byte) (AdmittedImage, error) {
	if selected.ConfigID == "" || selected.ConfigSize <= 0 || selected.ConfigSize > metadataLimit || int64(len(raw)) != selected.ConfigSize {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	sum := sha256.Sum256(raw)
	if selected.ConfigID != "sha256:"+hex.EncodeToString(sum[:]) {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	m, e := registryObject(ctx, p, raw)
	if e != nil {
		return AdmittedImage{}, e
	}
	if !canonicalDockerKeys(m, []string{"architecture", "os", "config", "variant", "rootfs", "history", "created", "author"}) {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	var typedRoot dockerspec.DockerOCIImage
	if json.Unmarshal(raw, &typedRoot) != nil {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	if m["os"] != "linux" || m["architecture"] != arch {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	cfg, ok := m["config"].(map[string]any)
	if !ok {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	image, e := admitDockerConfig(ctx, p, cfg)
	if e != nil {
		return AdmittedImage{}, e
	}
	return AdmittedImage{ID: selected.ConfigID, Config: image, Document: clone(m).(map[string]any)}, nil
}

// Registry reads public registry metadata through pinned official tools. It never
// calls pull/build/create, installs tools or accepts caller-provided tool flags.
// PrivateDirectory is a native host-owned private root, never repository input.
type Registry struct {
	CAFile, CASHA256                                      string
	BuildxPath, CranePath, PrivateDirectory, Architecture string
	Parser                                                config.JSONC
}

const metadataLimit = 1 << 20

func registryEnvironment(root, tool string) []string {
	return []string{"PATH=" + filepath.Dir(tool), "HOME=" + filepath.Join(root, "home"), "DOCKER_CONFIG=" + filepath.Join(root, "docker"), "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_RUNTIME_DIR=" + filepath.Join(root, "runtime"), "LANG=C.UTF-8"}
}
func registryPrivate(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !ownedDirectory(root) {
		return "", fail("tool_unavailable")
	}
	stage, e := os.MkdirTemp(root, "registry-")
	if e != nil {
		return "", fail("metadata_unavailable")
	}
	for _, dir := range []string{"home", "home/.docker", "docker", "config", "runtime"} {
		if os.Mkdir(filepath.Join(stage, dir), 0700) != nil {
			os.RemoveAll(stage)
			return "", fail("metadata_unavailable")
		}
	}
	for _, path := range []string{"home/.docker/config.json", "docker/config.json"} {
		if os.WriteFile(filepath.Join(stage, path), []byte("{}"), 0600) != nil {
			os.RemoveAll(stage)
			return "", fail("metadata_unavailable")
		}
	}
	return stage, nil
}
func verifyRegistryTool(ctx context.Context, path, digest, version, stage string, buildx bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fail("tool_unavailable")
	}
	physical, e := filepath.EvalSymlinks(path)
	if e != nil || physical != path {
		return fail("tool_unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return fail("tool_unavailable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return fail("tool_unavailable")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil || hex.EncodeToString(h.Sum(nil)) != digest {
		return fail("tool_unavailable")
	}
	out, e := captureMetadata(ctx, path, []string{"version"}, stage, registryEnvironment(stage, path), 1024)
	if e != nil {
		return fail("tool_unavailable")
	}
	if buildx {
		if !bytes.Contains(out, []byte(" "+version+" ")) {
			return fail("tool_unavailable")
		}
	} else if strings.TrimPrefix(strings.TrimSpace(string(out)), "v") != strings.TrimPrefix(version, "v") {
		return fail("tool_unavailable")
	}
	return nil
}
func (r Registry) Inspect(ctx context.Context, ref string) (AdmittedImage, error) {
	if e := registryTLS(ref); e != nil {
		return AdmittedImage{}, e
	}
	if _, _, ok := registryRef(ref); !ok {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil {
		return AdmittedImage{}, fail("metadata_unavailable")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != r.Architecture {
		return AdmittedImage{}, fail("tool_unavailable")
	}
	var pins struct{ Buildx, Crane map[string]string }
	if json.Unmarshal(toolPins, &pins) != nil {
		return AdmittedImage{}, fail("tool_unavailable")
	}
	stage, e := registryPrivate(r.PrivateDirectory)
	if e != nil {
		return AdmittedImage{}, e
	}
	defer os.RemoveAll(stage)
	if _, e = r.readEnvironment(stage, r.BuildxPath); e != nil {
		return AdmittedImage{}, e
	}
	if e = verifyRegistryTool(ctx, r.BuildxPath, pins.Buildx["linux_"+r.Architecture+"_sha256"], pins.Buildx["version"], stage, true); e != nil {
		return AdmittedImage{}, e
	}
	if e = verifyRegistryTool(ctx, r.CranePath, pins.Crane["linux_"+r.Architecture+"_sha256"], pins.Crane["version"], stage, false); e != nil {
		return AdmittedImage{}, e
	}
	manifest := func(ref string) ([]byte, error) {
		env, e := r.readEnvironment(stage, r.BuildxPath)
		if e != nil {
			return nil, e
		}
		return captureMetadata(ctx, r.BuildxPath, []string{"imagetools", "inspect", "--raw", ref}, stage, env, metadataLimit)
	}
	raw, e := manifest(ref)
	if e != nil {
		return AdmittedImage{}, e
	}
	selected, e := selectRegistryManifest(ctx, r.Parser, ref, r.Architecture, raw)
	if e != nil {
		return AdmittedImage{}, e
	}
	if selected.ConfigID == "" {
		raw, e = manifest(selected.Reference)
		if e != nil {
			return AdmittedImage{}, e
		}
		selected, e = selectRegistryManifest(ctx, r.Parser, selected.Reference, r.Architecture, raw)
		if e != nil || selected.ConfigID == "" {
			return AdmittedImage{}, fail("metadata_unavailable")
		}
	}
	// The declared size was independently checked before this spawn. Raw stdout
	// remains untouched until exact byte-count and SHA256 checks in the decoder.
	env, e := r.readEnvironment(stage, r.CranePath)
	if e != nil {
		return AdmittedImage{}, e
	}
	raw, e = captureMetadata(ctx, r.CranePath, []string{"config", selected.Reference}, stage, env, selected.ConfigSize)
	if e != nil {
		return AdmittedImage{}, e
	}
	return decodeRegistryImage(ctx, r.Parser, selected, r.Architecture, raw)
}

func admitDockerConfig(ctx context.Context, p config.JSONC, cfg map[string]any) (ImageConfig, error) {
	if !canonicalDockerKeys(cfg, []string{"User", "Env", "Entrypoint", "Cmd", "Volumes", "OnBuild", "Labels", "WorkingDir", "ExposedPorts", "StopSignal", "ArgsEscaped", "Healthcheck", "Shell", "StopTimeout"}) {
		return ImageConfig{}, fail("metadata_unavailable")
	}
	var typed dockerspec.DockerOCIImage
	typedRaw, marshalErr := json.Marshal(map[string]any{"config": cfg})
	if marshalErr != nil || json.Unmarshal(typedRaw, &typed) != nil {
		return ImageConfig{}, fail("metadata_unavailable")
	}
	image := ImageConfig{}
	if value, exists := cfg["Volumes"]; exists && value != nil {
		volumes, ok := value.(map[string]any)
		if !ok {
			return ImageConfig{}, fail("metadata_unavailable")
		}
		image.Volumes = volumes
	}
	if value, exists := cfg["OnBuild"]; exists && value != nil {
		hooks, ok := value.([]any)
		if !ok {
			return ImageConfig{}, fail("metadata_unavailable")
		}
		image.OnBuild = []string{}
		for _, hook := range hooks {
			s, ok := hook.(string)
			if !ok {
				return ImageConfig{}, fail("metadata_unavailable")
			}
			image.OnBuild = append(image.OnBuild, s)
		}
	}
	if value, exists := cfg["Labels"]; exists && value != nil {
		labels, ok := value.(map[string]any)
		if !ok {
			return ImageConfig{}, fail("metadata_unavailable")
		}
		for name, value := range labels {
			s, ok := value.(string)
			if !ok {
				return ImageConfig{}, fail("metadata_unavailable")
			}
			if name == "devcontainer.metadata" {
				image.Metadata = s
			}
		}
	}
	if _, e := ValidateImage(ctx, p, image); e != nil {
		return ImageConfig{}, e
	}
	return image, nil
}
