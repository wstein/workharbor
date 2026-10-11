# Feature Specification: Locally verifiable single-task entry

**Feature identifier**: `003-single-task-entry`

**Feature Branch**: Not created; specification only.

**Created**: 2026-10-12

**Status**: Draft — awaiting human specification review. Existing tooling is not
assumed to satisfy this specification; no implementation assessment is claimed.

**Input**: Define a locally verifiable entry for exactly one existing Spec Kit task,
validating feature, task ID, prerequisites and allowed changes, with implement,
handoff-only and owner-authorized rework modes. No live tracker or Symphony is
required for local verification. Features 001 and 002 remain unchanged.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Begin one approved task (Priority: P1)

A repository operator supplies an existing feature, exactly one task, locally
recorded approval evidence, prerequisite evidence, permitted changes and acceptance
checks. The operator receives an explicit decision to proceed or a precise blocker.
No other task is started, completed or silently included as prerequisite work.

**Why this priority**: A bounded task must have a trustworthy local starting context
before any implementation is permitted.

**Independent Test**: In a disposable local repository, select one of two pending
tasks with satisfied prerequisites; verify that only the selected task is eligible.
Repeat with missing and contradictory inputs and observe rejection without task
or implementation changes. No account credentials or network service are needed.

**Acceptance Scenarios**:
1. **Given** an existing feature and one pending task with complete approval/scope
   and prerequisite evidence, **When** entry is requested, **Then** mode is
   `implement` and the task, baseline and permitted changes are recorded.
2. **Given** an unknown feature/task, duplicate task ID, multiple selected tasks or
   unmet prerequisite, **When** entry is requested, **Then** entry is blocked with
   the specific reason; no prerequisite task runs automatically.
3. **Given** missing approval evidence or scope inconsistent with the approved task,
   **When** entry is requested, **Then** the operator must correct the context;
   existence of a task or locally supplied assertion alone is not a verified owner identity.

### User Story 2 - Verify scope and complete exactly one task (Priority: P1)

The operator checks the selected task's actual changes and acceptance results.
Completion is recorded only after required checks pass and unrelated tasks are
confirmed unchanged. The evidence supports review without claiming live dispatch.

**Why this priority**: Passing checks cannot justify changes outside the approved task.

**Independent Test**: Complete a permitted change in a disposable repository, then
repeat with out-of-scope files, another task's checkbox or a failing acceptance check.

**Acceptance Scenarios**:
1. **Given** permitted changes and successful acceptance, **When** completion is
   requested, **Then** only the selected completion checkbox may be changed and
   mode becomes `handoff-only`; other task text/order/checkmarks remain identical.
2. **Given** an unauthorized change, including a committed change since baseline,
   **When** verification runs, **Then** it fails and reports affected paths/tasks.
3. **Given** a failed, missing or unavailable required check, **When** completion is
   requested, **Then** no completion is claimed and evidence records the failure.
4. **Given** an acceptance check changes task content or scope, **When** it ends,
   **Then** a second scope/content check rejects completion until the changed
   state is reviewed and validated; no stale passing result is reused.

### User Story 3 - Resume handoff or explicitly authorized rework (Priority: P1)

After interruption, the operator resumes a completed task's handoff using matching
local evidence. Further implementation needs explicit owner-authorized rework;
neither a retry counter nor a completed checkbox grants permission to change code.

**Why this priority**: Retries must preserve completed work without permitting silent scope expansion.

**Independent Test**: Interrupt local completion at each recorded boundary, resume
using saved evidence, then try unauthorized and authorized corrective changes.

**Acceptance Scenarios**:
1. **Given** a completed task and matching baseline/content/check/commit evidence,
   **When** resumed, **Then** entry selects `handoff-only`, revalidates acceptance,
   and permits no new implementation, task reopening or subsequent task execution.
2. **Given** interruption between recording successful acceptance, changing the
   checkbox and recording completion mode, **When** resumed, **Then** matching
   evidence recovers the completed state without duplicating implementation.
3. **Given** explicit local owner rework approval identifying prior implementation,
   corrective scope and acceptance, **When** entry is requested, **Then** mode is
   `owner-authorized rework`; old evidence is retained and only corrections run.
4. **Given** missing/stale evidence, changed completed content, failing revalidation
   or absent rework approval, **When** resumed, **Then** entry blocks with recovery
   instructions; it never deletes state or silently chooses a new baseline.

### Edge Cases

- Reject traversal, escaping/symlinked feature paths and ambiguous task identities.
- Preserve pre-existing dirty work; block first entry until an isolated clean
  baseline is supplied. Never clean, reset, overwrite or adopt unrelated changes.
- Treat concurrent attempts to use the same local task state as a conflict; only
  one attempt may record completion. Retain recoverable evidence on interruption.
- Refuse missing/corrupt state rather than constructing evidence from a checkbox.
- Changes to requirements, task scope, acceptance or dependencies invalidate prior
  approval; pure progress checkbox/evidence updates may retain it after comparison.
- An intentionally empty prerequisite list is valid; an omitted list is not proof
  that there are no prerequisites. Prerequisite checkmarks alone are insufficient.
