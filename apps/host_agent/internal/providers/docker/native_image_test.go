// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func localImageFixture(t *testing.T) (*nativePort, map[string]any) {
	t.Helper()
	m := map[string]any{"Id": "sha256:" + strings.Repeat("b", 64), "Architecture": "arm64", "Os": "linux", "Config": map[string]any{"User": "image-default", "Labels": map[string]any{"devcontainer.metadata": `[{"userEnvProbe":"none","updateRemoteUserUID":false}]`}}, "RootFS": map[string]any{"Type": "layers", "Layers": []any{}}}
	p := &nativePort{scope: scopeFixture(), check: func(context.Context) error { return nil }, parser: registryParser(t)}
	p.query = func(context.Context, []string) ([]byte, error) { raw, _ := json.Marshal([]any{m}); return raw, nil }
	return p, m
}
func TestNativeLocalImageRequiresExactIDAndStrictActualMetadata(t *testing.T) {
	p, m := localImageFixture(t)
	im, raw, e := p.localImage(context.Background(), "vsc-fixture")
	if e != nil || im.ID != m["Id"] || len(raw) == 0 || im.Config.Metadata == "" {
		t.Fatal("local proof missing", im, e)
	}
	for _, bad := range []map[string]any{{"OnBuild": []any{"RUN hook"}}, {"Volumes": map[string]any{"/workspace": map[string]any{}}}, {"Labels": map[string]any{"devcontainer.metadata": "{bad"}}} {
		m["Config"] = bad
		if _, _, e = p.localImage(context.Background(), "vsc-fixture"); e == nil {
			t.Fatal("unsafe final image admitted")
		}
	}
}
func TestNativeBuiltTagCannotRetargetFrozenFinalImage(t *testing.T) {
	p, m := localImageFixture(t)
	expected, _, e := p.localImage(context.Background(), "vsc-fixture")
	if e != nil {
		t.Fatal(e)
	}
	p.images = map[string]AdmittedImage{"vsc-fixture": expected}
	if _, e = p.Image(context.Background(), "vsc-fixture"); e != nil {
		t.Fatal(e)
	}
	m["Id"] = "sha256:" + strings.Repeat("c", 64)
	if _, e = p.Image(context.Background(), "vsc-fixture"); e == nil {
		t.Fatal("retargeted image adopted")
	}
}
