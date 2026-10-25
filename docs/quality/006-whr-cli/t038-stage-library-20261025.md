# T038 stage-library evidence

This is the second replacement slice of #261: S1/S2 stage preparation and execution
library over the source-capture prerequisite in #265. It adds no CLI registration,
promotion, result transfer or candidate-home deletion. T038/T039 completion and
owner-host acceptance remain open. Command/schema composition follows separately.

## Initial e36f0645 evidence (historical)

### Review corrections

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
Compiled stage mutants failed on intended assertions: context wrapping in
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

## PR 266 review delta (historical 4faa1a01)

The base now names the real capture dependency, codex/006-t038-capture-20261025
at 33b090ac. The stage branch was restacked from capture 9a4e51e2 onto that
parent. Range-diff retained 22 patches unchanged; the CI-list patch has only the
additive parent-ID context adjustment. Further focused stage commits adapt the
parent interface and implement the corrections below; the complete round is not
claimed to be an equal-range-diff-only rebase. No parent-owned capture file is
changed by this stage delta. The exact rewritten head requires independent review
before its explicit-lease push; native CI and the foreman's review remain gates.

| Finding | Current implementation / regression |
| --- | --- |
| F1 | PR base is the capture branch; source-capture changes are reviewed in #265, not counted as stage-owned work. Selected execution inventory and candidate-only Git restrictions live in the stage layer after adapting the parent interface. |
| F2 | Flow regressions cover smoke exit, unhealthy candidate, primary stopped after up, foreign operation label and up returning the primary ID. The consent table independently checks matching trust plus confirmation without quiescence: exit 70, exact reason and no persistent state. `TestLocalStageReviewFlowRefusalGuards`, `TestLocalStageReviewUnhealthyCandidateFlow`, `TestLocalStageReviewCompletedSmokeFailureFlow`, `TestLocalStageRequiresTrustAndConsent`. |
| F3 | Remove PromoteOptions, public deletion consent flags, stagePromoteConfig, invocation Promote switch, driver promote mode and promotion-only journal states. Remove the two promote-config tests from this slice's CI inventory. Retain endpoint tests as stage tests and refuse the unsupported promote mode explicitly. The closed private receipt retains its required volumes/force fields as false; it authorizes no deletion. Stage records the primary configuration as data for the later slice, without executing promotion. |
| F4 | Primary mount type/source/destination validation runs in bindStagePrimary before home allocation or up. tmpfs refuses with exit 68 and its specific reason, no Docker effect calls. `TestLocalStageReviewImageAndTmpfsMountPreflight`. Image-VOLUME compatibility still needs the immutable candidate image and is checked after up, before hooks. |
| F5 | The fake now adds anonymous volume mounts for uncovered image VOLUMEs, as Docker does. Candidate volume names and destinations must match the declared home/additional-volume plan, not merely cover image destinations. `TestLocalStageReviewAnonymousImageVolumeRefused` gives the primary a named /cache volume but no candidate declaration, exposing the anonymous mount. |
| F6 | A normally reaped smoke process exit in 1..124 is observed and yields staged_unverified / failed_smoke with exit status retained in the error and guarded-remove/fresh-stage recovery. Docker-reserved exits 125..127, signals, deadline, cancellation and client/observation failures remain uncertain partial outcomes. `TestLocalStageReviewCompletedSmokeFailureFlow` asserts result, exact exit status, effect and durable receipt; existing uncertain-smoke tests remain. |
| F7 | Context cancellation or deadline, including the health sub-deadline, takes the partial path before recording terminal verification failure. No failed_health is invented. `TestLocalStageReviewInterruptedHealthIsPartial` and `TestLocalStageReviewInterruptDuringHealthWaitFlow` assert result and retained verifying journal/receipt. The actual wait flow synchronizes cancellation with a Unix-socket notification from the fake's post-hook candidate observation, with no sleep-based trigger. |

Test-only 8af85eb8 (87787e48 after restack) precedes the corrective source commits.
The ordinary smoke exit 1/124 cases, early tmpfs refusal, anonymous volume case
and interrupted-health outcome failed assertions against the original code. The
initial smoke subtest labels for 124..127 contained non-ASCII characters, which
made their temporary socket endpoints fail admission; corrected decimal labels
then produced the intended semantic RED. Those fixture failures are not counted
as RED evidence. The interrupted-health fixture was strengthened to contain a
valid observed hook receipt and verifying row, avoiding an unrelated receipt-shape
failure. Existing guard coverage and the later notification-flow test are
characterization qualified by mutation checks, not blanket RED-first claims.

Eleven compiled Go-overlay mutants were assertion-killed: smoke failure call-site,
unhealthy candidate, quiescence, primary running, candidate operation, up returning
the primary, early primary mount admission, candidate volume plan, interrupted
health mapping, Docker-reserved smoke exit boundary and retained exit status.
These include all six survivors reported in the review. Production source bytes
were not replaced on disk. Reproduce by deleting the named guard and running its
literal regression; no compiler error is counted as a killed mutant.

Current full-package, formatter/compiler, script and pristine make-check outcomes
are recorded in the PR body. Historical counts above do not describe the new head.
No Docker/official-CLI or owner-host lifecycle test was run; the fake remains a
fixture, including its anonymous-volume model. No task/progress completion ticks
are changed and the later promote/composition packages remain separate.

## Native macOS fixture correction and capture restack

Run 40213548607 on historical 4faa1a01 passed bootstrap but failed macos-pty.
The failing capture-selected stage regression used a Unix socket below the long
native macOS temporary test path; net.Listen returned bind: invalid argument.
This was a fixture path-length failure, not lifecycle or mount-admission evidence.

