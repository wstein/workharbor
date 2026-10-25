# T038 stage-library evidence

This is the second replacement slice of #261: S1/S2 stage preparation and execution
library over the source-capture prerequisite in #265. It adds no CLI registration,
promotion, result transfer or candidate-home deletion. T038/T039 completion and
owner-host acceptance remain open. Command/schema composition follows separately.

## Review corrections

| Finding | Correction and regression |
| --- | --- |
| F3 (stage) | Admission preserves deadline/cancel instead of wrapping them as 68: `TestLocalStageReviewAdmissionPreservesContext`, `TestLocalStageReviewApplyAdmissionPreservesContext`. |
| F4 (stage) | Partial errors retain escaped cause and phase: `TestLocalStageReviewPartialRetainsCause`. |
| F5 | Failed/interrupted pending smoke stays uncertain/73; known completed health failure retains its precise unverified reason: `TestLocalStageReviewFailedSmokeIsUncertain`, `TestLocalStageReviewCanceledUnverifiedWithoutCauseIsUncertain`, `TestLocalStageReviewKnownHealthFailureRetainsReason`. |
| F6 (stage) | Rehash exactly the smoke bytes about to be sent; compare with admitted digest and use the shared 256-KiB quota: `TestLocalStageReviewRejectsChangedSmokeBeforeExecution`, `TestLocalStageConfigurationAndSmokeQuotasRefuseBeforeEffects`, `TestLocalStageReviewToolManifestExcludesDriverAfterApply`. |
| F12 (stage) | Daemon-only lock/clone-store key; socket remains a trust binding: `TestLocalStageReviewSocketAliasesShareLock`. Execution profile reconciled accordingly; promotion store follows in its own slice. |
| F14 (stage) | Inspect image VOLUME targets and reject uncovered/implicit or tmpfs mounts before hooks: `TestLocalStageReviewImageAndTmpfsMountPreflight`. Promote's pre-stop reuse follows separately. |
| F15 | Capture prerequisite owns shared group fix; stage runner gives context errors precedence over progress errors: `TestLocalStageReviewInitializerContextPrecedesProgress`, both stage group tests. |
| F16 (stage) | Remove the repeated driver refusal branch. Capture and promotion portions are accounted separately. |
| F17 (stage) | Apply and verification persist the same trusted-tool projection, excluding generated driver: `TestLocalStageReviewToolManifestExcludesDriverAfterApply`. |

No exact-head CLEAR claim is made by the author. The extraction keeps historical
source/tests; new failing assertions precede the review fixes (6e883f73 and
044dab89). The prior compile-only failures during extraction are not TDD evidence.
Fifteen compiled stage mutants failed on intended assertions: context wrapping in
admission/apply; discarded cause; observed pending smoke; bypassed smoke digest;
socket-scoped lock; missing group kill; progress masking deadline; driver included
in receipt; uncovered image volume; admitted tmpfs; lost known-health reason;
omitted image-volume projection; increased smoke quota; discarded settings digest.

## S1/S2 literal acceptance-ID mapping (F19)

A planned literal name need not be duplicated as an alias when the mapped tests
already exercise its criterion. The following are source/fixture coverage, not
full T038 or native-host acceptance.

| Plan ID | Implemented test coverage / remaining boundary |
| --- | --- |
| `TestLocalStageSelectsExactPrimaryAndSource` | `TestLocalStagePrimarySelectionReasons` and `TestLocalStageIndependentPreviewsHaveStableTrustAndNoPersistentEffects`: exact target plus canonical source preparation. |
| `TestLocalStageRejectsUnresolvedLabel` | Literal test present; exit 68 and exact physical-source refusal reason. |
| `TestLocalStageDryRunExecutesNoEffects` | `TestLocalStageIndependentPreviewsHaveStableTrustAndNoPersistentEffects`: no persistent state/effects at preview; CLI --dry-run wiring remains in composition. |
| `TestLocalStageReservesDistinctHome` | Existing `TestCandidateHomeNewOpaqueName` plus `TestLocalStageHomeIntentDurableBeforeNativeCreation`: isolated intent and durable receipt before create. |
| `TestLocalStageRefusesCollision` | `TestLocalStageLostJournalBlocksFreshAttempt`, `TestLocalStageReviewSocketAliasesShareLock` and existing candidate-home checks; actual daemon allocation-race/native correspondence remains host acceptance. |
| `TestLocalStageRejectsChangedInputs` | Literal test present, plus changed tool/substitution and execution-dependency tests; source drift refuses 68 with exact cause before state or resource creation. |
| `TestLocalStageHealthAndSmoke` | Literal health-only and health-plus-explicit-smoke cases, `TestLocalStageMissingSmokeRetainsUnverifiedCandidate`, trusted-stdin/escaping and uncertain-failure regressions. |

The cleanup proposal's remaining missing literal IDs concern promotion/composition or
shared candidate-home acceptance. Their mapping remains required in those later
records; this stage-only slice does not claim home deletion or JSON command parity.

## Checks and limits

Run from apps/whr: `go test -json ./internal/localdocker ./internal/workspacecfg -count=1`.
The library/package run passed: localdocker 552 test identities, workspacecfg 775,
zero failures/skips. It preceded the three added literal acceptance tests above;
those passed separately in 7.781 s. Source production behavior did not change
between these runs. All inherited required IDs remain; this slice adds 69 literal
stage/JSONC tests to the CI minimum, on top of capture's 18.

Linux and Darwin/arm64 vet passed for both affected packages; gofmt and repository
checks passed. Fifteen stage mutants were assertion failures, not compile failures.
The required Python script run under umask 022 completed 450 tests with the same
8 failures and 7 errors in unchanged task automation as the reviewed main baseline
in #261; it is not called passing. Native CI is required for full acceptance.

No Docker daemon, real lifecycle or owner-host check was run. Native admission
currently refuses unsupported mount-sharing correspondence (including macOS);
Darwin vet is compile/static evidence, not macOS lifecycle conformance. Fingerprint
trust, cooperative quiescence, no adoption and all FR-067 protections remain.

The committed pristine `umask 022; timeout 180s make check` run ended with exit 124
in Python checks before application checks. It is incomplete, not green. No absent
Elixir/native-host checks are called passing; the published head needs native CI.
