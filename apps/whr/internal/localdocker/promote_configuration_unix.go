// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Preview owns only temporary P. The receipt-bound durable copy is created
// exclusively after all consent and preflight, before external effects.
func (p *LocalPromotion) persistConfiguration() error {
	op, e := p.store.operationDirectory(p.receipt.OperationID)
	if e != nil {
		return e
	}
	defer op.Close()
	if e = unix.Mkdirat(int(op.Fd()), "promote", 0700); e != nil {
		return stageConflict("promotion persistent configuration directory retained or unavailable")
	}
	fd, e := unix.Openat(int(op.Fd()), "promote", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("promotion persistent configuration directory changed")
	}
	defer unix.Close(fd)
	out, e := unix.Openat(fd, "devcontainer.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return stageConflict("promotion persistent configuration target retained or unavailable")
	}
	f := os.NewFile(uintptr(out), "promote/devcontainer.json")
	_, e = f.Write(p.configurationBytes)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return stageConflict("promotion persistent configuration publication failed")
	}
	if unix.Fsync(fd) != nil || op.Sync() != nil {
		return stageConflict("promotion persistent configuration durability uncertain")
	}
	p.persistentConfiguration = true
	return p.checkPersistedConfiguration()
}
func (p *LocalPromotion) checkPersistedConfiguration() error {
	op, e := p.store.operationDirectory(p.receipt.OperationID)
	if e != nil {
		return e
	}
	defer op.Close()
	fd, e := unix.Openat(int(op.Fd()), "promote", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("promotion persistent configuration directory changed")
	}
	defer unix.Close(fd)
	var dir unix.Stat_t
	if unix.Fstat(fd, &dir) != nil || int(dir.Uid) != os.Getuid() || dir.Mode&0777 != 0700 {
		return stageConflict("promotion persistent configuration directory ownership or mode")
	}
	target, e := unix.Openat(fd, "devcontainer.json", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("promotion persistent configuration unavailable")
	}
	f := os.NewFile(uintptr(target), "promote/devcontainer.json")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(target, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Size != int64(len(p.configurationBytes)) {
		return stageConflict("promotion persistent configuration identity changed")
	}
	data, e := io.ReadAll(io.LimitReader(f, int64(len(p.configurationBytes))+1))
	if e != nil || !bytes.Equal(data, p.configurationBytes) || promotionBytesDigest(data) != p.receipt.EffectiveConfigDigest {
		return stageConflict("promotion persistent configuration digest changed")
	}
	return nil
}

func (p *LocalPromotion) activatePromotionRunner(ctx context.Context) (string, error) {
	paths := map[string]string{}
	for name, tool := range stageTrustTools(p.runner.Tools) {
		paths[name] = tool.Path
	}
	root := filepath.Join(p.root, "apply-phase")
	if err := os.Mkdir(root, 0700); err != nil {
		return "", stageConflict("promotion apply runner directory unavailable")
	}
	r, err := newStageRunner(ctx, root, p.endpoint.Endpoint, paths)
	if err != nil {
		return "", err
	}
	if err = admitStageToolVersions(ctx, r); err != nil {
		return "", err
	}
	if stageDigest(stageTrustTools(r.Tools)) != stageDigest(stageTrustTools(p.receipt.ToolManifest)) {
		return "", stageConflict("promotion admitted tool changed before apply")
	}
	if stageDigest(stageTrustEnvironment(r.Environment)) != stageDigest(stageTrustEnvironment(p.runner.Environment)) {
		return "", stageConflict("promotion admitted environment changed before apply")
	}
	r.Policy = p.runner.Policy
	driver, err := r.DockerDriver("promote", p.receipt.Verification.ImageID)
	if err != nil {
		return "", err
	}
	p.runner = r
	p.receipt.ToolManifest = r.Tools
	p.invocation.UserData = filepath.Join(root, "user-data")
	return driver, nil
}
