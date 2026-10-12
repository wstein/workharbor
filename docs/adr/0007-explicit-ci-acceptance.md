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

The initial manifest retains exactly five T004 assertion failures, with exact test
IDs/messages, reasons and removal conditions. Only assertions arising in the test
body qualify. Missing IDs, new failures, unexpected passes, errors, skips, xfails,
failed subtests, duplicate outcomes/IDs and empty discovery fail acceptance. Changes
to required inventory or retained outcomes require owner PR review and merge;
a manifest edit or passing check cannot approve itself. Remove retained outcomes
with reviewed implementation and regression migration when their conditions hold.

PR #49 proposes extending that same manifest to the 58 preserved T005 assertions
for pending T007–T010 behavior at the T006 checkpoint, while requiring all 74
context tests, including the 16 implemented green controls. The five historical
records remain unchanged. [The proposal](../development/pr49-acceptance-proposal.md)
records exact scope and provenance; owner PR review and merge must adopt this
extension. It does not approve task execution or describe the raw suite as green.

`make check` and CI run the same qualified acceptance policy and repository/static
checks. `make check-raw` retains the honest baseline unittest result: currently 41
cases, 36 passes, five assertion failures, exit 2. Qualified green never describes
that raw suite as green or grants task execution permission.

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
