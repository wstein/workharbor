// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"io"
	"strings"
	"testing"
)

type removalFixture struct {
	result  localdocker.RemovalResult
	applies int
	err     error
}

func (f *removalFixture) Preview() localdocker.RemovalResult { return f.result }
func (f *removalFixture) Apply(context.Context) (localdocker.RemovalResult, error) {
	f.applies++
	return f.result, f.err
}
func removalHandlers(f *removalFixture, n *int, o *localdocker.RemovalOptions) map[string]commands.Handler {
	h := baseHandlers()
	h["workspace.rm"] = commands.LocalRmHandler(func(_ context.Context, v localdocker.RemovalOptions) (commands.RemovalTransaction, error) {
		*n++
		*o = v
		return f, nil
	})
	return h
}
func removalResult() localdocker.RemovalResult {
	id := strings.Repeat("a", 64)
	return localdocker.RemovalResult{PreserveHomeVolume: true, Containers: []localdocker.RemovalContainer{{ID: id, Name: "sample\x1b[31m", Role: "primary"}}, Volumes: []localdocker.RemovalVolume{}, PreservedHomeVolumes: []string{"home"}, Actions: []localdocker.RemovalAction{{Kind: "container", Target: id}}}
}
func TestLocalRmRejectsInvalidArgumentsBeforeBackend(t *testing.T) {
	f := &removalFixture{result: removalResult()}
	n := 0
	var o localdocker.RemovalOptions
	h := removalHandlers(f, &n, &o)
	for _, a := range [][]string{{"rm"}, {"rm", "--local"}, {"rm", "--local", "one", "two"}, {"rm", "--local", "one", "--role=temp"}, {"rm", "--local", "--role=primary"}, {"rm", "--local", "--role="}, {"rm", "--local", "--label="}, {"rm", "--local", "--label==bad"}} {
		if g := run(t, h, a...); g.code != 64 {
			t.Errorf("%v: %+v", a, g)
		}
	}
	if n != 0 || f.applies != 0 {
		t.Fatal("invalid args reached backend")
	}
}
func TestLocalRmNonInteractiveRequiresYes(t *testing.T) {
	f := &removalFixture{result: removalResult()}
	n := 0
	var o localdocker.RemovalOptions
	h := removalHandlers(f, &n, &o)
	for _, a := range [][]string{{}, {"--json"}, {"--volumes"}, {"--volumes", "--force"}} {
		if g := run(t, h, append([]string{"rm", "--local", "one"}, a...)...); g.code != 70 {
			t.Errorf("%v: %+v", a, g)
		}
	}
	if n != 0 || f.applies != 0 {
		t.Fatal("unconfirmed reached backend")
	}
}
func TestLocalRmDryRunExecutesNoMutations(t *testing.T) {
	f := &removalFixture{result: removalResult()}
	n := 0
	var o localdocker.RemovalOptions
	h := removalHandlers(f, &n, &o)
	for _, p := range [][]string{{"rm"}, {"ws", "rm"}, {"workspace", "rm"}} {
		g := run(t, h, append(p, "--local", "--role=temp", "--dry-run", "--json")...)
		if g.code != 0 || f.applies != 0 || o.Role != "temp" {
			t.Fatalf("%+v", g)
		}
		if err := compileSchema(t).Validate(decodeEnvelope(t, g.stdout)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(g.stdout, `"dry_run":true`) || !strings.Contains(g.stdout, `"applied":false`) {
			t.Fatal(g.stdout)
		}
	}
}
func TestLocalRmInteractiveConfirmationAndEscapedPreview(t *testing.T) {
	for _, answer := range []string{"yes\n", "y\n", "no\n", "yes please\n", "", strings.Repeat("y", 1025) + "\n", "yes"} {
		f := &removalFixture{result: removalResult()}
		f.result.Volumes = []localdocker.RemovalVolume{{Name: "home\x1b", Mounts: []localdocker.RemovalMount{{ContainerID: strings.Repeat("a", 64), Destination: "/home/captain\n"}}}}
		n := 0
		var o localdocker.RemovalOptions
		var out, diag bytes.Buffer
		c := commands.Execute(context.Background(), []string{"rm", "--local", "one", "--volumes", "--force", "--no-color"}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(answer), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, removalHandlers(f, &n, &o))
		yes := answer == "yes\n" || answer == "y\n"
		if yes && (c != 0 || f.applies != 1) || !yes && (c != 70 || f.applies != 0) {
			t.Errorf("answer %q code %d applies %d", answer, c, f.applies)
		}
		if strings.ContainsAny(out.String()+diag.String(), "\x1b") || !strings.Contains(diag.String(), "/home/captain<U+000A>") || strings.Count(diag.String(), "[y/N]") != 1 {
			t.Fatalf("bad preview %q", diag.String())
		}
	}
}
func TestLocalRmPartialVolumeFailureExits73(t *testing.T) {
	f := &removalFixture{result: removalResult(), err: exitcode.Errorf(exitcode.PathPartialFailure, "volume remains").WithRemediation("inspect remaining home volume before retrying")}
	f.result.Actions[0].Applied = true
	f.result.Actions = append(f.result.Actions, localdocker.RemovalAction{Kind: "volume", Target: "home", Attempted: true})
	n := 0
	var o localdocker.RemovalOptions
	g := run(t, removalHandlers(f, &n, &o), "rm", "--local", "one", "--volumes", "--force", "--yes", "--json")
	if g.code != 73 || f.applies != 1 || !o.Volumes || !o.Force {
		t.Fatalf("%+v", g)
	}
	env := decodeEnvelope(t, g.stdout)
	if env["result"] == nil {
		t.Fatal("partial result lost")
	}
	if err := compileSchema(t).Validate(env); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRmContractAndRemoteRefusal(t *testing.T) {
	n := 0
	var o localdocker.RemovalOptions
	f := &removalFixture{result: removalResult()}
	h := removalHandlers(f, &n, &o)
	for _, a := range [][]string{{"rm", "one"}, {"workspace", "rm", "--role=temp", "--yes", "--json"}, {"ws", "rm", "--label=workspace", "--dry-run"}} {
		g := run(t, h, a...)
		if g.code != 64 || !strings.Contains(g.stdout+g.err, "owner_api") {
			t.Fatalf("%+v", g)
		}
	}
	if n != 0 {
		t.Fatal("remote invocation reached local backend")
	}
	g := run(t, h, "rm", "--help")
	for _, word := range []string{"--local", "--role", "--label", "--volumes", "--force", "--dry-run", "--yes", "owner_api"} {
		if !strings.Contains(g.stdout, word) {
			t.Errorf("help lacks %s", word)
		}
	}
}

func TestLocalRmLabelSelectionAndContainerOnlyPreservesHome(t *testing.T) {
	n := 0
	var o localdocker.RemovalOptions
	f := &removalFixture{result: removalResult()}
	g := run(t, removalHandlers(f, &n, &o), "rm", "--local", "--label=anchorage-temp=1", "--yes", "--json")
	if g.code != 0 || n != 1 || f.applies != 1 || o.Label != "anchorage-temp=1" || o.Volumes || o.Force {
		t.Fatalf("%+v options=%+v", g, o)
	}
	if !strings.Contains(g.stdout, `"preserve_home_volume":true`) {
		t.Fatal(g.stdout)
	}
}

func TestLocalRmSchemaRejectsMalformedResults(t *testing.T) {
	schema := compileSchema(t)
	envelope := func(result any) map[string]any {
		return map[string]any{"schema_version": 1, "command": "workspace.rm", "ok": true, "exit_code": 0, "result": result}
	}
	if err := schema.Validate(envelope(map[string]any{})); err == nil {
		t.Fatal("accepted missing removal fields")
	}
	// Pre-parse failure has no result and remains a valid envelope.
	g := run(t, nil, "rm", "--bogus", "--json")
	if err := schema.Validate(decodeEnvelope(t, g.stdout)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRmForceAlonePreservesHome(t *testing.T) {
	n := 0
	var o localdocker.RemovalOptions
	f := &removalFixture{result: removalResult()}
	g := run(t, removalHandlers(f, &n, &o), "rm", "--local", "one", "--force", "--yes", "--json")
	if g.code != 0 || f.applies != 1 || o.Volumes || !o.Force || !strings.Contains(g.stdout, `"preserve_home_volume":true`) {
		t.Fatalf("%+v options=%+v", g, o)
	}
}

func TestLocalRmJSONTerminalStillRequiresYes(t *testing.T) {
	n := 0
	var o localdocker.RemovalOptions
	f := &removalFixture{result: removalResult()}
	var out, diag bytes.Buffer
	code := commands.Execute(context.Background(), []string{"rm", "--local", "one", "--json"}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader("yes\n"), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, removalHandlers(f, &n, &o))
	if code != 70 || n != 0 || f.applies != 0 || strings.Contains(diag.String(), "[y/N]") {
		t.Fatalf("code %d prepares %d applies %d: %s", code, n, f.applies, diag.String())
	}
}

func TestLocalRmBackendRefusalPreservesResult(t *testing.T) {
	expected := removalResult()
	ee := exitcode.Errorf(exitcode.PathConflict, "foreign container")
	ee.Result = expected
	h := baseHandlers()
	h["workspace.rm"] = commands.LocalRmHandler(func(context.Context, localdocker.RemovalOptions) (commands.RemovalTransaction, error) { return nil, ee })
	g := run(t, h, "rm", "--local", "one", "--dry-run", "--json")
	if g.code != 68 || decodeEnvelope(t, g.stdout)["result"] == nil {
		t.Fatalf("%+v", g)
	}
	if err := compileSchema(t).Validate(decodeEnvelope(t, g.stdout)); err != nil {
		t.Fatal(err)
	}
}
