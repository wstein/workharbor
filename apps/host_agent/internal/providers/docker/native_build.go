// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// buildIID is allocation evidence only, never an official up completion receipt.
func buildIID(path string) (string, error) {
	if !ownedDirectory(filepath.Dir(path)) {
		return "", fail("configuration_changed")
	}
	info, e := os.Lstat(path)
	if e != nil || !ownedRegular(info) || info.Size() != 71 {
		return "", fail("configuration_changed")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", fail("configuration_changed")
	}
	actual, se := f.Stat()
	raw, re := io.ReadAll(io.LimitReader(f, 72))
	ce := f.Close()
	current, le := os.Lstat(path)
	if se != nil || re != nil || ce != nil || le != nil || !ownedRegular(current) || !os.SameFile(info, actual) || !os.SameFile(info, current) || len(raw) != 71 || !strings.HasPrefix(string(raw), "sha256:") || !hexID.Match(raw[7:]) {
		return "", fail("configuration_changed")
	}
	return string(raw), nil
}
func (p *nativePort) build(ctx context.Context, c Call, in io.Reader, out io.Writer) error {
	if p.lifetime == nil || p.checkBuild == nil || c.Build == nil {
		return fail("configuration_changed")
	}
	if e := p.checkBuild(ctx, *c.Build); e != nil {
		return e
	}
	if e := p.authorize(ctx); e != nil {
		return e
	}
	p.stateMu.Lock()
	if p.buildStarted || p.allocation != nil || p.target != nil {
		p.stateMu.Unlock()
		return fail("runtime_unknown")
	}
	p.buildStarted = true
	p.stateMu.Unlock()
	dir, e := os.MkdirTemp(p.lifetime.root, "build-")
	if e != nil {
		return fail("provider_unavailable")
	}
	defer os.RemoveAll(dir) // Private build witness only; never project or Docker data.
	iid := filepath.Join(dir, "image.iid")
	args, e := nativeArguments(c, "")
	if e != nil {
		return e
	}
	args = append(args[:len(args)-1], "--iidfile", iid, c.Build.Context)
	if e = p.authorize(ctx); e != nil {
		return e
	}
	if e = p.stream(ctx, args, in, out); e != nil {
		return e
	}
	if e = p.authorize(ctx); e != nil {
		return e
	}
	id, e := buildIID(iid)
	if e != nil {
		return e
	}
	actual, _, e := p.localImage(ctx, id)
	if e != nil {
		return e
	}
	if actual.ID != id {
		return fail("configuration_changed")
	}
	p.stateMu.Lock()
	policy := copyPolicy(p.policy)
	p.stateMu.Unlock()
	if e = verifyMetadata(ctx, p.parser, policy.RuntimeMetadata, actual.Config.Metadata); e != nil {
		return e
	}
	tag, _, e := p.localImage(ctx, c.Build.Tag)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(actual, tag) {
		return fail("configuration_changed")
	}
	if e = p.authorize(ctx); e != nil {
		return e
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if _, exists := p.images[c.Build.Tag]; exists {
		return fail("configuration_changed")
	}
	p.images[c.Build.Tag] = copyImage(actual)
	if !p.policy.OverrideCommand {
		p.policy.StartupTail = imageCommand(actual)
	}
	p.policy.ImageRefs[c.Build.Tag] = true
	return nil
}
func (p *nativePort) BuiltImage(ctx context.Context, ref string) (AdmittedImage, bool, error) {
	if e := p.authorize(ctx); e != nil {
		return AdmittedImage{}, false, e
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.policy.Build == nil || ref != p.policy.Build.Tag {
		return AdmittedImage{}, false, nil
	}
	image, ok := p.images[ref]
	return copyImage(image), ok, nil
}
