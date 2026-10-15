// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

type metadataBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	cancel   context.CancelFunc
	overflow bool
}

func (b *metadataBuffer) Write(p []byte) (int, error) {
	if int64(b.buffer.Len())+int64(len(p)) > b.limit {
		b.overflow = true
		b.cancel()
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(p)
}

// captureMetadata is private to the fixed registry tool commands. Cancellation
// and overflow kill the owned child; Run waits/reaps it before returning. Tools
// receive no credential helpers or executable repository-controlled source.
func captureMetadata(ctx context.Context, program string, args []string, dir string, env []string, limit int64) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok || limit <= 0 || limit > metadataLimit {
		return nil, fail("metadata_unavailable")
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &metadataBuffer{limit: limit, cancel: cancel}
	c := exec.CommandContext(child, program, args...)
	c.Dir = dir
	c.Env = env
	c.Stdout = out
	c.Stderr = io.Discard
	c.WaitDelay = time.Second
	if e := ownedProcessGroup(c); e != nil {
		return nil, e
	}
	if e := c.Run(); e != nil || out.overflow || child.Err() != nil {
		return nil, fail("metadata_unavailable")
	}
	return append([]byte{}, out.buffer.Bytes()...), nil
}