- Preserve secrets: evidence contains command identities, statuses and safe
  references, never credentials. Untrusted task text is not an executable command.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Entry MUST identify exactly one existing feature and unique existing
  task; validate required spec/plan/tasks artifacts and reject unsafe path selection.
- **FR-002**: Entry MUST validate explicit local prerequisite IDs and matching
  completion/check evidence against the selected task's documented dependencies.
  Missing, contradictory or unsatisfied evidence blocks; no prerequisite runs implicitly.
- **FR-003**: Entry MUST require local approval records naming the reviewed artifact
  revision, approving owner reference and permitted task scope. It MUST compare
  current artifacts against those revisions, accepting only progress-only differences
  without renewed approval. It MUST distinguish supplied evidence from independently
  authenticated owner identity, which is outside this local feature's scope.
- **FR-004**: Entry MUST record an immutable per-attempt baseline and exact permitted
  repository paths, selected task, acceptance checks and execution mode. Scope/check
  changes require new approval and a new attempt retaining prior evidence.
- **FR-005**: Verification MUST inspect tracked, staged, unstaged, committed-since-
  baseline and nonignored untracked changes before and after acceptance. It MUST
  reject unauthorized changes, including modifications of other task text/order/
  checkmarks. Only the selected checkbox may change in tasks.md.
- **FR-006**: Completion MUST require all approved checks to execute successfully on
  the current content plus semantic-scope review evidence. Missing tools, failed
  checks or unverifiable results MUST NOT count as passing. Check invocation MUST
  not interpret untrusted issue/task prose as executable instructions.
- **FR-007**: Entry MUST distinguish `implement`, `handoff-only` and
  `owner-authorized rework`. Invalid context yields a blocked outcome, never an
  implicit implementation authorization. Rework MUST reference prior implementation,
  explicit owner approval, corrective scope and new acceptance requirements.
- **FR-008**: A completed-task retry MUST require matching content, check and
  implementation evidence and rerun acceptance. Handoff-only MUST NOT modify
  implementation content, reopen the task or select another task.
- **FR-009**: Local evidence MUST survive interrupted completion, prevent concurrent
  completion claims and retain prior attempt history. Missing or inconsistent state
  MUST block with actionable recovery instructions without destructive cleanup.
- **FR-010**: Local verification MUST run without live tracker/Symphony services,
  network access or credentials, using disposable fixtures for success, failure,
  interruption and authorized/unauthorized rework cases.
- **FR-011**: Implementation and verification of this feature MUST leave Features
  001 and 002 unchanged. Tests use copies or independent fixtures. Scope is development
  tooling; no product runtime, tracker mutation, issue dispatch or service activation.
- **FR-012**: Reports MUST distinguish local checks, supplied approval evidence and
  unperformed live tests. Local completion MUST NOT claim PR creation, label changes,
  authenticated owner verification or successful Symphony dispatch.

### Key Entities

- **Task context**: feature/task identity, authoritative artifacts, reviewed revision,
  prerequisite IDs/evidence, approved scope, allowed paths and acceptance requirements.
- **Approval record**: owner reference, reviewed artifact identity, scope, evidence
  reference and, for rework, prior implementation and corrective authorization.
- **Attempt**: task context, immutable baseline, execution mode, prior attempt reference
  and exclusive completion ownership.
- **Acceptance evidence**: content identity, executed checks/results, semantic-scope
  review, implementation revision when committed and completion/progress state.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All three modes can be demonstrated in disposable local repositories
  with zero network requests, tracker credentials or running orchestration services.
- **SC-002**: Every invalid-context case enumerated in the acceptance scenarios is
  rejected before task execution, with the missing/conflicting prerequisite identified.
- **SC-003**: Successful completion changes zero unrelated task descriptions,
  checkboxes or ordering; all out-of-scope mutation fixtures are rejected, including
  changes already committed after the baseline and changes made during acceptance.
- **SC-004**: Recovery tests at every completion boundary produce one completed task
  without duplicate implementation, lost evidence or destructive state resets;
  concurrent attempts cannot both claim completion.
- **SC-005**: Unauthorized rework is rejected; approved corrective scope is accepted
  while retaining previous evidence. Semantic artifact changes require renewed
  approval; progress-only updates preserve valid approvals in the test cases.
- **SC-006**: Feature 001 and 002 contents remain byte-for-byte unchanged after the
  feature's implementation and verification; reports label live behavior unverified.

## Assumptions

- A trusted operator supplies approval and prerequisite evidence locally; this feature
  checks completeness and consistency, not remote identity authenticity or forged evidence.
- Acceptance commands are reviewed before execution. Scope checking detects repository
  changes after they occur; it is not a sandbox and cannot prevent external side effects.
- Ignored files, host state and tracker state are outside repository-diff enforcement.
  Existing isolation and secret handling remain required for actual execution.
- Existing local single-task tooling may be reused after assessment against this spec;
  its present implementation or tests are not a substitute for that assessment.
- Human specification review precedes planning. This step creates no plan, task list,
  implementation, approval, GitHub issue or live-dispatch authorization.
