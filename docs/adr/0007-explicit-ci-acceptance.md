# 0007: Explicit repository CI acceptance

Date: 2026-10-12
Status: Accepted by owner merge; US3 inventory extension proposed for owner review and merge
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

Owner-merged PR #50 adopted the US1 checkpoint: reviewed T007–T010 made all
63 previous retained records pass, preserved every required ID, required all
99 context tests, and retained 19 measured T017/T023 test-body assertions.
The direct T004 class contributed four and remains in canonical collection.
The [US1 checkpoint record](../development/feature003-context-matrix.json)
preserves its exact outcomes and original test-authoring approval provenance.

Owner-merged PR #53 adopted the US2 checkpoint, removing 17 records after actual reviewed
T013–T017 implementation and regression migration. It preserves all 232 required
IDs and adds 145 scope, acceptance, completion and public CLI cases (377 total).
Exactly two measured same-attempt commit-binding/interrupted-resume assertions
then remained pending T023, with exact messages and removal conditions. The
[US2 checkpoint record](../development/feature003-acceptance-matrix.json)
retains raw measurements and original T012 approval records. Ordinary owner PR
review and merge adopted that proposal; no classifier, skip or waiver changes.

The US3 checkpoint proposes removing the final two retained outcomes after actual
T020–T023 implementation and unchanged retry assertions pass. It preserves all
377 required IDs and adds 108 recovery/rework/commit-binding owning controls
(485 total). The original owner-approved T018/T019 test-authoring matrices remain
historical evidence, including their then-missing public-command boundaries.
Final combined acceptance must pass every discovered test with no retained failures;
focused first-draft measurements are superseded-source evidence, not final source
qualification. Ordinary owner PR review and merge adopt the inventory proposal.

`make check` and CI run the same qualified acceptance policy and repository/static
checks. Historical raw unittest results remained red: the initial direct baseline
had 41 cases, 36 passes and five assertions; US1 focused/direct runs had 155 cases,
136 passes and 19 assertions. US2 focused collection observed 276 cases, 274 passes
and two assertions, with zero errors/skips/subtests; the direct 24 cases passed.
The US2 focused source snapshot was unqualified because concurrent preparation
created linked Git worktrees during collection. Those raw results remain evidence,
with the exact administrative delta recorded; final repository qualification uses
an independent publication clone. These focused measurements do not claim a raw
full-suite pass. Qualified green never describes a raw red suite as green or grants
task execution permission.

Use ordinary PR synthetic-merge checkout into `main` or into the same-repository parent branch of a stacked PR (plain branch name, `base.sha` an ancestor of `head.sha`, no forks), read-only contents permission
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

## Installable-pilot application extension (proposed)

Feature 006 T002 extends the same runner and manifest to Go/Phoenix scaffold
acceptance. Schema 2 retains all existing Python required IDs and retained-outcome
semantics and adds typed application required test identities, with fixed argv in
`scripts/application_checks.py`. Go JSON events and a test-only ExUnit formatter
prove each required case actually passed; all other discovered test failures/skips
remain blockers. At T002 the doc-only API client had no fabricated required test;
later tasks add required identities only for tests that exist.

The application gate performs pinned bootstrap/locked dependency acquisition from a
fresh checkout, then Go format/vet/build/tests and Phoenix format/compile/assets/tests.
Local and CI use the same function; focused `make check-apps` is development evidence
only. Tool versions, cache boundaries, event semantics and unchanged budgets are in
[the harness guide](../development/harness.md). This candidate inventory requires
the combined pilot's final owner PR review and merge; neither a local pass nor
independent code review adopts its own policy. No runtime or release is activated.
