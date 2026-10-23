// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"bytes"
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"strings"
	"testing"
)

func TestLocalShellWorkingDirFallbackWarnsBeforeHandover(t *testing.T) {
	for _, tty := range []bool{true, false} {
		p, _ := runFixture(t, 0, true)
		var stream bytes.Buffer
		p.Exec = func(_ context.Context, plan ExecPlan, _ Streams) (int, error) {
			if plan.Args()[6] != "--workdir=/fallback" {
				t.Fatal("wrong fallback argv")
			}
			stream.WriteString("CHILD\x1b[31m\u202e\n")
			return 0, nil
		}
		status, err := Run(context.Background(), "sample", Options{Stderr: &stream, StderrTerminal: tty}, p)
		want := ""
		if tty {
			want = "whr: local shell in sample (no grant; host Docker access; detach keys ctrl-] ctrl-^)\n"
		}
		want += "whr: warning: no workspace folder mount in sample; starting in /fallback\nCHILD\x1b[31m\u202e\n"
		if status != 0 || err != nil || stream.String() != want {
			t.Fatalf("tty%v: %q %v", tty, stream.String(), err)
		}
	}
	for _, dir := range []string{"relative", "/bad\x00path", "/bad\npath", "/" + strings.Repeat("x", 4096)} {
		p, calls := runFixture(t, 0, true)
		var stream bytes.Buffer
		p.Inspect = func(context.Context, string, string) ([]byte, error) {
			return inspected(t, func(m map[string]any) { m["Config"].(map[string]any)["WorkingDir"] = dir }), nil
		}
		_, err := Run(context.Background(), "sample", Options{Stderr: &stream, StderrTerminal: true}, p)
		if exitcode.FromError(err) != 68 || stream.Len() != 0 || strings.Contains(strings.Join(*calls, ","), "exec") {
			t.Fatalf("invalidfallback emitted/start: %v %q %v", err, stream.String(), *calls)
		}
	}
	p, _ := runFixture(t, 0, true)
	var stream bytes.Buffer
	p.Inspect = func(context.Context, string, string) ([]byte, error) {
		return inspected(t, func(m map[string]any) {
			m["Mounts"] = []any{map[string]any{"Source": "/host/project", "Destination": "/workspace"}}
		}), nil
	}
	if _, err := Run(context.Background(), "sample", Options{Stderr: &stream, StderrTerminal: true}, p); err != nil || strings.Contains(stream.String(), "warning") {
		t.Fatalf("matched mount warning: %q %v", stream.String(), err)
	}
}
func TestLocalShellFallbackWarningEscapesDirectory(t *testing.T) {
	p, _ := runFixture(t, 0, true)
	var stream bytes.Buffer
	p.Inspect = func(context.Context, string, string) ([]byte, error) {
		return inspected(t, func(m map[string]any) { m["Config"].(map[string]any)["WorkingDir"] = "/bad\x1b\u202e" }), nil
	}
	if _, err := Run(context.Background(), "sample", Options{Stderr: &stream}, p); err != nil {
		t.Fatal(err)
	}
	if got, want := stream.String(), "whr: warning: no workspace folder mount in sample; starting in /bad<U+001B><U+202E>\n"; got != want {
		t.Fatalf("%q != %q", got, want)
	}
}

func TestLocalShellAmbiguousMountFallsBackAndWarns(t *testing.T) {
	p, _ := runFixture(t, 0, true)
	p.Inspect = func(context.Context, string, string) ([]byte, error) {
		return inspected(t, func(m map[string]any) {
			m["Mounts"] = []any{map[string]any{"Source": "/host/project", "Destination": "/first"}, map[string]any{"Source": "/host/project", "Destination": "/second"}}
		}), nil
	}
	var stream bytes.Buffer
	p.Exec = func(_ context.Context, plan ExecPlan, _ Streams) (int, error) {
		if plan.metadata.DirectorySource != directoryFallback || plan.Args()[6] != "--workdir=/fallback" {
			t.Fatalf("ambiguous mount selected: %+v", plan)
		}
		want := "whr: warning: no workspace folder mount in sample; starting in /fallback\n"
		if stream.String() != want {
			t.Fatalf("pre-handover warning=%q, want %q", stream.String(), want)
		}
		stream.WriteString("CHILD")
		return 0, nil
	}
	got, err := Run(context.Background(), "sample", Options{Stderr: &stream}, p)
	if got != 0 || err != nil || !strings.HasSuffix(stream.String(), "\nCHILD") {
		t.Fatalf("%d %v %q", got, err, stream.String())
	}
}
