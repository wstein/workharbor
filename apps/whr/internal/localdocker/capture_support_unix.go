// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"io"
	"path/filepath"
	"strings"
)

func stageConflict(reason string) error {
	return exitcode.Errorf(exitcode.PathConflict, "local stage %s", reason)
}

type stageContextReader struct {
	ctx context.Context
	io.Reader
}

func (r stageContextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.Reader.Read(p)
}

func stageSelectedPath(path string, selected []string) bool {
	for _, p := range selected {
		if p == "." || path == p || strings.HasPrefix(path, p+string(filepath.Separator)) || strings.HasPrefix(p, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func captureIOError(err error, reason string) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var typed *exitcode.Error
	if errors.As(err, &typed) {
		return err
	}
	return stageConflict(reason)
}
