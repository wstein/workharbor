// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// candidateTemplates are the documented socket locations after DOCKER_HOST.
var candidateTemplates = []string{
	"/var/run/docker.sock",
	"$XDG_RUNTIME_DIR/docker.sock",
	"~/.orbstack/run/docker.sock",
	"~/.docker/run/docker.sock",
	"~/.colima/default/docker.sock",
	"~/.rd/docker.sock",
}

// CandidateOrder lists the discovery candidates in order, after DOCKER_HOST.
func CandidateOrder() []string { return append([]string(nil), candidateTemplates...) }

const dialCap = 500 * time.Millisecond

const socketFix = "Start an existing Docker-compatible runtime and rerun whr doctor. On macOS OrbStack is the recommended runtime; " +
	"on Linux use Docker Engine and check that your user may open its socket. whr never installs or configures a runtime. See " + runtimeDoc + "."

// candidates expands the templates; unknown home or XDG_RUNTIME_DIR drops the
// candidates that need them rather than guessing.
func (r *run) candidates() []string {
	var out []string
	for _, t := range candidateTemplates {
		switch {
		case strings.HasPrefix(t, "$XDG_RUNTIME_DIR/"):
			if x := r.p.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(x) {
				out = append(out, filepath.Join(x, strings.TrimPrefix(t, "$XDG_RUNTIME_DIR/")))
			}
		case strings.HasPrefix(t, "~/"):
			if r.home != "" {
				out = append(out, filepath.Join(r.home, strings.TrimPrefix(t, "~/")))
			}
		default:
			out = append(out, t)
		}
	}
	return out
}

func classify(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return "timeout"
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return "not_found"
	case errors.Is(err, os.ErrPermission):
		return "denied"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	}
	return "error"
}

// dockerSocket resolves DOCKER_HOST: a unix path, or a reason it cannot be dialed.
func (r *run) dockerSocket() (path string, remote bool, ok bool) {
	h := r.dockerHost
	if rest, found := strings.CutPrefix(h, "unix://"); found {
		if filepath.IsAbs(rest) {
			return rest, false, true
		}
		return "", false, false
	}
	return "", true, false
}

func (r *run) socket() Check {
	const id = "container.socket"
	var paths []string
	source := "candidates"
	if r.dockerHost != "" {
		p, remote, ok := r.dockerSocket()
		switch {
		case ok:
			paths, source = []string{p}, "DOCKER_HOST"
		case remote && !strings.HasPrefix(r.dockerHost, "unix:"):
			scheme, _, _ := strings.Cut(r.dockerHost, ":")
			if len(scheme) > 12 || scheme == "" {
				scheme = "other"
			}
			return unknown(id, reasonRemoteNotChecked, "DOCKER_HOST names a remote daemon, which is not checked.",
				"Unset DOCKER_HOST to check a local socket, or verify the remote daemon with its own tools.", map[string]any{"scheme": scheme})
		default:
			return unknown(id, reasonNotMeasurable, "DOCKER_HOST holds a unix socket path that is empty or not absolute.",
				"Set DOCKER_HOST to unix:// followed by an absolute socket path, or unset it.", nil)
		}
	} else {
		paths = r.candidates()
	}
	var tried []any
	unmeasured := false
	for _, p := range paths {
		if r.b.remaining() == 0 {
			return exhausted(id)
		}
		dctx, cancel := context.WithTimeout(r.ctx, r.b.limit(dialCap))
		err := r.p.DialUnix(dctx, p)
		cancel()
		if err == nil {
			return passed(id, "A Docker-compatible socket accepted a connection; nothing was sent.", map[string]any{"path": p, "source": source})
		}
		res := classify(err)
		if res == "timeout" || res == "error" {
			unmeasured = true
		}
		tried = append(tried, map[string]any{"path": p, "result": res})
	}
	ev := map[string]any{"source": source, "tried": tried}
	if unmeasured {
		return unknown(id, reasonNotMeasurable, "A candidate socket neither accepted nor refused a connection, so absence is not proven.", socketFix, ev)
	}
	return failed(id, "No Docker-compatible socket accepted a connection.", socketFix, ev)
}
