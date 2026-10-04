package apple

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/wstein/workharbor/internal/runtime"
)

// lookPath finds the container CLI; a test replaces it.
var lookPath = exec.LookPath

// InteractiveCommand returns the program and arguments that run req in the
// running environment envID with the caller's own terminal attached (`container
// exec -t -i`): the human's `whr` process replaces itself with it, so no
// supervisor, pty, pipe or file of whr lies between the terminal and the guest
// (design §7.3, "The sign-in shell"). It is pure: it starts nothing, and it needs
// no adapter, because the caller is the `whr` command, not the supervisor.
//
// The variables go as -e flags, on the command line: they must hold no secret (an
// --env-file pipe cannot survive the exec, and a file would stay behind). Entries
// are checked as for the supervisor's own execs. The flags (-t, -i, -u, -w, -e)
// are those `container exec --help` lists for container 1.5.0; that this command
// line gives a working terminal on a host is unverified, it was not run. Two more
// parts are unverified (review of #281, L2): that the `container` CLI reaches the
// services that own the environment from a dedicated whr account outside the
// supervisor user's desktop session (spike #82 ran it from the developer's own
// session), and what Apple's container services do with the terminal's bytes on
// their way into the VM (the same path the API key's environment takes, D48).
func InteractiveCommand(envID string, req runtime.InteractiveRequest) (bin string, argv []string, err error) {
	if envID == "" || strings.HasPrefix(envID, "-") {
		return "", nil, errors.New("apple: an interactive command needs an environment ID")
	}
	if len(req.Cmd) == 0 {
		return "", nil, errors.New("apple: an interactive command needs a command")
	}
	for i, e := range req.Env {
		k, v, ok := strings.Cut(e, "=")
		switch {
		case !ok:
			return "", nil, fmt.Errorf("%w: entry %d has no '='", ErrBadEnv, i)
		case !envKey.MatchString(k):
			return "", nil, fmt.Errorf("%w: entry %d has a key that is not a plain name", ErrBadEnv, i)
		case strings.ContainsAny(v, "\n\r\x00"):
			return "", nil, fmt.Errorf("%w: the value of %s spans lines or holds NUL", ErrBadEnv, k)
		}
	}
	bin, err = lookPath("container")
	if err != nil {
		return "", nil, fmt.Errorf("apple: %w", err)
	}
	argv = []string{bin, "exec", "-t", "-i"}
	if req.User != "" {
		argv = append(argv, "-u", req.User)
	}
	if req.Dir != "" {
		argv = append(argv, "-w", req.Dir)
	}
	for _, e := range req.Env {
		argv = append(argv, "-e", e)
	}
	argv = append(argv, envID)
	argv = append(argv, req.Cmd...)
	return bin, argv, nil
}
