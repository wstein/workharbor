// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type localLifecycleKey struct{}
type localLifecycleInvocation struct {
	local, dryRun, yes, interactive, smoke, volumes, force bool
	source, trust                                          string
	input                                                  io.Reader
	diagnostics                                            io.Writer
}

func (a *app) localLifecycleOptions(c *cobra.Command) localLifecycleInvocation {
	v := localLifecycleInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.source, _ = c.Flags().GetString("source")
	v.trust, _ = c.Flags().GetString("trust")
	v.smoke, _ = c.Flags().GetBool("smoke")
	v.volumes, _ = c.Flags().GetBool("volumes")
	v.force, _ = c.Flags().GetBool("force")
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	if v.diagnostics == nil {
		v.diagnostics = io.Discard
	}
	return v
}

// LocalStageHandler separates exact source trust from consent to resource changes.
func LocalStageHandler(prepare func(context.Context, localdocker.StageOptions) (localdocker.LifecycleTransaction, error)) Handler {
	return localLifecycleHandler("stage", func(ctx context.Context, v localLifecycleInvocation, target string) (localdocker.LifecycleTransaction, error) {
		return prepare(ctx, localdocker.StageOptions{Source: v.source, Target: target, Smoke: v.smoke, Diagnostics: v.diagnostics})
	})
}

