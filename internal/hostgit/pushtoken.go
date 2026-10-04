package hostgit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pipeHelper is the one credential helper a token push uses (D51, D48's handover
// pattern). Git runs a "!" helper through the shell with the action appended, and
// every child of git inherits file descriptor 3, which runPipe makes the read end
// of a pipe already holding the whole credential answer. The helper answers only
// "get" and copies the pipe, so the token is in no argv (cat and the shell's test
// are given none), no environment, no file and no URL. The pipe is read once: a
// second "get" (after a rejected credential) finds it empty and git fails
// instead of retrying with a token GitHub already refused.
const pipeHelper = `!f() { test "$1" = get && cat <&3; }; f`

// credentialAnswer is the git credential protocol's answer for the token.
func credentialAnswer(token string) string {
	return "username=x-access-token\npassword=" + token + "\n\n"
}

// attachToken makes the read end of a filled pipe file descriptor 3 of cmd, and
// of no other process: os.Pipe sets close-on-exec in the supervisor, so a child
// started elsewhere never inherits it, and cmd.ExtraFiles hands it to this one
// command alone. The returned release closes the supervisor's copy of the read
// end; call it after cmd.Start, or when the start did not happen.
func attachToken(cmd *exec.Cmd, token string) (release func(), err error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("hostgit: %w", err)
	}
	// The answer is small, far below a pipe's capacity, so this does not block.
	_, werr := w.WriteString(credentialAnswer(token))
	cerr := w.Close()
	if werr != nil || cerr != nil {
		_ = r.Close()
		return nil, fmt.Errorf("hostgit: could not fill the credential pipe")
	}
	cmd.ExtraFiles = []*os.File{r}
	return func() { _ = r.Close() }, nil
}

// runToken runs git like run, with the token (when not empty) on the pipe. Its
// error text never contains the token.
func (g *Git) runToken(ctx context.Context, dir string, extraEnv []string, token string, args ...string) ([]byte, error) {
	cmd := g.command(ctx, dir, true, extraEnv, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	release := func() {}
	if token != "" {
		var err error
		if release, err = attachToken(cmd, token); err != nil {
			return nil, err
		}
	}
	err := cmd.Start()
	release()
	if err == nil {
		err = cmd.Wait()
	}
	if err != nil {
		msg := fmt.Sprintf("git %s: %v: %s", strings.Join(redactArgs(args), " "), err, strings.TrimSpace(stderr.String()))
		if token != "" {
			msg = strings.ReplaceAll(msg, token, "[redacted]")
		}
		return nil, fmt.Errorf("%s", msg) //nolint:err113 // the scrubbed git output is the error
	}
	return stdout.Bytes(), nil
}

// redactArgs drops the credential helper's -c pair from the text of an error:
// it holds no secret, but it is noise.
func redactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "credential.helper=") {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}
