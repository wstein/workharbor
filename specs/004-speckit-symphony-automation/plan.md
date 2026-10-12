# Implementation Plan: Spec Kit and Symphony task automation

**Branch**: `docs/004-speckit-automation-plan` | **Date**: 2026-10-12
**Spec**: [spec.md](spec.md), approved at `8f4debe5cd2ee2770f5963d292a9eb60af6270d7`
**Status**: Proposed for independent and owner review; no tasks, implementation,
queue change or live verification. The spec's historical draft status is preserved.
The [owner specification approval](https://github.com/wstein/workharbor-nxt/pull/36#issuecomment-6117167526)
authorizes planning only; plan approval remains separate.

## Summary

Build reusable Python development tooling to prepare one immutable task package,
validate it offline, expose effective execution and reconcile handoff. Feature 003
remains the sole implementation/completion/rework guard. Deliver offline preparation
first; gate later actions on demonstrated capabilities rather than every pending
Feature 003 task. Keep pinned Symphony external and Features 001–003 unchanged.

## Bounded owner-requested corrections

The four requested corrections are defined in the
[proposal/integration/approval contract](bounded-source-contract.md). This bounded
authorization does not approve the remaining plan or authorize tasks/runtime changes.
Keep head scope/content approval separate from synthetic integration; permit unrelated
trusted-base updates, revalidate affected protected prerequisites, prepare/check an
actual pinned merge before publication, and retain approval by guarded content contract.
The contract is a design requirement until exact-revision implementation evidence exists.
Its proposed policy-equivalence revision retains the owner-adopted immutable policy
anchor while permitting evidenced content-identical rebase/squash history without
renewing that pin; it does not approve the remaining plan.

## Technical Context

**Language/Version**: Python 3.11+, existing Bash hooks, Git executable.
**Primary Dependencies**: standard library; Feature 003 guard interfaces; external
Symphony `be10a1b79df723d6d7612b5651c8522704dafb2e`.
**Storage**: immutable JSON packages and ignored per-issue SQLite orchestration journal;
reuse `single_task_state.py` path/durability primitives where applicable. Never edit
or substitute the guard's attempt journal.
**Testing**: unittest, disposable Git repositories, injected local tracker/runtime
ports, collected-case outcomes, subprocess races and crash boundaries.
**Target Platform**: local Linux/macOS POSIX filesystems; no Windows/network-filesystem
durability claim. Supported Python is required even where system Python is older.
**Project Type**: developer CLI and CI validation library, outside product code.
**Performance Goals**: prospective SC-001 administration target; no measured saving.
**Constraints**: credential-free/network-free offline commands, strict argv, immutable
revisions, bounded checks/retries, no new task-specific CI exemptions.
**Scale/Scope**: existing repository/GitHub tracker, one task per package, one owner per
issue checkout; retain Symphony's existing concurrency limit.

## Constitution Check

| Principle | Before research | After design |
| --- | --- | --- |
| I Product scope | Tooling only | No product AI dependency |
| II Portable configuration | No provider change | Explicit runtime capability negotiation |
| III Reproducibility | Approved source/artifact pins | Bind content, modes, workflow and settings |
| IV Identity | Issue prose is data | Authenticated live approval; host-side secrets |
| V Durability | Reuse guard, reconcile writes | Immutable identities; no reset or duplicate PR |
| VI Authority | Preserve approved spec | Proposed plan; semantic drift renews approval |
| VII Feedback | Assess existing checks honestly | Offline rejection matrix; separate live evidence |
| VIII Human control | Planning authorization only | No implicit task/dispatch/merge/release/deploy |

Both design gates pass without exceptions. Unavailable capabilities block their
actions; recording limitations never waives acceptance criteria.

## Project Structure

Documentation: this plan, [research.md](research.md), [data-model.md](data-model.md),
[contracts/cli.md](contracts/cli.md), [quickstart.md](quickstart.md).
`tasks.md` stays absent until separate owner plan approval.

Planned source touch points (not created by planning):

```text
scripts/task_automation.py       # prepare/validate/effective/ready/resume/handoff CLI
scripts/task_package.py          # schema, identity, summary, capability gates
scripts/task_outcomes.py         # reusable exact-case runner; approved guard check argv
scripts/task_source.py           # accepted/proposal/synthetic source evidence
scripts/task_handoff.py          # journal and external-write reconciliation
scripts/task_tracker.py          # narrow host-side GitHub port; offline fake
scripts/prepare_symphony.py      # render validated non-secret effective execution
scripts/check_ci.py             # caller of the same generic package validator
tests/test_task_automation.py    # offline packages and invalid inputs
tests/test_task_source.py        # history/index/types/modes/wrapper rejection
tests/test_task_handoff.py       # interruptions, ownership, uncertain writes
tests/fixtures/task-automation/  # three synthetic packages
docs/development/symphony.md     # activation/capability/operator recovery
WORKFLOW.md                     # invoke gates; retain one-task/final-action rules
```

Existing Feature 003 context/scope/acceptance interfaces are dependencies when
implemented and demonstrated, not additional tasks authorized here. No frozen
bootstrap verifier/contract, product file, vendored skill or runtime fork changes.

## Phase 0 — Research

[Research](research.md) resolves existing-code reuse, outcomes, runtime/source
identity, API limits and recovery. No missing API is assumed. Current GitHub cannot
demonstrate concurrency-safe full label replacement; affected live readiness blocks.
Offline fixtures implement the contract independently of service availability.

## Phase 1 — Design and delivery boundaries

1. **Offline MVP (FR-001–004, 011–014)**: strict schema, immutable package/summary,
   inert approval/prerequisite references, effective preview and shared validation.
   Disposable fake ports cover interruptions/failures. No implementation, real
   issue eligibility or check execution during preparation. Repeat inputs reuse identity.
2. **Guard-backed execution (FR-005–009)**: entry needs demonstrated Feature 003
   context/dependency/approval-comparison and current/index/history scope validation;
   completion additionally needs acceptance/content/review sealing and commit binding;
   resume/rework additionally needs durable identity/recovery. Call those interfaces;
   unavailable behavior blocks, without legacy fallback or duplicate guard logic.
3. **Live readiness/handoff (FR-002, 005, 008–012)**: authenticate owner/actions,
   verify actual runtime/workflow/config, publication permission, issue ownership and
   every capability of the approved execution before eligibility. Journal/reconcile
   external writes and finish evidence before labels. A local mutex/read-PUT-read
   cannot exclude external label writers. Verified serialization of *all* writers
   could satisfy the port; current GitHub alone cannot. Do not invent CAS support.

CLI and CI call one package validator; task context/scope/comparison/acceptance stay
owned by Feature 003. Feature 004's shared exact-outcome runner is an explicitly
approved check argv returning zero only when the complete reviewed matrix matches.
The guard executes it through its zero-exit acceptance contract; no competing
completion guard or Feature 003 schema change is needed. Current frozen verifiers
provide research fixtures; their task IDs/exceptions never enter the generic engine.

CI uses trusted validator/policy code pinned independently of proposal data, in a
credential-free isolated job. Execute only revision-approved argv under host isolation;
proposal code/package assertions cannot authenticate their own exceptions. Full-green
is default. Approved test-authoring matrices can validate bounded reds; raw exits
stay red. Keep old phase gates until separately reviewed migration.

Source evidence distinguishes accepted base, proposal head and synthetic merge.
Validate local objects, ordered parents, complete tree, index/worktree types/modes
and intervening history. Rebase requires explicit old/new commit correspondence,
protected task-contract content and independent review of the new candidate; changed
affected requirements/scope/checks/dependencies renew approval. Unrelated accepted base
changes do not require whole-tree equality to the approved source. Head scope and
integration checks are separate gates under the bounded contract. Equivalence never
silently refreshes pins. See the
[contract](contracts/cli.md) for readiness/rejection rules.

## Validation and handoff

[Quickstart](quickstart.md) separates runnable checks from future acceptance.
Three synthetic packages cover documentation, test-authoring and implementation
successor; all modes, malformed approvals/outcomes, source variants and handoff write
boundaries must reject safely. Measure SC-001 only after live gates and SC-006
observations. Guides/dry runs prove neither safety of unattended operation nor savings.

Run `make check` and record raw exit. This approved base has ten known CLI failures;
planning creates no exception and cannot claim green. CI changes belong to the
separate bounded implementation lane; no CI edits belong to this design-document
change. Stop after Phase 1, obtain
independent exact-SHA review and owner plan approval before tasks. No complexity
exceptions are requested.
