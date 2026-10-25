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

type lifecycleFixture struct {
	preview, applied               localdocker.LifecycleResult
	prepareErr, applyErr, closeErr error
	prepares, applies, closes      int
	stage                          localdocker.StageOptions
	promote                        localdocker.PromoteOptions
	consent                        localdocker.LifecycleConsent
}

func (f *lifecycleFixture) Preview() localdocker.LifecycleResult { return f.preview }
func (f *lifecycleFixture) Apply(_ context.Context, c localdocker.LifecycleConsent) (localdocker.LifecycleResult, error) {
	f.applies++
	f.consent = c
	return f.applied, f.applyErr
}
func (f *lifecycleFixture) Close() error { f.closes++; return f.closeErr }
func lifecycleHandlers(f *lifecycleFixture) map[string]commands.Handler {
	h := baseHandlers()
	h["workspace.stage"] = commands.LocalStageHandler(func(_ context.Context, o localdocker.StageOptions) (localdocker.LifecycleTransaction, error) {
		f.prepares++
		f.stage = o
		return f, f.prepareErr
	})
	h["workspace.promote"] = commands.LocalPromoteHandler(func(_ context.Context, o localdocker.PromoteOptions) (localdocker.LifecycleTransaction, error) {
		f.prepares++
		f.promote = o
		return f, f.prepareErr
	})
	return h
}
func lifecycleFixtureFor(command string) *lifecycleFixture {
	status := "staged_verified"
	if command == "promote" {
		status = "promoted"
	}
	return &lifecycleFixture{preview: lifecycleFixtureResult("preview"), applied: lifecycleFixtureResult(status)}
}
func requireLifecycleFailure(t *testing.T, g result, code int, reason string) {
	t.Helper()
	if g.code != code || !strings.Contains(g.err, reason) {
		t.Fatalf("want %d %q, got %+v", code, reason, g)
	}
	if strings.TrimSpace(g.stdout) == "" {
		return
	}
	env := decodeEnvelope(t, g.stdout)
	name, _ := exitcode.Name(code)
	e := lifecycleObject(env["error"])
	if env["ok"] != false || env["exit_code"] != float64(code) || e["code"] != name || !strings.Contains(e["message"].(string), reason) {
		t.Fatalf("incorrect refusal envelope: %v", env)
	}
	if err := compileSchema(t).Validate(env); err != nil {
		t.Fatal(err)
	}
}
func TestLocalLifecycleInvalidArgumentsRefuseBeforeBackend(t *testing.T) {
	cases := []struct {
		args   []string
		reason string
	}{
		{[]string{"workspace", "stage", "primary"}, "--local"},
		{[]string{"workspace", "stage", "--local", "--source=/project"}, "exactly one"},
		{[]string{"workspace", "stage", "--local", "--source=/project", "one", "two"}, "exactly one"},
		{[]string{"workspace", "stage", "--local", "--source=/project", "../directory"}, "exact container"},
		{[]string{"workspace", "stage", "--local", "primary"}, "--source"},
		{[]string{"workspace", "stage", "--local", "--source=", "primary"}, "--source"},
		{[]string{"workspace", "stage", "--local", "--source=/project", "--trust=invalid", "primary"}, "--trust must"},
		{[]string{"workspace", "stage", "--local=remote", "primary"}, "invalid argument"},
		{[]string{"workspace", "promote", "primary"}, "--local"},
		{[]string{"workspace", "promote", "--local"}, "exactly one"},
		{[]string{"workspace", "promote", "--local", "one", "two"}, "exactly one"},
		{[]string{"workspace", "promote", "--local", "--source=/project", "primary"}, "unknown flag"},
		{[]string{"workspace", "promote", "--local", "--trust=invalid", "primary"}, "unknown flag"},
		{[]string{"workspace", "promote", "--local", "--role=primary", "primary"}, "unknown flag"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f := lifecycleFixtureFor("stage")
			g := run(t, lifecycleHandlers(f), append(tc.args, "--json")...)
			requireLifecycleFailure(t, g, 64, tc.reason)
			if f.prepares+f.applies+f.closes != 0 {
				t.Fatal("usage refusal reached backend")
			}
		})
	}
}
func TestLocalStageDryRunAndExactTrustConsent(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, group := range []string{"workspace", "ws"} {
		f := lifecycleFixtureFor("stage")
		g := run(t, lifecycleHandlers(f), group, "stage", "--local", "--source=/project", "--smoke", "--dry-run", "--json", "primary")
		if g.code != 0 || f.prepares != 1 || f.applies != 0 || f.closes != 1 || f.stage.Source != "/project" || f.stage.Target != "primary" || !f.stage.Smoke || f.stage.Diagnostics == nil {
			t.Fatalf("preview %+v fixture %+v", g, f)
		}
		if err := compileSchema(t).Validate(decodeEnvelope(t, g.stdout)); err != nil {
			t.Fatal(err)
		}
		f = lifecycleFixtureFor("stage")
		g = run(t, lifecycleHandlers(f), group, "stage", "--local", "--source=/project", "--trust="+digest, "--yes", "--json", strings.Repeat("b", 64))
		if g.code != 0 || f.applies != 1 || f.closes != 1 || f.consent.TrustDigest != digest || !f.consent.Confirmed || !f.consent.Quiescence || f.consent.Volumes || f.consent.Force {
			t.Fatalf("exact trust/consent %+v fixture %+v", g, f)
		}
		if err := compileSchema(t).Validate(decodeEnvelope(t, g.stdout)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalStagePreservesLiteralExplicitSourcePath(t *testing.T) {
	f := lifecycleFixtureFor("stage")
	g := run(t, lifecycleHandlers(f), "workspace", "stage", "--local", "--source=   ", "--dry-run", "primary", "--json")
	if g.code != 0 || f.stage.Source != "   " || f.applies != 0 || f.closes != 1 {
		t.Fatalf("literal explicit path was rewritten/refused: %+v", g)
	}
}
func TestLocalStageUnattendedTrustAndConsentAreIndependent(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		flags    []string
		code     int
		reason   string
		prepared bool
	}{
		{nil, 70, "--yes", false},
		{[]string{"--yes"}, 70, "--trust", false},
		{[]string{"--trust=" + digest}, 70, "--yes", false},
		{[]string{"--yes", "--trust=sha256:" + strings.Repeat("f", 64)}, 68, "does not match", true},
	} {
		f := lifecycleFixtureFor("stage")
		args := append([]string{"workspace", "stage", "--local", "--source=/project", "primary", "--json"}, tc.flags...)
		g := run(t, lifecycleHandlers(f), args...)
		requireLifecycleFailure(t, g, tc.code, tc.reason)
		if f.applies != 0 || (f.prepares == 1) != tc.prepared || f.closes != f.prepares {
			t.Fatalf("refusal had effects: %+v", f)
		}
	}
}
func TestLocalPromoteDeletionConsentMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  []string
		code   int
		reason string
		apply  bool
	}{
		{"preview_neither", []string{"--dry-run"}, 0, "", false},
		{"preview_force", []string{"--dry-run", "--force"}, 0, "", false},
		{"preview_volumes", []string{"--dry-run", "--volumes"}, 70, "--volumes and --force", false},
		{"preview_both", []string{"--dry-run", "--volumes", "--force"}, 0, "", false},
		{"apply_neither", []string{"--yes"}, 70, "--volumes and --force", false},
		{"apply_volumes", []string{"--volumes", "--yes"}, 70, "--volumes and --force", false},
		{"apply_force", []string{"--force", "--yes"}, 70, "--volumes and --force", false},
		{"apply_unconfirmed", []string{"--volumes", "--force"}, 70, "--yes", false},
		{"apply_both", []string{"--volumes", "--force", "--yes"}, 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := lifecycleFixtureFor("promote")
			g := run(t, lifecycleHandlers(f), append([]string{"ws", "promote", "--local", "primary", "--json"}, tc.flags...)...)
			if tc.code != 0 {
				requireLifecycleFailure(t, g, tc.code, tc.reason)
			} else if g.code != 0 {
				t.Fatal(g)
			}
			if (f.applies == 1) != tc.apply {
				t.Fatalf("wrong apply count: %+v", f)
			}
			if tc.apply && (!f.consent.Volumes || !f.consent.Force || !f.consent.Confirmed || !f.consent.Quiescence || f.consent.TrustDigest != "") {
				t.Fatalf("deletion options synthesized/lost: %+v", f.consent)
			}
			if f.closes != f.prepares {
				t.Fatal("prepared transaction not closed exactly once")
			}
		})
	}
}
func TestLocalStageInteractiveTrustAndMutationHaveSeparatePrompts(t *testing.T) {
	for _, tc := range []struct {
		answer        string
		code, prompts int
		reason        string
	}{
		{"yes\ny\n", 0, 2, ""}, {"y\nno\n", 70, 2, "not confirmed"}, {"no\n", 70, 1, "source trust was not confirmed"}, {"cancel\n", 72, 1, "cancelled"}, {"yes\ncancel\n", 72, 2, "cancelled"}, {"yes", 70, 1, "source trust was not confirmed"}, {strings.Repeat("y", 1025) + "\n", 70, 1, "source trust was not confirmed"},
	} {
		t.Run(strings.ReplaceAll(tc.answer, "\n", "/"), func(t *testing.T) {
			f := lifecycleFixtureFor("stage")
			*f.preview.Primary.Name = "primary\x1b[31m\n"
			f.preview.Repositories[0].SourcePath = "/project\u202e"
			var out, diag bytes.Buffer
			code := commands.Execute(context.Background(), []string{"workspace", "stage", "--local", "--source=/project", "primary", "--no-color"}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(tc.answer), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, lifecycleHandlers(f))
			if code != tc.code || (f.applies == 1) != (tc.code == 0) || f.closes != 1 || strings.Count(diag.String(), "[y/N]") != tc.prompts {
				t.Fatalf("interactive result %d %q fixture %+v", code, diag.String(), f)
			}
			if tc.reason != "" && !strings.Contains(diag.String(), tc.reason) {
				t.Fatalf("wrong refusal: %q", diag.String())
			}
			if strings.ContainsAny(out.String()+diag.String(), "\x1b\u202e") || !strings.Contains(diag.String(), "sha256:") || !strings.Contains(diag.String(), "Pause") {
				t.Fatalf("unsafe/incomplete preview %q", diag.String())
			}
			if tc.code == 0 && (!f.consent.Confirmed || !f.consent.Quiescence || f.consent.TrustDigest != *f.preview.Fingerprint) {
				t.Fatal("interactive source trust missing")
			}
		})
	}
}
func TestLocalLifecyclePartialResultsAndCleanupFailures(t *testing.T) {
	for _, command := range []string{"stage", "promote"} {
		f := lifecycleFixtureFor(command)
		f.applied = lifecycleFixtureResult("partial")
		f.applied.Effects[0].State = "observed"
		f.applyErr = exitcode.Errorf(exitcode.PathPartialFailure, "retained candidate after local deadline")
		f.closeErr = exitcode.Errorf(exitcode.PathConflict, "temporary capture retained")
		flags := []string{"--volumes", "--force"}
		if command == "stage" {
			flags = []string{"--source=/project", "--trust=" + *f.preview.Fingerprint}
		}
		g := run(t, lifecycleHandlers(f), append([]string{"workspace", command, "--local", "primary", "--yes", "--json"}, flags...)...)
		requireLifecycleFailure(t, g, 73, "retained candidate after local deadline")
		if f.closes != 1 || lifecycleObject(decodeEnvelope(t, g.stdout)["result"])["status"] != "partial" {
			t.Fatal("lost partial inventory")
		}
	}
	f := lifecycleFixtureFor("stage")
	f.closeErr = exitcode.Errorf(exitcode.PathConflict, "temporary capture retained").WithRemediation("inspect the retained temporary capture")
	g := run(t, lifecycleHandlers(f), "workspace", "stage", "--local", "--source=/project", "primary", "--dry-run", "--json")
	requireLifecycleFailure(t, g, 68, "temporary capture retained")
	f = lifecycleFixtureFor("promote")
	f.closeErr = exitcode.Errorf(exitcode.PathConflict, "temporary capture retained")
	g = run(t, lifecycleHandlers(f), "workspace", "promote", "--local", "primary", "--volumes", "--force", "--yes", "--json")
	requireLifecycleFailure(t, g, 73, "temporary capture retained")
}
func TestLocalLifecyclePublicArraysAndDiagnosticChannel(t *testing.T) {
	f := lifecycleFixtureFor("stage")
	f.preview.Effects = nil
	f.preview.Recovery = nil
	f.preview.Repositories = nil
	f.preview.ResultRefs = nil
	h := lifecycleHandlers(f)
	h["workspace.stage"] = commands.LocalStageHandler(func(_ context.Context, o localdocker.StageOptions) (localdocker.LifecycleTransaction, error) {
		f.prepares++
		_, _ = io.WriteString(o.Diagnostics, "escaped smoke <U+001B>\n")
		return f, nil
	})
	g := run(t, h, "workspace", "stage", "--local", "--source=/project", "primary", "--dry-run", "--json")
	if g.code != 0 || !strings.Contains(g.err, "escaped smoke") || strings.Contains(g.stdout, "escaped smoke") {
		t.Fatalf("diagnostic channel: %+v", g)
	}
	r := lifecycleObject(decodeEnvelope(t, g.stdout)["result"])
	for _, key := range []string{"effects", "recovery", "repositories", "result_refs"} {
		if a, ok := r[key].([]any); !ok || len(a) != 0 {
			t.Fatalf("%s is not a nonnull array: %v", key, r[key])
		}
	}
	if err := compileSchema(t).Validate(decodeEnvelope(t, g.stdout)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalLifecycleCancellationAfterPreparationIsNotPreviewSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := lifecycleFixtureFor("stage")
	h := lifecycleHandlers(f)
	h["workspace.stage"] = commands.LocalStageHandler(func(context.Context, localdocker.StageOptions) (localdocker.LifecycleTransaction, error) {
		f.prepares++
		cancel()
		return f, nil
	})
	var out, diag bytes.Buffer
	code := commands.Execute(ctx, []string{"workspace", "stage", "--local", "--source=/project", "primary", "--dry-run", "--json"}, commands.Env{Stdout: &out, Stderr: &diag}, h)
	requireLifecycleFailure(t, result{code, out.String(), diag.String()}, 72, "interrupted")
	if f.applies != 0 || f.closes != 1 {
		t.Fatal("cancelled preview not closed without effects")
	}
}

func TestLocalPromoteInteractiveConfirmationDisclosesSessionLoss(t *testing.T) {
	for _, tc := range []struct {
		answer string
		code   int
		reason string
	}{{"yes\n", 0, ""}, {"no\n", 70, "promote was not confirmed"}, {"cancel\n", 72, "cancelled"}} {
		f := lifecycleFixtureFor("promote")
		*f.preview.Primary.Name = "primary\x1b[31m"
		var out, diag bytes.Buffer
		code := commands.Execute(context.Background(), []string{"ws", "promote", "--local", "primary", "--volumes", "--force", "--no-color"}, commands.Env{Stdout: &out, Stderr: &diag, Stdin: strings.NewReader(tc.answer), InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, lifecycleHandlers(f))
		if code != tc.code || (f.applies == 1) != (code == 0) || f.closes != 1 || strings.Count(diag.String(), "[y/N]") != 1 || strings.ContainsAny(out.String()+diag.String(), "\x1b") {
			t.Fatalf("promotion confirmation %q: %d %q", tc.answer, code, diag.String())
		}
		if tc.reason != "" && !strings.Contains(diag.String(), tc.reason) {
			t.Fatalf("wrong refusal: %q", diag.String())
		}
		for _, required := range []string{strings.Repeat("b", 64), "terminates all its exec sessions", "shell panes", "original primary home is preserved", "--volumes --force", f.preview.ResultRefs[0].TargetRef, "Git objects", "manifest="} {
			if !strings.Contains(diag.String(), required) {
				t.Fatalf("promotion preview omitted %q", required)
			}
		}
		if code == 0 && (!f.consent.Volumes || !f.consent.Force || !f.consent.Confirmed || !f.consent.Quiescence || f.consent.TrustDigest != "") {
			t.Fatalf("wrong promotion consent: %+v", f.consent)
		}
	}
}

func TestLocalStageYesNeverSuppliesSourceTrust(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		f := lifecycleFixtureFor("stage")
		var out, diag bytes.Buffer
		reads := 0
		input := removalAnswerReader(func(p []byte) (int, error) { reads++; return copy(p, "yes\n"), io.EOF })
		args := []string{"workspace", "stage", "--local", "--source=/project", "primary", "--yes", "--no-color"}
		if jsonMode {
			args = append(args, "--json")
		}
		code := commands.Execute(context.Background(), args, commands.Env{Stdout: &out, Stderr: &diag, Stdin: input, InputTerminal: true, IsTerminal: func(io.Writer) bool { return true }}, lifecycleHandlers(f))
		if jsonMode {
			requireLifecycleFailure(t, result{code, out.String(), diag.String()}, 70, "--trust")
			if reads != 0 || f.prepares != 0 || f.applies != 0 {
				t.Fatal("JSON invocation prompted or inferred source trust")
			}
		} else if code != 0 || reads != 1 || f.applies != 1 || strings.Count(diag.String(), "[y/N]") != 1 || f.consent.TrustDigest != *f.preview.Fingerprint {
			t.Fatalf("--yes bypassed separate interactive trust: %d %q", code, diag.String())
		}
	}
}

func TestLocalStageUnverifiedOutcomeRetainsTypedReasonAndInventory(t *testing.T) {
	f := lifecycleFixtureFor("stage")
	f.applied.Status = "staged_unverified"
	f.applied.Phase = "staged_unverified"
	f.applied.UnverifiedReason = lifecycleText("missing_smoke")
	f.applyErr = exitcode.Errorf(exitcode.PathPartialFailure, "candidate retained without required smoke verification")
	g := run(t, lifecycleHandlers(f), "workspace", "stage", "--local", "--source=/project", "primary", "--yes", "--trust="+*f.preview.Fingerprint, "--json")
	requireLifecycleFailure(t, g, 73, "candidate retained without required smoke verification")
	r := lifecycleObject(decodeEnvelope(t, g.stdout)["result"])
	if r["status"] != "staged_unverified" || r["unverified_reason"] != "missing_smoke" || f.closes != 1 {
		t.Fatalf("lost unverified candidate reason: %v", r)
	}
}

func TestLocalLifecyclePreparedFailuresPreserveCategoryAndReason(t *testing.T) {
	for _, tc := range []struct {
		path   exitcode.Path
		code   int
		reason string
	}{
		{exitcode.PathBadArguments, 64, "ambiguous labelled primary"}, {exitcode.PathTargetAbsent, 65, "no labelled primary"}, {exitcode.PathConflict, 68, "candidate collision"}, {exitcode.PathDeadlineExceeded, 71, "local preflight deadline exceeded"}, {exitcode.PathUserInterrupt, 72, "local preflight cancelled"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			f := lifecycleFixtureFor("stage")
			f.prepareErr = exitcode.Errorf(tc.path, "%s", tc.reason)
			g := run(t, lifecycleHandlers(f), "workspace", "stage", "--local", "--source=/project", "primary", "--dry-run", "--json")
			requireLifecycleFailure(t, g, tc.code, tc.reason)
			if f.applies != 0 {
				t.Fatal("preparation refusal applied transaction")
			}
		})
	}
}
