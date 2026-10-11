# Draft issue: Document Symphony smoke-test and interrupted-handoff recovery

Status: draft; blocked on human approvals and verified single-task execution.
Do not apply `workharbor-ready` yet.

## Feature and task
Feature: 002-symphony-doc-smoke
Task: T001
Authoritative documents:
- specs/002-symphony-doc-smoke/spec.md
- specs/002-symphony-doc-smoke/plan.md
- specs/002-symphony-doc-smoke/tasks.md

## Human approvals
Spec review: pending owner review link and reviewed commit.
Plan review: pending separate owner review link and reviewed commit.
Artifact revision: record the actual committed revision before dispatch.

## Scope
Exclusively T001: write `docs/development/symphony-smoke-test.md`, add its link in
`docs/development/symphony.md`, then update only T001's checkbox after acceptance.
Preserve Feature 001 and all other files. Do not run Symphony or change tracker
state as part of writing this procedure.

## Retry or rework
Execution mode: implement. If T001 is already complete, verify matching commits and
passing acceptance, then use handoff-only. Rework requires explicit owner approval.

## Prerequisites
Task dependencies: none.
Dispatch gates: owner spec/plan approval evidence; locally verified single-task entry
point; installed/authenticated pinned Symphony and Codex; GitHub issue read/write
access; labels and isolation/secret handling verified per the operator runbook.
Do not interpret this draft or its existence as proof those gates passed.

## Acceptance
1. Guide includes normal completion and interruption after commit/checkbox but before
   PR/workpad/label handoff; retry verifies evidence and does not reimplement T001.
2. Guide includes prerequisites, expected outcomes, evidence capture, final label
   replacement preserving unrelated labels, and stopping the foreground service.
3. Unexecuted live scenarios are explicitly unverified; no credentials in documents.
4. From checkout root: `make check` and `git diff --check` return zero.
5. Inspect `git diff --name-only <pre-task-commit>` and the full diff: only plan-approved
   paths change, with only a link added to the runbook and T001's checkbox updated.
6. Workpad records actual check results and implementation commit before final
   handoff. Live dispatch/handoff verification belongs to the separately authorized
   smoke-test execution, not this documentation task's completion claim.

## Dispatch
Owner supplies approval evidence and verifies prerequisites before adding
`workharbor-ready`. Agent completes evidence before the final atomic label switch.
