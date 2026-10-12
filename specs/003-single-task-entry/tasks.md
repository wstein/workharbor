# Tasks: Locally verifiable single-task entry

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md),
[data-model.md](data-model.md), [CLI contract](contracts/cli.md), [quickstart.md](quickstart.md).
**Status**: Generated proposal; all tasks pending. No owner approval or implementation
is inferred from generation. Obtain required human reviews and run analyze before work.
**Regeneration basis**: Current design includes canonical inspection/review identities,
dependency-table grammar v1, phased regression migration and pre-intent recovery.
Existing T001–T028 IDs and all pending markers are retained.
**Tests**: Explicitly required by FR-010 and SC-001–SC-006; write the specified failing
cases before the corresponding implementation, then verify each story checkpoint.
**Constraints**: Python 3.11+ standard library and Git only. Features 001/002,
WORKFLOW.md, product code and vendored skills remain unchanged. Scope is local tooling.
Use disposable synthetic repositories; never mutate the real earlier features in tests.
`[P]` denotes independent files after prerequisites, not permission to spawn workers.

## Phase 1: Setup

Purpose: reusable offline fixtures with safe local evidence.

- [x] T001 Create synthetic two-task feature/context/prerequisite/review fixtures under `tests/fixtures/single-task/` and fixture helpers in `tests/test_single_task.py`; generate local commits/digests at runtime, isolate Git config/hooks/signing, and remove tracker credentials without printing them; assert fixture creation makes no network request (FR-010, FR-011).

## Phase 2: Foundational — durable attempt infrastructure

Purpose: shared state, locking and safe outcomes; blocks all story entry commands.

- [x] T002 Add failing journal/locking/output contract tests in `tests/test_single_task_state.py`: one lock owner, transaction failure, append-only events, immutable contexts, corrupt/unsupported/legacy state, and sanitized structured errors; assert no task mutation on invalid entry (FR-004, FR-009, FR-012).
- [x] T003 Implement shared state primitives in `scripts/single_task_state.py`: ignored per-worktree `.symphony/single-task/`, SQLite journal, durable context copies and nonblocking POSIX lock spanning command execution; enforce "Only one attempt is active per worktree", "Never edit/delete old events" and "Lock acquisition is nonblocking"; store immutable ID/context/baseline/tasks bytes/entry_mode/predecessor/time, monotonic event sequence/kind/payload digest/time, and transactional progress; reject unsafe state paths and unsupported persistence guarantees without cleanup (FR-004, FR-009).
- [x] T004 Add shared argument/error/output handling in `scripts/single_task.py` with schema_version, attempt ID, mode, progress, local verification and safe affected IDs/paths; preserve exact exit categories 0/2/3/4/5/6 from `specs/003-single-task-entry/contracts/cli.md`, always report owner_identity_authenticated=false and live_dispatch_verified=false, and never echo whole contexts or secret-bearing argv; wire lock acquisition for all commands including read-only status (FR-012).

Checkpoint: foundation tests pass; no feature-task execution or completion is implemented yet.

## Phase 3: US1 — Begin one approved task (P1; entry MVP)

Goal: return one validated implement context or a precise blocker without executing tasks.
Independent acceptance: a local repository with two pending tasks accepts one approved
selection with explicit satisfied/empty prerequisites; invalid selections, missing
approval and unmet prerequisites fail before acceptance commands or implementation.

### Tests

- [x] T005 [US1] Add failing context/entry tests in `tests/test_single_task_context.py` for malformed/missing/extra fields, duplicate/multiple/unknown task IDs, fenced-code pseudo-tasks, dirty worktrees, invalid artifact commits/digests, missing/changed approvals, missing/malformed dependency tables, omitted/extra/duplicate rows, duplicate tables, unknown/self IDs, cycles, contradictory rows, context/table mismatches, stale prerequisite evidence and progress-only versus semantic changes; assert preserved bytes and no implicit prerequisite execution (US1/AC1–AC3; SC-002, SC-005).

### Implementation

