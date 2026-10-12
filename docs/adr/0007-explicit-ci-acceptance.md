# 0007: Explicit repository CI acceptance

Date: 2026-10-12
Status: Proposed; requires owner review and merge
Supersedes: the historical CI acceptance policy under [ADR 0005](0005-development.md),
not its Feature 003 task-entry or ownership rules

## Context

Historical T003/T004 seals coupled ordinary repository changes to old commit trees
and an external policy-source setting. They blocked legitimate new green tests and
required repeated task-specific CI exceptions. Feature 004 research already identifies
these seals as transitional; replacing them does not complete Feature 004.

## Decision

Use one repository-owned runner, `scripts/check_ci.py`, and the reviewed
[acceptance manifest](../development/ci-acceptance.json). Discover all `tests/test*.py`
and `scripts/test_*.py`, plus the direct T004 CLI and T003 state classes. Required
IDs must remain present. Additional green regressions need no manifest/hash change.

The initial historical manifest retained exactly five T004 assertion failures, with exact test
IDs/messages, reasons and removal conditions. Only assertions arising in the test
body qualify. Missing IDs, new failures, unexpected passes, errors, skips, xfails,
failed subtests, duplicate outcomes/IDs and empty discovery fail acceptance. Changes
to required inventory or retained outcomes require owner PR review and merge;
a manifest edit or passing check cannot approve itself. Remove retained outcomes
with reviewed implementation and regression migration when their conditions hold.

Owner-merged PR #49 adopted the T006 checkpoint proposal: 58 pending T005
assertions and the five historical records, with 74 required context tests.
[That proposal](../development/pr49-acceptance-proposal.md) remains a historical
record of its exact scope and provenance.

The US1 checkpoint proposes replacing those 63 records after reviewed T007–T010
implementation makes every corresponding test pass. It preserves all required
IDs, requires all 99 context tests, and retains exactly 19 measured test-body
assertions for pending T017/T023 verification, completion and retry behavior.
The direct T004 class contributes four of those records and remains part of
canonical collection. The [checkpoint record](../development/feature003-context-matrix.json)
contains exact IDs, messages, fingerprints, successors and removal conditions.
This replacement is a proposal until ordinary owner PR review and merge adopt it;
no classifier change, skipped test or acceptance waiver is introduced.

`make check` and CI run the same qualified acceptance policy and repository/static
checks. Raw unittest results remain red: the initial historical direct baseline
had 41 cases, 36 passes and five assertions; the measured US1 focused and direct
runs have 155 cases, 136 passes and 19 assertions, zero errors or skips, with
both raw commands exiting 1. These are focused measurements, not a claim that
the complete repository suite passed. Qualified green never describes a raw
red suite as green or grants task execution permission.

Use ordinary PR synthetic-merge checkout into `main`, read-only contents permission
and no persisted credentials. Keep focused Conventional Commits and exact-candidate
independent review. No external policy-source variable, historical-tree equivalence
framework or per-task hash refresh is needed. Existing unused repository settings
are left untouched. Prepublication's exact `--source` identifies checked checker
code; it does not authenticate owner adoption. The checker's deprecated `--policy-source`
alias has the same code-identity meaning.

## Boundaries and migration

Keep historical manifests/verifiers/evidence as records, without using their frozen
entry points as CI authority. Retain direct behavioral assertions. Replace the
legacy test's orphaned Git-object fetch with byte-identical tracked fixture bytes
and retain its digest assertion. Feature 003 guards, scope/ownership, external
approval and task completion remain unchanged. Feature 004 approval-binding ports
still fail closed when unavailable. No future task, provider setting, merge,
publication or deployment is authorized by this decision.

The bounded Feature 004 CI/prepublication contracts are updated to this decision;
other planned interfaces remain proposed. Owner review and merge adopt this policy
through the ordinary repository process, without a separate external pin ceremony.
