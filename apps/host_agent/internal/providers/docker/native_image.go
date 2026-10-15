// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"reflect"
	"strings"
)

// localImage is post-pull/build corroboration, never raw registry authority.
// Its ID must equal the independently frozen config digest/build IID witness.
func (p *nativePort) localImage(ctx context.Context, ref string) (AdmittedImage, []byte, error) {
	raw, e := p.read(ctx, []string{"inspect", "--type", "image", ref})
	if e != nil {
		return AdmittedImage{}, nil, e
	}
	value, e := wirejson.Parse(raw)
	if e != nil {
		return AdmittedImage{}, nil, fail("metadata_unavailable")
	}
	values, ok := value.([]any)
	if !ok || len(values) != 1 {
		return AdmittedImage{}, nil, fail("metadata_unavailable")
	}
	m, ok := values[0].(map[string]any)
	if !ok || !canonicalDockerKeys(m, []string{"Id", "Architecture", "Os", "Config", "RootFS"}) {
		return AdmittedImage{}, nil, fail("metadata_unavailable")
	}
	id, idOK := stringField(m, "Id")
	arch, archOK := stringField(m, "Architecture")
	os, osOK := stringField(m, "Os")
	cfg, cfgOK := objectField(m, "Config")
	rootfs, rootOK := objectField(m, "RootFS")
	if !idOK || !archOK || !osOK || !cfgOK || !rootOK || !strings.HasPrefix(id, "sha256:") || !hexID.MatchString(strings.TrimPrefix(id, "sha256:")) || os != "linux" || (arch != "arm64" && arch != "amd64") {
		return AdmittedImage{}, nil, fail("metadata_unavailable")
	}
	image, e := admitDockerConfig(ctx, p.parser, cfg)
	if e != nil {
		return AdmittedImage{}, nil, e
	}
	return AdmittedImage{ID: id, Config: image, Document: map[string]any{"architecture": arch, "os": os, "config": clone(cfg), "rootfs": clone(rootfs)}}, raw, nil
}
func (p *nativePort) Image(ctx context.Context, ref string) (AdmittedImage, error) {
	if e := p.authorize(ctx); e != nil {
		return AdmittedImage{}, e
	}
	p.stateMu.Lock()
	expected, ok := p.images[ref]
	p.stateMu.Unlock()
	if !ok {
		return AdmittedImage{}, fail("configuration_changed")
	}
	if _, _, digest := registryRef(ref); digest {
		if p.registry == nil {
			return AdmittedImage{}, fail("metadata_unavailable")
		}
		actual, e := p.registry.Inspect(ctx, ref)
		if e != nil {
			return AdmittedImage{}, e
		}
		if !reflect.DeepEqual(actual, expected) {
			return AdmittedImage{}, fail("configuration_changed")
		}
		return actual, nil
	}
	actual, _, e := p.localImage(ctx, ref)
	if e != nil {
		return AdmittedImage{}, e
	}
	if !reflect.DeepEqual(actual, expected) {
		return AdmittedImage{}, fail("configuration_changed")
	}
	return actual, nil
}
func (p *nativePort) requireLocal(ctx context.Context, ref string, expected AdmittedImage) ([]byte, error) {
	actual, raw, e := p.localImage(ctx, ref)
	if e != nil {
		return nil, e
	}
	// The digest binds original raw bytes; Docker may normalize empty collections
	// in inspect. Never normalize the raw registry input before its own admission.
	if actual.ID != expected.ID || actual.Config.Metadata != expected.Config.Metadata || len(actual.Config.Volumes) != 0 || len(actual.Config.OnBuild) != 0 {
		return nil, fail("configuration_changed")
	}
	return raw, nil
}

func imageCommand(image AdmittedImage) []string {
	out := []string{}
	cfg, ok := image.Document["config"].(map[string]any)
	if !ok {
		return out
	}
	for _, key := range []string{"Entrypoint", "Cmd"} {
		if values, ok := cfg[key].([]any); ok {
			for _, value := range values {
				if text, ok := value.(string); ok {
					out = append(out, text)
				}
			}
		}
	}
	return out
}