- [x] T006 [US1] Implement strict TaskContext validation/canonical digesting in `scripts/single_task_context.py`: "schema_version: 1", "reject unknown/missing fields", one task_id/feature, "nonempty owner_ref/evidence_ref", context_digest excluding itself, "nonempty unique exact paths", "nonempty reviewed descriptions", and checks with "ID, argv array of nonempty strings", "repository-relative cwd (or `.`), positive timeout_seconds, and expected exit 0"; enforce entry mode exactly `implement` or `owner-authorized rework`, artifact paths/commit/SHA-256 identities and source_assertion fields path/sha256/excerpt/owner_ref; canonical context JSON uses "UTF-8, sorted keys, compact separators", "ensure_ascii=False", "no NaN/Infinity and no trailing newline", excluding only approval.context_digest; require rework-only fields only in rework contexts and forbid globs/directories/.git/state/protected artifacts in allowed paths (FR-001, FR-003, FR-004).
- [x] T007 [US1] Implement feature/task/baseline/path validation in `scripts/single_task_scope.py`: existing direct specs child, regular unsymlinked spec/plan/tasks, unique `TNNN` outside fences, pending selected task for implement, clean Git worktree including nonignored untracked files, canonical contained paths with no symlink components; never reset or adopt existing changes (FR-001, FR-004; US1/AC2).
- [x] T008 [US1] Implement reviewed-artifact/approval comparison in `scripts/single_task_context.py`: locally resolvable ancestor commits and matching SHA-256 bytes; bind exact feature/task/scope/checks/dependencies/mode to approval; preserve approval only for unchanged task lines with normalized checkbox markers and strictly appended EOF `## Execution evidence` records `- Evidence: TNNN PATH SHA256`, requiring "64 lowercase hex digest", valid existing task and matching local content; existing footer records cannot change/disappear and all other prose remains byte-exact (FR-003; SC-005).
- [x] T009 [US1] Implement prerequisite/source validation in `scripts/single_task_context.py`: "explicit unique task IDs including `[]`", source_assertion path/sha256/excerpt/owner_ref and the exact dependency-table grammar v1 from data-model.md ("exactly one dependency table outside fenced code", exact headers `| Task | Prerequisites |` / `| --- | --- |`, "Rows are sorted by task ID", all tasks exactly once, sorted unique local dependency IDs, "`none` is the sole empty-list" spelling, LF excerpt with no trailing LF and only the documented CRLF comparison exception; no unknown/self/duplicate/cyclic dependencies), and "exactly one local record per dependency" containing task/feature/completed status/content manifest/implementation commit/passing check results/evidence_ref; verify current checked prerequisites, local commit-tree manifest identity and check binding; reject extra/missing/stale evidence or ambiguous source rather than guessing (FR-002; US1/AC2).
- [x] T010 [US1] Wire `begin --context PATH` and `status --attempt ID` in `scripts/single_task.py` to validated context, scope and journal; create one immutable implement attempt only after all gates pass, retain context/checks/baseline, and reject legacy ad hoc arguments or legacy state with recovery instructions; migrate existing begin/entry regression callers and shared context/attempt helpers in `tests/test_single_task.py` in this same task, with explicit legacy-argument rejection cases; enumerate retained completion/retry scenarios as pending until T017/T023, without deleting, skipping or weakening them to force green; never execute implementation or acceptance at entry (FR-001–FR-004, FR-007, FR-009, FR-012).

Checkpoint: entry/context tests pass for the validated-entry MVP. Existing end-to-end
completion/retry regressions remain required and may still fail while their commands
are unimplemented. Report each such failure as a blocker, never as a green full-suite
checkpoint. US1 alone is not a completion/retry tool or a passing repository handoff.

## Phase 4: US2 — Verify scope and complete exactly one task (P1)

Goal: accept only approved current content and checks, then change only the selected marker.
Independent acceptance: permitted local changes complete one task; wrong files/tasks,
staged-only changes, committed violations and failed/mutating checks reject completion.

### Tests

- [x] T011 [P] [US2] Add failing scope/semantic-review tests in `tests/test_single_task_scope.py` covering current/staged/untracked changes, commit-then-revert history, executable mode/file type/symlink/gitlink changes, other task marker/text/order mutation, typed absent manifest values, canonical manifest/digest vectors with fixed expected bytes and SHA-256 values (including non-ASCII paths, modes, absence and checkbox normalization), strict rejection of unknown/missing manifest/review fields, inspect without checks/task mutation, and missing/stale/negative semantic review (US2/AC1–AC2; FR-005, FR-006; SC-003).
- [x] T012 [P] [US2] Add failing acceptance/completion CLI tests in `tests/test_single_task.py` for inspect output identities/no-check/no-mutation behavior and local argv/cwd/timeout execution, missing executable, nonzero exit, missing check, pre/post-check content mutation, safe failure evidence and selected-checkbox-only completion; keep existing guard regressions as explicit scope cases (US2/AC1–AC4; FR-006, FR-012).

