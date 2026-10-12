# Validation guide: Task automation

Status: planning only. Future commands/scenarios are acceptance requirements, not
observed results. No package CLI exists yet; no live runtime is activated.

## Runnable planning checks

Prerequisites: Git, Bash, Make, Python 3.11+. This host's system python3 is 3.9.6;
use installed /opt/homebrew/bin/python3.14 through Make's PYTHON override.

```sh
python3 scripts/select_feature.py 004-speckit-symphony-automation
make check PYTHON=/opt/homebrew/bin/python3.14
/opt/homebrew/bin/python3.14 scripts/verify_t002_red.py --self-test
/opt/homebrew/bin/python3.14 scripts/verify_t003_state.py
git diff --check
git diff 8f4debe5cd2ee2770f5963d292a9eb60af6270d7 -- specs/001-workspace-lifecycle specs/002-symphony-doc-smoke specs/003-single-task-entry specs/004-speckit-symphony-automation/spec.md
```

Expected approved-source raw make check: exit 2, ten known CLI assertion failures,
31 passes; suite is red. Direct primitive tests/verifier self-test are narrower evidence,
not Feature 003 CLI or Feature 004 implementation. Final diff must be empty. Do not
change old tests/contracts/CI to make planning green. Run repo/diff/shell/prerequisite
checks separately when red Make stops early; report actual exits.

## Future offline acceptance

After approved tasks/implementation, use disposable repositories, cleared credentials,
disabled network and fake runtime/tracker ports. Assert no attempted socket/network
subprocess or external mutation. Fixtures: documentation task, owner-approved exact
test-authoring matrix and implementation successor. Preparation must not run checks
or prerequisites. Use [contract](contracts/cli.md) and [model](data-model.md).
Fixture generator/tests must ship before these future commands can run:

```sh
python3 -m unittest discover -s tests -p 'test_task_*.py' -v
python3 scripts/task_automation.py prepare --input INPUT.json --output OUTPUT
python3 scripts/task_automation.py validate --package PACKAGE.json --offline
python3 scripts/task_automation.py effective --package PACKAGE.json --offline
```

Repeat preparation: same digest/summary, no duplicates. Three packages use one
validator/CI flow. Inject every missing/extra/duplicate/skipped/timeout/setup/subtest/
unexpected outcome and regression; reject all, retain raw reds. Test-authoring proof
can satisfy the declared successor prerequisite without claiming implemented behavior.

Exercise semantic versus checkbox/evidence-only changes, local versus authenticated
approval, unavailable guard/runtime capabilities, accepted/proposal/synthetic/rebased
source, dirty index/modes, unsafe paths and commit/revert. Every invalid case blocks
before eligibility; ambiguous comparison renews approval. Keep real Features 001–003
byte-identical. Sentinel secrets must be absent from files/diagnostics/children.

Interrupt before/after completion, commit binding, PR/evidence publication and labels;
include lost responses and competing ownership. Resume retains one implementation/PR.
Failed acceptance blocks without reopening. Concurrent unrelated labels cause fake
conditional conflicts and safe recomputation; unsupported GitHub performs no write
and reports incomplete recovery. No reset, evidence deletion or baseline recreation.

## Future authorized live observation and measurement

Require distinct owner approvals, exact-candidate independent review, publication
permission, demonstrated guard interfaces, runtime provenance/effective workflow,
host isolation and safe final labels. Current GitHub API alone fails the last gate.
Use only a separately owner-authorized disposable issue/checkout.

Observe normal completion, interruption after completion before handoff and verified
resume against actual pinned Symphony. Record checkout/task/check/commit/PR and
approval/review identities; prove no duplicate implementation or unrelated label
change. Unknown final writes remain incomplete until operator reconciliation.
Then sample five consecutive eligible tasks, including specified categories/retry;
publish each active/excluded interval and revisions. No guide/estimate/dry run proves
live execution, unattended safety or efficiency.

## Bounded correction verification

The [bounded source contract](bounded-source-contract.md) is the acceptance matrix
for the four owner-requested corrections. Its prepublication command and approval
ports require implementation and exact-revision evidence before use. Use disposable
remotes to test independent head/merge gates, unrelated base advancement, affected
protected prerequisites, forged merge trees, ref races and unavailable guard/provider
ports. Retained content approval must coexist with fresh exact-candidate review.
Do not label offline fake-port results as owner authentication or live eligibility.
The accepted source/PR38 revisions in the contract were inspected locally; a fresh
forge fetch must be reported separately, including unavailable authentication.
