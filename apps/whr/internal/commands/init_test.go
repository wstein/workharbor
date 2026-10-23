// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func initFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestInitPublicJSONWritesAndValidates(t *testing.T) {
	dir := initFixture(t)
	got := run(t, nil, "workspace", "init", dir, "--non-interactive", "--yes", "--json")
	if got.code != 0 {
		t.Fatalf("public init unavailable or failed: %+v", got)
	}
	env := decodeEnvelope(t, got.stdout)
	doc, err := jsonUnmarshal(got.stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Fatalf("invalid init envelope: %v", err)
	}
	if env["command"] != "workspace.init" {
		t.Fatal("noncanonical command")
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); err != nil {
		t.Fatal("no manifest written")
	}
	if !strings.Contains(got.err, "create file workharbor.json") || !strings.Contains(got.err, "not creation-ready") {
		t.Fatal("missing pre-write preview and resolver warning")
	}
}
func TestInitPublicDryRunWritesNothing(t *testing.T) {
	dir := initFixture(t)
	before, _ := os.Stat(dir)
	got := run(t, nil, "init", dir, "--dry-run", "--json")
	if got.code != 0 {
		t.Fatalf("public dry run refused: %+v", got)
	}
	env := decodeEnvelope(t, got.stdout)
	result := env["result"].(map[string]any)
	if result["dry_run"] != true || result["git_checks"] != "not_run" {
		t.Fatal("missing dry-run metadata")
	}
	for _, a := range result["actions"].([]any) {
		if a.(map[string]any)["applied"] != false {
			t.Fatal("dry-run applied")
		}
	}
	after, _ := os.Stat(dir)
	names, _ := os.ReadDir(dir)
	if len(names) != 1 || before.ModTime() != after.ModTime() {
		t.Fatal("dry run changed directory")
	}
}
func TestInitPublicModesNameRequiredFlags(t *testing.T) {
	dir := initFixture(t)
	for _, tc := range []struct {
		flags   []string
		code    int
		message string
	}{
		{nil, 70, "--yes"}, {[]string{"--non-interactive"}, 70, "--yes"},
	} {
		got := run(t, nil, append([]string{"init", dir}, tc.flags...)...)
		if got.code != tc.code || !strings.Contains(got.err, tc.message) {
			t.Fatalf("mode mismatch %+v: %+v", tc, got)
		}
	}
}
func TestInitPublicInvalidFlagsBeforeFilesystemOrConsent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	for _, flags := range [][]string{
		{"--non-interactive", "--template=unsupported"},
		{"--non-interactive", "--devcontainer=../custom.json"},
		{"--dry-run", "--name=BAD!"}, {"--dry-run", "--human-shell=unknown"},
		{"--dry-run", "--project-url=file:///tmp/project"},
		{"--dry-run", "--overwrite=../escape"},
	} {
		got := run(t, nil, append([]string{"init", missing, "--json"}, flags...)...)
		if got.code != 64 {
			t.Errorf("invalid flags must win over missing directory/consent %v: %+v", flags, got)
		}
	}
}
func TestInitPublicCreatedPathsAreTyped(t *testing.T) {
	dir := initFixture(t)
	got := run(t, nil, "init", dir, "--non-interactive", "--yes", "--json")
	if got.code != 0 {
		t.Fatal(got.err)
	}
	result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
	created, ok := result["created"].([]any)
	if !ok || len(created) != 6 {
		t.Fatalf("missing typed created paths: %v", result["created"])
	}
	for _, v := range created {
		if strings.HasPrefix(v.(string), ".whr-init-tmp-") {
			t.Fatal("reported removed temporary as retained file")
		}
	}
}

type initPreviewWriter struct {
	bytes.Buffer
	before  func()
	once    sync.Once
	failure error
}

