# Throwaway staging amendment

Status: **DECIDED by the owner, 2026-10-25**. This document records that decision
and proposes the corresponding implementation delta for review. It does not
claim that the code, CLI, tests or real-host acceptance already implement it.

## Owner decision and precedence

Source: the owner's request of 2026-10-25, quoted verbatim:

> Staging, bitte alle Artefakte weglöschen, wenn Staging gelöscht wird oder Staging promoted wird ... Staging ist Wegwerf ... sonst sammelt sich zu viel Müll ... wer das Volume behalten will, der soll es vorher mit Docker-Mitteln sichern ... nicht overengineeren ... nicht aus übertriebenem Sicherheitsgedanken Müll ansammeln lassen.

For **new whr-managed staging candidates**, this supersedes the retained-clone
and separate candidate-home deletion-consent rules of the 2026-10-24 decision,
execution profile, and
[local-promote-resource-recovery.md](local-promote-resource-recovery.md). This
also supersedes that recovery contract's retained failed-replacement and
candidate-artifact rules after successful resource rollback. The accepted body of
[local-stage-execution-profile.md](local-stage-execution-profile.md) remains
byte-for-byte unchanged; this separate amendment takes precedence for the scope
below. D1/D2, fingerprint trust, verified-image promotion, original-home
preservation and the complete existing `anchorage-home-*` guard stay decided.

A new whr-managed candidate means the existing receipt-bound allocation model:
its candidate container and fresh `whr-stage-home-<id>` home are correlated to
the admitted daemon and operation. A matching name prefix or label alone does
not adopt an unrelated container or volume. Existing Anchorage candidates are
not renamed or adopted.

## Terminal cleanup and consent

A successful stage keeps its live candidate and working tree available for work.
They are throwaway: normal removal or promotion disposes of the entire candidate.
There is no keep flag, retained-clone cleanup step or volume-backup feature.
Users who want to retain the staging home must back it up with Docker themselves
before removal or promotion.

The normal preview and confirmation cover all candidate artifacts:

| Candidate artifact | Required terminal action |
| --- | --- |
| Exact candidate container | Remove it without targeting the primary. |
| Its fresh `whr-stage-home-<id>` volume | Remove it after its container; retain all primary/shared/protected-volume guards. |
| Private candidate clone and result/export tree | Remove the whole operation-owned tree, including uncommitted and unfetched work on ordinary removal. |
| Candidate receipts, journals and private metadata/configuration | Delete last, after the candidate container, home and clone/result tree are confirmed absent; do not archive or leave a tombstone requiring later cleanup. |
| Temporary preparation/tool-process state | Close/remove through the existing transaction lifecycle. |

If cleanup is incomplete or uncertain, keep the candidate receipt/journal as a
reported residual alongside any remaining resources, so normal confirmed `rm`
of that candidate can finish disposal. Delete those records only after the
container, home and clone/result tree are confirmed absent. This is unfinished
cleanup, not an archive or keep mode.

Generic daemon coordination infrastructure is not a retained candidate. It must
contain no completed candidate record or clone requiring separate cleanup.
Original repositories and their fetched result refs, the current primary, and
its original home are never candidate-cleanup targets. Normal promotion still
performs its already accepted primary replacement; this amendment changes
candidate disposal, not that replacement procedure.

The ordinary prompt or `--yes` is the sole mutation consent for candidate
disposal. Its preview says that unfetched commits and uncommitted clone work are
lost on removal. Do not ask a second cleanup question or require `--volumes` and
`--force` for these candidates. Do not manufacture those flags internally.
Other container/home removal retains its existing flags and protections.

Canonical promotion is again:

```text
whr workspace promote --local [--dry-run] [--yes] <id|name>
```

The obsolete promote-only `--volumes`/`--force` requirements and flags are removed.
Generic `workspace rm` retains its existing flags for other resources; those
flags provide no keep mode for a new whr-managed candidate. Dry-run remains a
zero-apply preview of all candidate deletions. Stage retains its independent
source fingerprint trust and normal mutation confirmation.

## Promotion ordering and failures

Before deleting the clone on promotion, verify that the result refs required by
the existing promote contract are present in the original repository. Reuse the
existing fetch/import and verification; do not add a backup copy or new transfer
protocol. Those original-repository results remain after candidate cleanup.

After the replacement primary is verified, remove the candidate container, its
home, clone/result tree and candidate metadata. Success requires that no
candidate-owned artifact remains to be cleaned separately.

