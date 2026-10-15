// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func nativeExecutionFixture(t *testing.T) (*nativePort, Call, context.Context) {
	t.Helper()
	p := policyFixture(Resources)
	p.RuntimeMetadata = `[{"userEnvProbe":"none","updateRemoteUserUID":false}]`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	port := &nativePort{scope: p.Scope, policy: p, check: func(context.Context) error { return nil }, parser: registryParser(t), lifetime: &nativeLifetime{ctx: ctx, cancel: cancel, root: bridgeRoot(t)}}
	image := AdmittedImage{ID: "sha256:" + strings.Repeat("b", 64), Config: ImageConfig{}}
	ref := imageSource()["image"].(string)
	port.images = map[string]AdmittedImage{ref: image}
	port.query = func(_ context.Context, args []string) ([]byte, error) {
		switch args[0] {
		case "ps":
			return nil, nil
		case "info":
			return []byte(`{"OSType":"linux","Architecture":"arm64","ServerVersion":"29.8.2","SecurityOptions":[]}`), nil
		case "inspect":
			raw, _ := json.Marshal([]any{map[string]any{"Id": image.ID, "Architecture": "arm64", "Os": "linux", "Config": map[string]any{}, "RootFS": map[string]any{"Type": "layers", "Layers": []any{}}}})
			return raw, nil
		}
		return nil, fail("provider_refused")
	}
	call, e := Classify(p, runFixture(p))
	if e != nil {
		t.Fatal(e)
	}
	call.Run.Labels["devcontainer.metadata"] = p.RuntimeMetadata
	call.Scope = p.Scope
	call.Run.ImageID = image.ID
	return port, call, ctx
}
func TestNativeRunUsesVerifiedIDNeverPullAndBindsOnlyLiveCID(t *testing.T) {
	p, c, ctx := nativeExecutionFixture(t)
	forwarded := 0
	p.stream = func(ctx context.Context, args []string, _ io.Reader, _ io.Writer) error {
		forwarded++
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--pull never --cidfile ") || !strings.Contains(joined, c.Run.ImageID) || strings.Contains(joined, c.Run.Image+" ") {
			t.Fatal("native run identity", args)
		}
		a := p.allocation
		if a == nil {
			t.Fatal("run not reserved")
		}
		if _, e := os.Lstat(a.cid); !os.IsNotExist(e) {
			t.Fatal("precreated cid")
		}
		id := strings.Repeat("a", 64)
		os.WriteFile(a.cid, []byte(id), 0644)
		target, e := p.ObserveAllocation(ctx, []Container{{ID: id, Name: "/" + p.scope.Name(), Image: c.Run.ImageID, User: p.policy.ContainerUser, Labels: c.Run.Labels, Mounts: []Mount{{Type: "bind", Source: p.policy.DataPath, Destination: "/workspace", RW: true}}}})
		if e != nil || target == nil || target.ContainerID != id {
			t.Fatal("live identity binding", target, e)
		}
		return nil
	}
	if e := p.InvokeStream(ctx, c, bytes.NewReader(nil), io.Discard); e != nil {
		t.Fatal(e)
	}
	if forwarded != 1 {
		t.Fatal("run not forwarded")
	}
	if e := p.InvokeStream(ctx, c, nil, io.Discard); e == nil || forwarded != 1 {
		t.Fatal("run replayed")
	}
	if _, e := p.allocation.Observe(ctx, nil); e == nil {
		t.Fatal("closed allocation reopened")
	}
}
func TestNativeRunMissingImageWrongIdentityAndMountRefuseBeforeForward(t *testing.T) {
	for _, kind := range []string{"missing-image", "wrong-image-id", "wrong-user", "foreign-mount"} {
		t.Run(kind, func(t *testing.T) {
			p, c, ctx := nativeExecutionFixture(t)
			effects := 0
			p.stream = func(context.Context, []string, io.Reader, io.Writer) error { effects++; return nil }
			switch kind {
			case "missing-image":
				p.query = func(_ context.Context, a []string) ([]byte, error) {
					if a[0] == "ps" {
						return nil, nil
					}
					if a[0] == "info" {
						return []byte(`{"OSType":"linux","Architecture":"arm64","ServerVersion":"29.8.2","SecurityOptions":[]}`), nil
					}
					return nil, fail("metadata_unavailable")
				}
			case "wrong-image-id":
				c.Run.ImageID = "sha256:" + strings.Repeat("c", 64)
			case "wrong-user":
				c.Run.User = "0:1"
			case "foreign-mount":
				c.Run.Mount = "type=bind,source=/var/run/docker.sock,target=/workspace"
			}
			if e := p.InvokeStream(ctx, c, nil, io.Discard); e == nil || effects != 0 {
				t.Fatal("unsafe native run forwarded", e, effects)
			}
		})
	}
}