### Implementation

- [x] T013 [US2] Implement separate worktree/index/HEAD/intervening-commit inventories in `scripts/single_task_scope.py`, including nonignored untracked paths and path/type/mode/bytes manifests with "distinct typed absent values, not a sentinel string"; reject unauthorized changes even when later reverted, all unsafe symlink/gitlink outputs, and every tasks.md byte except the selected checkbox; emit exactly schema_version=1 and entries sorted "in Unicode code-point order", one entry per allowed path plus selected tasks path, regular entries path/type/mode/sha256 with type="regular", mode="100644" or "100755" and 64 lowercase hex digest, absent entries path/type="absent"/mode=null/sha256=null (FR-005; SC-003).
- [x] T014 [US2] Implement content-bound SemanticReview validation in `scripts/single_task_scope.py` for required "attempt_id", "task_id", "context_digest", "content_manifest_digest", "reviewer_ref", "evidence_ref", "approved: true"; invalidate review when approved content changes, preserve exact before/after tasks bytes/digests and normalize only the selected checkbox across completion; implement canonical manifest serialization/digesting ("UTF-8 JSON with sorted object keys, compact separators", "ensure_ascii=False", "no NaN/Infinity and no trailing newline", no Unicode normalization; hash raw bytes and normalize only the selected checkbox to ASCII space) and `inspect --attempt ID` output in `scripts/single_task.py` per data-model.md, return attempt_id/task_id/context_digest/content_manifest/content_manifest_digest with no review requirement, acceptance execution, task mutation or acceptance/completion events; independently recompute identity before/after acceptance and reject reviews made stale since inspection (FR-006; US2/AC1–AC2).
- [ ] T015 [US2] Implement acceptance execution in `scripts/single_task_acceptance.py`: use only reviewed ordered argv/cwd/finite timeout with no shell evaluation, validate scope/content before and after, bind check identity/exit/timeout/error category and semantic review to the current manifest, reject missing tools/mutations, and journal safe results without persisting raw subprocess output (FR-006, FR-010, FR-012; US2/AC3–AC4).
- [ ] T016 [US2] Implement completion intent and marker publication in `scripts/single_task_state.py`: under exclusive lock persist acceptance and exact pre/post intent durably before atomic tasks.md replace and directory fsync; advance active → acceptance-recorded → completion-prepared → checkbox-applied → completed-awaiting-commit without overwriting immutable entry_mode or old events; do not mark any task on failed validation/acceptance (FR-004, FR-005, FR-009).
- [ ] T017 [US2] Wire `verify --attempt ID --review PATH` and `complete --attempt ID --review PATH` in `scripts/single_task.py`; rerun current acceptance at completion, derive handoff-only after valid marker completion, report awaiting-commit separately from handoff-ready, and make repeated complete idempotent with no new attempt or implicit next-task selection; migrate completion/verification regression callers and review preparation in `tests/test_single_task.py` in this same task, preserving all earlier path/checkbox/failure assertions; retry/commit-binding expectations remain explicit blockers until T023 (FR-005–FR-008, FR-012).

Checkpoint: entry and scope/acceptance/completion tests pass; one selected task can
complete locally. Retry/commit-binding regressions not yet supported by US3 remain
reported blockers; do not claim a passing full-suite checkpoint or live handoff.

## Phase 5: US3 — Resume handoff or explicitly authorized rework (P1)

Goal: recover the same completed work or create a separately approved corrective attempt.
Independent acceptance: interrupt each durable boundary, resume without duplicated work,
bind a matching implementation commit and revalidate; unauthorized rework blocks while
explicit approved correction keeps the checkbox checked and preserves old evidence.

### Tests

