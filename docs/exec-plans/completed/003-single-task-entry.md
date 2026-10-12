# Feature 003 local single-task evidence

Status: T024–T027 are locally accepted with independent review and the corrected
Linux canonical results below. T028 remains pending because current macOS canonical
acceptance is unmet; the full feature is not verified and PR #55 remains draft.
Scope is [Feature 003](../../../specs/003-single-task-entry/spec.md) local tooling.
The [task list](../../../specs/003-single-task-entry/tasks.md),
[acceptance matrix](../../../specs/003-single-task-entry/quickstart.md) and
[operator guide](../../development/single-task.md) remain authoritative.

## Revisions and protected scope

The finish route starts from accepted main
`59dde433cbb82b71332afb5bf61c5e4c4128d691` (owner-merged PR #54).
Recorded implementation and subsequent acceptance revisions are distinct:

| Work | Implementation | Subsequent acceptance marker |
| --- | --- | --- |
| T024 synchronized process/durability coverage | `6920ab196e878c8579013a33765799f257ea7cbf`, corrected fault boundary `f8f83815f7f977ba1ee4d18d614f1becf38a64f3` | `de705de04d4fb45885c1acada6d8aa7f61850a67` |
| T025 migrated regression audit | `41610451d2f5c0ad22909e94414d71d6a9234bd0` | `37f6aa4a56590d76ee3bef83a3aafb0feaac8750` |
| T026 operator documentation | `5106c671e47864281a1644157a81ccb673a65fb7` | `b888b2514fddeb57efb8931a7ae73b76f2725932` |
| T027 offline/protected-feature tests | `50f234793a2d5be8cc6af0d5bacc2646f0675ab7`; subsequent FR-009 correction `b103d7efa96a7f2ccda5d1167375cbdcc7f8555c` | Accepted after the corrected-source gates below; marker follows those revisions |

Protected tree identities match at immutable pre-Feature-003 baseline
`95b5bed59c918bc100880771ac6be4d127b8edb7`, accepted route baseline `59dde433`,
and corrected implementation `b103d7ef`: Feature 001 `0fda80d6f62c7576394495969187f349f7d0a81a`;
Feature 002 `3b2dce04b696b247e21535c741759cc5633b514a`.
The protected-identity controls compare Git trees, the index, physical file modes
and raw bytes before/after disposable fixture operations. Final protected-path diffs
against both baselines are empty. Tests use synthetic disposable repositories, not
these protected features as mutation targets.

## Measured local and Linux checks

Native evidence below uses Darwin 27 arm64, Python 3.14.8, executable
`/opt/homebrew/bin/python3.14`, with
`PATH=/opt/homebrew/opt/python@3.14/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`.
All listed successful commands exited zero; counts are observed test results,
not a substitute for final acceptance at the final revision.

| Revision / check | Actual result |
| --- | --- |
| T024 original `6920ab19`: `python3.14 -m unittest discover -s tests -p 'test_single_task_state.py' -v` | 67 tests, 134.502s, exit 0 |
| T024 corrected `f8f83815`: required-file preflight followed by `python3.14 -m unittest discover -s tests -p 'test_single_task*.py' -v` | 391 tests, 405.181s, exit 0 |
| T024 corrected `f8f83815`: focused `CompetingTaskProcessTests` | 7 tests, 18.315s, exit 0 |
| T025 `41610451`: `python3.14 -m unittest discover -s tests -p 'test_single_task.py' -v` | 69 tests, 63.709s, exit 0 |
| T025 `41610451`: author focused migration audit | 3 tests, 8.255s, exit 0; distinct from independent review below |
| T025 `41610451`: independent focused review | 4 tests, 10.573s, exit 0, no skips; exact command below |

The standalone 67-case run loaded `6920ab19` before the fault-boundary correction
was written; it is not evidence for `f8f83815`. The corrected 391-case story run
includes all 67 corrected state cases, and the focused seven-case run also binds
to `f8f83815`.

Retained native log SHA-256 identities, in the same row order (excluding the
independent reviewer result retained only in the direct review transcript):

- State: `eb1b2972ff2f19a1e9a3af95d8259041f7826ec3987fcfb2d8fda391fa0b8dc4`.
- Story: `129bacb108af2fac744bd9c572035d242cc7730be976f2b4911d0fb8c60d2049`.
- T024 focused: `fcd6606b11817e0afe74bfd5dea54fbc94151e84685db8cd17f8349c9829b280`.
- T025 regression: `33571b94bad0c6c23dcf8f9db65700fe9ef3af850c3ef2e478a8c02698666e87`.
- T025 author focused: `b655b4aedfd743d8194362b318159c8753c28e528377261cf0778c520580d4fd`.

The independent T025 reviewer ran this exact command in a detached scratch
checkout of `41610451d2f5c0ad22909e94414d71d6a9234bd0`. No raw log file was
retained; the direct review transcript records the result. A separate AST comparison
confirmed all 16 pre-existing regression bodies unchanged by the T025 addition.

```sh
/opt/homebrew/bin/python3.14 -I -B - <<'PY'
import pathlib, sys, unittest
root = pathlib.Path.cwd()
sys.path.insert(0, str(root/'scripts'))
sys.path.insert(0, str(root/'tests'))
import test_single_task as m
suite = unittest.defaultTestLoader.loadTestsFromTestCase(m.SingleTaskMigrationAuditTests)
suite.addTest(m.SingleTaskTests('test_valid_context_cannot_replace_attempt_awaiting_handoff'))
result = unittest.TextTestRunner(verbosity=2).run(suite)
assert result.testsRun == 4 and not result.skipped
sys.exit(not result.wasSuccessful())
PY
```

The T024 Linux jobs checked head
`f8f83815f7f977ba1ee4d18d614f1becf38a64f3` on Ubuntu 24.04.5 with
`/usr/bin/python3`; the interpreter version was **not logged**. Both required
canonical jobs ran `python3 -I -B scripts/check_ci.py` and passed. This is the
runner delegated to by `make check`; its raw child invokes `make check-raw`. The PR job used synthetic merge checkout
`472c000b30513694b22ba9b4514cd55936e88fe1` with baseline `59dde433` and head `f8f83815`.

| Linux run | Raw `make check-raw` | Combined acceptance |
| --- | --- | --- |
| [PR 38321533623](https://github.com/wstein/workharbor-nxt/actions/runs/38321533623) | 400 tests, 134.227s, exit 0 | 798 tests, 219.703s, exit 0 |
| [Push 38321527591](https://github.com/wstein/workharbor-nxt/actions/runs/38321527591) | 400 tests, 95.346s, exit 0 | 798 tests, 155.725s, exit 0 |

These runs qualify T024's named source/merge, not the later T025/T026/final head.

## Later T027 measurements and corrected durability behavior

At `50f234793a2d5be8cc6af0d5bacc2646f0675ab7`, native required-file preflight and
`python3.14 -B -m unittest discover -s tests -p 'test_single_task*.py' -v` passed
400 tests in 426.535s, exit 0. Log SHA-256:
`96f92da2d250da842510a568095fa35555dee85773df3a8578caebcfea70d050`.
OfflineCommandMatrixTests passed 3 tests/8.481s; ProtectedFeatureIdentityTests
passed 2 tests/1.518s. Independent scoped review ran those five cases with
Python 3.14.8 `-I -B`, asserted exactly five tests/no skips and passed in 9.723s,
exit 0 (direct review transcript only; no retained raw log).

These subsequent Linux results also belong to **historical `50f23479`**, not the
corrected source:

| Linux run at `50f23479` | Raw `make check-raw` | Combined acceptance |
| --- | --- | --- |
| [Push 38323032130](https://github.com/wstein/workharbor-nxt/actions/runs/38323032130) | 409 tests, 112.137s, exit 0 | 807 tests, 165.780s, exit 0, zero failures/errors |
| [PR 38323037578](https://github.com/wstein/workharbor-nxt/actions/runs/38323037578) | 409 tests, 114.290s, exit 0 | 807 tests, 163.225s, exit 0, zero failures/errors |

The PR checked ordinary merge `1ef28a4e4db0e29a3462a1a0e2889cc4c15308e9`
onto route baseline `59dde433`. Despite these passing observations, a confirmed
FR-009 finding superseded `50f23479` clearance: resumed publication could rename
an existing retained temporary file without first flushing that validated file's
contents. A focused diagnostic failed one test in 2.148s, exit 1; log SHA-256
`a728b33a18841fd4cb3c85281d38658b92163a4aa13b619a8ad74578c1cfe7d0`.
The green runs do not waive the durability finding.

Correction `b103d7efa96a7f2ccda5d1167375cbdcc7f8555c` flushes the validated
retained descriptor before close/rename and adds the owning regression control.
It received independent scoped CLEAR and the fresh final Linux qualification below. Native affected checks use Python 3.14.8 with `-B`:

| Corrected `b103d7ef` check | Actual result | Log SHA-256 |
| --- | --- | --- |
| `CompetingTaskProcessTests` loaded with unittest | 8 tests, 20.764s, exit 0 | `6c90b784e1c5f460955b768814432fd07dceea1b8555546cef93ac05db177774` |
| `python3.14 -B -m unittest discover -s tests -p 'test_single_task_state.py' -v` | 68 tests, 134.078s, exit 0 | `fc6a50ef5bb6cbb4ec6158a38c90193b8f4cce3004ce3578476f421fbad37bf6` |
| Existing CLI `test_interruption_after_checkbox_before_phase_update` and `test_complete_and_retry_preserve_other_task` | 2 tests, 5.390s, exit 0 | `8e58f86700a5aeeadd78b90414b4d02f5a0ec690948c30d5dba3c2ce19495017` |

Independent review passed the eight owning controls in 21.910s, exit 0/no skips.
Its child-only negative control omitted the new fsync in memory and produced one
raw assertion failure in 2.266s; the control wrapper exited 0 only after confirming
the expected retained-inode-flush-before-rename assertion. These reviewer results
are retained in direct tool transcripts only, not a raw log. No source edit or
passing implementation claim follows from the negative control.

Both fresh corrected-source Linux jobs ran `python3 -I -B scripts/check_ci.py`
and passed on `b103d7efa96a7f2ccda5d1167375cbdcc7f8555c`. The PR job used
ordinary synthetic merge `b61ddcce8dcae3273dad69fd8202b2f2f36d24b9` onto
`59dde433cbb82b71332afb5bf61c5e4c4128d691`. These runs qualify the corrected
runtime/test source; the subsequent bookkeeping changes only evidence, status
and the accepted T027 marker. No full native suite or canonical macOS result is
claimed for the corrected source.

| Corrected Linux run | Raw `make check-raw` | Canonical acceptance |
| --- | --- | --- |
| [Push 38324010995](https://github.com/wstein/workharbor-nxt/actions/runs/38324010995) | 410 tests, 133.432s, exit 0 | 808 tests, 212.687s, exit 0, zero failures/errors |
| [PR 38324015004](https://github.com/wstein/workharbor-nxt/actions/runs/38324015004) | 410 tests, 112.812s, exit 0 | 808 tests, 170.617s, exit 0, zero failures/errors |

No retained expected-red exceptions remain. Required discovery and all checks
passed without skips or weakened deadlines. The later documentation/marker delta
is checked with native `python3.14 -B scripts/check_repo.py`, `git diff --check`
and independent exact review; a full canonical suite is not rerun for that delta.

## Scenario coverage and evidence limits

| Requirement / scenario | Existing evidence and remaining gate |
| --- | --- |
| FR-001–004; selected entry, artifact approval and prerequisites | Context suites validate explicit dependency tables, local commit/content evidence, semantic drift, dirty entry and unsafe identities. The corrected Linux full suite passes these cases. |
| FR-005–006; paths, task markers, reviews and acceptance | Scope/acceptance suites inspect staged, physical, untracked and historical changes; reject stale review, missing tools, failed/timed-out/mutating checks. T025 retains all 16 original guard scenarios from `95b5bed59c918bc100880771ac6be4d127b8edb7` with context/attempt/review equivalents. |
| FR-007–009; handoff, rework, interruption and concurrency | T024 adds synchronized command conflicts, one completion owner, killed-owner lock release, SQLite/flush fault recovery and retained history. Recovery/rework/binding suites retain the same attempt or explicitly linked correction and exact prior identity. |
| FR-010–012; offline fixtures, protected features, safe reporting | Synthetic fixture isolation and versioned responses distinguish supplied approvals from authentication and live dispatch. T025 audits command availability and legacy bypass rejection. Corrected Linux full-suite/offline/protected-tree evidence passes; current macOS canonical acceptance remains blocked. |

The original regression migration audit preserves path/checkbox/failure/retry
invariants; it neither defers migration nor treats unavailable commands as passing.
No new test exception, skip, deadline waiver or product dependency is claimed here.

| Success criterion | Observed coverage |
| --- | --- |
| SC-001 | Actual CLI children exercise implement, handoff-only and authorized rework with no network/credentials/service dependency; the attempted-network negative control is detected before journal creation. |
| SC-002 | Context/precondition suites reject invalid artifacts, dependencies, approval and entry before implementation. |
| SC-003 | Scope/acceptance suites retain exact unrelated marker/description/order bytes and reject staged, historical, physical and check-induced violations. |
| SC-004 | Recovery/state/binding suites cover durable boundaries and one completion owner; the retained-file flush regression blocks repeated failure before later exact recovery. |
| SC-005 | Context/rework suites reject unauthorized correction, preserve immutable prior evidence, and distinguish semantic drift from permitted progress. |
| SC-006 | Protected-tree controls compare both immutable baseline tree IDs and raw file bytes/modes; final diffs remain empty. Live behavior remains unverified. |

## Original regression migration map

Original source is `95b5bed59c918bc100880771ac6be4d127b8edb7`; all 16 cases
retain these method names in `tests/test_single_task.py`. Shared setup now supplies
reviewed artifacts, the complete dependency table, canonical approval digest,
returned attempt ID and content-bound review. Rejected verify/complete scenarios
cannot pass through `command_unavailable`. T025's independent AST comparison
confirmed these migrated bodies were unchanged by the audit addition.

| Original/current method name | Preserved scenario |
| --- | --- |
| `test_complete_and_retry_preserve_other_task` | Context-backed begin; selected-only marker, real record-commit, completed verify/complete and unauthorized-edit rejection retained. |
| `test_interruption_after_checkbox_before_phase_update` | Durable interrupted publication is injected after marker replacement; resume recovers same attempt and exact checked bytes. |
| `test_reject_outside_scope_committed_and_untracked` | Both nonignored-untracked and then committed outside-file violations reject. |
| `test_reject_other_checkbox_and_selected_description` | Other marker and selected description mutations reject. |
| `test_staged_violation_hidden_by_worktree_restore` | Staged protected artifact remains rejected after worktree restoration; marker unchanged. |
| `test_committed_violation_hidden_by_revert` | Unauthorized committed artifact followed by revert remains rejected; marker unchanged. |
| `test_staged_other_task_hidden_by_worktree_restore` | Staged other-task marker remains rejected after restore; marker unchanged. |
| `test_historical_other_task_hidden_by_revert` | Other-task description history followed by revert remains rejected; marker unchanged. |
| `test_mode_change_requires_rework_even_without_git_filemode` | Physical executable mode drift after completion rejects even with core.filemode=false. |
| `test_absent_file_differs_from_sentinel_contents` | Typed absence after completion differs from literal <absent> file contents. |
| `test_staged_symlink_hidden_by_worktree_restore` | Staged symlink rejects after regular-file worktree restoration; marker unchanged. |
| `test_failed_acceptance_does_not_complete` | Nonzero acceptance blocks; marker unchanged. |
| `test_early_completion_without_evidence_rejected` | Manually checked marker without prepared intent rejects. |
| `test_acceptance_cannot_mutate_scope_or_complete` | Acceptance writes outside scope; completion rejects and selected marker remains unchanged. |
| `test_dirty_checkout_and_unknown_task_rejected` | Unknown task and dirty baseline produce specific context blockers. |
| `test_traversal_and_symlink_rejected` | Traversing feature/allowed path reject; symlink output rejects. |

In `test_complete_and_retry_preserve_other_task`, the final ad hoc T002 begin proves
legacy-entry rejection only. The added
`test_valid_context_cannot_replace_attempt_awaiting_handoff` separately proves a
valid context with prerequisite evidence cannot replace an attempt whose handoff
is pending. `entry_legacy_rejected` in the context suite checks exit 2,
`invalid_arguments`, empty stderr and unchanged state; the state suite retains
actual legacy evidence and blocks implicit adoption. The migration audit also
exercises every contract command's safe versioned response and rejects missing
commands/tools rather than accepting empty discovery or unavailable behavior.

## Historical macOS failure and outstanding acceptance

The earlier canonical run at
`abebbd637e470e7eb94febb26a14c3964f2163cb` is a **failed historical run**:
`make check PYTHON=/opt/homebrew/bin/python3.14` exited 2 at the unchanged
300-second raw aggregate deadline; the qualified phase did not run. It recorded
343 pre-timeout `ok` lines and no failure/error lines before timeout. Subsequent
cleanup removed the disposable baseline while the child finished, yielding 393
cases with 44 missing-file errors and two assertions. That cascade is retained as
failure evidence, not accepted behavior. See the
[historical combined-check record](../../development/bootstrap-t018-proposal.md#final-local-combined-check-incomplete-at-aggregate-deadline).

The **current** macOS canonical `make check` has not run and remains an unmet
acceptance gate. The historical native story measurements of 391 cases/405.181s
and 400 cases/426.535s exceed the canonical raw phase's 300-second
limit; this explains a runtime concern but is neither a current canonical outcome
nor permission to waive the gate. Windows and network-filesystem lock behavior
remain outside the initial supported local POSIX matrix.

T027 corrected-source local/offline/protected-tree and Linux qualification is
accepted. T028 remains unchecked: current macOS canonical acceptance is unmet.
Resolving that platform criterion requires a separately scoped, reviewed diagnosis
and validation path; this record changes no timeout, harness or acceptance rule.
The separately authorized draft [PR #55](https://github.com/wstein/workharbor-nxt/pull/55)
is a publication action, not a result established by the local CLI. CLI success
proves no authenticated owner identity, PR publication, label change, tracker
mutation, successful live Symphony dispatch, provider runtime, deployment or
unattended operation.
