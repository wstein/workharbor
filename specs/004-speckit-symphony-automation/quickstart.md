# Validation guide: Task automation

Status: local package preparation, source/approval/runtime ports, readiness,
durable recovery, publication and final handoff
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

## Durable recovery and final handoff

Run the `test_task_handoff.py`, `test_task_tracker.py` and `test_task_automation.py`
discovery suites with the supported interpreter. `task_automation.py resume` and
`handoff` accept `--package PACKAGE.json --attempt ATTEMPT --offline`; standalone
commands return blocked exit 3 without creating a missing journal. Actual guard
completion/resume and GitHub conditional labels remain unavailable by default.

Preserve the checkout, journal, immutable package and original guard implementation.
Failed revalidation/changed inputs block; only explicit owner corrective scope/checks
permit a linked guard-owned attempt. Publication reuses the accepted local
preparation helper and exact reviewed repository/base/head refs and commits;
durable stable keys and exact authenticated queries prevent blind duplicate writes.
Checks → commit binding → exact review → authorized publication → durable evidence
precede final labels. The conditional intent retains observed labels/version,
desired labels, evidence digest and recovery before writing; unrelated labels stay,
ready is removed, human-review or durably explained blocked is added, issue stays open.

The final authenticated receipt requires no later provider action. Unknown writes
remain incomplete (exit 6). Retry reconciles a durably attempted final intent before
new guard/publication work, even with those ports absent; ownership, label capability
and action permission still apply. Mere intent without an attempt cannot confirm
externally matching labels. Conflicts recompute with at most four attempts;
uncertain drift/ambiguity/exhaustion require owner recovery. No reset, journal deletion
or baseline invention. Fixture tests interrupt every progress boundary and final
response/local persistence, including actual CLI unknown/crash retry; these prove
no real provider/runtime operation. Combined canonical/raw evidence belongs to T017.

## T017 combined local acceptance

Verified 2026-10-12 against accepted GitHub `main`
`2cc1825413573ea9c49c01db92e44a221bf18d13`, with tested code
`ebe4833cd2750cfefa9b115116d00d8b69a05457`. The serial integration preserves
reviewed atomic commits: T014 `c65cfc92` → `c8d8f03`, T013 `9864e993` →
`77bf4e9`, T015 `49b326a6` → `e697d3a`, T016 `a618a89b` → `2225e59`.
The combined ready/resume CLI conflict resolution retained both reviewed routes;
99 shared readiness/handoff/automation cases passed before the final integration.
T016's complete integrated tree equals its reviewed source tree.

With `PATH=/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`, the raw check followed
the initial timeout; the corrected canonical then passed on the listed snapshot:

| Command | Exact tested code | Observed result |
| --- | --- | --- |
| `make check PYTHON=/opt/homebrew/bin/python3.14` | `ebe4833cd2750cfefa9b115116d00d8b69a05457` | Exit 0; 422 cases, 417 passes, five exact retained raw assertion failures, zero errors; manifest-qualified GREEN; 374.244 seconds |
| `make check-raw PYTHON=/opt/homebrew/bin/python3.14` | `2225e5987b1b6e957c2779ec36c33365b13590f7` | Make exit 2; 41 cases, 36 passes, five assertion failures; 10.331 seconds |

Tools: Python 3.14.8, Git 2.56.0, GNU Make 3.81, GNU Bash 5.3.20.
Python 3.11 was not run on this host. Canonical acceptance includes all discovered
new strict-green suites, minimum required IDs, exact retained outcomes, direct twelve
T004 CLI and twelve T003 state cases, repository/diff/shell/prerequisite checks.
No historical verifier entry point, per-task hash or new red exception is a gate.

The first start refused ignored cache files before running tests. After preserving
those outputs outside the checkout, the initial aggregate suite hit its fixed
300-second timeout. Both failures remain failed evidence. Reviewed correction
`ebe4833` keeps ordinary command limits at 300 seconds and gives only the aggregate
repository suite a finite 600 seconds; package per-check deadlines, assertions,
classifier and manifest are unchanged. Its two new regressions failed before the
fix; all 46 CI tests and independent timeout/nonzero controls passed afterward.
Raw baseline code is identical across this correction; no second raw run is claimed.

Scope/history comparison to the accepted base leaves Feature 001–003 specs,
`single_task.py`, `single_task_state.py`, `approval_binding.py`, Feature 004
`spec.md`, the acceptance manifest and shared outcome classifier unchanged.
The separately reviewed CI Git-child environment/no-lazy-fetch hardening changes
no approval comparator/schema. Symphony remains external development tooling at
pin `be10a1b79df723d6d7612b5651c8522704dafb2e`; no product/runtime activation occurred.

T001–T017 deliver local tooling and combined acceptance. Disposable fixtures verify
rejection, ownership, interruption/recovery and conditional-provider behavior;
they authenticate no owner and prove no live guard/provider/isolation/safe-label
capability. Standalone ready/resume/handoff remain blocked without trusted ports.
T018–T020 remain pending: no live completion, measured saving or full Feature 004
completion is claimed. The final evidence commit changes only this guide, the quality
register and T017's checkbox: source/repository/diff checks and exact independent
review bind that prose-only head to tested code; no full rerun at that head is claimed.
Current-head forge checks must succeed before PR readiness.

PR 47 portability follow-up: Linux run `38283008010` at `150bbeba` failed
with 422 cases, 14 raw failures and zero errors (71.772 seconds). Readiness fixtures
inherited the enclosing GitHub event/SHA/path, so the real source gate correctly
rejected their unrelated local commits. The fixture now isolates/restores exactly
those three variables, matching existing source fixtures; production validation,
assertions, manifest and deadlines are unchanged. The original positive case
reproduced that failure locally. After correction, all 32 readiness cases passed
under inherited PR metadata (286.674 seconds, exit 0), including push/PR isolation,
cleanup restoration and continued rejection of mismatched host event pins.
The prior 422-case local result remains bound to `ebe4833`; no new full local
run or current-head Linux success is claimed. Fresh forge validation is required.

## Remaining acceptance scenarios

The scenarios below require live evidence beyond disposable fixture coverage.
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