TestLocalStageFixtureSocketIgnoresLongTMPDIR first reproduced the same bind
failure under an intentionally long temporary root, before the fixture change.
The source and state fixtures continue to use the ordinary test temporary root.
Only the socket uses a private 0700 directory under /tmp, resolves that directory
to its physical path and checks the resulting socket stays within 103 bytes.
The fixture removes its own short directory at cleanup. The CI acceptance list
adds the literal regression ID without dropping inherited tests. Independent
review found the second occurrence in the health-notification listener; the same
long-root regression now runs that health flow too. This additional subcase was
assertion-RED before moving that listener to the shared short-path helper. Both
stage net.Listen Unix-socket allocations use the helper; constant endpoints in
other tests do not bind sockets.

This round restacks the stage branch from capture 33b090ac onto 1267662b.
Of the 29 previously published stage patches, 28 are range-diff equal; the CI
inventory patch changes only additive parent-ID context and the required comma.
The socket regression and correction are appended. The parent retains the
accepted 100,000 working-entry ceiling. The subsequent N1 completion removes
the unapproved administration-entry and store-count limits from code and profile;
this stage branch now rests on that correction. No stage-owned
capture implementation is changed. The complete round includes a fixture fix,
so it is not described as a pure equal-range-diff rebase.

The PR body records this head's affected-package, script, formatter/compiler,
vet and pristine make-check results and the independent exact-SHA rewrite gate.
Historical counts and native results above are not current-head proof. Native CI,
the foreman's review, prerequisite acceptance and owner-host lifecycle evidence
remain separate gates. No task/progress checkboxes or completion claim changes.

The current full affected-package run passed 635 localdocker, 775 workspacecfg
and 264 commands identities, with no failures or skips (154.700 s / 7.479 s /
27.183 s). It preceded only the extra health-socket subcase and its fixture fix;
the final three-test selection, including both sockets under long TMPDIR, passed
in 6.364 s. No production source changed after the full run. The mandatory Python
suite completed 450 tests with 8 failures and 7 errors in unchanged task automation
(139.798 s), matching the previously reported baseline classes; it is not green.
Local mix format check and warning-free compile passed. No native macOS result
for this final head is claimed before CI.

## Accepted-bounds parent correction

The capture parent is now 8f6efe18. The 36 stage patches from published ed29ea7c
replay unchanged onto it with equal range-diff. No stage source, fixture or CI ID
changes in this follow-up; only this record clarifies the withdrawn parent caps.
The previous native result and current-head acceptance gates remain separate.

## Ready capture parent and native macOS physical paths

This follow-up uses Ready capture parent 0feaa033. The owner accepted the F8
administration-walk residual on 2026-10-25, recorded in PR 265's body without
changing its branch. No capture admission limit is added here. All 37 previously
published stage patches from 6f7b792e replay unchanged on this parent; the later
macOS corrections below are appended, so this round is not a pure rebase.

Run 40217019624 on 6f7b792e passed bootstrap but failed macos-pty in
TestLocalStageGitStatusPredicateRejectsCorruptIndexWithoutExecutingConfig:
configuration outside admitted source. After resolving the earlier socket limit,
macOS's logical temporary prefix exposed mismatched physical source/config facts.
TestLocalStageGitFixtureCanonicalSourceUnderAlias reproduces that exact refusal
on Linux by running the native test below a symlinked temporary root.

The fixture now resolves its created source before writing Docker label/mount
facts. That alone still left the regression RED with execution dependency path
outside workspace: the product's private preparation root remained logical while
its dependency resolved physically. Preparation now resolves that root only after
recording its identity, then requires the same inode before using the physical
path. Administrative and source confinement rules are not relaxed. Existing
private context injection exposes the identity checkpoint; the negative race
redirects the temporary alias and checks the original/replacement are retained,
foreign bytes stay intact and no phase directory is written in the replacement.
Both literal regressions are included in the CI inventory and macOS Git selection.

The alias regression is assertion-RED before the corrections. The negative test
first lacked its injection point; that wiring failure is not semantic evidence
that the already-present identity guard was wrong. Its characterization is
qualified by a compiled guard-removal probe that detects the foreign-root write.
The PR body records actual current-head package, mutation, format/compiler and
script/check results. No native macOS PASS for the new head is claimed before CI;
no real Docker lifecycle, task completion or full T038/T039 acceptance is claimed.

## Lexical-label and mount review correction

The exact-head review of `6d32ee79376f23553088db9cfeb469289b507227`
([review](https://github.com/wstein/workharbor/pull/266#pullrequestreview-5644720441))
kept three findings open. Canonicalizing fixture labels had concealed two product
path comparisons. The fixture again preserves the CLI's as-given workspace/config
labels and bind source. Configuration paths are now relative to the original
workspace label; capture remains bound to the admitted physical source.
Persisting promotion configuration compares the workspace bind's physical source
and omits that bind from the extra-mount list.

The existing aliased corrupt-index selection was assertion-RED with the real
lexical label. After configuration routing was fixed, the new full alias flow
was assertion-RED because the persisted configuration duplicated the workspace
bind. Fixture setup failures (a TempDir scope error and an intentionally forbidden
symlinked state root) are not RED evidence. The corrected fixture keeps its state
root physical and workspace labels lexical.

`TestLocalStageCandidateVolumeNamesMustMatchDeclaredPlan` characterizes the exact
volume-name guard for home and extra volumes, asserting both exit 68 and the
specific refusal reason. Removing the name comparison must fail both cases.
No fixture-only correction is presented as a product path fix. The accepted
profile, consent and all protected-home gates remain unchanged.
