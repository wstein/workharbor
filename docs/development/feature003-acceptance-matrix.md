# T012 executable test proposal

Status: **T012 test-authoring accepted; behavior implementation remains pending**.
[Issue 13](https://github.com/wstein/workharbor-nxt/issues/13) selects
[Feature 003 T012](../../specs/003-single-task-entry/tasks.md). The
[JSON record](feature003-acceptance-matrix.json) pins 39 proposed CLI cases, their
successors, all collected IDs and exact observed assertion fingerprints using the
unchanged [native runner](../../scripts/task_outcomes.py).

The accepted predecessor is main `e3e80a2e5b1cf0726e82c110793327a86eb35a72`
([PR 50](https://github.com/wstein/workharbor-nxt/pull/50)). Historical T010 code
`e24fcf1755655c4a0d2b3a252afafee6be0f727b` and checkpoint
`4683665f07c641c5dbfebca8e5a4f1c5619fadab` remain recorded. The inspected CLI,
context/scope/state modules, runner and retained tests/direct checks are identical
between that checkpoint and accepted main. Original preparation design
`d4532f7a46be9564493bec0b65c85518bd32cf0e` is preserved; design review supplied no
execution or expected-red acceptance authorization.

## Scope and meaningful outcomes

The test file adds 39 public CLI cases: seven inspect, ten execution/evidence,
nine review-binding, five mutations during checks and eight verification/completion
cases. T014–T017 implement their behaviors; shared inventory comes from T013.
T011 owns direct scope/manifest/review tests separately. The original 40th intention,
full durable acceptance-payload binding, remains explicitly with T015/T016 because
no authoritative observation schema exists. T012 invents no `status.evidence` field
or storage API. Its safe failure evidence case still asserts output redaction and
preservation of safe state without raw subprocess output.

All 16 legacy CLI methods and six fixture tests remain, including every existing
assertion. T010 owns entry/shared migration; T017 owns completion/verification and
T023 retry/commit recovery. Pending migrated scenarios remain explicit blockers;
none are deleted, skipped or weakened. The only shared-fixture adjustment adds
`PYTHONDONTWRITEBYTECODE=1` for existing subprocesses that lack `-I`, preventing
ignored source caches. The new SQLite inspection helper closes its connections.

Each new case uses a real isolated Git repository and successfully begins one valid
reviewed attempt before testing behavior. Independent fixture manifests prepare
content-bound reviews without requiring unimplemented inspect as setup. The approved
versioned review fixture remains unchanged; authoritative schema clarification is
coordinator-owned. No future module is imported at discovery. An explicit fixture
entry assertion differs from each intended public-command assertion; every measured
new red fingerprint was checked against the latter, rejecting entry/setup failures.

The canonical fixture disables Git's macOS Unicode precomposition locally before
committing decomposed names, so raw-path identity is preserved on Linux/macOS. It
includes Unicode paths, executable mode, binary bytes, typed absence and CRLF tasks.
A pipe establishes inspect lock ownership. Checks use reviewed argv/cwd and finite
inner timeouts with larger outer deadlines. Output-secret sentinels live outside
argv/context. Mutating checks retain their actual changes as blocker evidence;
rollback is not promised. Failure assertions reject false successful verification,
completion or handoff claims and preserve immutable attempts/prior event prefixes.

## Exact focused observations

The native runner's measurement-only child collected these complete inventories:

| Check | Cases | Pass | Assertion | Raw child exit |
| --- | --- | --- | --- | --- |
| CLI | 61 | 9 | 52 | 1 |
| Retained context | 99 | 99 | 0 | 0 |
| Retained state | 10 | 10 | 0 | 0 |
| Retained direct state/CLI | 24 | 20 | 4 | 1 |
| Total | 194 | 138 | 56 | — |

Of the 39 new cases, **two pass and 37 fail intended behavior assertions**.
`review_missing` and `inspect_respects_worktree_lock` already pass; they were kept
green. The other reds encounter actual `command_unavailable`, exit 2, at their
specified public-command assertion after valid entry. These observations exercise
the first missing behavior; assertions after that blocker remain future acceptance
obligations, not behavior already verified by the red run.

All 155 retained cases keep their accepted outcomes: 136 pass and 19 remain explicit
T017/T023 assertions, including all four direct CLI reds. Errors, skips, failed
subtests and unclassified outcomes are rejected. The JSON stores exact ordered
native case records (`pass`/null or `assertion`/fingerprint), reviewed discovery argv,
raw exits, tool revision and observation input snapshots. It distinguishes the
initial retained-suite source from the narrow CLI rerun after its canonical-fixture
fix; no unchanged retained suite is claimed rerun afterward.

Source snapshots remained unchanged during the final captures. An earlier capture
was rejected when existing direct subprocesses generated an ignored bytecode file;
the fixture environment fix was applied, that generated cache removed, and all four
focused checks rerun. A separate canonical-case entry failure was rejected and fixed
before the narrow CLI rerun. Native Git emitted macOS temporary-directory warnings;
there were no Python resource warnings in the final capture.

## Approved expected-outcome acceptance

The owner authorized the exact frozen proposal `4fde142667f4a33651dc79da1c0de6b546e04bd8`
and native checks digest
`sha256:7eee1eaf5fe67ae1da72387a04b196602917ddc50d98f448924c9387b102e51b`.
The recorded exact wrapper argv ran with explicit matrix approval before any
source or task-marker edit. It returned **exit 0, accepted=true**, with no blocker.
All four ordered inventories, fingerprints and raw exits matched the sealed matrix.
The approved input and measured post-verifier snapshots were identical:
`sha256:2a5a6ba33e16cb91b6e60128a0be1c1775a2a0b9616a7b41ee9a12cc3df7dbab`.

Actual results remain **194 cases: 138 passes and 56 expected assertions** (37 new,
19 retained), with raw child exits **1, 0, 0, 1**. The safe full verifier result has
SHA-256 `e067952fb34cb49b371de523e137e48661253183313849bcba00e835a0f98c33`.
The JSON records the source/input/tool bindings and actual per-check acceptance.
Its exact approved native matrix is reproducible from `observed_checks` by replacing
only each input revision with the approved input snapshot, retaining every other
field and outcome. No new binding was substituted during execution.

The earlier measurements and their historical pending-approval status did not grant
authorization. Owner session authorization was supplied separately; this tooling
still makes no independent identity-authentication claim. Only after the accepted
verifier result was preserved did bookkeeping update T012's checkbox and this
evidence. The later bookkeeping revision is distinct from the approved execution
source; it does not silently refresh the old matrix digest or source approval.

T012 completion means verified expected-red test evidence. T014–T017 still own
missing behavior; the 19 retained T017/T023 assertions remain explicit blockers.
No raw red suite is called passing. Canonical `make check` and raw full repository
suites were not run by this leaf and remain coordinator-owned. Repository/document
and whitespace checks passed for the frozen proposal before execution; no tests or
guard code changed during completion bookkeeping.

No actual repository SingleTask attempt was created by this preparation route.
The executed task attempts were isolated test fixtures. No guard implementation,
real Feature 001/002 mutation, provider/runtime operation, tracker change,
publication or activation was performed.