// LocalPromoteHandler preserves explicit home-deletion flags and the receipt trust.
func LocalPromoteHandler(prepare func(context.Context, localdocker.PromoteOptions) (localdocker.LifecycleTransaction, error)) Handler {
	return localLifecycleHandler("promote", func(ctx context.Context, v localLifecycleInvocation, target string) (localdocker.LifecycleTransaction, error) {
		return prepare(ctx, localdocker.PromoteOptions{Target: target, Diagnostics: v.diagnostics})
	})
}
func lifecycleDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, c := range value[7:] {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func lifecycleSelector(value string) bool {
	if value == "" || len(value) > 1024 {
		return false
	}
	for i, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '_' || c == '.' || c == '-') {
			continue
		}
		return false
	}
	return true
}
func localLifecycleHandler(kind string, prepare func(context.Context, localLifecycleInvocation, string) (localdocker.LifecycleTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (res Result, err error) {
		v, _ := ctx.Value(localLifecycleKey{}).(localLifecycleInvocation)
		if !v.local {
			return res, exitcode.Errorf(exitcode.PathUnavailableCommand, "workspace.%s requires --local; remote lifecycle operations are unavailable", kind)
		}
		if len(call.Args) != 1 {
			return res, exitcode.Errorf(exitcode.PathBadArguments, "%s --local requires exactly one primary container id/name", kind)
		}
		if !lifecycleSelector(call.Args[0]) {
			return res, exitcode.Errorf(exitcode.PathBadArguments, "%s --local requires an exact container name or full 64-hex id", kind)
		}
		if kind == "stage" {
			if v.source == "" {
				return res, exitcode.Errorf(exitcode.PathBadArguments, "stage --local requires explicit --source <dir>")
			}
			if v.trust != "" && !lifecycleDigest(v.trust) {
				return res, exitcode.Errorf(exitcode.PathBadArguments, "--trust must be sha256:<64 lowercase hex>")
			}
		} else if (!v.dryRun || v.volumes) && (!v.volumes || !v.force) {
			return res, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "promotion home deletion requires --volumes and --force")
		}
		if !v.dryRun && !v.interactive {
			if !v.yes {
				return res, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended %s requires --yes (or --dry-run)", kind)
			}
			if kind == "stage" && v.trust == "" {
				return res, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended stage requires exact --trust <fingerprint>; --yes grants no source trust")
			}
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		tx, err := prepare(ctx, v, call.Args[0])
		if err != nil {
			return res, lifecycleError(err, nil)
		}
		if tx == nil {
			return res, exitcode.Errorf(exitcode.PathUnclassified, "local lifecycle preparation returned no transaction")
		}
		applied := false
		defer func() {
			closeErr := tx.Close()
			if closeErr == nil {
				return
			}
			if err != nil {
				ce := asError(closeErr)
				_, _ = fmt.Fprintf(v.diagnostics, "whr: %s\n", terminal.Escape(ce.Message))
				if ce.Remediation != "" {
					_, _ = fmt.Fprintf(v.diagnostics, "  hint: %s\n", terminal.Escape(ce.Remediation))
				}
				return
			}
			r, ok := res.Value.(localdocker.LifecycleResult)
			if !ok {
				err = lifecycleError(closeErr, nil)
				return
			}
			if applied {
				r.Status = "partial"
				r.UnverifiedReason = nil
				ce := asError(closeErr)
				err = exitcode.Errorf(exitcode.PathPartialFailure, "local %s completed but cleanup failed: %s", kind, ce.Message).WithRemediation("%s", ce.Remediation)
			} else {
				r.Status = "refused"
				r.UnverifiedReason = nil
				err = closeErr
			}
			res.Value = normalizeLifecycleResult(r)
			res.Human = lifecycleHuman(r, kind)
			err = lifecycleError(err, &r)
		}()
		preview := normalizeLifecycleResult(tx.Preview())
		refuse := func(e error) (Result, error) {
			r := preview
			r.Status = "refused"
			r.UnverifiedReason = nil
			return Result{Value: r}, lifecycleError(e, &r)
		}
		if ctx.Err() != nil {
			return refuse(ctx.Err())
		}
		if v.dryRun {
			return Result{Value: preview, Human: lifecycleHuman(preview, kind)}, nil
		}
		consent := localdocker.LifecycleConsent{Confirmed: v.yes, Volumes: v.volumes, Force: v.force, Quiescence: v.yes}
		reader := bufio.NewReader(io.LimitReader(v.inputOrEmpty(), 2050))
		if v.interactive {
			if _, err = io.WriteString(v.diagnostics, lifecycleHuman(preview, kind)+"Pause ordinary writers to execution inputs throughout each lifecycle phase; hashes do not freeze paths.\n"); err != nil {
				return refuse(err)
			}
		}
		if kind == "stage" {
			if preview.Fingerprint == nil || !lifecycleDigest(*preview.Fingerprint) {
				return refuse(exitcode.Errorf(exitcode.PathConflict, "stage preview has no valid source fingerprint"))
			}
			if v.trust != "" {
				if v.trust != *preview.Fingerprint {
					return refuse(exitcode.Errorf(exitcode.PathConflict, "--trust does not match the previewed source fingerprint; preview and trust the current source"))
				}
				consent.TrustDigest = v.trust
			} else {
				if err = lifecycleConfirm(ctx, reader, v.diagnostics, "Trust this source fingerprint for build and hooks? [y/N] ", "source trust"); err != nil {
					return refuse(err)
				}
				consent.TrustDigest = *preview.Fingerprint
			}
		}
		if !v.yes {
			if err = lifecycleConfirm(ctx, reader, v.diagnostics, fmt.Sprintf("Confirm local %s effects? [y/N] ", kind), kind); err != nil {
				return refuse(err)
			}
			consent.Confirmed = true
			consent.Quiescence = true
		}
		if ctx.Err() != nil {
			return refuse(ctx.Err())
		}
		result, applyErr := tx.Apply(ctx, consent)
		result = normalizeLifecycleResult(result)
		if applyErr != nil {
			if result.Status == "" {
				result = preview
				result.Status = "refused"
				result.UnverifiedReason = nil
			}
			return Result{Value: result, Human: lifecycleHuman(result, kind)}, lifecycleError(applyErr, &result)
		}
		applied = true
		return Result{Value: result, Human: lifecycleHuman(result, kind)}, nil
	}
}
func (v localLifecycleInvocation) inputOrEmpty() io.Reader {
	if v.input != nil {
		return v.input
	}
	return strings.NewReader("")
}
func lifecycleConfirm(ctx context.Context, reader *bufio.Reader, diagnostics io.Writer, prompt, label string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := io.WriteString(diagnostics, prompt); err != nil {
		return err
	}
	answer, err := reader.ReadString('\n')
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	value := strings.ToLower(strings.TrimSpace(answer))
	if err == nil && value == "cancel" {
		return exitcode.Errorf(exitcode.PathUserInterrupt, "local lifecycle cancelled before effects")
	}
	if err != nil || len(answer) > 1025 || value != "yes" && value != "y" {
		return exitcode.Errorf(exitcode.PathHumanDecisionMissing, "%s was not confirmed", label)
	}
	return nil
}
func normalizeStageTarget(t localdocker.StageTarget) localdocker.StageTarget {
	if t.Refs != nil {
		refs := *t.Refs
		refs.Creates = append([]localdocker.StageRefCreate{}, refs.Creates...)
		t.Refs = &refs
	}
	return t
}
func normalizeLifecycleResult(r localdocker.LifecycleResult) localdocker.LifecycleResult {
	r.Effects = append([]localdocker.StageEffect{}, r.Effects...)
	for i := range r.Effects {
		r.Effects[i].Target = normalizeStageTarget(r.Effects[i].Target)
	}
	r.Recovery = append([]localdocker.StageRecoveryStep{}, r.Recovery...)
	for i := range r.Recovery {
		if r.Recovery[i].Target != nil {
			t := normalizeStageTarget(*r.Recovery[i].Target)
			r.Recovery[i].Target = &t
		}
	}
	r.Repositories = append([]localdocker.StageRepository{}, r.Repositories...)
	r.ResultRefs = append([]localdocker.StageResultRef{}, r.ResultRefs...)
	return r
}
func lifecycleError(err error, r *localdocker.LifecycleResult) *exitcode.Error {
	ee := asError(err)
	if ee.Result == nil && r != nil {
		ee.Result = normalizeLifecycleResult(*r)
	} else {
		switch existing := ee.Result.(type) {
		case localdocker.LifecycleResult:
			ee.Result = normalizeLifecycleResult(existing)
		case *localdocker.LifecycleResult:
			if existing != nil {
				ee.Result = normalizeLifecycleResult(*existing)
			}
		}
	}
	return ee
}