| Path | Required cleanup and primary outcome |
| --- | --- |
| Prepare refused or dry-run | No native apply, execution receipt or persistent candidate allocation. Close temporary preparation. |
| Stage fails after partial allocation, build, hooks or verification, including `staged_unverified` (exit 73) when no healthcheck or explicit smoke verification ran | **Derived from the owner's throwaway intent:** clean the attempt's allocated candidate resources and private clone/result tree, then its records last. Leave the selected primary and its home alone. `staged_unverified` is disposed of like any other failed stage, not retained for later recovery. |
| Ordinary confirmed candidate removal | Remove all candidate artifacts, including unfetched/uncommitted clone work; leave primary/source data alone. |
| Successful promotion | Verify result refs and replacement first, then remove all candidate artifacts. Preserve the resulting primary and original home. |
| Promotion fails after result transfer and primary replacement begins; resource rollback succeeds | **Derived from the owner's throwaway intent:** restore the original primary through the existing rollback, then remove the failed replacement and candidate artifacts. Do not retain a stopped failed-primary container as a backup. Preserve original home and already imported result refs. |
| A required result-transfer, rollback or deletion step fails or is uncertain | Do not claim successful promotion/removal or zero remaining artifacts. Report the existing failure category, exact cleanup attempted/completed/remaining and cause. Keep and report the receipt/journal until resource disposal is confirmed complete, alongside any actual residual; do not retain a deliberate archive or backup. |

The existing exit table remains: 70 for missing/declined normal confirmation,
68/71/72 for the applicable pre-effect refusal/deadline/interruption, and 73 for
partial or uncertain effects. Successful cleanup does not erase an execution
failure or pretend that hooks, external side effects or terminated sessions were
rolled back. No automatic rebuild, retry, volume backup or new recovery service
is introduced.

## Planned implementation and acceptance

These are planned changes and test ideas, not claims of existing passing tests.

1. **Shared candidate disposal, ordinary removal and failed-stage cleanup: one
   code PR.** Reuse receipt/daemon/resource bindings and existing removal guards
   in `apps/whr/internal/localdocker/`. Update `removal.go`, `removal_plan.go`,
   `stage_apply_unix.go`, `stage_resource_unix.go`, `stage_recovery_unix.go` and
   candidate state/receipt handling as needed; keep the helper focused on this
   operation. Adapt `apps/whr/internal/commands/local_rm.go` so normal candidate
   consent covers the complete disposal rather than the generic home flags.
2. **Promotion terminal/rollback cleanup and CLI reconciliation: one code PR,
   updating the existing CLI work where appropriate.** Change
   `promote_apply_unix.go`, `promote_rollback_unix.go` and their tests to finish
   result transfer before clone disposal and remove all candidate/private state.
   Revise the promote adapter/registry/help and `contracts/cli/` to remove the
   obsolete flags. Keep stage source trust and primary-home behavior unchanged.
3. **Init evidence, user docs and owner runbook: the remaining planned packages.**
   Update quickstart and command docs with ordinary confirmation, disposable
   staging and the one-sentence manual Docker-backup advice. Record actual Linux
   Docker and macOS+OrbStack results separately from fixture tests.

Red-first acceptance must cover:

- Successful stage keeps exactly its usable candidate artifacts; confirmed remove
  then leaves no candidate container, home, clone/export tree or candidate record.
- Successful promote leaves no candidate artifact; the expected original result
  refs exist before clone deletion, and the primary's original home/data remain.
- Inject failure after each stage allocation/execution step: allocated candidate
  artifacts are removed; original primary/home and unrelated resources remain.
- Inject failure after primary stop/rename/create/hooks/verification: existing
  rollback restores the original when applicable, removes failed replacement and
  candidate artifacts, and retains the cause. Cleanup failures report residuals.
- Normal prompt/`--yes` covers unfetched-work loss; no extra flag or confirmation;
  declined confirmation and dry-run have zero apply effects.
- Existing protected Anchorage, primary/shared/read-only/tool-volume and foreign
  target refusals still check both exit code and reason. Mixed removal selections
  do not apply the candidate exception to another container/home.
- Inject an incomplete or uncertain cleanup: receipts/journals are deleted last,
  only after container, home and clone/result tree are confirmed absent. Until
  then, the receipt/journal remains a reported residual, and a normal confirmed
  `rm` can finish disposal without adopting an orphan. A failed record deletion
  also reports that record as a residual; no zero-artifact success is claimed.
- A fresh stage after completed cleanup needs no archive, manual metadata deletion
  or separate clone cleanup. JSON remains closed and reports actual cleanup;
  it does not point to a deliberately deleted receipt as retained recovery state.

Use fake Docker/official-CLI fixtures for the exact action ordering and native
owner-host checks for final usability. No task checkbox, feature completion,
real-host success, build, release or live Docker action is implied by this doc PR.