- [ ] T018 [P] [US3] Add failing commit-binding/resume/crash tests in `tests/test_single_task_state.py` for every acceptance/intent/marker/journal boundary, including interruption before acceptance and after durable acceptance-recorded but before intent; assert fresh checks/review validation, unchanged entry task bytes, same attempt ID, retained old events, and no marker write before durable intent; cover stale review, changed sealed content, failing rerun and corrupt later-state missing intent as blockers (also for already-checked rework); exact pre/post-byte recovery, missing/corrupt evidence, unavailable or mismatched implementation commits, repeated verification preserving original commit identity, and failed handoff revalidation (US3/AC1–AC2, AC4; FR-008, FR-009; SC-004).
- [ ] T019 [P] [US3] Add failing rework tests in `tests/test_single_task_context.py` for absent owner approval/prior implementation, wrong predecessor/task/scope/checks, semantic drift, authorized new attempt with retained checked marker and immutable prior evidence; a retry counter or ready assertion alone must not authorize corrections (US3/AC3–AC4; FR-007; SC-005).

### Implementation

- [ ] T020 [US3] Implement immutable implementation binding in `scripts/single_task_state.py`: locally reachable commit, selected completed marker, exact sealed allowed-path bytes/types/modes and valid baseline/index/worktree/history scope; retain original implementation identity separately from latest verification HEAD, refuse conflicting rebinding and append verification events instead of overwriting evidence (FR-008, FR-009).
- [ ] T021 [US3] Implement crash recovery in `scripts/single_task_state.py`: implement both recovery branches in data-model.md: for valid active/acceptance-recorded state without intent, require exact entry task bytes and matching recorded content when sealed, validate current review/scope and rerun all checks before appending acceptance and durable intent; for prepared state, compare exact task pre/post bytes and sealed manifests, rerun acceptance with matching review and finish only that transition; retain the same attempt ID and all old events, never reuse passing results or write a marker before intent; treat later-state missing required intent as corruption, not pre-intent recovery; block corrupt/missing/unsupported/legacy state or unexpected content without resetting, reconstructing approval or deleting evidence (FR-008, FR-009; US3/AC2, AC4).
- [ ] T022 [US3] Implement owner-authorized rework entry in `scripts/single_task_context.py` and `scripts/single_task_state.py`: require "prior_attempt_id", "prior_implementation_commit", "rework_approval" binding previous evidence and exact corrective scope/checks; create a new linked immutable attempt from a clean reviewed baseline for an already checked task, keep pre/post marker bytes identical, and seal new acceptance without reopening tasks (FR-004, FR-007, FR-009; US3/AC3).
- [ ] T023 [US3] Wire `rework --context PATH`, `record-commit --attempt ID --commit SHA` and `resume --attempt ID --review PATH` in `scripts/single_task.py`; enforce three effective modes and contract exits, require bound commit plus fresh passing acceptance for handoff-ready, allow commit binding during awaiting-commit recovery and never implement in handoff-only mode; migrate the remaining retry/interruption/commit-binding regression callers in `tests/test_single_task.py` now, preserving their original invariants under the new interface; all retained regression scenarios must pass before the US3 checkpoint (FR-007–FR-009, FR-012).
- [ ] T024 [US3] Add synchronized competing-process tests in `tests/test_single_task_state.py` for begin/complete/rework/status conflicts, one completion owner, kill-and-resume lock release, SQLite/flush failures and immutable attempt history; use barriers/pipes and injected durable boundaries, not timing-only sleeps; verify blockers retain recoverable evidence (FR-009; SC-004).

Checkpoint: all three modes work offline; interruption/concurrency/rework tests pass and history is preserved.

## Phase 6: Polish and cross-cutting verification

- [ ] T025 Audit `tests/test_single_task.py` regression migration already delivered in T010/T017/T023: map every original case to its new context/attempt/review equivalent, require all preserved path/checkbox/failure/retry invariants to pass with no missing/skipped cases, and assert rejected legacy entry cannot bypass validation; confirm all contract commands return safe versioned outcomes and missing commands/tools cannot pass discovery. This task audits coverage; it must not defer interface migration or repair an earlier falsely green checkpoint (FR-001–FR-012).
- [ ] T026 Update `docs/development/single-task.md` for context preparation, semantic review, prerequisites, approvals, begin/inspect/verify/complete/record-commit/resume/rework, explicit legacy recovery and lock/platform limits; use synthetic examples and distinguish local evidence from owner authentication and untested live Symphony; document completion/commit/evidence order without changing WORKFLOW.md or Features 001/002 (FR-010–FR-012).
- [ ] T027 Run the matrix in `specs/003-single-task-entry/quickstart.md` with required-file preflight and all three modes; add offline/network-attempt and protected-tree checks in `tests/test_single_task_context.py` and `tests/test_single_task_scope.py`, assert zero credential/service dependencies and unchanged Feature 001/002 tree identities against the recorded baseline, then run `make check` and `git diff --check` (FR-010, FR-011; SC-001–SC-006).
- [ ] T028 Record actual Linux/macOS commands, exits, scenario/requirement coverage, immutable baseline and implementation commit references in `docs/exec-plans/completed/003-single-task-entry.md`; record missing-platform evidence as a blocker, not a pass, and retain local-only/live-unverified limitations; capture commit hashes after their referenced commits, without self-referential evidence or automatic publication (FR-012; SC-001–SC-006).

