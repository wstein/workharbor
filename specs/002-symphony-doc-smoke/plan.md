# Feature 002 implementation plan

Status: proposed; awaiting separate human spec and plan approvals.

## Approach
Use existing repository Markdown and Symphony policy. No new dependencies.
Create `docs/development/symphony-smoke-test.md` and add a relative link to it from
`docs/development/symphony.md`. The guide describes prerequisites, normal completion,
interruption after the task commit/checkbox but before PR/workpad/final labels,
and handoff-only recovery. Include expected outcomes, evidence to capture and how
to stop the foreground service. Mark all unexecuted scenarios as unverified.
Reuse the existing rule: finish evidence first, then replace dispatch labels in one
operation preserving unrelated labels. Do not invent a different recovery policy.

## Allowed changes
- `docs/development/symphony-smoke-test.md`: new operator procedure.
- `docs/development/symphony.md`: link to that procedure only.
- `specs/002-symphony-doc-smoke/tasks.md`: T001 checkbox only after acceptance passes.
All other files, including Feature 001 and workflow/skill code, remain unchanged.
Tracker workpad/PR/labels are handled only under separately authorized live dispatch.

## Preconditions and gates
Spec and plan require owner review linked to their actual committed revisions.
No prerequisite feature tasks exist. Dispatch additionally requires an installed,
authenticated pinned Symphony runtime and Codex, GitHub tracker access, existing
labels and verified isolation/secret handling per the operator runbook.
A locally verified single-task execution entry point is required before dispatch;
the generic phase-oriented speckit-implement skill alone does not satisfy this gate.
This feature does not implement that prerequisite. Missing gates leave it blocked.

## Constitution check
Development tooling stays outside the product. No owner/provider gate is bypassed.
Per-issue work and retries preserve evidence. Human review controls dispatch and
publication. Missing tools/checks and unexecuted live scenarios are not successes.

## Validation
Run `make check` and `git diff --check` from the checkout root. Inspect the diff
against the recorded pre-task revision using the allowlist above. Confirm the guide
covers the required scenarios and does not contain credentials or fabricated results.
Record exact commands, exit status and implementation commit in the workpad.
