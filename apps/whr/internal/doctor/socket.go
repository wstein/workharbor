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
// A candidate is dialed at path and reported as shown, which names environment
// sources by variable instead of value.
type candidate struct{ path, shown string }

func (r *run) candidates() []candidate {
	var out []candidate
	for _, t := range candidateTemplates {
		switch {
		case strings.HasPrefix(t, "$XDG_RUNTIME_DIR/"):
			if x := r.p.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(x) {
				out = append(out, candidate{filepath.Join(x, strings.TrimPrefix(t, "$XDG_RUNTIME_DIR/")), t})
			}
		case strings.HasPrefix(t, "~/"):
			if r.home != "" {
				p := filepath.Join(r.home, strings.TrimPrefix(t, "~/"))
				out = append(out, candidate{p, p})
			}
		default:
			out = append(out, candidate{t, t})
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
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENAMETOOLONG):
		return "path_too_long" // longer than sockaddr_un; absent unless the path exists
	}
	return "error"
}

// dockerSocket resolves DOCKER_HOST: a unix path, or a reason it cannot be dialed.
func (r *run) dockerSocket() (path string, remote bool, ok bool) {
	h := r.dockerHost
	if rest, found := cutUnix(h); found {
		if filepath.IsAbs(rest) {
			return rest, false, true
		}
		return "", false, false
	}
	return "", true, false
}

func (r *run) socket() Check {
	const id = "container.socket"
	var paths []candidate
	source := "candidates"
	if r.dockerHost != "" {
		p, remote, ok := r.dockerSocket()
		switch {
		case ok:
			paths, source = []candidate{{p, "$DOCKER_HOST"}}, "DOCKER_HOST"
		case remote && !isUnixHost(r.dockerHost):
			scheme := dockerScheme(r.dockerHost)
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
	for _, c := range paths {
		p := c.path
		if r.b.remaining() == 0 {
			return exhausted(id)
		}
		dctx, cancel := context.WithTimeout(r.ctx, r.b.limit(dialCap))
		err := r.p.DialUnix(dctx, p)
		cancel()
		if err == nil {
			r.selectedSocket = p
			return passed(id, "A Docker-compatible socket accepted a connection; nothing was sent.", map[string]any{"path": c.shown, "source": source})
		}
		res := classify(err)
		if res == "path_too_long" {
			res = "error"
			if _, serr := r.lstat(p); errors.Is(serr, os.ErrNotExist) || errors.Is(serr, syscall.ENOTDIR) {
				res = "not_found"
			}
		}
		if res == "timeout" || res == "error" {
			unmeasured = true
		}
		tried = append(tried, map[string]any{"path": c.shown, "result": res})
	}
	ev := map[string]any{"source": source, "tried": tried}
	if unmeasured {
		return unknown(id, reasonNotMeasurable, "A candidate socket neither accepted nor refused a connection, so absence is not proven.", socketFix, ev)
	}
	return failed(id, "No Docker-compatible socket accepted a connection.", socketFix, ev)
}

// dockerScheme names the scheme of DOCKER_HOST only if it is a known one; any
// other text is an environment value and is never echoed.
func dockerScheme(h string) string {
	s, _, ok := strings.Cut(h, "://")
	if !ok {
		return "other"
	}
	switch s = strings.ToLower(s); s {
	case "tcp", "ssh", "http", "https", "npipe", "fd", "unix":
		return s
	}
	return "other"
}

// cutUnix strips a case-insensitive unix:// scheme.
func cutUnix(h string) (string, bool) {
	if len(h) >= 7 && strings.EqualFold(h[:7], "unix://") {
		return h[7:], true
	}
	return h, false
}

func isUnixHost(h string) bool { return len(h) >= 5 && strings.EqualFold(h[:5], "unix:") }

// ResolveLocalSocket shares doctor's exclusive DOCKER_HOST and ordered candidate
// selection with local commands. The raw Unix endpoint is for the Docker argv,
// never diagnostics. Only the socket check runs; no config or Git checks run.
// Callers supply their shared operation deadline in ctx.
func ResolveLocalSocket(ctx context.Context, p Ports) (string, Check) {
	r := newRun(ctx, p.withDefaults())
	check := r.socket()
	if check.Status != StatusPass {
		return "", check
	}
	return "unix://" + r.selectedSocket, check
}
