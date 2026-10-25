# T038 promotion library evidence, 2026-10-25

Scope: callable local promotion and create-only Git result transfer. Command
registration, public schema wiring, native Docker/official-CLI acceptance and task
completion remain separate. The prototype tests are characterization of reused
code, not newly demonstrated historical TDD. Initial missing-API compilation
failure is not behavioral RED evidence.

## Contract reconciliation and recovery limits

The accepted execution profile prohibited automatic old-primary stop/restart.
The later owner direction explicitly requested promotion with rollback. The
[resource recovery amendment](../../../specs/006-whr-cli/contracts/local-promote-resource-recovery.md)
records the narrow technical reconciliation for review; its detailed mechanism
was not separately approved in advance. Recovery preserves container resources,
not application data. Starting the original runs its normal entrypoint. Shared
workspace/home writes, imported objects/refs and terminated sessions are not undone.

After a reaped failed hook/smoke process plus a fresh live replacement binding,
or an observed unhealthy replacement,
recovery checks endpoint, receipt, inputs, exact bindings, occupied names and
shared primary mounts. It stops and retains the replacement under its operation
name, restores the exact original name and starts that original ID. Native
uncertainty, signals, deadlines, identity/input drift and verified cleanup failures
require manual reconciliation. Recovery remains partial with exit 73. Docker
reserved smoke statuses 125, 126 and 127 do not permit recovery.

Inspect the reported full IDs, durable effects, retained clone and result refs
before manual recovery. Do not retry an uncertain native action, delete either
retained writable layer or assume shared data was restored. Candidate cleanup is
separate and retains all FR-067 guards and explicit deletion consent.

## Measured author checks

- Known hook recovery assertion RED: replacement remained running and original
  was not restored. Exact test-only checkpoint `36ba456395d930238233c49d37ba752e34371f07` reconfirmed that failure
  in 9.274 seconds; implementation was restored afterward.
- Observed unhealthy recovery assertion RED: original remained stopped. Its
  focused corrected run, with stage mode/state regressions, passed in 11.088 seconds.
- Aliased TMPDIR assertion RED: preparation retained the alias path. Late-sharing
  assertion RED: candidate was removed before discovering newly shared home use.
  Both corrected tests passed together in 20.138 seconds.
- Docker reserved smoke status 125 assertion RED: unsafe original restart was
  attempted. The test fixture also covers statuses 126 and 127.
- Negative recovery matrix passed in 47.100 seconds before reserved-status cases
  were added: shared mount, occupied retention name, failed stop/start, signaled
  hook and failed official up. Refusals assert both exit and reason.
- Affected promotion/result suite ran once: 308.847 seconds. Three failures were
  precise port differences: candidate alternates reached the hardened reader
  first, malformed packed namespaces received the hardened administration-path
  reason, and stage-only driver refusal wording changed. The focused corrections
  passed in 6.715 seconds. Unchanged stage tests were preserved.
- Final focused hook recovery plus all three reserved-status refusals passed in
  41.642 seconds.
- Two compiled guard mutants were killed by unsafe-restart assertions: removing
  the shared-primary-mount guard and accepting a signaled hook as known failure.
  An earlier unused-variable mutant failed compilation and is not counted.
- `go vet ./internal/localdocker` passed. Script suite: 450 tests in 173.165 seconds,
  with eight failures and seven errors, matching the supplied baseline; this is
  not a passing script-suite claim. Full checks and exact-head independent review
  remain with the coordinator. No build, real Docker operation or publication ran.

## Behavioral claims and proof

All paths below are within `apps/whr/internal/localdocker/`. These are source and
fake-process tests, not native compatibility evidence.

