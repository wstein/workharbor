# Feature 003 local validation guide

Status: the context/attempt/review/journal CLI is implemented locally; final feature
acceptance still requires the matrix and platform evidence below. See the
[operator guide](../../docs/development/single-task.md) for the current commands.
No live tracker, Symphony service, credentials or product host is required; live
dispatch remains unverified.

## Prerequisites
Use Python 3.11+, Git, Make and Bash on Linux or macOS with a local filesystem.
Run only in a development checkout; fixtures create disposable synthetic repositories.
Remove tracker credentials from the test environment; never print them. Approvals
and semantic review records in tests are explicit synthetic owner attestations,
not claims of authenticated real-owner approval.

## Inspect local regression coverage
From the repository root:

```sh
python3 -m unittest discover -s tests -p 'test_single_task.py' -v
make check
git diff --check
```
Passing this regression file alone does not establish the full Feature 003 matrix
or evidence on both supported platforms.
Select Feature 003 before Spec Kit planning/tasks commands; the repository Makefile
still explicitly checks Feature 001 bootstrap prerequisites and does not change the
selected local feature.

## Full local acceptance
Run these commands against the implementation under review. Required-file
preflight prevents an empty unittest discovery from being mistaken for success:

```sh
test -f tests/test_single_task_context.py &&
test -f tests/test_single_task_state.py &&
test -f tests/test_single_task_scope.py &&
python3 -m unittest discover -s tests -p 'test_single_task*.py' -v
make check
git diff --check
```

The suites must invoke the [CLI contract](contracts/cli.md) against temporary fixture
repositories, generating local revisions/context/review records rather than using
Features 001/002 as mutation targets. Exercise this matrix on both target platforms:

| Scenario | Required observation | Source |
| --- | --- | --- |
| Begin one task with explicit empty or satisfied dependencies | One implement attempt, no other task starts | US1; FR-001–004 |
| Unknown/duplicate/multiple task, unsafe feature, dirty worktree | Block before checks/implementation; preserve existing work | US1; edge cases |
| Missing/malformed/duplicate/incomplete dependency table, unknown/self IDs, cycles, wrong context IDs, missing/failed/stale evidence | Precise prerequisite blocker; never auto-run dependencies | FR-002 |
| Missing approval, changed requirements/scope/checks/dependencies | Block; no remote owner-authentication claim | FR-003/004 |
| Checkbox-only or valid appended evidence-footer progress | Approval retained; unrelated execution-task markers still protected | FR-003/005 |
| Inspect current content, then review its returned manifest/digests | No acceptance execution or task mutation; canonical vectors agree on both platforms; subsequent content mutation makes review stale | FR-006 |
| Missing/stale semantic review or allowed-file semantic violation | Reject completion until content-bound operator review | FR-006 |
| Allowed content with passing checks | Only selected marker checked; sealed evidence retained | US2 |
| Untracked, staged-only, mode/type change, commit then revert | Reject unauthorized scope across all inventories | FR-005 |
| Missing tool, nonzero exit, timeout, mutating acceptance command | Failed evidence; no new completion claim | FR-006 |
| Interrupt before acceptance or after acceptance-recorded but before intent | Same attempt/history; exact entry task bytes; fresh review/check validation before durable intent and marker publication; stale review, changed sealed content, failed rerun or later-state missing intent blocks; include checked rework | US3; FR-009 |
| Interrupt after prepared intent, marker write and final journal event | Resume exact transition without duplicate work or lost history | US3; FR-009 |
| Commit binding, repeated verification, changed completed content | Original implementation identity retained; handoff-only never reimplements | FR-008 |
| Two begin/complete/rework processes and process termination | One lock owner; competitor conflicts; crash releases lock safely | FR-009 |
| Explicit authorized rework vs missing authorization | New linked attempt only for approved correction; checked marker preserved | FR-007 |
| Missing/corrupt/legacy journal | Actionable blocker, no reset/adopted baseline | FR-009 |
| Network attempt by entry code or accidental credential dependency | Local test fails; no tracker/Symphony integration silently required | FR-010/012 |

Use controlled test synchronization for races and failure injection at durable write
boundaries, not timing-only sleeps. Keep original evidence snapshots to assert
history did not change. Instrument entry-code network calls as failures; acceptance
fixtures are known local programs, not arbitrary sandboxed commands.

## Scope and evidence review
Record the pre-implementation baseline commit before making changes. Compare both
`specs/001-workspace-lifecycle/` and `specs/002-symphony-doc-smoke/` tree identities
against that baseline before/after tests and in the final diff; both must match.
Record actual command/exit/platform results, scenario coverage and implementation
commit in the review evidence. Do not put a commit's own hash in a file included in
that commit: complete/commit first, then record the hash in local journal/review.

No tasks are marked complete by this guide. Local acceptance does not
create an issue/PR, change labels, authenticate a real owner or demonstrate live
Symphony. Those remain separately authorized and evidenced operations.
