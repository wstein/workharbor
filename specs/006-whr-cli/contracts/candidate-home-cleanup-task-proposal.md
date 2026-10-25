# Proposed task: new candidate allocation and cleanup consent

**Current disposal override (owner, 2026-10-25):** the historical retained-clone
and separate deletion-consent expectations below are superseded for new
whr-managed candidates by the [throwaway staging amendment](throwaway-staging-amendment-20261025.md).
Its planned code/test packages replace the old consent-matrix expectations;
allocation, ownership and protected-home cases remain applicable. This pointer
does not turn the historical proposed T040 number into a new task or tick.

Status: **proposal only**, 2026-10-24. T040 is unused at proposal base
`d55cd6b413512cd88c25cfbcabdd403ea1935aba`; reserve it here only. This is not an
approved task entry, execution order or completion claim. Authoritative
[tasks and dependencies](../tasks.md) remain unchanged.

Proposed T040 [US1, US4]: implement new candidate allocation and promotion cleanup
consent under the separately accepted [decision](candidate-home-cleanup-decision.md),
FR-067(d–h), FR-068 and SC-024. Only new `whr-stage-home-<opaque-id>` homes at
`/home/captain` or `/home/vscode`; no Anchorage rename/adoption, guard change,
protected-candidate cleanup, new dependency or orphan-volume recovery capability.

After separate task approval and completed, independently reviewed, owner-approved
T038 S0, expected paths are `apps/whr/internal/localdocker/`,
`apps/whr/internal/commands/`, `contracts/cli/commands.json`,
`contracts/cli/output.schema.json`, focused tests and command docs. S0 fixes exact
argv/budgets, fresh absence/rechecks, journal/receipt/lock correlation, explicit
same-Docker-user race limits, recovery identity and closed output schemas first.
The [proposed S0 execution profile](local-stage-execution-profile.md) supplies a
concrete review candidate for those details; its exact-profile approval and
remaining engineering limits still gate product work. A successful idempotent
volume-create reply or name prefix proves no ownership.
Reuse T038's endpoint, selectors, trusted-byte/image binding and verification;
retain merged removal invariants without a second provisioning adapter.

## Proposed red-first coverage

These are future failing-first tests, not tests created or passed by this proposal.
Fake tools must record exact actions and prove refusals before primary replacement.

| Proposed test | Required evidence |
|---|---|
| `TestCandidateHomeNewOpaqueName` | Random 128-bit/32-lowercase-hex ID, isolated mount and receipt-bound daemon/name/IDs; no source/user content. |
| `TestCandidateHomeCollisionAndReservationRace` | Existing volume/candidate/retained intent and create-or-return-existing races refuse 68; no adoption, automatic retry or competing-resource deletion; no false exclusivity claim. |
| `TestCandidateHomeOnePerDaemon` | Different source/primary cannot bypass fixed name/label or interrupted-attempt collision. |
| `TestCandidateCleanupPreservesWholeAnchorageGuard` | Staging/temp exact-ID/role/label, container-only and deletion flags all retain protected-prefix refusal 68/zero effects; no existing resource renamed. |
| `TestCandidateCleanupPrimarySharedAndProtectedRefused` | New prefix never bypasses primary/shared (running or stopped), read-only, `/opt/bdock` descendants or `bundledock-*` protection; foreign/missing metadata refuses 68. |
| `TestLocalPromoteExplicitDeletionOptionsMatrix` | Missing either flag refuses 70 even with yes; both flags still require confirmation/yes; automation never manufactures human consent or deletion options. |
| `TestLocalPromoteDryRunDeletionConsentMatrix` | No flags preview 0 honestly requires apply consent; volumes alone 70; both flags preview 0; zero Docker/hook/build/receipt effects. |
| `TestLocalPromoteCleanupPreflightBeforePrimaryReplacement` | Fresh complete cleanup preflight before stopping/recreating primary; drift refuses 68, absent consent 70; preserve original primary home. |
| `TestLocalPromoteCleanupAfterVerifiedPrimary` | Verified immutable image; primary verification before candidate-container removal before home deletion; preview exact targets/all mounts and pane loss. |
| `TestLocalPromoteCleanupLateDriftPartialFailure` | Late sharing/drift, failed verification/removal and uncertain deletion report 73 with attempted/applied/remaining inventory; no unsafe deletion/retry/rollback or false success. |
| `TestCandidateHomeOrphanRecoveryNotClaimed` | Lost receipt, partial allocation and detached residual home retained/reported; restaging blocked, container-selector rm not advertised as orphan recovery. |
| `TestCandidateCleanupJSONAndEscaping` | Closed schema, canonical IDs, escaped hostile output, no secrets; preview/apply parity and existing exit categories. |

Retain all merged removal regressions, including ambiguity and apply-once. Future
gates: applicable Go format/vet/tests and contracts, `make check`, independent
exact-head review and separately authorized real-host Linux/macOS evidence.
Fixtures do not prove actual daemon exclusivity or official CLI behavior. This
proposal authorizes no builds, Docker operations or installs.

## Proposed dependencies and approval gates

A later owner-approved spec/plan/tasks amendment must encode the scope. Proposed
foundations: T025 and merged T038 discovery/removal subset (#226), **not full
T038 completion**. S0 needs approved decision/task artifacts, not implemented T040.
T040 product execution requires approved S0 and the T038 selector/receipt/
verification package interfaces it consumes. Specify these package prerequisites
before code rather than inserting partial-task edges into the current task table.
Full T038 acceptance would require accepted T040 plus remaining S1–S4 coverage;
T039 still depends on full T038. No T038↔T040 cycle, task entry, dependency change
or completion checkbox is introduced here.

Separate records are required for exact-revision owner acceptance of the decision,
owner approval of the concrete task/artifact reconciliation, and completed S0
approval before code. A documentation proposal merge alone makes no task
executable. Source review/checks, host evidence, owner merge and live-operation
gates remain separate; neither this proposal nor PR229 completes T038.