| Claim | Executable proof |
| --- | --- |
| Official lookup selects a distinct replacement and rejects candidate reuse. | `TestLocalPromoteRealCLILookupCreatesDistinctPrimary`, `TestLocalPromoteGuardReasonsAndProtectedEffects` |
| Original writable layer remains until distinct replacement verification. | `TestLocalPromoteFailedReplacementRetainsOriginalWritableLayer`, `TestLocalPromotePartialFailureInventory` |
| Fresh explicit deletion consent precedes Git imports and primary effects. | `TestLocalPromoteRequiresExplicitHomeDeletionConsent`, `TestLocalPromoteTransfersResultsBeforeReplacingPrimary` |
| Recovery has a fresh 120-second context and 30-second native action budgets. | `promote_rollback_unix.go`, `restoreOriginalResource` and its `effect` closure |
| Known hook/health failure restores original while retaining stopped replacement. | `TestLocalPromoteKnownHookFailureRestoresOriginalAndRetainsReplacement`, `TestLocalPromoteKnownUnhealthyReplacementRestoresOriginal` |
| Uncertainty, sharing, occupied names, failed stop/start and reserved Docker statuses refuse further recovery. | `TestLocalPromoteRollbackRefusesUncertainOrSharedResources` |
| Promotion preparation canonicalizes aliased temporary roots. | `TestLocalPromotionCanonicalizesAliasedTemporaryRoot` |
| Cleanup reinspects after original removal and preserves newly shared candidate resources. | `TestLocalPromoteRechecksCleanupAfterOriginalRemoval` |
| Result transfer adds objects/new refs without moving original HEAD/index/worktree. | `TestLocalStageResultTransferCreateOnlyAndFrozenVersion` |
| Candidate Git configuration is inert and alternates/symlink administration cannot supply transport. | `TestLocalStageResultHostileCandidateConfigNeverExecutes`, `TestLocalStageResultCandidateUntrustedAdministration` |
| Pre-effect deadline/interrupt retain their categories; later partial effects use 73. | `TestLocalPromotePreEffectErrorsKeepExitAndReason`, `TestLocalPromotePartialFailureInventory` |

No new dependency, command, flag or public JSON schema is introduced. Internal
recovery action names await explicit command/schema integration. This record does
not claim a review verdict, task completion or owner-host acceptance.

## Six-finding review correction

Review basis: [Opus review of b60e3d99](https://github.com/wstein/workharbor/pull/267#pullrequestreview-5644806836).
The test-only recovery commit was assertion-RED on lexical alias/parent/child and
unavailable bind sources, both configuration copies after each recovery action,
and stopped/unavailable replacement observations after exit 1. Home-only sharing
and harmless sibling/read-only controls characterize existing behavior; the
home-only case must kill removal of the home-volume guard.

The native-policy test-only commit separately reproduced missing HostConfig
projection, unsafe AutoRemove/restart admission and a late AutoRemove change
that previously promoted. The product now observes/binds both policy fields and
refuses unsafe originals before effects. Refusal tests check exit and reason.

The promote path sweep found that other comparisons bind generated physical paths
or immutable configuration fields. The foreign-bind check now resolves both paths
and checks overlap in both directions. Temporary and persisted configuration are
checked before each recovery action. A failed hook/smoke result is uncertain until
a fresh inspection proves the replacement running with its admitted binding.
Exit 1 can still be a client failure with a running replacement; the amendment
explicitly limits that observation to conditional resource recovery.

## Open owner proposal: result-transfer budgets (F6)

The existing implementation uses separate 120-second context caps for preparing
result snapshots and for each standalone recheck. Apply has one shared 120-second
cap for the complete export/import sequence; nested checks inherit its remaining
time. These caps are within the calling promotion context, not an extension of
it. This is a proposed resource bound, not an accepted profile decision. The
accepted execution-profile body is unchanged by this review correction.

Proposal for the owner: retain those 120-second caps as explicit transfer phase
budgets. Admitted repositories approaching the existing size/object limits may
time out within them; no completion guarantee follows from size admission.
Deriving transfer solely from the enclosing promotion deadline would be a
separate owner choice. Keep this PR Draft for that decision, exact-head review
and required CI. No T038/T039 completion or native owner-host proof is claimed.

## Review-fix verification

The integrated safety matrix passed 31 test identities in 55.622 seconds, with no
failures/skips. The final client/binding and conditional exit-one matrix passed
nine identities in 33.340 seconds. The latter characterizes the fresh-binding
limit; it does not claim that exit 1 identifies the process failure's cause.

Nine compiling isolated overlays were assertion-killed against the integrated
code: foreign symlink normalization, ancestor and descendant overlap, home-only
sharing, temporary and persisted configuration checks, and fresh running,
immutable-binding and available observations. The native-policy unit separately
killed three compiling overlays (AutoRemove, restart policy and HostConfig
projection) before integration. No production file was changed by these probes.
The original root test-only checkpoint had 14 failing leaf assertions before its
fixes; home-only sharing and harmless-bind controls already passed. The isolated
native-policy checkpoint was assertion-RED in 16.548 seconds, then GREEN in
11.691 seconds. Compiler errors are excluded from mutation/RED evidence.

After #266 merged, all 17 promote/review-fix patches replayed equally from parent
`34b5cf05d28ee7f9456ce3ca7c86e4a7a07a83e7` onto fresh main
`75889f5364863538530f3fd78fcabd6b375b643e`; acceptance identities were then added.
The inherited accepted profile body is unchanged by this correction. The source
remains frozen while broad author checks and the required exact-head rewrite
review run; final outcomes belong in the PR body. Native CI and the owner's F6
budget decision remain blockers. No independent Ready verdict is claimed here.
