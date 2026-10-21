// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A source acceptance flow through the existing test-executable command helper.
// No built CLI, network namespace, provider or host-runtime acceptance is claimed.
// Transaction failure/rollback matrices and main's signal boundary stay in their
// existing scaffold and cmd/whr tests; this flow does not simulate rollback.
func TestFirstIncrementInitOfflineSource(t *testing.T) {
	dir := initFixture(t)
	home, bin := t.TempDir(), t.TempDir()
	calls := filepath.Join(bin, "external-calls")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for _, name := range []string{"git", "docker", "devcontainer", "ssh", "curl", "wget", "fish", "bash", "sh"} {
		script := "#!/bin/sh\nprintf '%s\\n' " + quote(name) + " >> " + quote(calls) + "\nexit 97\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Prove the external-program trap records execution before using its absence.
	control := exec.Command(filepath.Join(bin, "git"))
	if err := control.Run(); err == nil {
		t.Fatal("trap did not fail")
	}
	if data, err := os.ReadFile(calls); err != nil || string(data) != "git\n" {
		t.Fatalf("trap control: %q %v", data, err)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	homeBefore := doctorTreeSnapshot(t, home)
	schema := compileSchema(t)
	invoke := func(want int, args ...string) result {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestFirstIncrementCommandProcess$", "--"}, args...)...)
		cmd.Dir = dir
		// A closed environment excludes inherited profiles, credentials and sockets.
		cmd.Env = []string{"WHR_ACCEPTANCE_PROCESS=1", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "XDG_STATE_HOME=" + home, "PATH=" + bin, "TMPDIR=" + home, "TERM=dumb"}
		var out, diagnostics bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &diagnostics
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("start %v: %v", args, err)
			}
			code = exit.ExitCode()
		}
		if ctx.Err() != nil {
			t.Fatalf("command timeout: %v", args)
		}
		got := result{code: code, stdout: out.String(), err: diagnostics.String()}
		if code != want {
			t.Fatalf("%v: exit %d, want %d; stdout=%s stderr=%s", args, code, want, got.stdout, got.err)
		}
		value, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%v invalid JSON envelope: %v", args, err)
		}
		if data, err := os.ReadFile(calls); !os.IsNotExist(err) {
			t.Fatalf("external program invoked: %q (%v)", data, err)
		}
		if after := doctorTreeSnapshot(t, home); !reflect.DeepEqual(homeBefore, after) {
			t.Fatal("profile/credential/temp home changed")
		}
		return got
	}
	unchanged := func(before map[string]string) {
		t.Helper()
		if after := doctorTreeSnapshot(t, dir); !reflect.DeepEqual(before, after) {
			t.Fatal("read-only/refused/no-op command changed workspace")
		}
	}
	base := []string{"init", dir, "--non-interactive", "--yes", "--name=source-demo", "--json"}
	before := doctorTreeSnapshot(t, dir)
	preview := invoke(0, append(append([]string{}, base...), "--dry-run")...)
	p := decodeEnvelope(t, preview.stdout)["result"].(map[string]any)
	if p["dry_run"] != true || p["git_checks"] != "not_run" || !strings.Contains(preview.err, "create file workharbor.json") || !strings.Contains(preview.err, "not creation-ready") || !strings.Contains(preview.stdout, "not creation-ready") {
		t.Fatalf("missing truthful preview: %+v", preview)
	}
	for _, a := range p["actions"].([]any) {
		if a.(map[string]any)["applied"] != false {
			t.Fatal("preview reported applied action")
		}
	}
	unchanged(before)
	written := invoke(0, base...)
	if !strings.Contains(written.err, "not creation-ready") || !strings.Contains(written.stdout, "not creation-ready") {
		t.Fatal("written v2 lacks warning")
	}
	manifest := filepath.Join(dir, "workharbor.json")
	raw, err := os.ReadFile(manifest)
	if err != nil || !bytes.Contains(raw, []byte(`"name": "source-demo"`)) {
		t.Fatalf("manifest not authored: %s %v", raw, err)
	}
	state := doctorTreeSnapshot(t, dir)
	validated := invoke(0, "validate", dir, "--json")
	if decodeEnvelope(t, validated.stdout)["result"].(map[string]any)["valid"] != true {
		t.Fatal("written workspace did not validate")
	}
	unchanged(state)
	noop := invoke(0, base...)
	for _, a := range decodeEnvelope(t, noop.stdout)["result"].(map[string]any)["actions"].([]any) {
		if a.(map[string]any)["applied"] != false {
			t.Fatal("no-op reported writes")
		}
	}
	unchanged(state)
	edit := []string{"init", dir, "--non-interactive", "--yes", "--name=edited", "--json"}
	refused := invoke(70, edit...)
	if !strings.Contains(refused.err, "--overwrite workharbor.json") {
		t.Fatal("missing per-path consent guidance")
	}
	unchanged(state)
	replacement := invoke(0, append(edit, "--overwrite=workharbor.json")...)
	if !strings.Contains(replacement.err, "not creation-ready") || !strings.Contains(replacement.stdout, "not creation-ready") {
		t.Fatal("replacement v2 lacks preview/result warning")
	}
	raw, err = os.ReadFile(manifest)
	if err != nil || !bytes.Contains(raw, []byte(`"name": "edited"`)) {
		t.Fatalf("consented replacement absent: %s %v", raw, err)
	}
	state = doctorTreeSnapshot(t, dir)
	invoke(0, "validate", dir, "--json")
	for _, entry := range loadContract(t).Commands {
		if entry.BlockedOn != "owner_api" {
			continue
		}
		args := append(append([]string{}, entry.Path...), "--json")
		got := invoke(64, args...)
		if decodeEnvelope(t, got.stdout)["command"] != entry.ID {
			t.Fatal("unavailable command lost canonical identity")
		}
	}
	unchanged(state)
}
