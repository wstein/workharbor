# Feature Specification: Spec Kit and Symphony task automation

**Feature identifier**: `004-speckit-symphony-automation`
**Feature Branch**: `docs/speckit-symphony-automation`
**Created**: 2026-10-12
**Status**: Draft — awaiting human specification review; not implemented or live-verified.
**Input**: Reduce repeated administration around approved Spec Kit tasks executed
through pinned Symphony. The operator reports roughly two hours per task, with
about fifteen minutes implementing code and most remaining time spent preparing
approvals, checks, tracker evidence and handoff. This estimate is not a measured baseline.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Prepare one reviewable task package (Priority: P1)

An operator selects one existing task and receives a reusable execution package and one concise approval summary,
without repeatedly writing task-specific scripts or copying scope, prerequisites, checks and approval evidence by
hand.

**Why this priority**: Reliable preparation removes repeated administration and makes the exact proposed execution
easy for a human to approve.
**Independent Test**: Prepare different synthetic tasks offline, including a reviewed test-authoring matrix, and
validate their packages without live credentials.
**Acceptance Scenarios**:

1. **Given** one task with explicit scope/prerequisites/checks, **When** prepared, **Then** its package and summary
   identify reviewed revisions, missing decisions and the next action; no implementation or tracker mutation occurs.
2. **Given** unchanged inputs, **When** preparation repeats, **Then** package identity and valid approvals are
   reused without duplicate artifacts or approval requests.
3. **Given** missing prerequisites or ambiguous context, **When** validated, **Then** a precise blocker appears; no
   prerequisite runs and no check is weakened.
4. **Given** an approved test-authoring outcome matrix, **When** validated, **Then** every collected case matches
   and implemented behavior passes; raw reds stay red.

### User Story 2 - Queue only the approved effective execution (Priority: P1)

The operator reviews the effective execution before making one approved task eligible.
**Why this priority**: Reduced administration must preserve what the owner reviewed.
**Independent Test**: Use local tracker/runtime substitutes to test valid readiness, semantic/progress-only changes,
missing capabilities and mismatched source identities.
**Acceptance Scenarios**:

1. **Given** an approved package, **When** readiness is requested, **Then** effective source, runtime/workflow
   identities, mode, checks and capabilities are validated before eligibility; unrelated issues remain unchanged.
2. **Given** only checkbox progress or appended evidence, **When** comparison proves unchanged artifact semantics,
   **Then** approval is retained with recorded evidence.
3. **Given** semantic drift, unauthenticated approval, unavailable pins or capabilities, **When** readiness is
   requested, **Then** it blocks with remediation and no ready label.
4. **Given** a rebased or synthetic pull-request checkout, **When** validated, **Then** only evidenced equivalence
   to approved source/content is accepted; unexpected parents, tree, index, file modes or intervening history block.

### User Story 3 - Resume and finish one handoff (Priority: P1)

The operator retries interrupted work and receives the existing reviewable commit, pull request and evidence without
repeating implementation or handoff work.
**Why this priority**: Retries and handoff repair create repeated administration.
**Independent Test**: Interrupt disposable attempts at preparation, completion, commit, pull-request, evidence and
final-label boundaries; include uncertain writes.
**Acceptance Scenarios**:

1. **Given** matching completed-task evidence, **When** resumed, **Then** existing single-task safeguards select
   handoff-only, rerun acceptance and reuse commits, pull request and evidence; no task is reopened or
   reimplemented.
2. **Given** failed revalidation or missing evidence, **When** retried, **Then** work is retained and blocked; only
   explicit owner-approved corrective scope permits rework.
3. **Given** complete durable checks/review/authorized publication/evidence, **When** handed off, **Then** one final
   label replacement preserves unrelated labels, removes ready and adds human-review; no later agent action is
   required.
4. **Given** a blocker or failed/uncertain external write, **When** resumed, **Then** recovery evidence precedes a
   blocked-label transition and current remote state is reconciled; incomplete handoff is reported and no duplicate
   PR is created.

### Edge Cases

Reject conflicting per-issue ownership, dirty first-entry checkouts, missing history, unsafe/symlinked escaping
paths, stale review and missing/corrupt completion evidence; preserve work without reset or invented baselines.
Missing/skipped/empty/timed-out checks and unexpected outcomes block. Reconcile uncertain writes and concurrent
label changes; unavailable atomic replacement leaves handoff incomplete. Treat task prose as data and prevent
credential disclosure in files, diagnostics and children.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Package exactly one feature/task, authoritative artifacts/approved revisions, scope/allowed paths,
  explicit prerequisites/proof, ordered checks, source identity and mode. Never run other tasks implicitly.
- **FR-002**: Present one concise approval summary per revision with durable links. Keep spec approval, plan approval
  and independent review distinct. Live readiness authenticates owner/revision/action; local assertions are not proof.
- **FR-003**: Record approval comparisons. Only checkbox progress/appended evidence preserving requirements, task
  descriptions/scope, acceptance, dependencies and ordering retains approval; semantic/ambiguous changes need renewal.
- **FR-004**: Use one locally testable package/outcome validator without new task-specific CI exceptions. Default
  implementation/handoff checks pass; revision-approved test-authoring matrices may prove bounded intended reds.
  Report raw exits; reject extra/missing/skipped cases, setup errors, unexpected green/red and regressions.
- **FR-005**: Expose/validate effective source, runtime pin, workflow, package, mode, checks, capabilities and external
  actions before readiness. Missing pins/capabilities or mismatches block; rendering is not activation.
- **FR-006**: Reuse Feature 003 for implement, handoff-only, owner-authorized rework, scope/check enforcement,
  immutable completion identity and recovery. Never duplicate its guard; missing demonstrated capabilities block.