// Forward StringWriter too: io.WriteString must exercise the same fault seam.
func (w *initPreviewWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *initPreviewWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		if w.before != nil {
			w.before()
		}
	})
	if w.failure != nil {
		return 0, w.failure
	}
	return w.Buffer.Write(p)
}
func TestInitPublicCancelledPreviewWritesNothing(t *testing.T) {
	dir := initFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	diagnostics := &initPreviewWriter{before: cancel}
	code := runWithContext(ctx, &out, diagnostics, "init", dir, "--non-interactive", "--yes", "--json")
	if code != 72 {
		t.Fatalf("cancel not reported72: %d %s", code, out.String())
	}
	doc, err := jsonUnmarshal(out.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = compileSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	names, _ := os.ReadDir(dir)
	if len(names) != 1 || names[0].Name() != ".devcontainer" {
		t.Fatal("cancelled preview wrote files")
	}
}
func TestInitPublicStalePreviewRefused(t *testing.T) {
	dir := initFixture(t)
	var out bytes.Buffer
	diagnostics := &initPreviewWriter{before: func() {
		if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"foreign"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}}
	code := runWithContext(context.Background(), &out, diagnostics, "init", dir, "--non-interactive", "--yes", "--json")
	if code != 68 {
		t.Fatalf("stale preview not refused: %d %s", code, out.String())
	}
	doc, err := jsonUnmarshal(out.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = compileSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
		t.Fatal("stale preview wrote manifest")
	}
}
func TestInitPublicPreviewFailureWritesNothing(t *testing.T) {
	dir := initFixture(t)
	var out bytes.Buffer
	diagnostics := &initPreviewWriter{failure: io.ErrClosedPipe}
	code := runWithContext(context.Background(), &out, diagnostics, "init", dir, "--non-interactive", "--yes", "--json")
	if code != 74 {
		t.Fatalf("preview output failure=%d: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
		t.Fatal("unseen preview was applied")
	}
}
func TestInitPublicRerunNoOpAndOverwriteConsent(t *testing.T) {
	dir := initFixture(t)
	base := []string{"init", dir, "--non-interactive", "--yes", "--json"}
	if got := run(t, nil, base...); got.code != 0 {
		t.Fatal(got.err)
	}
	name := filepath.Join(dir, "workharbor.json")
	before, _ := os.Stat(name)
	raw, _ := os.ReadFile(name)
	got := run(t, nil, base...)
	if got.code != 0 {
		t.Fatal(got.err)
	}
	after, _ := os.Stat(name)
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("rerun changed manifest")
	}
	result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
	if _, ok := result["created"]; ok {
		t.Fatal("no-op reported created paths")
	}
	for _, a := range result["actions"].([]any) {
		if a.(map[string]any)["applied"] == true {
			t.Fatal("no-op applied")
		}
	}
	got = run(t, nil, append(append([]string{}, base...), "--name=edited")...)
	if got.code != 70 || !strings.Contains(got.err, "--overwrite workharbor.json") {
		t.Fatalf("missing per-path consent: %+v", got)
	}
	unchanged, _ := os.ReadFile(name)
	if !bytes.Equal(raw, unchanged) {
		t.Fatal("unconsented edit wrote file")
	}
	got = run(t, nil, append(append([]string{}, base...), "--name=edited", "--overwrite=workharbor.json")...)
	if got.code != 0 {
		t.Fatalf("consented edit failed: %+v", got)
	}
	written, _ := os.ReadFile(name)
	if !bytes.Contains(written, []byte(`"name": "edited"`)) {
		t.Fatal("consented edit not written")
	}
}
func TestInitPublicAliasesAndConflictSchema(t *testing.T) {
	schema := compileSchema(t)
	for _, sp := range [][]string{{"workspace", "init"}, {"ws", "init"}, {"init"}} {
		dir := initFixture(t)
		for _, bad := range []bool{false, true} {
			if bad {
				if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte(`{"schema_version":true}`), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got := run(t, nil, append(append([]string{}, sp...), dir, "--dry-run", "--json")...)
			want := 0
			if bad {
				want = 68
			}
			if got.code != want {
				t.Fatalf("%v bad=%t: %+v", sp, bad, got)
			}
			env := decodeEnvelope(t, got.stdout)
			if env["command"] != "workspace.init" {
				t.Fatal("alias not canonical")
			}
			doc, err := jsonUnmarshal(got.stdout)
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(doc); err != nil {
				t.Fatal(err)
			}
			if bad {
				validation := env["result"].(map[string]any)["validation"].(map[string]any)
				if validation["valid"] != false {
					t.Fatal("invalid preserved manifest reported valid")
				}
			}
		}
	}
}
func TestInitPublicExitCodesAndClosedEnvelopes(t *testing.T) {
	schema := compileSchema(t)
	for _, kind := range []string{"absent", "file", "missing-devcontainer", "invalid-manifest", "link"} {
		t.Run(kind, func(t *testing.T) {
			dir := initFixture(t)
			want := 68
			switch kind {
			case "absent":
				dir = filepath.Join(t.TempDir(), "absent")
				want = 65
			case "file":
				dir = filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(dir, []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				want = 64
			case "missing-devcontainer":
				dir = t.TempDir()
				want = 70
			case "invalid-manifest":
				if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte(`{"schema_version":true}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "link":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link + "/."
			}
			got := run(t, nil, "init", dir, "--non-interactive", "--yes", "--json")
			if got.code != want {
				t.Fatalf("wrong exit: %+v want%d", got, want)
			}
			doc, err := jsonUnmarshal(got.stdout)
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(doc); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got.err, "whr:") {
				t.Fatal("missing CLI diagnostic")
			}
		})
	}
}
func TestInitPublicEscapesUntrustedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace\n\u202econtrol")
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, jsonMode := range []bool{false, true} {
		args := []string{"init", dir, "--dry-run"}
		if jsonMode {
			args = append(args, "--json")
		}
		got := run(t, nil, args...)
		if got.code != 0 {
			t.Fatal(got.err)
		}
		if strings.ContainsRune(got.stdout, '\u202e') || strings.ContainsRune(got.err, '\u202e') || strings.Contains(got.stdout, dir) {
			t.Fatal("raw hostile directory reached terminal/output")
		}
		if jsonMode {
			doc, err := jsonUnmarshal(got.stdout)
			if err != nil {
				t.Fatal(err)
			}
			if err = compileSchema(t).Validate(doc); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestInitPublicRunsNoStartupCommandOrGit(t *testing.T) {
	dir := initFixture(t)
	private := filepath.Join(t.TempDir(), "fixture's home")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(private, "startup-executed")
	record := filepath.Join(private, "invocations")
	bin := filepath.Join(private, "bin")
	home := filepath.Join(private, "home")
	for _, folder := range []string{bin, home} {
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	for _, name := range []string{"git", "sh", "bash", "zsh", "fish", "nu", "devcontainer", "docker"} {
		script := "#!/bin/sh\nprintf '%s\\n' " + quote(name) + " >> " + quote(record) + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "TMPDIR"} {
		t.Setenv(name, private)
	}
	// POSIX ':' and redirection are shell builtins: this marker does not
	// depend on PATH, unlike touch. The private launchers record ignored calls.
	startup := ": > " + quote(marker)
	fixtureRun := func(executable string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Dir = private
		cmd.Env = []string{"PATH=" + bin, "HOME=" + home, "TMPDIR=" + private}
		if err := cmd.Run(); err != nil {
			t.Fatalf("owned executable control failed: %v", err)
		}
	}
	// Prove the traps work, then reset only their owned marker files.
	fixtureRun(filepath.Join(bin, "git"), "status")
	if data, err := os.ReadFile(record); err != nil || !bytes.Contains(data, []byte("git")) {
		t.Fatal("Git recorder did not work")
	}
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	fixtureRun("/bin/sh", "-c", startup)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("PATH-independent startup marker did not work")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	dc := map[string]any{"image": "debian:bookworm", "initializeCommand": startup}
	raw, err := json.Marshal(dc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ".devcontainer/devcontainer.json"))
	got := run(t, nil, "init", dir, "--non-interactive", "--yes", "--json")
	if got.code != 0 {
		t.Fatal(got.err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("startup command executed")
	}
	if data, err := os.ReadFile(record); !os.IsNotExist(err) {
		t.Fatalf("forbidden Git/launcher invocation recorded: %q (%v)", data, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".devcontainer/devcontainer.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("startup command bytes changed")
	}
}

// FR-020/SC-007: the public result agrees with the reconciled schema default.
func TestInitResultConsoleDefaultPlainMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(repoFile("output.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	properties := defs["initResult"].(map[string]any)["properties"].(map[string]any)
	settings := properties["settings"].(map[string]any)["properties"].(map[string]any)
	value := settings["console_mode"].(map[string]any)["properties"].(map[string]any)["value"].(map[string]any)
	if value["default"] != "plain" {
		t.Fatalf("schema default %v, want plain", value["default"])
	}
	for _, mode := range []string{"", "plain", "herdr"} {
		dir := initFixture(t)
		args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
		want, origin := mode, "flag"
		if mode == "" {
			want, origin = "plain", "default"
		} else {
			args = append(args, "--console-mode="+mode)
		}
		got := run(t, nil, args...)
		if got.code != 0 {
			t.Fatalf("%q: %+v", mode, got)
		}
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := compileSchema(t).Validate(doc); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest["console_mode"] != want {
			t.Fatalf("%q: stored mode %v, want %s", mode, manifest["console_mode"], want)
		}
		result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
		setting := result["settings"].(map[string]any)["console_mode"].(map[string]any)
		if setting["value"] != want || setting["origin"] != origin {
			t.Fatalf("%q: %+v", mode, setting)
		}
	}
}
