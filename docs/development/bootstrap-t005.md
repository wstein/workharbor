# T005 preparation: task-context and entry tests

Status: **proposal only; T004 acceptance and T005 execution approval pending**.
This prepares [Issue 6](https://github.com/wstein/workharbor-nxt/issues/6) on
accepted source `8f8211afdda9e46984ccfb95505f83434da06db7`, using existing
Feature 003. It implements no tests, validator, CLI behavior or task marker.
The [machine contract](bootstrap-t005.json) contains 54 exact proposed case IDs,
fixture mutations, final feature exits and implementing successors. Append each
matrix suffix to its `case_id_prefix` to obtain the full collected ID.

T004 / [Issue 5](https://github.com/wstein/workharbor-nxt/issues/5) has owner
approval at proposal pin `cf2be65eb912c580af041052f418b5f147b876e4` and its
preparation was merged in [PR 34](https://github.com/wstein/workharbor-nxt/pull/34).
That approves T004's scope and acceptance, not its implementation or completion.
T004's marker is pending. Its implementation commit and accepted results are
currently absent; never execute T004 as part of T005 or infer them from PR 34.

## Proposed scope and observations

Only `tests/test_single_task_context.py` may change during T005, followed by only
T005's marker after acceptance. All test helpers and proposed verifier modes stay
inside that file. Preserve other tasks, Feature 001/002, source/runtime workflow,
existing verifiers, CLI, modules and planning semantics. No new feature is needed.

Use T001's real `SingleTaskFixture`, canonical bytes and local Git evidence.
Make each fixture internally valid before introducing its named mutation. Commit
artifact/dependency mutations and reseal their digests, source assertion and
approval unless the case deliberately targets one of those bindings. This avoids
an earlier dirty-tree or stale-digest failure masking the intended condition.
Unknown/nonancestor commits require real local Git objects, without network access.
A test-authoring prerequisite's passing check must bind its reviewed red verifier
and raw outcome evidence; do not misrepresent its missing behavior as implemented.

Exercise the public `begin --context PATH`; do not import a future missing context
module at collection time. Every case must reach its test body and invoke the real
CLI with finite timeout in a disposable repository. Assert the response envelope
and false identity/live-dispatch flags already owned by T004. Reject scenarios
require exit 2 and a specific context/dependency/approval blocker, distinct from
T004's `command_unavailable`; successful controls require exit 0 and one immutable
implement attempt. Final feature exits in the JSON describe those later semantics,
not observed T005 results or additional stable error codes imposed on T004.

Capture current tasks/spec/plan/output bytes, Git index and local prerequisite
state before each call. Require identical task and implementation bytes afterward,
including on successful entry. No acceptance command, prerequisite implementation
or task marker may run/change during begin: include a harmless acceptance sentinel
and assert it is absent, retain the prerequisite commit/tree identity and assert
no new implementation commit. For rejected entry also require no attempt/context
copy or journal entry; T004's safe lock artifact alone is permitted. Preserve all
preexisting evidence bytes. Progress comparison permits only the data model's
checkbox normalization and valid appended EOF evidence records; execution still
preserves the exact current task bytes. Fixture-only footer records use actual
local references and matching digests.

## Proposed expected-red verifier

Use `--self-test` and `--verify-red` modes in the permitted test file; these modes
are **not implemented here**. Reuse the existing `Outcomes`, `evaluate`, `flatten`
and `validate_inventory` utilities listed in the JSON; they exist at this base.
Do not add another generic verifier framework or change their pinned behavior.

Discover the full suite before running it and compare exactly against T004's 41
existing IDs plus these 54 IDs, rejecting missing/extra/duplicate cases and loader
errors. Run the existing T004 direct class as well: its imported IDs begin with
`verify_t004_cli.` instead of `__main__.`; translate that prefix only in the
contract's twelve expected direct IDs. Require all 31 baseline, five authored and
twelve direct T004 cases green, with exactly T004's five retained-red assertions.
The unchanged T004 standalone verifier cannot be invoked directly after adding
T005: its deliberate exact-discovery gate would reject the added tests. Reuse its
case class and matrix instead. Run the twelve direct T003 state checks separately.

Require all 54 T005 outcomes to match individually reviewed test-body assertions
against missing context/entry validation. Do not accept generic exit 2, setup/import
failures or unconditional synthetic failures. Test bodies must inspect the real
response and preservation invariants before a final missing-behavior assertion.
Self-tests must reject empty/extra/duplicate discovery, wrong failure text,
unexpected green, errors, setup/cleanup failures, skips, xfail, unexpected success,
failed subtests and regressions in any required green case. No matrix overrides,
`|| true`, blanket nonzero acceptance or changed assertions to preserve red status.

The exact raw assertion strings, test-content digest and accepted T004 baseline
must be measured after T004 acceptance, reviewed and pinned before T005 dispatch.
They are deliberately null now. T004's approved proposal specifies deferred
`command_unavailable` responses; this document does not pretend those responses
have been delivered or invent measured future failures. If any proposed case is
already green, preserve the observation and obtain revised owner review before
entry. Do not force a green case to fail. The JSON's checks are proposed argv,
not runnable acceptance commands until the test file and reviewed record exist.
The final US3/full-feature suite must become green.

## Current evidence and next gate

At the accepted source base, raw `make check` returned **2**: 41 tests ran,
31 passed and ten T002 CLI assertions failed; there were no test errors/skips.
`python3 scripts/verify_t002_red.py` returned 0 for that exact existing matrix;
`python3 scripts/verify_t003_state.py` returned 0 for twelve direct state checks;
`python3 scripts/verify_t004_cli.py --self-test` returned 0. These are baseline
observations, not T004 acceptance or T005 red-test evidence. The initial host
`python3` was 3.9. Repeating `make check` with Homebrew Python 3.15.0 returned
2 with the same 41 tests, 31 passes and ten assertion failures. JSON parsing,
54 unique IDs, pending-gate checks and the reused utility API checks passed on
3.15.0; `git diff --check` returned 0.
No T005 suite exists, and empty discovery cannot pass this proposal's gate.

Before authorizing T005: accept T004's implementation and exact green/five-red
record, pin its commit and current source, review this case design, then measure
and approve the immutable T005 test bodies/raw matrix and exact verifier argv
under an explicitly authorized test-authoring route. Preserve retries and prior
approvals. Preparation publication grants no ready label, queue, execution,
completion, merge, deployment or automatic successor work.
