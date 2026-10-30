// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

var homeFingerprint = "sha256:" + strings.Repeat("d", 64)

func homeCLIResult(mode string) localdocker.HomeCopyResult {
	size := "1.5GB"
	return localdocker.HomeCopyResult{Mode: mode, Status: "preview", DryRun: true,
		Source:       localdocker.HomeCopySource{ContainerID: strings.Repeat("a", 64), ContainerName: "ws1\x1b[31m", Role: "primary", Volume: "ws1-home"},
		TargetVolume: "whr-home-" + mode + "-ws1-1", HelperImage: "docker.io/library/busybox@sha256:" + strings.Repeat("0", 64),
		Excludes: []string{"./.cache"}, CredentialsIncluded: mode == "backup", EstimatedSize: &size, Fingerprint: homeFingerprint,
		Actions: []localdocker.HomeCopyAction{}, Residuals: []string{}}
}

type homeTxFixture struct {
	preview, result localdocker.HomeCopyResult
	applies         int
	trust           string
	err             error
}

func (f *homeTxFixture) Preview() localdocker.HomeCopyResult { return f.preview }
func (f *homeTxFixture) Apply(_ context.Context, trust string) (localdocker.HomeCopyResult, error) {
	f.applies++
	f.trust = trust
	return f.result, f.err
}

func homeHandlers(f *homeTxFixture, prepares *int, options *localdocker.HomeCopyOptions) map[string]commands.Handler {
	h := baseHandlers()
	prepare := func(_ context.Context, o localdocker.HomeCopyOptions) (commands.HomeCopyTransaction, error) {
		*prepares++
		*options = o
		return f, nil
	}
	h["workspace.home.clone"] = commands.LocalHomeCopyHandler("clone", prepare)
	h["workspace.home.backup"] = commands.LocalHomeCopyHandler("backup", prepare)
	return h
}

func TestLocalHomeCopyDryRunPreviewAndOptions(t *testing.T) {
	for _, group := range []string{"workspace", "ws"} {
		f := &homeTxFixture{preview: homeCLIResult("clone")}
		n := 0
		var o localdocker.HomeCopyOptions
		got := run(t, homeHandlers(f, &n, &o), group, "home", "clone", "--local", "--dry-run", "--to", "new-home", "--exclude", "/projects/big", "--exclude", "tmp", "--include-credentials", "ws1", "--json")
		if got.code != 0 || n != 1 || f.applies != 0 {
			t.Fatalf("%s: %+v prepares=%d applies=%d", group, got, n, f.applies)
		}
		if o.Mode != "clone" || o.Target != "ws1" || o.To != "new-home" || strings.Join(o.Excludes, ",") != "/projects/big,tmp" || !o.IncludeCredentials {
			t.Fatalf("options %+v", o)
		}
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := compileSchema(t).Validate(doc); err != nil {
			t.Fatal(err)
		}
		if env := decodeEnvelope(t, got.stdout); env["command"] != "workspace.home.clone" {
			t.Fatalf("command %v", env["command"])
		}
	}
	f := &homeTxFixture{preview: homeCLIResult("backup")}
	n := 0
	var o localdocker.HomeCopyOptions
	human := run(t, homeHandlers(f, &n, &o), "workspace", "home", "backup", "--local", "--dry-run", "ws1")
	for _, want := range []string{homeFingerprint, "busybox@sha256:", "whr-home-backup-ws1-1", "ws1-home", "1.5GB", "./.cache", "credentials: included", "Pause writers"} {
		if !strings.Contains(human.stdout, want) {
			t.Errorf("human preview lacks %q:\n%s", want, human.stdout)
		}
	}
	if strings.Contains(human.stdout, "\x1b") {
		t.Fatal("untrusted name not escaped")
	}
	if o.Mode != "backup" {
		t.Fatalf("mode %+v", o)
	}
	if bad := run(t, homeHandlers(f, &n, &o), "workspace", "home", "backup", "--local", "--exclude", "x", "ws1"); bad.code != 64 {
		t.Fatalf("backup accepted --exclude: %+v", bad)
	}
}

func TestLocalHomeCopyConsentAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no-local", []string{"--dry-run", "ws1"}, 64, "--local"},
		{"no-selector", []string{"--local", "--dry-run"}, 64, "exact"},
		{"two-selectors", []string{"--local", "--dry-run", "a", "b"}, 64, "exact"},
		{"no-yes", []string{"--local", "--trust", homeFingerprint, "ws1"}, 70, "--yes"},
		{"mismatch", []string{"--local", "--yes", "--trust", "sha256:" + strings.Repeat("e", 64), "ws1"}, 68, "fingerprint"},
	} {
		f := &homeTxFixture{preview: homeCLIResult("backup")}
		n := 0
		var o localdocker.HomeCopyOptions
		got := run(t, homeHandlers(f, &n, &o), append([]string{"workspace", "home", "backup"}, tc.args...)...)
		if got.code != tc.code || !strings.Contains(got.err, tc.want) || f.applies != 0 {
			t.Errorf("%s: %+v applies=%d", tc.name, got, f.applies)
		}
	}
	ok := homeCLIResult("backup")
	ok.Status, ok.DryRun = "copied", false
	ok.Verification = &localdocker.HomeCopyVerification{Status: "match", Source: localdocker.HomeCopyCounts{RegularFiles: 3, ClaudeSessionFiles: 2}, Target: localdocker.HomeCopyCounts{RegularFiles: 3, ClaudeSessionFiles: 2}}
	ok.Actions = []localdocker.HomeCopyAction{{Kind: "copy", Target: ok.TargetVolume, Attempted: true, Applied: true}}
	f := &homeTxFixture{preview: homeCLIResult("backup"), result: ok}
	n := 0
	var o localdocker.HomeCopyOptions
	got := run(t, homeHandlers(f, &n, &o), "workspace", "home", "backup", "--local", "--yes", "--trust", homeFingerprint, "ws1", "--json")
	if got.code != 0 || f.applies != 1 || f.trust != homeFingerprint {
		t.Fatalf("apply %+v %+v", got, f)
	}
	// --yes alone is enough; the previewed fingerprint is applied implicitly.
	f = &homeTxFixture{preview: homeCLIResult("backup"), result: ok}
	got = run(t, homeHandlers(f, &n, &o), "workspace", "home", "backup", "--local", "--yes", "ws1", "--json")
	if got.code != 0 || f.applies != 1 || f.trust != homeFingerprint {
		t.Fatalf("yes alone %+v %+v", got, f)
	}
	doc, _ := jsonUnmarshal(got.stdout)
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	partial := ok
	partial.Status = "failed"
	partial.Verification = &localdocker.HomeCopyVerification{Status: "differs", Source: localdocker.HomeCopyCounts{RegularFiles: 3}, Target: localdocker.HomeCopyCounts{RegularFiles: 2}}
	partial.Residuals = []string{"volume:" + ok.TargetVolume}
	e := exitcode.Errorf(exitcode.PathPartialFailure, "home copy verification differs")
	e.Result = partial
	f = &homeTxFixture{preview: homeCLIResult("backup"), result: partial, err: e}
	got = run(t, homeHandlers(f, &n, &o), "workspace", "home", "backup", "--local", "--yes", "--trust", homeFingerprint, "ws1", "--json")
	if got.code != 73 {
		t.Fatalf("partial %+v", got)
	}
	doc, _ = jsonUnmarshal(got.stdout)
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.stdout, "volume:"+ok.TargetVolume) {
		t.Fatal("residual not reported")
	}
}

func TestLocalHomeCopyInteractiveConfirmation(t *testing.T) {
	for _, tc := range []struct {
		input   string
		code    int
		applies int
	}{
		{"y\n", 0, 1},
		{"yes\n", 0, 1},
		{"n\n", 70, 0},
		{"\n", 70, 0},
	} {
		ok := homeCLIResult("clone")
		ok.Status, ok.DryRun = "copied", false
		f := &homeTxFixture{preview: homeCLIResult("clone"), result: ok}
		n := 0
		var o localdocker.HomeCopyOptions
		var out, diag bytes.Buffer
		env := commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(tc.input), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}
		code := commands.Execute(context.Background(), []string{"workspace", "home", "clone", "--local", "ws1"}, env, homeHandlers(f, &n, &o))
		if code != tc.code || f.applies != tc.applies || !strings.Contains(diag.String(), "[y/N]") || strings.Contains(diag.String(), "exact fingerprint") {
			t.Errorf("%q: code=%d applies=%d diag=%q", tc.input, code, f.applies, diag.String())
		}
	}
}

// Disposal help points at the supported backup command instead of manual Docker steps.
func TestCandidateDisposalHelpNamesHomeBackup(t *testing.T) {
	for _, path := range [][]string{{"workspace", "rm"}, {"workspace", "promote"}} {
		got := run(t, nil, append(path, "--help")...)
		if !strings.Contains(got.stdout, "whr workspace home backup --local") {
			t.Errorf("%v help:\n%s", path, got.stdout)
		}
	}
}
