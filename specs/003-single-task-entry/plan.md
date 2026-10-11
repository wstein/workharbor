# Implementation Plan: Locally verifiable single-task entry

**Branch**: current checkout `main`; logical feature `003-single-task-entry`
**Date**: 2026-10-12 | **Spec**: [spec.md](spec.md)
**Status**: Proposed design for human review; no implementation or dispatch approval.
**Input**: `/specs/003-single-task-entry/spec.md`

## Summary
Extend the repository's local single-task CLI to validate a reviewed task context,
prerequisite evidence and actual repository changes, then preserve one selected task
through implementation, interrupted completion, handoff and explicitly approved
rework. Offline local fixtures provide acceptance; no tracker or Symphony is needed.
Keep Features 001 and 002 byte-for-byte unchanged. Existing guard code is a reusable
baseline, not an implementation of all Feature 003 requirements.

## Technical Context

**Language/Version**: Python 3.11+ (repository tooling requirement), Bash for harness.
**Primary Dependencies**: Python standard library, Git executable; no pip or product dependency.
**Storage**: ignored per-worktree `.symphony/single-task/` with SQLite attempt/evidence
journal, immutable copied contexts and an advisory process lock; no credentials.
**Testing**: unittest; real temporary Git repositories, subprocess races and crash
injection; acceptance fixtures run offline without credentials or orchestration.
**Target Platform**: local POSIX filesystems on Linux and macOS; network filesystems
and Windows locking semantics are outside the initial supported matrix.
**Project Type**: developer CLI, separate from product applications.
**Performance Goals**: bounded single-task validation; no latency SLA in the spec.
**Constraints**: exactly one selected task; approved scope/checks immutable per attempt;
no destructive recovery, remote identity claims or network-dependent local tests.
**Scale/Scope**: one active attempt per worktree; multiple retained historical attempts.

## Constitution Check

| Principle | Pre-research gate | Post-design gate |
| --- | --- | --- |
| I — Product scope | Tooling only | No product dependencies or AI runtime feature |
| II — Portable config | No provider/config implementation | Feature context paths validated locally |
| III — Reproducibility | Git revisions identify input | Baselines, artifact and content digests retained |
| IV — Identity boundaries | Local evidence is not identity authentication | No secrets in state; no provider/owner gate bypass |
| V — Durable operations | Apply durable retry discipline to task attempts | Journaled completion, bounded recovery, no resets |
| VI — Repository authority | Existing spec governs design | Proposed status retained; semantic changes need renewed review |
| VII — Feedback | Existing tests insufficient for new obligations | Offline acceptance/failure/race matrix specified |
| VIII — Human control | Explicit scope/rework approval | No tracker mutation, issue dispatch or automatic next task |

Both design checks pass without exceptions. Plan generation does not declare the
spec reviewed or the feature implemented; owner review remains required before work.

## Project Structure

### Documentation (this feature)
```text
specs/003-single-task-entry/
  spec.md
  plan.md
  research.md
  data-model.md
  contracts/cli.md
  quickstart.md
  checklists/requirements.md
```
`tasks.md` is a subsequent tasks-generation artifact, not created by this plan.

### Source Code (planned touch points)
```text
scripts/single_task.py               # CLI integration; retain useful existing checks
scripts/single_task_context.py       # context/schema/artifact/prerequisite validation
scripts/single_task_state.py         # attempt journal, locks and recovery
scripts/single_task_scope.py         # repository inventory, digests and semantic review binding
scripts/single_task_acceptance.py    # trusted argv checks and durable outcomes
scripts/check_repo.py                # mechanical context/feature-protection harness checks if needed
tests/test_single_task.py           # existing regression coverage
tests/test_single_task_context.py   # approval/dependency/progress cases
tests/test_single_task_state.py     # interruption, concurrency, history and rework
tests/test_single_task_scope.py     # scope/content/staged/committed mutations
tests/fixtures/single-task/         # synthetic feature and local context/evidence fixtures
docs/development/single-task.md     # updated operator interface/limits
```
Paths are repository-relative. Retain the
existing entry script, extracting modules by responsibility rather than rewriting
product code. Do not change WORKFLOW.md, Features 001/002 or vendored Spec Kit skills
in this feature. Symphony integration changes require separate authorization.

## Phase 0 — Research and existing-code assessment
[research.md](research.md) records decisions and alternatives. Reuse current strict
paths, task identity checks, baseline diff and pre/post-check verification. Add missing
approval/prerequisite validation, semantic review binding, explicit rework, immutable
history, committed evidence and exclusive completion. No unresolved research choices.

## Phase 1 — Design
[Data model](data-model.md) and [CLI contract](contracts/cli.md) define validated input,
mode versus progress, evidence identities, crash boundaries and error outcomes.

1. Entry requires a local reviewed context: exact feature/task, artifact revisions,
   dependency IDs/source references, completion evidence, permitted paths/checks and
   approval records. Validate fields strictly; compare dependencies with approved
   source assertions. Ambiguous prose cannot be inferred as an empty dependency set.
2. Treat local approval and semantic review as trusted operator attestations bound
   to content/revisions; validate completeness and consistency without claiming to
   authenticate the owner. No prose-to-command evaluation or remote API calls.
3. Allow progress-only differences through a deliberately narrow comparison:
   checkbox markers on otherwise identical task lines, plus strictly structured
   evidence-reference footers. Anything else blocks until renewed approval.
4. Inventory baseline-to-current, baseline-to-index, baseline-to-HEAD and each
   intervening commit, plus nonignored untracked paths. This catches staged-only
   changes and unauthorized commit-then-revert history. Reject unsafe symlinks,
   submodule/gitlink changes, nonregular allowed outputs and repository escapes.
5. Lock one worktree while a command validates/writes state and completes checks.
   Journal acceptance identity and completion intent before atomic checkbox update.
   On recovery compare exact pre/post task bytes and content identities, then finish
   only the interrupted transition. Corrupt or unexpected state blocks, never resets.
6. Separate acceptance, checkbox completion and commit binding. After completion,
   only `record-commit` binds an existing matching local implementation commit;
   handoff readiness requires it. No self-referential commit hash is put in the
   implementation file. Repeated verify never replaces original implementation identity.
7. Authorized rework creates a new linked immutable context/baseline and attempt;
   keep the already checked task and prior evidence. Rework acceptance seals new
   evidence without reopening any task. Other tasks always remain byte-identical.

## Validation and evidence
Follow [quickstart.md](quickstart.md). Required matrix covers every spec acceptance
scenario, prerequisite/approval mismatches, progress-only vs semantic changes,
current/staged/history scope, unavailable/mutating checks, semantic review hashes,
three modes, commit binding, crash boundaries, concurrent attempts and corrupt state.
Use disposable synthetic repositories; never mutate Features 001/002 for tests.
Check their tree identities before and after against the recorded implementation
baseline. `make check`, `git diff --check` and the targeted unittest suites must pass.
Documentation must label future commands/scenarios as planned until implemented.
No existing passing suite is represented as evidence for new behavior.

## Review handoff
Stop after design. Obtain owner plan review before generating executable tasks.
Then run tasks generation and read-only analysis; implement only approved work.
Release evidence must state local-only verification and all remaining live limits.
