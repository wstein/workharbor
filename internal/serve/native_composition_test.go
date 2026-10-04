package serve

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/skillset"
)

func TestBuildRefusesUnmeasuredNativeComposition(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	proxy := filepath.Join(root, "libexec", "whr", "whr-proxy-linux-arm64")
	tools := filepath.Join(root, "tools")
	tokenFile := filepath.Join(root, "api.token")
	for _, directory := range []string{bin, filepath.Dir(proxy), filepath.Join(tools, "profiles", "fixture", "bin"), filepath.Join(tools, "store")} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for filename, contents := range map[string]string{
		filepath.Join(bin, "container"): "#!/bin/sh\nexit 0\n",
		filepath.Join(bin, "whr"):       "offline fixture",
		proxy:                           "offline fixture",
	} {
		file, err := os.OpenFile(filepath.Clean(filename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		if _, err := file.WriteString(contents); err != nil {
			t.Fatal(err)
		}
		if err := file.Chmod(0o500); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(root, "fixture.pem")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("offline-api-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	cfg := &config.Config{
		APITokenFile:      tokenFile,
		SkillSet:          skillset.Config{Selection: "none"},
		StateDir:          filepath.Join(root, "state"),
		AgentAllowedTools: []string{"Read"},
		Roots:             config.Roots{ToolStore: tools},
		Environment:       config.Environment{Image: "whr.invalid/whr-base/fedora:abc123abc123"},
		GitHub:            config.GitHub{AppID: 1, KeyFile: keyFile},
		Repositories:      []config.Repository{{Name: "fixture/project"}},
	}
	deps, closeFn, err := Build(cfg, filepath.Join(bin, "whr"), t.TempDir(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	composed := serviceConfig(deps, t.Logf)
	if composed.ProjectInstructions == nil || composed.SkillBinding == nil || composed.PrepareSkills == nil {
		t.Fatal("production factory omitted mandatory composition gates")
	}
	for _, mode := range []string{"none", "package"} {
		for _, session := range []string{"", "recorded-session"} {
			run := domain.Run{SessionID: session, Skills: domain.SkillSelection{Mode: mode}}
			project, err := composed.ProjectInstructions(context.Background(), domain.Task{Repo: "fixture/project"}, run)
			if !errors.Is(err, agent.ErrUnsupported) || project.Revision != "" || project.Text != "" || len(project.Inputs) != 0 {
				t.Fatalf("missing reviewed policy trusted on %s/%s: %+v, %v", mode, session, project, err)
			}
			if err := composed.PrepareSkills(context.Background(), run, service.SkillMount{Source: root, Target: "/skills/fixture", ReadOnly: true}); !errors.Is(err, agent.ErrUnsupported) {
				t.Fatalf("unmeasured mount accepted on %s/%s: %v", mode, session, err)
			}
		}
	}
	declared := []skillset.Binding{{Name: "codex", Version: "1", Model: "gpt-6.1-sol", Effort: "low"}}
	binding, err := composed.SkillBinding(context.Background(), declared)
	if !errors.Is(err, agent.ErrUnsupported) || binding != (skillset.Binding{}) {
		t.Fatalf("offline/generic version established native compatibility: %+v, %v", binding, err)
	}
	if deps.Agent.Name() != "claude-code" {
		t.Fatalf("factory implicitly switched native providers: %s", deps.Agent.Name())
	}
	spec := deps.AgentSpec(domain.Task{}, domain.Run{})
	if spec.Auth != agent.AuthSubscription || spec.PermissionMode != agent.PermissionDontAsk || len(spec.AllowedTools) != 1 || spec.AllowedTools[0] != "Read" {
		t.Fatalf("factory changed legacy controls: %+v", spec)
	}
	if _, err := cfg.SkillSet.Resolve(); err != nil {
		t.Fatalf("explicit none became an incompatible package: %v", err)
	}
	if !strings.Contains(spec.Env[0], GuestHome) {
		t.Fatal("factory changed the environment-local sign-in home")
	}
}
