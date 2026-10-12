# T012 executable test proposal

Status: **measured proposal; owner approval and task entry/completion pending**.
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

## Owner decision and remaining gate

The external exact-checks file is generated after the final proposal commit using
its native `snapshot(root)` binding. It preserves observed cases/tool bytes and
rebinds only the input revision. Its exact checks digest, final candidate SHA and
wrapper argv are supplied in the handoff, avoiding self-referential commit hashes.
The JSON's placeholder wrapper shows the structure, not executable authorization.
No matrix approval flag, zero-exit expected-red acceptance or authenticated owner
claim was supplied. Measurement-only child requests expected all cases green and
recorded actual results; they did not accept the red matrix.

The owner must approve the exact revision-bound matrix and runnable argv before
SingleTask entry or the expected-red acceptance gate. Task authoring is the bounded
preparation authorized here; T012's checkbox stays pending. Then run the reviewed
native verifier with explicit matrix approval, required repository checks and scope
validation before any completion decision. Preserve raw reds separately. Missing,
extra, unexpected green/red, import/setup/cleanup failure, skip, unfinished process
or source mutation blocks acceptance. No permissive nonzero-exit exception applies.

Repository/document and whitespace checks accompany this proposal. Canonical
`make check` and raw full suites are unperformed here and remain coordinator-owned.
No guard implementation, real Feature 001/002 mutation, provider/runtime operation,
tracker change, publication or activation was performed.
