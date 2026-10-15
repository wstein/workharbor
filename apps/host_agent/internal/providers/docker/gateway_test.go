// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type recordingEngine struct {
	candidates []Container
	info       EngineInfo
	image      AdmittedImage
	effects    []Call
	reads      []string
}

func (e *recordingEngine) Candidates(_ context.Context, _ Scope) ([]Container, error) {
	e.reads = append(e.reads, "candidates")
	return e.candidates, nil
}
func (e *recordingEngine) Information(context.Context) (EngineInfo, error) {
	e.reads = append(e.reads, "info")
	return e.info, nil
}
func (e *recordingEngine) Image(_ context.Context, _ string) (AdmittedImage, error) {
	e.reads = append(e.reads, "image")
	return e.image, nil
}
func (e *recordingEngine) Invoke(_ context.Context, c Call) ([]byte, error) {
	e.effects = append(e.effects, c)
	return []byte("result"), nil
}
func gatewayFixture(t *testing.T, phase Phase) (*Gateway, *recordingEngine) {
	t.Helper()
	p := policyFixture(phase)
	p.RuntimeMetadata = `[{"userEnvProbe":"none","updateRemoteUserUID":false}]`
	id := strings.Repeat("a", 64)
	imageID := "sha256:" + strings.Repeat("b", 64)
	c := Container{ID: id, User: p.ContainerUser, Name: "/" + p.Scope.Name(), Image: imageID, Labels: p.Scope.Labels(), Mounts: []Mount{{Type: "bind", Source: p.DataPath, Destination: "/workspace", RW: true}}}
	c.Labels["devcontainer.metadata"] = p.RuntimeMetadata
	im := AdmittedImage{ID: imageID, Config: ImageConfig{}}
	e := &recordingEngine{candidates: []Container{c}, image: im, info: EngineInfo{OSType: "linux", Architecture: "arm64", ServerVersion: "29.8.2", SecurityOptions: []string{}}}
	return &Gateway{Parser: registryParser(t), Policy: p, Port: e, Authorize: func(context.Context) error { return nil }, Target: &Target{Scope: p.Scope, ContainerID: id, ImageID: imageID, DataPath: p.DataPath}, Images: map[string]AdmittedImage{imageSource()["image"].(string): im}}, e
}
func TestGatewayRechecksIdentityAndAuthorityBeforeEveryExec(t *testing.T) {
	g, e := gatewayFixture(t, Hooks)
	args := []string{"exec", "-i", "-u", "1000:1000", g.Target.ContainerID, "true"}
	if _, err := g.Forward(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(e.effects) != 1 || !reflect.DeepEqual(e.reads, []string{"info", "candidates"}) {
		t.Fatal(e)
	}
	e.candidates[0].Labels["workharbor.owner"] = "foreign"
	_, err := g.Forward(context.Background(), args)
	errorCode(t, err, "runtime_conflict")
	if len(e.effects) != 1 {
		t.Fatal("foreign exec forwarded")
	}
	g.Authorize = func(context.Context) error { return errors.New("revoked") }
	e.reads = nil
	_, err = g.Forward(context.Background(), args)
	if err == nil || len(e.reads) != 0 || len(e.effects) != 1 {
		t.Fatal("authorization not first", e, err)
	}
}
func TestGatewayRefusesDuplicateNameAndRootlessBeforeRun(t *testing.T) {
	g, e := gatewayFixture(t, Resources)
	g.Target = nil
	for _, candidates := range [][]Container{e.candidates, append(e.candidates, e.candidates[0])} {
		e.candidates = candidates
		_, err := g.Forward(context.Background(), runFixture(g.Policy))
		errorCode(t, err, "runtime_conflict")
	}
	e.candidates = nil
	e.info.SecurityOptions = []string{"name=rootless"}
	_, err := g.Forward(context.Background(), runFixture(g.Policy))
	errorCode(t, err, "unsupported_configuration")
	if len(e.effects) != 0 {
		t.Fatal("run forwarded")
	}
}
func TestGatewayFrozenImageAndSecondAuthorizationBeforePull(t *testing.T) {
	g, e := gatewayFixture(t, Resources)
	e.candidates = nil
	ref := imageSource()["image"].(string)
	g.Target = nil
	e.image.Config.Volumes = map[string]any{"/workspace": map[string]any{}}
	_, err := g.Forward(context.Background(), []string{"pull", ref})
	errorCode(t, err, "configuration_changed")
	if len(e.effects) != 0 {
		t.Fatal("changed image forwarded")
	}
	e.image = g.Images[ref]
	calls := 0
	g.Authorize = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("fence lost")
		}
		return nil
	}
	_, err = g.Forward(context.Background(), []string{"pull", ref})
	if err == nil || calls != 2 || len(e.effects) != 0 {
		t.Fatal("second authorization absent", calls, err)
	}
}

func TestGatewayRunChecksEffectiveMetadataAndFinalImageID(t *testing.T) {
	for _, imageLabel := range []bool{false, true} {
		g, e := gatewayFixture(t, Resources)
		g.Target = nil
		e.candidates = nil
		source := imageSource()
		source["containerUser"] = "0:0"
		source["remoteUser"] = "1000:1000"
		raw, _, err := ExpectedMetadata(source, nil)
		if err != nil {
			t.Fatal(err)
		}
		g.Policy.RuntimeMetadata = raw
		g.Parser = registryParser(t)
		var pretty bytes.Buffer
		if err = json.Indent(&pretty, []byte(raw), "", "  "); err != nil {
			t.Fatal(err)
		}
		args := runFixture(g.Policy)
		if imageLabel {
			ref := source["image"].(string)
			im := g.Images[ref]
			im.Config.Metadata = pretty.String()
			g.Images[ref] = im
			e.image = im
		} else {
			args = append(args[:1], append([]string{"-l", "devcontainer.metadata=" + pretty.String()}, args[1:]...)...)
		}
		if _, err = g.Forward(context.Background(), args); err != nil {
			t.Fatal("equivalent approved final metadata refused", imageLabel, err)
		}
		if len(e.effects) != 1 || e.effects[0].Run.ImageID != e.image.ID {
			t.Fatal("verified final imageID not bound")
		}
		g.Policy.RuntimeMetadata = `[{"userEnvProbe":"none","updateRemoteUserUID":false,"postStartCommand":"changed"}]`
		if _, err = g.Forward(context.Background(), args); err == nil || len(e.effects) != 1 {
			t.Fatal("foreign metadata forwarded")
		}
	}
}

func TestGatewayRuntimeUserNamespaceAndMetadataRecheckedBeforeExec(t *testing.T) {
	for _, kind := range []string{"user", "namespace", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			g, p := gatewayFixture(t, Hooks)
			g.Parser = registryParser(t)
			g.Policy.RuntimeMetadata = `[{"userEnvProbe":"none","updateRemoteUserUID":false}]`
			p.candidates[0].User = g.Policy.ContainerUser
			p.candidates[0].Labels["devcontainer.metadata"] = g.Policy.RuntimeMetadata
			switch kind {
			case "user":
				p.candidates[0].User = "0:1"
			case "namespace":
				p.candidates[0].UsernsMode = "host"
			case "metadata":
				p.candidates[0].Labels["devcontainer.metadata"] = `{"initializeCommand":"hostile"}`
			}
			_, e := g.Forward(context.Background(), []string{"exec", "-i", "-u", g.Policy.RemoteUser, g.Target.ContainerID, "true"})
			if e == nil || len(p.effects) != 0 {
				t.Fatal("unsafe actual runtime forwarded", kind, e)
			}
		})
	}
}
