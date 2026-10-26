# Throwaway staging cleanup evidence

The [owner-approved amendment](../../../specs/006-whr-cli/contracts/throwaway-staging-amendment-20261025.md)
controls this package. This record covers source tests and command wiring; it
claims no real Docker, OrbStack, or owner-host acceptance. Task checkboxes remain
unchanged.

## Behavior and evidence

| Behavior | Executable evidence |
| --- | --- |
| A confirmed promote needs no candidate-home deletion flags. | `TestThrowawayPromoteNeedsOnlyNormalConfirmation`; `TestLocalPromoteCLIConsentAndPreview` |
| Failed stage verification disposes of the candidate, fresh home, private tree, and records. | `TestThrowawayFailedStageDisposesArtifacts`; `TestLocalStageReviewAnonymousImageVolumeRefused` also checks anonymous-volume absence. |
| Successful rollback restores the original and disposes of the failed replacement and candidate artifacts. | `TestThrowawayPromoteRollbackDisposesCandidate`; the existing uncertain/shared-resource rollback tests still require residual resources and records. |
| Ordinary remove disposes of a receipt-bound candidate without extra deletion consent. | `TestThrowawayRemoveDisposesBoundCandidateWithoutVolumeFlags`; `TestLocalRmCandidateDisposalUsesNormalConsent` |
| Incomplete cleanup keeps a resumable binding until final record deletion. | `TestThrowawayRemovalFailureKeepsRecordsAndReportsAttempts`; `TestThrowawayRemovalResumesAcrossFinalRecordDeletion` |
| Early allocation and invalid creation responses remain removable. | `TestThrowawayStageFailureBeforeCandidateCanBeRemoved`; `TestThrowawayEarlyCreatedCandidateResidual` |
| Failed cleanup-intent publication cannot become an archived completed promotion. | `TestThrowawayPromotionCleanupIntentFailureStaysResumable` |
| Candidate-controlled directory modes and symlinks do not prevent normal disposal or redirect deletion outside the candidate tree. | `TestThrowawayTreeCleanupHandlesReadOnlyCloneAndSymlink` |
| Promotion exports live inside the candidate operation, so uncertain transfer storage is removable before its records. | `TestThrowawayUncertainImportIsInsideCandidateTree`; successful and rollback tests check export absence before `Close`. |
| Imported result refs precede primary replacement and survive candidate disposal. | `TestLocalPromoteTransfersResultsBeforeReplacingPrimary` checks success and an incomplete-cleanup path. |
| Generic consent ordering and protected homes remain guarded. | `TestCandidateCleanupPreservesWholeAnchorageGuard`; `TestCandidateCleanupPrimarySharedAndProtectedRefused`; `TestLocalPromotePreservesPrimaryHome` |

The private daemon journal carries the unfinished cleanup binding. It remains
available if individual binding artifacts, the receipt, or the operation
directory have already been deleted. It is removed on completed disposal; this
adds no archive or backup. Docker's native container removal also removes its
anonymous volumes. Named shared volumes are outside that anonymous-volume
operation; the fresh named home uses its existing allocation binding.

## Test history and reproducibility

The test-only commits `72db0cf4`, `d7f52ff1`, `120490d3`, `bd1c60b6`, and
`eeb066dd` record observed failures for normal consent, JSON inventory,
receipt-bound removal, residual removal, and read-only trees before their
implementation. `b1d102aa` records the independently reproduced invalid-creation
response regression. Later fault-boundary and retention-reconciliation tests are
characterization and regression coverage; they are not claimed as original RED
acceptance commits.

Run from the repository root:

```sh
umask 022
go -C apps/whr test ./internal/localdocker -count=1 -timeout 40m
go -C apps/whr test ./internal/commands -count=1
go -C apps/whr vet ./...
python3 scripts/check_repo.py
python3 -B -m unittest discover -s scripts -p 'test_*.py'
git diff --check
```

The PR body records the completed runs and the revisions they covered. The
native acceptance manifest requires the new cleanup identities. The existing
macOS backend job now selects the throwaway tests as well as its capture tests;
that CI result is required before readiness. Native CI and the independent
exact-head review remain separate gates.

The local script run reproduced the existing task-automation baseline failure:
450 tests, 8 failures and 7 errors, with preparation returning `output-conflict`
and exit 5. No task-automation implementation changed here. A bounded local
`make check` attempt did not complete, and Elixir/Mix is unavailable locally.
Neither is reported as passing.

The owner still runs the native end-to-end acceptance on macOS with OrbStack or
a Linux Docker host. Those results, the init gap audit, and the final quickstart
and owner-host runbook follow this code package.

## macOS fixture correction

Native run 40255933284 failed before candidate allocation because the shared
fixture supplied a persistent state path below macOS's `/var` symlink. The
accepted profile requires a physical state path with no symlink components.
The fixture now resolves its existing state parent before constructing the
absent state root. Workspace labels keep their as-given spelling; product
state-path validation is unchanged.

`TestLocalStageFixtureStateRootUsesPhysicalAncestors` reproduces this on Linux
by setting an aliased TMPDIR before the first test temporary directory is
allocated. It failed before the fixture correction and passes afterward. The
Linux acceptance inventory and macOS backend selection both require this case.
Native macOS rerun evidence remains a readiness gate.

The follow-up native run 40256795291 passed the shared fixture but exposed the
same precondition in the direct read-only disposal fixture. The regression now
executes that fixture and direct journal/mode fixtures under the aliased TMPDIR.
All admitted state-store test constructors share a physical-parent helper;
negative symlink tests still construct an explicit symlink beneath a physical
parent. This changes test setup only, without normalizing product state inputs.
