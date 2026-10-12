# PR #49 acceptance proposal

Status: Proposed for owner PR review and merge; local checks cannot adopt policy.

At checkpoint `d8b84547bc0d8f26f4bba4332068689befa786a7`, T006 is implemented,
but 58 preserved T005 tests assert future behavior: T007 scope (16), T008 reviewed
artifacts/approval (17), T009 prerequisites/source (23), and T010 entry (2).
The existing [schema-v1 manifest](ci-acceptance.json) expresses the minimal change:
retain those exact test-body assertions alongside the unchanged five historical
records. Its required inventory grows from 133 to 207 IDs by adding all 74 context
tests, including the 16 implemented controls that must remain present and green.
Every other collected case must pass. No test, assertion, runner, classifier or
workflow is changed.

Each new retained record enumerates its exact ID and message, original fingerprint,
missing behavior, successor and removal condition. The fingerprint is the existing
`task_outcomes.assertion_fingerprint(id, message)`: SHA-256 of canonical JSON
`[id,"AssertionError",message]`. Schema v1 already enforces that identity by exact
ID/message and test-body assertion classification; adding a fingerprint field or
a second classifier would duplicate enforcement.

The authoritative [T005 matrix](feature003-context-matrix.json) records original
owner-reviewed source `9b7fa407dd1aebf2278a946df9ce45d02e8b775a`, fixture-repaired
source `624bb958a093e02c69c83bb5b2f201822650f32e`, and checks digest
`sha256:6516958817e23b7879d9abe0696893271b50441d38122cb9e0a1958783a1da7a`.
The proposal selects only its T007–T010 cases, preserving their exact fingerprints;
it does not reuse the original 14 T006 assertion outcomes after implementation.
T006 also added two green schema controls, bringing context collection to 74.

Original Linux CI run `38290114295` measured 513 aggregate tests: 450 passes,
63 failures and zero errors, with acceptance RED under the five-record manifest.
Raw baseline discovery measured 115 cases, 52 passes and 63 failures (unittest exit
1; Make exit 2). These remain raw red after this proposal; only the separate
manifest-qualified result can become green. This proposal introduces no skips,
xfails, deletion, failure suppression or historical-tree equivalence.

For review, run `make check` with Python 3.11+ in a pristine checkout of the exact
candidate. The same code can be identified explicitly with
`python3 -I -B scripts/check_ci.py --source d8b84547bc0d8f26f4bba4332068689befa786a7`;
that pin identifies unchanged checker code, not owner adoption. Rejection checks
must still block an unexpected pass, changed message, new failure, missing required
ID, error, skip, xfail, failed subtest, duplicate or empty discovery.

When a successor implements a retained behavior, its reviewed migration must remove
that exact retained record as the preserved regression turns green, keep its required
ID and keep every implemented case green. An unexpected pass deliberately blocks
until that migration. The original five records retain their existing T010 migration
conditions. T010 must separately report any remaining completion/retry blockers;
qualified CI never completes an unfinished task.

[ADR 0007](../adr/0007-explicit-ci-acceptance.md) requires owner PR review and merge
for inventory/outcome changes: “a manifest edit or passing check cannot approve
itself.” This prepared diff is the concrete decision item. It requires no separate
approval stamp, external setting, provider pin, matrix approval flag or framework.
Review/publication, owner adoption, task execution and runtime activation remain
distinct actions.