## Verification commands

Run from the repository root. After a task's implementation, run its matching suite:
T002–T004/T016/T018/T020–T024 use `python3 -m unittest discover -s tests -p 'test_single_task_state.py' -v`;
T005/T006/T008/T009/T019 use `python3 -m unittest discover -s tests -p 'test_single_task_context.py' -v`;
T007/T011/T013/T014 use `python3 -m unittest discover -s tests -p 'test_single_task_scope.py' -v`;
T001/T010/T012/T015/T017/T023/T025 use `python3 -m unittest discover -s tests -p 'test_single_task.py' -v`.
At every story checkpoint run `python3 -m unittest discover -s tests -p 'test_single_task*.py' -v`
with required-file preflight for that story's suites, then `make check` and
`git diff --check`. T010/T017 migrate callers with their commands; they must list
remaining completion/retry failures against T017/T023. Those intermediate runs can
be red and are not repository acceptance or implementation handoff. Do not skip,
delete, weaken assertions or report pending regressions as passing. The first
required green full-suite checkpoint is US3 after T024, and final acceptance remains
T027/T028. Test-authoring tasks initially demonstrate meaningful failure
against missing behavior; do not report those tests as passing implementation evidence.
T026 checks documentation links via `make check`; T027/T028 require the full quickstart
matrix and platform evidence, not just one filtered suite. If a shared module changes,
include all affected existing suites. All commands must actually run; missing files
or tools are blockers, never passing empty discovery. T014 also runs
`python3 -m unittest discover -s tests -p 'test_single_task.py' -v` for the inspect
CLI contract, with focused inspect cases authored in T012 before implementation.

### Test-authoring completion and prerequisite evidence

T002, T005, T011, T012, T018 and T019 are test-authoring tasks. Their own completion
means **verified red-test evidence**, not implemented behavior. Before entry, the
owner must approve an exact expected-outcome matrix: collected test IDs, expected
assertion failure/CLI outcome, missing behavior, and implementing successor task.
Use T003/T004 for T002, T006–T010 for T005, T013/T014 for T011,
T014–T017 for T012, T020/T021/T023 for T018 and T022/T023 for T019.
Every new case must execute its intended assertions against a valid fixture. A
missing module causing failed test import, empty discovery, syntax/setup errors,
missing tools, timeout, skipped tests or an unrelated failure blocks completion.
Unsupported CLI behavior is an expected red outcome only when a collected test
actually invokes the CLI and fails its specified behavior assertion.

Record the immutable source/content, exact commands, collected IDs, raw exits and
per-case actual versus expected outcomes. A reviewed local verifier must exit zero
only if that exact matrix matches and all previously implemented behavior still
passes; it must reject extra/missing cases, unexpected failures and unexpected green
cases (which require review of whether the test exercises its intended behavior).
The verifier may live in the task's permitted test file or in explicitly reviewed
argv; this rule grants no additional file scope. Never accept any nonzero suite
exit indiscriminately or use `|| true`, skips, xfail or weakened assertions.

The test-authoring task can be checked complete after this verifier and its other
task-specific acceptance checks pass. Its successor requires that passing verifier
record, commit/content binding and the explicit red matrix; it does **not** require
the implementation tests to be green before their implementation task can start.
Retain raw red outcomes as evidence, separately from the verifier's zero exit.
Unfinished T017/T023 regression migrations remain explicit checkpoint blockers as
described above; they are not test-authoring acceptance. T024 and every implementation
task require passing evidence for their delivered behavior. US3 and final full-suite
acceptance require all tests green, with no remaining expected-red exceptions.
No test-authoring issue is dispatchable until its concrete matrix and verifier argv
have been reviewed; this planning rule alone supplies neither artifact nor approval.

## Dependencies and execution order