- **FR-007**: Distinguish accepted source, proposal and synthetic PR checkout. Rebase/merge equivalence needs
  reviewed content/history evidence; unexpected parents/tree/index/types/modes/history and missing revisions block.
- **FR-008**: Make prepare/resume/handoff idempotent, reuse commits/PR/evidence and reconcile uncertain writes.
  Isolate per-issue checkouts, prevent concurrent ownership, retain history/unrelated work; never reset/adopt changed inputs.
- **FR-009**: Require fresh scope/check evidence, bound commit, exact-candidate independent review where required
  and publication authorization. Failed revalidation blocks; rework needs owner-approved corrective scope/checks
  linked to prior implementation. Retain history; merge/release/deploy remain separate owner actions.
- **FR-010**: Finish durable checks/commits/authorized PR/evidence before final atomic label replacement: preserve
  unrelated labels, remove ready, add human-review or blocked, leave issue open. Record blockers first; reconcile
  concurrent labels without loss. Failed/unavailable atomic replacement means incomplete operator-recoverable handoff.
- **FR-011**: Treat inputs as data, reject escaping/symlinked paths, contain repository/output access and prevent
  secrets in files/logs/comments/children. Preserve ownership/provider/sandbox gates; never execute task prose.
- **FR-012**: Support offline preparation/dry-run without credentials/services/network or external mutations,
  using disposable fixtures for all modes, invalid inputs, interruptions, concurrency and write failures. Report
  exits, supplied/authenticated approval, independent review and observed live execution separately.
- **FR-013**: Remain reusable tooling around pinned Symphony without a fork/product AI dependency. Preserve
  Features 001–003 and history; this feature never authorizes their pending tasks or requirement changes.
- **FR-014**: Record measurement revisions/sample/active/excluded intervals. Preparation/approval/check/configuration/
  handoff repair counts as administration. Initial estimates, guides and dry runs prove no efficiency or live result.

### Key Entities

- **Task package**: One task's reviewed artifacts, scope, prerequisites, checks, source and mode.
- **Approval decision**: Actual identity assurance, reviewed revision/action and evidence.
- **Effective execution**: Package plus actual source/runtime/workflow and available safeguards.
- **Handoff record**: Attempt/commit, fresh checks/review, PR/evidence and final-label outcome.
- **Administration observation**: Eligible task/revision, timed active work and explicit exclusions.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After one-time setup, each of five consecutive eligible routine tasks needs less than ten minutes
  active operator administration from preparation through review handoff. Include documentation, implementation,
  test-authoring and an interrupted retry (categories may overlap). Routine means existing approved artifacts,
  satisfied prerequisites and available capabilities, with no new scope/architecture decision. Count approval
  preparation/active approval-review administration, configuration, check administration and handoff repair; exclude
  task code/test implementation, unattended computation and waiting for human response. Publish individual intervals
  and exclusions. This is a prospective target, not an achieved result.
- **SC-002**: All defined invalid package/approval/source/capability scenarios block before
  eligibility/implementation, reproducibly offline without credentials/network.
- **SC-003**: Every interruption scenario yields one implementation identity and one PR per task, retained evidence,
  zero unrelated task/label changes and zero unauthorized rework.
- **SC-004**: At least three different synthetic packages, including a test-authoring matrix and implementation
  successor, use the same validation flow without new per-task CI exemptions; every injected
  missing/extra/skipped/unexpected/regressing outcome is rejected.
- **SC-005**: Every observed final label transition follows durable check/review evidence; failed/uncertain writes
  remain incomplete until reconciliation proves intended state.
- **SC-006**: Before unattended live verification is claimed, actual observations prove normal completion,
  interruption after completion before handoff, and verified resume, including checkout/task/check evidence, no
  duplicate implementation/PR and attributable human approvals/independent review of their actual revisions.

## Assumptions

- Pinned Symphony remains externally installed tooling; live access is separately authorized.
- Feature 003 owns context/approval/scope/acceptance/durable-state safeguards. Offline package work can use
  independent fixtures. Entry needs validated context/scope; completion needs acceptance/content/review;
  resume/rework needs durable identity/recovery. Require demonstrated capabilities for the affected step, not all
  Feature 003 tasks as a blanket prerequisite. Current availability must be assessed; checkmarks alone prove none.
- Feature 002 supplies a smoke-test procedure, not observed live proof.
- Reuse initially targets the existing repository/tracker configuration. Additional providers and a general workflow
  designer are out of scope. Scope checks are not a hostile-code sandbox; existing isolation and trust boundaries
  remain necessary.
- This specification creates no plan/tasks/implementation/approval/issue/queue/runtime change. Human specification
  review is the next gate.

## Provider facts — reviewed 2026-10-13

This dated context supplements the historical specification; it changes no FR, SC or approved execution.
Additional providers remain out of scope. Detailed primary sources, interpretation and future options appear in
[provider policy context](../../docs/development/provider-policy.md).

- Anthropic's consumer automation restriction has API-key and explicit-permission exceptions. Current Claude Code
  guidance permits conditional use of its unmodified binary with each user's own authentication; the October 7
  Agent SDK notice also describes subscription-limit usage. Neither a blanket subscription automation ban nor
  universal permission to reuse Claude login credentials follows from these sources.
- Antigravity distinguishes direct headless scripting/CI from unsupported consumer third-party agent/application
  wrapping and account rotation. Enterprise agreements and Gemini API access need separate assessment.
- Pinned Symphony launches Codex App Server. Changing the executable alone does not demonstrate a compatible
  Claude or Antigravity adapter.

These new documentation bytes do not automatically retain, renew or transfer execution approval. FR-003 still
requires the applicable recorded comparison and renewed approval for semantic or ambiguous changes.
