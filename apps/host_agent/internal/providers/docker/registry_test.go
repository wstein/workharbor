// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func registryDigest(raw string) string {
	s := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(s[:])
}
func registryParser(t *testing.T) config.JSONC {
	t.Helper()
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	root, e := filepath.Abs("../../../tools/devcontainer")
	if e != nil {
		t.Fatal(e)
	}
	return config.JSONC{NodePath: node, CompanionDir: root}
}
func TestRegistrySingleManifestChecksDigestPlatformAndRawCapabilities(t *testing.T) {
	p := registryParser(t)
	raw := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + strings.Repeat("b", 64) + `","size":128},"layers":[]}`
	ref := "example.test/app@" + registryDigest(raw)
	selected, e := selectRegistryManifest(context.Background(), p, ref, "arm64", []byte(raw))
	if e != nil || selected.Reference != ref || selected.ConfigID != "sha256:"+strings.Repeat("b", 64) {
		t.Fatal(selected, e)
	}
	configRaw := `{"architecture":"arm64","os":"linux","config":{"User":"image-default","Labels":{"devcontainer.metadata":"[{\"remoteUser\":\"image-user\"}]"}}}`
	selected.ConfigID = registryDigest(configRaw)
	selected.ConfigSize = int64(len(configRaw))
	im, e := decodeRegistryImage(context.Background(), p, selected, "arm64", []byte(configRaw))
	if e != nil || im.ID != selected.ConfigID || im.Config.Metadata == "" {
		t.Fatal(im, e)
	}
	for _, bad := range []string{`{"architecture":"amd64","os":"linux","config":{}}`, `{"architecture":"arm64","os":"linux","config":{"Volumes":{"/workspace":{}}}}`, `{"architecture":"arm64","os":"linux","config":{"OnBuild":["RUN touch hook"]}}`, `{"architecture":"arm64","os":"linux","config":{"Labels":{"devcontainer.metadata":"{bad"}}}`, `{"architecture":"arm64","os":"linux","config":{"Labels":{"devcontainer.metadata":"{\"initializeCommand\":\"touch hook\"}"}}}`} {
		selected.ConfigID = registryDigest(bad)
		selected.ConfigSize = int64(len(bad))
		if _, e := decodeRegistryImage(context.Background(), p, selected, "arm64", []byte(bad)); e == nil {
			t.Fatal("unsafe raw image admitted", bad)
		}
	}
	_, e = selectRegistryManifest(context.Background(), p, ref, "arm64", []byte(raw+" "))
	errorCode(t, e, "metadata_unavailable")
}
func TestRegistryIndexSelectsUniqueExplicitPlatformWithoutPull(t *testing.T) {
	p := registryParser(t)
	digest := "sha256:" + strings.Repeat("c", 64)
	raw := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":"` + digest + `","platform":{"os":"linux","architecture":"arm64","variant":"v8"}},{"digest":"sha256:` + strings.Repeat("d", 64) + `","platform":{"os":"linux","architecture":"amd64"}}]}`
	ref := "example.test/app@" + registryDigest(raw)
	selected, e := selectRegistryManifest(context.Background(), p, ref, "arm64", []byte(raw))
	if e != nil || selected.Reference != "example.test/app@"+digest || selected.ConfigID != "" {
		t.Fatal(selected, e)
	}
	for _, arch := range []string{"386", "unknown"} {
		if _, e := selectRegistryManifest(context.Background(), p, ref, arch, []byte(raw)); e == nil {
			t.Fatal("unsupported arch accepted")
		}
	}
	dup := strings.Replace(raw, `"architecture":"amd64"`, `"architecture":"arm64"`, 1)
	_, e = selectRegistryManifest(context.Background(), p, "example.test/app@"+registryDigest(dup), "arm64", []byte(dup))
	errorCode(t, e, "metadata_unavailable")
}

func TestRegistryRawConfigDigestAndDuplicateKeysBeforeNormalization(t *testing.T) {
	p := registryParser(t)
	raw := `{"architecture":"arm64","os":"linux","config":{}}`
	selected := registrySelection{Reference: "example.test/app@sha256:" + strings.Repeat("a", 64), ConfigID: registryDigest(raw), ConfigSize: int64(len(raw))}
	if _, e := decodeRegistryImage(context.Background(), p, selected, "arm64", []byte(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e := decodeRegistryImage(context.Background(), p, selected, "arm64", []byte(raw+" ")); e == nil {
		t.Fatal("raw digest mismatch admitted")
	}
	for _, bad := range []string{`{"architecture":"arm64","os":"linux","config":{"OnBuild":["RUN bad"],"OnBuild":[]}}`, `{"architecture":"arm64","os":"linux","config":{"Labels":{"devcontainer.metadata":"{}","devcontainer.metadata":"[]"}}}`} {
		selected.ConfigID = registryDigest(bad)
		selected.ConfigSize = int64(len(bad))
		selected.ConfigID = registryDigest(bad)
		selected.ConfigSize = int64(len(bad))
		if _, e := decodeRegistryImage(context.Background(), p, selected, "arm64", []byte(bad)); e == nil {
			t.Fatal("raw duplicate hidden", bad)
		}
	}
}

func TestRegistryDeclaredConfigSizeRefusesBeforeToolSpawn(t *testing.T) {
	p := registryParser(t)
	for _, size := range []string{"0", "-1", "1048577", "1.5", `"123"`} {
		raw := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + strings.Repeat("a", 64) + `","size":` + size + `},"layers":[]}`
		_, e := selectRegistryManifest(context.Background(), p, "example.test/app@"+registryDigest(raw), "arm64", []byte(raw))
		errorCode(t, e, "metadata_unavailable")
	}
	raw := `{"architecture":"arm64","os":"linux","config":{}}`
	_, e := decodeRegistryImage(context.Background(), p, registrySelection{ConfigID: registryDigest(raw), ConfigSize: int64(len(raw) + 1)}, "arm64", []byte(raw))
	errorCode(t, e, "metadata_unavailable")
}

func TestRegistryDockerCaseAliasesCannotHideInheritedEffects(t *testing.T) {
	p := registryParser(t)
	for _, raw := range []string{`{"architecture":"arm64","os":"linux","config":{"onbuild":["RUN bad"]}}`, `{"architecture":"arm64","os":"linux","config":{"volumes":{"/workspace":{}}}}`, `{"architecture":"arm64","os":"linux","config":{},"Config":{"OnBuild":["RUN bad"]}}`, `{"architecture":"arm64","os":"linux","config":{"Labels":{},"labels":{"devcontainer.metadata":"{bad"}}}}`} {
		_, e := decodeRegistryImage(context.Background(), p, registrySelection{ConfigID: registryDigest(raw), ConfigSize: int64(len(raw))}, "arm64", []byte(raw))
		if e == nil {
			t.Fatal("Docker case-insensitive field bypass", raw)
		}
	}
}

func TestRegistryMalformedRuntimeFieldsRefuseBeforeOfficialCLI(t *testing.T) {
	p := registryParser(t)
	for _, field := range []string{`"Env":{"A":"B"}`, `"Entrypoint":42`, `"User":[]`, `"Healthcheck":{"Interval":"forever"}`} {
		raw := `{"architecture":"arm64","os":"linux","config":{` + field + `}}`
		_, e := decodeRegistryImage(context.Background(), p, registrySelection{ConfigID: registryDigest(raw), ConfigSize: int64(len(raw))}, "arm64", []byte(raw))
		if e == nil {
			t.Fatal("malformed runtime field admitted", field)
		}
	}
}