Explicit prerequisite table; `none` means an intentionally empty list. All listed IDs
are conjunctive. Same-file tasks remain sequential. These implementation dependencies
are distinct from the synthetic prerequisites the CLI validates in its acceptance tests.

| Task | Prerequisites |
| --- | --- |
| T001 | none |
| T002 | T001 |
| T003 | T002 |
| T004 | T003 |
| T005 | T004 |
| T006 | T005 |
| T007 | T006 |
| T008 | T007 |
| T009 | T008 |
| T010 | T009 |
| T011 | T010 |
| T012 | T010 |
| T013 | T011, T012 |
| T014 | T013 |
| T015 | T014 |
| T016 | T015 |
| T017 | T016 |
| T018 | T017 |
| T019 | T017 |
| T020 | T018, T019 |
| T021 | T020 |
| T022 | T021 |
| T023 | T022 |
| T024 | T023 |
| T025 | T024 |
| T026 | T025 |
| T027 | T026 |
| T028 | T027 |

Phase graph: Setup → Foundation → US1 → US2 → US3 → Polish.
US2 reuses US1's validated attempt; US3 reuses US2's sealed completion. Each story's
acceptance can run alone against disposable fixtures once its prerequisites exist;
no real Feature 001/002 tasks are executed to satisfy these dependencies.

## Parallel examples per story

- US1: no parallel implementation proposed; T006–T010 build on shared context validation.
- US2: after T010, T011 (scope tests) and T012 (acceptance CLI tests) use different files
  and may be authored independently. They must both finish before T013.
- US3: after T017, T018 (state/recovery tests) and T019 (rework context tests) use
  different files and may be authored independently. They both precede T020.

These are authoring opportunities, not instructions to dispatch multiple agents.
Tests for unimplemented behavior must fail meaningfully first; a passing empty suite
or absent test executable is not evidence. Do not broaden acceptance of a partial
story because a preceding/following phase is still pending.

## Implementation strategy

1. Deliver Setup/Foundation and US1 as the entry MVP; stop for review after T010 with exact targeted results and outstanding full-suite blockers, not a green repository handoff.
2. Add US2 and review real local scope/acceptance/checkbox behavior after T017; explicitly retain US3 retry blockers until T023 migrates them.
3. Add US3 and validate interruptions, commit identity and authorized rework after T024.
4. Complete cross-platform/offline/protected-feature evidence before declaring the
   feature implemented. Initial 17 repository tests are baseline evidence only.
5. Use focused Conventional Commits. Mark a task complete only after its own concrete
   acceptance is met. This file's generation checks no boxes and grants no approval.

## Traceability

| Requirement | Primary tasks |
| --- | --- |
| FR-001 | T005–T007, T010 |
| FR-002 | T005, T009, T010 |
| FR-003 | T005, T006, T008 |
| FR-004 | T003, T006, T010, T022 |
| FR-005 | T011, T013, T016, T017 |
| FR-006 | T012, T014–T017 |
| FR-007 | T010, T019, T022, T023 |
| FR-008 | T018, T020, T021, T023 |
| FR-009 | T002–T004, T016, T018, T020–T024 |
| FR-010 | T001, T015, T027 |
| FR-011 | T001, T027 |
| FR-012 | T004, T015, T023, T026, T028 |
| SC-001 | T010, T017, T023, T027 |
| SC-002 | T005–T010 |
| SC-003 | T011–T017, T027 |
| SC-004 | T018, T021, T024 |
| SC-005 | T005, T008, T019, T022 |
| SC-006 | T026–T028 |

## Contract and entity coverage

| Contract command | Tests | Implementation / wiring |
| --- | --- | --- |
| begin, status | T002, T005, T010 | T003–T010 |
| inspect | T011, T012 | T013, T014 |
| verify, complete | T011, T012 | T013–T017 |
| record-commit | T018 | T020, T023 |
| resume | T018, T024 | T021, T023 |
| rework | T019, T024 | T022, T023 |

TaskContext and approval/source/prerequisite records belong to US1 (T006–T010).
Shared immutable Attempt/Event state belongs to Foundation (T002–T004).
ContentManifest, SemanticReview and acceptance evidence belong to US2 (T011–T017).
Implementation identity, recovery and linked rework belong to US3 (T018–T024).
T025 audits regressions across these contracts; T027/T028 establish final local and
platform evidence. No task generation result substitutes for human approval or
implementation verification.
