# Feature 003 local single-task evidence

Status: T024–T028 are locally accepted with independent review and actual native
Linux/macOS canonical results below. Feature 003 is verified for the supported
local POSIX matrix; live dispatch and product runtimes remain unverified.
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
| FR-010–012; offline fixtures, protected features, safe reporting | Synthetic fixture isolation and versioned responses distinguish supplied approvals from authentication and live dispatch. T025 audits command availability and legacy bypass rejection. The corrected T028 Linux/macOS canonical gates below pass the full collection, including offline and protected-tree controls. |

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

## Historical macOS failure

The earlier canonical run at
`abebbd637e470e7eb94febb26a14c3964f2163cb` is a **failed historical run**:
`make check PYTHON=/opt/homebrew/bin/python3.14` exited 2 at the unchanged
300-second raw aggregate deadline; the qualified phase did not run. It recorded
343 pre-timeout `ok` lines and no failure/error lines before timeout. Subsequent
cleanup removed the disposable baseline while the child finished, yielding 393
cases with 44 missing-file errors and two assertions. That cascade is retained as
failure evidence, not accepted behavior. See the
[historical combined-check record](../../development/bootstrap-t018-proposal.md#final-local-combined-check-incomplete-at-aggregate-deadline).

The earlier T027 record left macOS canonical acceptance unmet. Its native story
measurements of 391 cases/405.181s and 400 cases/426.535s explained a runtime
concern without qualifying the canonical gate. The separately scoped T028
diagnosis and actual corrected-platform gates below resolve that criterion;
they do not convert this historical failure into a pass. Windows and
network-filesystem lock behavior remain outside the supported local POSIX matrix.

T027 corrected-source local/offline/protected-tree and Linux qualification was
accepted before the T028 unblock. Historical
[PR #55](https://github.com/wstein/workharbor-nxt/pull/55) was a separately authorized
publication action, not a result established by the local CLI. CLI success
proves no authenticated owner identity, PR publication, label change, tracker
mutation, successful live Symphony dispatch, provider runtime, deployment or
unattended operation.

## T028 macOS diagnosis and canonical acceptance

The separately authorized unblock starts from accepted main
`fe5dcda4d25107808f1aa2e02d35f74741f52ad2`. The dedicated `--no-local`
author clone uses the actual GitHub origin and branch `fix/macos-canonical-checks`;
the existing workspace and live work were preserved. Diagnostic execution used
a separate frozen clone and pristine disposable worktrees of that accepted SHA.

The same native macOS 27.0.1 arm64 setup and pinned PATH above were used. Both
the outer `/opt/homebrew/bin/python3.14` and nested PATH-resolved `python3`
reported Python 3.14.8 and resolved to
`/opt/homebrew/opt/python@3.14/bin/python3.14`.

The raw and qualified phases were measured **sequentially**, each with a finite
900-second diagnostic deadline and process-group cleanup before removing its
scratch checkout. The accepted checker's own phase deadlines were not modified
for those measurements. Actual harness invocations were:

```sh
env PATH=/opt/homebrew/opt/python@3.14/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin \
  /opt/homebrew/bin/python3.14 -I -B /private/tmp/workharbor-macos-diagnostic.py raw
env PATH=/opt/homebrew/opt/python@3.14/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin \
  /opt/homebrew/bin/python3.14 -I -B /private/tmp/workharbor-macos-diagnostic.py qualified
```

The raw child ran `make check-raw` with the host interpreter's `-I -B` flags and
an external `pycache_prefix`. The qualified child used that same interpreter and
flags to invoke the accepted checker's `acceptance_suite` in a fresh process.
Exact child argv, disposable cwd, revisions, versions, elapsed times and exits
are retained in the corresponding JSON records alongside the unedited logs at
`/private/tmp/workharbor-macos-diagnostic-evidence/`.

| Frozen accepted-main diagnostic | Actual result |
| --- | --- |
| Raw `make check-raw` | 410 tests, 489.194s; outer elapsed 490.104s; exit 0 |
| Manifest-qualified collection | 808 tests, 836.836s; outer elapsed 837.411s; exit 0; zero failures/errors |

These are **diagnostic observations, not canonical acceptance**. They establish
that the old raw 300-second and qualified 600-second aggregate limits are both
below actual native completion time. The focused implementation commits are:

- `845264f05ed1dfacbf09232187592f2a4cf5ef8f`: own the ordinary POSIX check process
  group, kill it on timeout/interruption/parent exit, reap the leader, observe
  absence or zombie-only state with bounded native `ps`, and drain inherited
  output before checkout removal. Unavailable cleanup evidence retains the
  disposable checkout and fails closed. This is process cleanup, not a sandbox
  for deliberately detached owner-approved commands.
- `3e625c7b3a2d0e76aca517814169fac4e42a46c0`: set separate finite raw/qualified
  aggregate budgets to 600/1200 seconds, approximately 22%/43% above the measured
  elapsed times. The 300-second utility-command default, approved per-task and
  package deadlines, required IDs, retained-failure policy and existing assertions
  are preserved.

Independent lifecycle review passed the complete owning suite: 48 tests in
33.078s, exit 0. The new real-grandchild regression exercises timeout and successful
parent exit, asserting that the group has no live member before removing its
actual disposable cwd. Against the frozen original checker, both scenarios fail
at the inherited-pipe deadline (16.017s total); failing probes are cleaned up.
The final budget/timeout checks passed two tests in 0.932s, exit 0. Independent
review cleared exact code head `3e625c7b3a2d0e76aca517814169fac4e42a46c0`.

Initial required-file preflight at that head passed all ten quickstart files and
validated 810 unique collected cases against all 485 required manifest IDs, with
no retained failures. The full collection includes all 401 Feature 003 story
cases and the offline, protected-tree, recovery and three-mode matrix above;
the canonical run executes those cases, so no additional duplicate story run is
claimed. Protected Feature 001/002 trees match the immutable `95b5bed5` baseline,
accepted `fe5dcda4` and this implementation head.

The first native canonical run on `3e625c7b3a2d0e76aca517814169fac4e42a46c0`
**failed**: raw 410 tests/425.529s, exit 0; qualified 810 tests/781.427s,
zero assertion failures but one error, manifest-qualified RED. Make exited 2,
the wrapper exited 1 after 1209.826s, and source identity remained unchanged.
`test_successful_parent_cannot_leave_background_descendant` raised `EPERM`
at the existing acceptance runner's final group `SIGKILL`; the raw OS error
became `PersistenceFailure`. The failed group's state was not captured, so no
retrospective zombie or sandbox-permission conclusion is claimed. Three bounded
native repeats and 20 direct owned-background cleanup probes did not reproduce
the error. The failed logs/JSON remain intact at
`/private/tmp/workharbor-macos-canonical-failed-3e-evidence/` (log SHA-256
`affc8dfcfb2e486414627d6fa70a06738d07391c0ab2935e4bd357ec360226b5`, JSON
`c25e86d2ccba44d8880bbdbe2f3a1d85e8de6184168f0c17b0c2dc09d77ac558`).

The separately authorized focused correction
`31ce19813a980877402a43d48e5ed5a57366d08a` observes native group state only when
signal 0, TERM or KILL returns `EPERM`. A zombie-only group still counts as
background for acceptance rejection; TERM/KILL permission failures are cleared
only for confirmed absent/zombie-only groups. Live, unavailable or malformed
observations raise safe `AcceptanceFailure('process_cleanup')`. All per-check,
TERM-grace and leader-reap deadlines and previous assertions are unchanged.
Its complete owning suite passed 12 tests/11.822s, including real background and
timeout controls plus deterministic absent/zombie/live/unobservable/malformed
cases for each signal stage. This correction requires a fresh full native run
and matching Linux gates; the earlier `3e625c7` result is not a passing native run.
Independent exact review cleared `31ce198`: the deterministic control and both
real background/timeout controls passed three tests/5.772s. Against frozen
`3e625c7`, the new deterministic test fails all 15 EPERM subcases in 0.200s.
Corrected-head preflight passed 811 unique cases/485 required IDs, including all
402 Feature 003 story cases and the same ten required files/protected trees.

The corrected native canonical command was run from the pristine dedicated
checkout at exact `31ce19813a980877402a43d48e5ed5a57366d08a`:

```sh
env PATH=/opt/homebrew/opt/python@3.14/libexec/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin \
  make check PYTHON=/opt/homebrew/bin/python3.14
```

The native wrapper `/private/tmp/workharbor-macos-corrected-run.py` recorded
that command with a finite 2700-second outer deadline, source/version evidence,
unedited output and process-group drain. Its actual result is **PASS**:

| Corrected native macOS gate | Actual result |
| --- | --- |
| Raw `make check-raw` | 411 tests, 425.257s, exit 0 |
| Manifest-qualified collection | 811 tests, 783.530s, exit 0; zero failures/errors; GREEN |
| Canonical command and wrapper | Both exit 0; no timeout; 1211.603s total; source unchanged; groups drained |

Both outer and nested interpreters remained Python 3.14.8 on macOS 27.0.1 arm64.
The unedited raw output retains pre-existing SQLite resource warnings; this is
zero test failures/errors, not a warning-free claim. The corrected evidence is
retained at `/private/tmp/workharbor-macos-corrected-evidence/`: canonical log
SHA-256 `3f5204aa3f7de5f3b17445192825f55f8504fc77696a5426887e67372cbec653`,
JSON `98db27a0d76f88922a6f508809b1ca2007ee078824dc4483da6afae5f7ea933d`.

The earlier `3e625c7` Linux CI ran `python3 -I -B scripts/check_ci.py`, the same checker that
`make check` delegates to, on Ubuntu 24.04.5 LTS. Both jobs invoked
`/usr/bin/python3`; its version was not logged. Both reported successful canonical
acceptance with zero raw failures/errors and no retained-failure exception:

| Linux code-head gate | Raw `make check-raw` | Qualified acceptance |
| --- | --- | --- |
| [Push 38347200559](https://github.com/wstein/workharbor-nxt/actions/runs/38347200559) | 410 tests, 168.748s, exit 0 | 810 tests, 248.443s, exit 0 |
| [PR 38347235698](https://github.com/wstein/workharbor-nxt/actions/runs/38347235698) | 410 tests, 104.354s, exit 0 | 810 tests, 168.368s, exit 0 |

The push checkout is exact code head
`3e625c7b3a2d0e76aca517814169fac4e42a46c0`. The PR checkout is actual synthetic
merge `10079b012ac49a4b680f76036408a7d8290ce771`, with ordered baseline
`fe5dcda4d25107808f1aa2e02d35f74741f52ad2` and that same head. These historical
results do not stand in for corrected `31ce198` acceptance or CI on the later
evidence/marker bookkeeping head.

Corrected `31ce198` Linux gates also passed on native Ubuntu 24.04.5 LTS using
`python3 -I -B scripts/check_ci.py`; the `/usr/bin/python3` version was not logged.
Both qualified runs reported zero raw failures/errors, manifest-qualified GREEN:

| Corrected Linux gate | Raw `make check-raw` | Qualified acceptance |
| --- | --- | --- |
| [Push 38349598810](https://github.com/wstein/workharbor-nxt/actions/runs/38349598810) | 411 tests, 124.008s, exit 0 | 811 tests, 189.233s, exit 0 |
| [PR 38349602352](https://github.com/wstein/workharbor-nxt/actions/runs/38349602352) | 411 tests, 137.541s, exit 0 | 811 tests, 219.447s, exit 0 |

The push checkout is exact `31ce19813a980877402a43d48e5ed5a57366d08a`.
The PR checkout is synthetic merge `ae5be657e5511bae77227a2083584ea2a371e26e`
with ordered baseline `fe5dcda4d25107808f1aa2e02d35f74741f52ad2` and that head.
Later evidence/marker bookkeeping still requires its own exact review and fresh CI.

Retained diagnostic SHA-256 identities:

- raw log: `c56aee117feea5b84be7a0c9832238c98c3920b8537c596cf7a96f6dd4f87b7e`;
  raw JSON: `863829c49f288e721c738e6f651c32bd788b84d6c3237726302204d89a33d5e8`.
- qualified log: `c58013e71ffcae5c36ba8a63e845847934e7c4afecf0d3cacdf5104cb0885e86`;
  qualified JSON: `ea70e75331c504c774a5d0eb45f3965430343ec78b45b87441f8b77674547ac6`.

This record names implementation revisions that already existed before its
bookkeeping commit; it does not embed that bookkeeping commit's own hash.
Local POSIX acceptance still proves no owner authentication, successful live
Symphony dispatch, provider runtime, deployment or unattended operation.
