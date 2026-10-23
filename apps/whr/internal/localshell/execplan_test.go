// SPDX-License-Identifier: EUPL-1.2
package localshell

import (
	"reflect"
	"strings"
	"testing"
)

func validMetadata() Metadata {
	return Metadata{ID: strings.Repeat("a", 64), Name: "sample", User: "captain", Directory: "/workspace", Term: "xterm-256color"}
}
func TestLocalShellArgvMatchesFR004(t *testing.T) {
	plan, err := NewExecPlan("unix:///test/docker.sock", validMetadata(), "fish")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--host=unix:///test/docker.sock", "exec", "--interactive", "--tty", "--detach-keys=ctrl-],ctrl-^", "--user=captain", "--workdir=/workspace", "--env=TERM=xterm-256color", "--env=WHR_LOCAL_WORKSPACE=sample", "--env=WHR_LOCAL_MODE=1", strings.Repeat("a", 64), "fish", "-l"}
	if got := plan.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot %q\nwant%q", got, want)
	}
}
func TestLocalShellHostileValuesNeverEvaluateShell(t *testing.T) {
	m := validMetadata()
	m.Directory = "/workspace/$(touch never); literal\x1b\u202e"
	p, err := NewExecPlan("unix:///test/socket", m, "bash")
	if err != nil || p.Args()[6] != "--workdir="+m.Directory {
		t.Fatalf("path lost argv boundaries: %+v %v", p, err)
	}
	for _, change := range []func(*Metadata){func(m *Metadata) { m.Name = "bad;name" }, func(m *Metadata) { m.ID = "--help" }, func(m *Metadata) { m.User = "captain;cmd" }, func(m *Metadata) { m.Directory = "/bad\npath" }, func(m *Metadata) { m.Term = "bad TERM" }} {
		m := validMetadata()
		change(&m)
		if _, err := NewExecPlan("unix:///test/socket", m, "fish"); err == nil {
			t.Fatalf("invalid metadata accepted: %+v", m)
		}
	}
	for _, shell := range []string{"sh", "fish -l", "/bin/fish", "fish;id"} {
		if _, err := NewExecPlan("unix:///test/socket", validMetadata(), shell); err == nil {
			t.Fatalf("shell accepted %q", shell)
		}
	}
	if _, err := NewExecPlan("ssh://remote", validMetadata(), "fish"); err == nil {
		t.Fatal("remote endpoint accepted")
	}
}
func TestLocalShellContainerEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "canary-secret")
	t.Setenv("WHR_LOCAL_MODE", "wrong")
	p, err := NewExecPlan("unix:///test/socket", validMetadata(), "nu")
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, arg := range p.Args() {
		if strings.HasPrefix(arg, "--env=") {
			env = append(env, arg)
		}
	}
	if !reflect.DeepEqual(env, []string{"--env=TERM=xterm-256color", "--env=WHR_LOCAL_WORKSPACE=sample", "--env=WHR_LOCAL_MODE=1"}) {
		t.Fatalf("%v", env)
	}
}
