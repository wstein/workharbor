package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SystemFzf returns the HaveFzf and Fzf of PickOptions for the fzf on PATH.
// Nothing needs it: when it is missing, or the session is not interactive,
// Pick uses its numbered list.
func SystemFzf() (have func() bool, run func(items []string) (int, error)) {
	have = func() bool {
		_, err := exec.LookPath("fzf")
		return err == nil
	}
	run = func(items []string) (int, error) {
		path, err := exec.LookPath("fzf")
		if err != nil {
			return 0, err
		}
		var in strings.Builder
		for i, it := range items {
			fmt.Fprintf(&in, "%d\t%s\n", i, strings.NewReplacer("\n", " ", "\t", " ").Replace(it))
		}
		var out bytes.Buffer
		cmd := exec.CommandContext(context.Background(), path, "--delimiter=\t", "--with-nth=2..", "--no-multi") //nolint:gosec // the fzf the person has installed
		cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(in.String()), &out, os.Stderr
		if err := cmd.Run(); err != nil {
			return 0, err
		}
		idx, _, _ := strings.Cut(strings.TrimSpace(out.String()), "\t")
		n, err := strconv.Atoi(idx)
		if err != nil || n < 0 || n >= len(items) {
			return 0, errors.New("fzf chose nothing")
		}
		return n, nil
	}
	return have, run
}
