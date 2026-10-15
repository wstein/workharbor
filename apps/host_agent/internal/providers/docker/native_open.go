// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// nativeOptions is trusted host setup, never protocol/config-repository input.
// Opening prepares private host plumbing only; no Docker mutation is performed.
type nativeOptions struct {
	Policy                                           DriverPolicy
	Images                                           map[string]AdmittedImage
	Registry                                         *Registry
	Parser                                           config.JSONC
	Target                                           *Target
	CheckBuild                                       func(context.Context, BuildCall) error
	DockerPath, Socket, BuildxPath, PrivateDirectory string
	Scope                                            Scope
	Check                                            func(context.Context) error
}
type nativeLifetime struct {
	once                  sync.Once
	err                   error
	mu                    sync.Mutex
	closed                bool
	active                sync.WaitGroup
	ctx                   context.Context
	cancel                context.CancelFunc
	root, program, socket string
	env                   []string
}

func physicalExecutable(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	actual, e := filepath.EvalSymlinks(path)
	info, se := os.Lstat(path)
	return e == nil && se == nil && actual == path && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 && info.Mode().Perm()&0022 == 0
}
func (l *nativeLifetime) enter(ctx context.Context) (context.Context, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.ctx.Err() != nil || ctx.Err() != nil {
		return nil, nil, fail("provider_unavailable")
	}
	l.active.Add(1)
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(l.ctx, cancel)
	return child, func() { stop(); cancel(); l.active.Done() }, nil
}
func (l *nativeLifetime) arguments(args []string) []string {
	return append([]string{"--config", filepath.Join(l.root, "docker"), "--host", "unix://" + l.socket}, args...)
}
func (l *nativeLifetime) Close() error {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.cancel()
		l.mu.Unlock()
		l.active.Wait()
		if !ownedDirectory(l.root) {
			l.err = fail("provider_unavailable")
			return
		}
		l.err = os.RemoveAll(l.root)
	})
	return l.err
}

func openNativePort(ctx context.Context, o nativeOptions) (*nativePort, error) {
	if o.Check == nil {
		return nil, fail("forbidden")
	}
	if e := o.Check(ctx); e != nil {
		return nil, e
	}
	if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil || runtime.GOOS != "linux" || (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") || !o.Scope.valid() || !physicalExecutable(o.DockerPath) || !nativeDockerSocket(o.Socket) {
		return nil, fail("provider_unavailable")
	}
	if o.Target != nil {
		target := *o.Target
		o.Target = &target
	}
	root, e := registryPrivate(o.PrivateDirectory)
	if e != nil {
		return nil, e
	}
	failed := true
	defer func() {
		if failed {
			os.RemoveAll(root)
		}
	}()
	env := append(registryEnvironment(root, o.DockerPath), "DOCKER_BUILDKIT=1")
	version, e := captureMetadata(ctx, o.DockerPath, []string{"-v"}, root, env, 1024)
	if e != nil || !strings.HasPrefix(strings.TrimSpace(string(version)), "Docker version 29.8.2,") {
		return nil, fail("tool_unavailable")
	}
	var pins struct{ Buildx map[string]string }
	if json.Unmarshal(toolPins, &pins) != nil {
		return nil, fail("tool_unavailable")
	}
	if e = verifyRegistryTool(ctx, o.BuildxPath, pins.Buildx["linux_"+runtime.GOARCH+"_sha256"], pins.Buildx["version"], root, true); e != nil {
		return nil, e
	}
	// Docker's plugin lookup is confined to the private config, with pinned bytes.
	plugins := filepath.Join(root, "docker", "cli-plugins")
	if os.Mkdir(plugins, 0700) != nil {
		return nil, fail("tool_unavailable")
	}
	raw, e := os.ReadFile(o.BuildxPath)
	sum := sha256.Sum256(raw)
	if e != nil || hex.EncodeToString(sum[:]) != pins.Buildx["linux_"+runtime.GOARCH+"_sha256"] {
		return nil, fail("tool_unavailable")
	}
	plugin := filepath.Join(plugins, "docker-buildx")
	f, e := os.OpenFile(plugin, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		return nil, fail("tool_unavailable")
	}
	_, we := f.Write(raw)
	se := f.Sync()
	ce := f.Close()
	if we != nil || se != nil || ce != nil {
		return nil, fail("tool_unavailable")
	}
	parent, cancel := context.WithCancel(ctx)
	life := &nativeLifetime{ctx: parent, cancel: cancel, root: root, program: o.DockerPath, socket: o.Socket, env: env}
	p := &nativePort{scope: o.Scope, check: o.Check, lifetime: life, policy: copyPolicy(o.Policy), images: copyImages(o.Images), registry: o.Registry, parser: o.Parser, target: o.Target, checkBuild: o.CheckBuild}
	p.query = func(ctx context.Context, args []string) ([]byte, error) {
		child, done, e := life.enter(ctx)
		if e != nil {
			return nil, e
		}
		defer done()
		if e = o.Check(child); e != nil {
			return nil, e
		}
		return captureMetadata(child, life.program, life.arguments(args), life.root, life.env, metadataLimit)
	}
	p.stream = func(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
		child, done, e := life.enter(ctx)
		if e != nil {
			return e
		}
		defer done()
		if e = o.Check(child); e != nil {
			return e
		}
		return nativeProcess(child, life.program, life.arguments(args), life.root, life.env, in, out)
	}
	if e = o.Check(ctx); e != nil {
		life.Close()
		return nil, e
	}
	failed = false
	return p, nil
}
