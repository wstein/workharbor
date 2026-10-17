// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

// jsonInvocations covers success, every refusal path and the usage errors.
func jsonInvocations() [][]string {
	out := [][]string{
		{"version"}, {"--version"}, {"help"}, {"completion", "bash"}, {"completion"}, {"completion", "tcsh"},
		{"version", "extra"}, {"nonsense"}, {"ws", "nonsense"}, {"host", "service", "nope"}, {"--bogus"},
		{"--color", "sometimes", "version"}, {"ls", "--bogus"}, {"init", "--bogus"}, {"init", "--name"},
		{"bad\x1b[2J\u202e\u009b\nword"}, {"ws"}, {"host"},
	}
	for _, e := range commands.Table() {
		for _, sp := range spellings(e) {
			out = append(out, sp)
		}
	}
	return out
}

func TestCommandEnvelopesMatchSchema(t *testing.T) {
	schema := compileSchema(t)
	for _, args := range jsonInvocations() {
		withJSON := append(append([]string{}, args...), "--json")
		got := run(t, nil, withJSON...)
		env := decodeEnvelope(t, got.stdout) // exactly one document, nothing else
		doc, _ := jsonUnmarshal(got.stdout)
		if err := schema.Validate(doc); err != nil {
			t.Errorf("%q: %v\n%s", withJSON, err, got.stdout)
		}
		if env["exit_code"] != float64(got.code) {
			t.Errorf("%q: exit_code %v != process status %d", withJSON, env["exit_code"], got.code)
		}
		if got.code != 0 && !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%q: failure without a whr: line: %q", withJSON, got.err)
		}
		if got.code == 0 && got.err != "" {
			t.Errorf("%q: success wrote to stderr: %q", withJSON, got.err)
		}
	}
}

func TestCommandJSONStdoutPurity(t *testing.T) {
	// Raw hostile bytes never reach stdout in JSON mode, in any invocation.
	for _, args := range jsonInvocations() {
		got := run(t, nil, append(append([]string{}, args...), "--json")...)
		for _, r := range got.stdout {
			if r == '\x1b' || r == '\u202e' || r == '\u009b' || r == '\x00' || r == '\a' {
				t.Errorf("%q: raw control rune U+%04X on stdout: %q", args, r, got.stdout)
			}
		}
		if strings.Count(got.stdout, "\n") != 1 || !strings.HasSuffix(got.stdout, "\n") {
			t.Errorf("%q: stdout must be one line with one newline: %q", args, got.stdout)
		}
	}
	// Human mode: stdout carries data only, and failures leave it empty.
	for _, args := range [][]string{{"nonsense"}, {"ls"}, {"--bogus"}, {"completion"}} {
		if got := run(t, nil, args...); got.stdout != "" || got.code != 64 {
			t.Errorf("%q: stdout %q exit %d", args, got.stdout, got.code)
		}
	}
	// Human errors never carry raw control bytes either.
	for _, args := range jsonInvocations() {
		if got := run(t, nil, args...); strings.ContainsAny(got.err, "\x1b\x00\a\u202e\u009b") || strings.Contains(strings.TrimSuffix(got.err, "\n"), "\r") {
			t.Errorf("%q: raw control bytes on stderr: %q", args, got.err)
		}
	}
}

func TestColorFlagsEndToEnd(t *testing.T) {
	type env map[string]string
	runColor := func(tty bool, e env, args ...string) result {
		var out, errOut bytes.Buffer
		code := commands.Execute(context.Background(), args, commands.Env{
			Stdout: &out, Stderr: &errOut,
			Getenv:     func(k string) string { return e[k] },
			IsTerminal: func(io.Writer) bool { return tty },
		}, commands.DefaultHandlers())
		return result{code, out.String(), errOut.String()}
	}
	const red = "\x1b[1;31mwhr:\x1b[0m"
	cases := []struct {
		name string
		got  result
		want bool
	}{
		{"auto on a terminal", runColor(true, nil, "ls"), true},
		{"auto off a terminal", runColor(false, nil, "ls"), false},
		{"auto with NO_COLOR", runColor(true, env{"NO_COLOR": "1"}, "ls"), false},
		{"always off a terminal", runColor(false, nil, "--color", "always", "ls"), true},
		{"always with NO_COLOR", runColor(false, env{"NO_COLOR": "1"}, "--color=always", "ls"), true},
		{"never on a terminal", runColor(true, nil, "--color", "never", "ls"), false},
		{"--no-color on a terminal", runColor(true, nil, "--no-color", "ls"), false},
		{"--no-color beats always", runColor(true, nil, "--color", "always", "--no-color", "ls"), false},
	}
	for _, tc := range cases {
		if tc.got.code != 64 || tc.got.stdout != "" {
			t.Errorf("%s: exit %d stdout %q", tc.name, tc.got.code, tc.got.stdout)
		}
		if has := strings.Contains(tc.got.err, red); has != tc.want {
			t.Errorf("%s: colored=%v, want %v: %q", tc.name, has, tc.want, tc.got.err)
		}
	}
	// Data on stdout is never colored.
	if got := runColor(true, nil, "--color", "always", "version"); strings.Contains(got.stdout, "\x1b") {
		t.Errorf("version output is colored: %q", got.stdout)
	}
	if got := runColor(true, nil, "--color", "always", "ls", "--json"); strings.Contains(got.stdout, "\x1b") {
		t.Errorf("JSON output is colored: %q", got.stdout)
	}
}
