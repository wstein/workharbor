# Validation guide: Task automation

Status: local package preparation, source/approval/runtime ports and readiness
composition are implemented with disposable fixture evidence. Actual guard-backed
execution and live readiness/handoff remain blocked on demonstrated host adapters.
No live runtime is activated; offline results prove no owner authentication or
Feature 004 completion.

## Runnable offline checks

Prerequisites: Git, Bash, Make, Python 3.11+. This host's system python3 is 3.9.6;
use installed /opt/homebrew/bin/python3.14 through Make's PYTHON override.

```sh
make check PYTHON=/opt/homebrew/bin/python3.14
make check-raw PYTHON=/opt/homebrew/bin/python3.14  # raw baseline: expected exit 2
/opt/homebrew/bin/python3.14 -B -m unittest discover -s scripts -p 'test_task_*.py' -v
git diff --check
git diff 8f4debe5cd2ee2770f5963d292a9eb60af6270d7 -- specs/001-workspace-lifecycle specs/002-symphony-doc-smoke specs/003-single-task-entry specs/004-speckit-symphony-automation/spec.md
```

Current repository acceptance follows [ADR 0007](../../docs/adr/0007-explicit-ci-acceptance.md):
`make check` qualifies the reviewed manifest and includes static/operator/direct
checks. `make check-raw` reports the actual baseline result: 41 tests, 36 passes,
five exact assertion failures, Make exit 2. Direct primitive tests are narrower
evidence, not Feature 003 CLI or Feature 004 implementation. Historical approved-base
results (31 passes/ten CLI reds) remain evidence of that earlier source only.
Feature 001–003 specs/guard contracts and the approved Feature 004 specification
remain unchanged; the legacy test fixture's source loading migration is documented
in ADR 0007. Report canonical acceptance and raw exits separately.

## Offline package preparation

Run from the repository root with a supported interpreter. Local GitHub origin
metadata supplies repository identity; no remote is contacted. Input is a complete
TaskPackage envelope from [data-model.md](data-model.md), with locally available
source/artifact objects and this interpreter's exact isolated discovery argv.
T001 JSON files are intentionally inert fixture inputs, not CLI packages. The
`test_three_fixture_mvp_uses_same_prepare_validate_effective_flow` case in
`scripts/test_task_automation.py` adapts all three inside disposable repositories
and exercises the same CLI flow without mutating real feature artifacts.

```sh
/opt/homebrew/bin/python3.14 -B -m unittest discover -s scripts -p test_task_automation.py -v
/opt/homebrew/bin/python3.14 -I -B scripts/task_automation.py prepare --input INPUT.json --output OUTPUT
/opt/homebrew/bin/python3.14 -I -B scripts/task_automation.py validate --package OUTPUT/REVISION/package.json --offline
/opt/homebrew/bin/python3.14 -I -B scripts/task_automation.py effective --package OUTPUT/REVISION/package.json --offline
```

`prepare` returns exit 0 for structurally valid preparation and emits the relative
package/summary names. `REVISION` is the raw serialization digest, including approval
references. Identical bytes reuse the same files; changed references create a new
revision even when the package identity is unchanged. Existing different bytes or
unknown files block with exit 5; incomplete interrupted output remains preserved
and blocks with exit 6. Do not reset or adopt it. Paths must remain inside the
repository, without symlinks/hardlinks/gitlinks; output files are created exclusively.
Keep credentials out of input data: opaque guard context is preserved verbatim in
the immutable package. Summaries/diagnostics omit context, raw argv and raw output;
local Git children receive no credential environment.

`validate --offline` performs shared package/source/outcome-shape validation. With
current unavailable guard/authority it returns exit 3 and precise blockers, never
runs the matrix or prerequisites and leaves raw exits empty. `effective --offline`
returns exit 0 for a structural preview, showing expected source/execution identities,
check/case counts and missing capabilities. Neither result grants eligibility.
Malformed input/interface is exit 2, invalid outcomes 4, output ownership conflict 5
and persistence/incomplete output 6; source/scope/capability blockers use 3.

Inputs are bounded to 1 MiB, 100 checks and 10,000 cases, without truncation;
positive deadlines retain T006's 300-second bound. Transport retry limit is three
and offline transport attempts are always zero. Authentication, independent review
and live observation remain null, execution eligibility false. Declaration of an
intended red in the test-authoring fixture proves no executed/approved matrix;
raw outcomes remain separate. Preparation/preview make no network, tracker,
runtime, check or prerequisite calls. Canonical acceptance still reports the separate five retained raw baseline reds.

## Readiness and entry revalidation

```sh
/opt/homebrew/bin/python3.14 -B -m unittest discover -s scripts -p test_task_readiness.py -v
/opt/homebrew/bin/python3.14 -I -B scripts/task_automation.py ready --package PACKAGE.json --live-config LIVE.json
```

`ready` requires trusted host-injected guard, authenticated authority/tracker,
runtime and exclusive ownership ports. `LIVE.json` is inert non-secret configuration;
the standalone command returns exit 3 (`readiness-host-unavailable`) and writes
nothing. Missing Feature 003 APIs, live observations or GitHub conditional safe-label
capability also block. No legacy guard fallback or provider/configuration factory is
supplied. Fake ports exercise the gate without proving live capability.

Readiness validates every capability needed by the approved execution, not only
entry, authenticates separate owner actions and checks exact source/runtime/settings/
argv/actions. Publication approval is separate from execution approval. Guard-owned
content comparison alone may retain proven progress-only changes. Runtime/workflow/
argv drift requires renewal. Two fresh readiness observations precede one conditional
write adding ready to the selected open issue while preserving unrelated labels.
Unknown/failed writes are incomplete, never blindly retried. Repeat the original
binding at entry and post-check; changed/dirty retry state blocks and is preserved.
No reset, pin adoption, journal deletion or baseline recreation is a recovery step.

T013 local evidence: accepted GitHub `main` was freshly read/cloned at
`2cc1825413573ea9c49c01db92e44a221bf18d13`; implementation commits are `55bb1eb`
and `c49eff1`. Python 3.14.8, with `/opt/homebrew/bin` first in child PATH:
`/opt/homebrew/bin/python3.14 -B -m unittest discover -s scripts -p PATTERN -v`
passed for `test_task_readiness.py` (31), `test_task_automation.py` (28) and
`test_check_ci.py` (44), each exit 0. `git diff --check` passed. Review regressions
cover missing seals, final-read source/runtime/ownership drift, extra-capability
loss/change and credential-free local-only Git children. The shared Git helper
correction was explicitly authorized after that credential-boundary finding;
acceptance policy/classifier/manifest and Feature 001–003 guard/contracts are unchanged.
The initial missing-API run and intermediate run overlapping checker-file edits
were red and are not acceptance evidence. Canonical `make check` and raw baseline
reporting belong to the coordinator's final combined T017 run, not these focused
results. No actual provider/runtime/guard capability or live dispatch was verified.

## Remaining acceptance scenarios

The scenarios below belong to later tasks and are not claimed by this offline MVP.
Use disposable repositories, disabled network and fake runtime/tracker ports before
separately authorized live work. Use [contract](contracts/cli.md) and
[model](data-model.md); do not infer execution authority from these examples.

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
