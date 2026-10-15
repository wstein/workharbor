// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNativeBuildFreezesOnlySuccessfulIIDAndMatchingFinalTag(t *testing.T) {
	for _, kind := range []string{"success", "missing", "partial", "foreign-tag", "failed-build", "cancelled", "hostile-metadata", "symlink", "revoked-after-build", "cancel-after-build"} {
		t.Run(kind, func(t *testing.T) {
			p, _, ctx := nativeExecutionFixture(t)
			policy := p.policy
			b := policy.Build
			c := Call{Scope: p.scope, Kind: "build", Build: &BuildCall{Context: b.Context, Dockerfile: b.Scratch + "/one/Dockerfile-with-features", Tag: b.Tag, Target: b.Target, Args: b.Args}}
			p.checkBuild = func(context.Context, BuildCall) error { return nil }
			finalID := "sha256:" + strings.Repeat("d", 64)
			oldQuery := p.query
			p.query = func(ctx context.Context, a []string) ([]byte, error) {
				if a[0] != "inspect" {
					return oldQuery(ctx, a)
				}
				id := finalID
				if a[len(a)-1] == b.Tag && kind == "foreign-tag" {
					id = "sha256:" + strings.Repeat("e", 64)
				}
				metadata := policy.RuntimeMetadata
				if kind == "hostile-metadata" {
					metadata = `[{"initializeCommand":"false"}]`
				}
				return json.Marshal([]any{map[string]any{"Id": id, "Architecture": "arm64", "Os": "linux", "Config": map[string]any{"User": policy.ContainerUser, "Cmd": []any{"/bin/echo", "final-built-command"}, "Labels": map[string]any{"devcontainer.metadata": metadata}}, "RootFS": map[string]any{"Type": "layers", "Layers": []any{}}}})
			}
			p.stream = func(_ context.Context, a []string, _ io.Reader, _ io.Writer) error {
				iid := ""
				for i, v := range a {
					if v == "--iidfile" && i+1 < len(a) {
						iid = a[i+1]
					}
				}
				if iid == "" {
					t.Fatal("missing provider IID witness", a)
				}
				if _, e := os.Lstat(iid); !os.IsNotExist(e) {
					t.Fatal("IID precreated", e)
				}
				switch kind {
				case "failed-build":
					return errors.New("independent build failure")
				case "cancelled":
					return context.Canceled
				case "missing":
					return nil
				case "partial":
					return os.WriteFile(iid, []byte("sha256:bad"), 0600)
				case "symlink":
					other := iid + ".foreign"
					if e := os.WriteFile(other, []byte(finalID), 0600); e != nil {
						return e
					}
					return os.Symlink(other, iid)
				case "revoked-after-build":
					if e := os.WriteFile(iid, []byte(finalID), 0600); e != nil {
						return e
					}
					p.check = func(context.Context) error { return errors.New("revoked") }
					return nil
				case "cancel-after-build":
					if e := os.WriteFile(iid, []byte(finalID), 0600); e != nil {
						return e
					}
					p.lifetime.cancel()
					return nil
				default:
					return os.WriteFile(iid, []byte(finalID), 0600)
				}
			}
			e := p.InvokeStream(ctx, c, nil, io.Discard)
			_, registered := p.images[b.Tag]
			if kind == "success" {
				if e != nil || !registered || p.images[b.Tag].ID != finalID || strings.Join(p.policy.StartupTail, " ") != "/bin/echo final-built-command" {
					t.Fatal("valid build not frozen", e, registered)
				}
			} else if e == nil || registered {
				t.Fatal("unproven build registered", e, registered)
			}
		})
	}
}
