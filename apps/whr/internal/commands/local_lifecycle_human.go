// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"fmt"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

func lifecycleHuman(r localdocker.LifecycleResult, kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "local %s: %s (phase: %s)\n", kind, terminal.Escape(r.Status), terminal.Escape(r.Phase))
	if r.Fingerprint != nil {
		fmt.Fprintf(&b, "source fingerprint: %s\n", terminal.Escape(*r.Fingerprint))
	}
	for _, item := range []struct {
		label string
		value *localdocker.StageResource
	}{{"primary", r.Primary}, {"candidate", r.Candidate}, {"candidate home", r.Home}} {
		if item.value != nil {
			fmt.Fprintf(&b, "%s: %s\n", item.label, lifecycleTargetHuman(localdocker.StageTarget{Resource: item.value}))
		}
	}
	for _, repo := range r.Repositories {
		fmt.Fprintf(&b, "source: %s\nretained clone: %s (owner uid: %d, root mode: %04o)\nrepository HEAD: %s (%s); dirty=%t, uncommitted_retained=%t\n", terminal.Escape(repo.SourcePath), terminal.Escape(repo.ClonePath), repo.CloneOwnerUID, repo.CloneRootMode, terminal.Escape(repo.HeadOID), terminal.Escape(repo.ObjectFormat), repo.Dirty, repo.UncommittedRetained)
	}
	if r.Verification != nil {
		fmt.Fprintf(&b, "verification: %s, %s; binding: %s; quiescence: %s\n", terminal.Escape(r.Verification.Policy), terminal.Escape(r.Verification.Status), terminal.Escape(r.Verification.BindingMode), terminal.Escape(r.Verification.Quiescence))
	} else {
		b.WriteString("verification policy: passing declared healthcheck; otherwise explicit --smoke is required. Running alone is insufficient.\n")
	}
	if r.UnverifiedReason != nil {
		fmt.Fprintf(&b, "unverified reason: %s\n", terminal.Escape(*r.UnverifiedReason))
	}
	for _, effect := range r.Effects {
		fmt.Fprintf(&b, "effect: %s %s; state=%s\n", terminal.Escape(effect.Action), lifecycleTargetHuman(effect.Target), terminal.Escape(effect.State))
	}
	for _, ref := range r.ResultRefs {
		label := "unverified result"
		for _, repo := range r.Repositories {
			if repo.SourcePath == ref.RepositoryPath && repo.HeadOID == ref.OID {
				label = "matches recorded stage HEAD"
				break
			}
		}
		fmt.Fprintf(&b, "Git result: %s %s %s -> %s; %s; state=%s\n", terminal.Escape(ref.RepositoryPath), terminal.Escape(ref.ExportRef), terminal.Escape(ref.OID), terminal.Escape(ref.TargetRef), label, terminal.Escape(ref.State))
	}
	if len(r.ResultRefs) > 0 {
		b.WriteString("Git results are retained for owner integration; no worktree/index checkout, existing ref update or automatic push.\n")
	}
	if kind == "promote" {
		b.WriteString("Primary replacement terminates all its exec sessions, including shell panes. The original primary home is preserved.\nApply requires --volumes --force and confirmation (--yes unattended). Candidate cleanup retains the project clone.\n")
	}
	if r.RecoveryID != nil {
		fmt.Fprintf(&b, "local recovery: %s\n", terminal.Escape(*r.RecoveryID))
	}
	for _, step := range r.Recovery {
		fmt.Fprintf(&b, "recovery %s: %s", terminal.Escape(step.Action), terminal.Escape(step.Instruction))
		if step.Target != nil {
			fmt.Fprintf(&b, " (%s)", lifecycleTargetHuman(*step.Target))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
func lifecycleTargetHuman(t localdocker.StageTarget) string {
	var b strings.Builder
	switch {
	case t.Resource != nil:
		r := t.Resource
		fmt.Fprintf(&b, "%s", terminal.Escape(r.Kind))
		if r.Name != nil {
			fmt.Fprintf(&b, " name=%s", terminal.Escape(*r.Name))
		}
		if r.ID != nil {
			fmt.Fprintf(&b, " id=%s", terminal.Escape(*r.ID))
		} else {
			b.WriteString(" id=unknown")
		}
		fmt.Fprintf(&b, " daemon=%s", terminal.Escape(r.DaemonID))
	case t.Objects != nil:
		r := t.Objects
		fmt.Fprintf(&b, "Git objects repository=%s git_dir=%s common_dir=%s format=%s original_HEAD=%s binding=%s export=%s manifest=%s count=%d", terminal.Escape(r.Repository.SourcePath), terminal.Escape(r.Repository.GitDir), terminal.Escape(r.Repository.CommonDir), terminal.Escape(r.Repository.ObjectFormat), terminal.Escape(r.Repository.OriginalHeadOID), terminal.Escape(r.Repository.BindingDigest), terminal.Escape(r.ExportPath), terminal.Escape(r.ObjectManifestDigest), r.ObjectCount)
	case t.Refs != nil:
		r := t.Refs
		fmt.Fprintf(&b, "Git refs repository=%s git_dir=%s common_dir=%s binding=%s", terminal.Escape(r.Repository.SourcePath), terminal.Escape(r.Repository.GitDir), terminal.Escape(r.Repository.CommonDir), terminal.Escape(r.Repository.BindingDigest))
		for _, c := range r.Creates {
			fmt.Fprintf(&b, " create %s=%s expected_absent=%t", terminal.Escape(c.Name), terminal.Escape(c.OID), c.ExpectedAbsent)
		}
	case t.Output != nil:
		r := t.Output
		fmt.Fprintf(&b, "clone output clone=%s path=%s digest=%s producer=%s", terminal.Escape(r.ClonePath), terminal.Escape(r.RelativePath), terminal.Escape(r.Digest), terminal.Escape(r.ProducerOperationID))
	}
	return b.String()
}
