# T019 rework test proposal

Status: **T019 test-authoring accepted under the exact owner-approved matrix**.
[Feature 003 T019](../../specs/003-single-task-entry/tasks.md) adds tests only in
`tests/test_single_task_context.py`. Its implementing successors are T022/T023.
The original 99 context cases and every existing CLI regression remain unchanged.
This completion records verified red-test evidence. Actual rework behavior remains
pending T022/T023; live dispatch remains unverified.

The preparation base is the independently reviewed T017 publication candidate
`767eb60fa6b69240e98b69ff379081469ea55484`. Its context/CLI tests, owning modules
and native runner are byte-identical to the historical preparation source
`52b0d3dc86689440c0f90f765925293a64da38d1`.
The exact collected IDs, ordered outcome records, assertion fingerprints, source
snapshots and discovery argv are recorded in the
[JSON proposal](feature003-rework-matrix.json). An earlier
measurement with 16 setup errors was rejected and retained
separately; those errors never qualify as expected failures.

## Fixture and reached boundaries

Every new case creates a disposable local Git repository and runs actual
`begin`, `inspect`, reviewed `verify`, and `complete`, then commits the sealed
implementation and checked task. No implementation-binding event is synthesized.
Approval/reviewer references are synthetic local assertions, with authenticated
owner identity and live dispatch both reported false. Checks use finite trusted
argv and write only a counter outside the repository.

Nine cases directly invoke `rework` with an invalid corrective context or absent
prior implementation binding. The
current missing command returns `command_unavailable`; each test requires its
specific validation code/category, so this cannot qualify as a successful
authorization rejection. These observations establish missing public behavior;
the corresponding validation assertions remain unexecuted requirements.

The other eight cases first call actual `record-commit` in their test bodies,
requiring success, then prepare a fresh review and call actual `resume`, requiring
`handoff-ready`. Only then do they exercise T022 rework semantics. Their current
first failures are the missing T023 commit-binding command. T020 supplies the
binding implementation. Deeper rework, fresh-resume and corrective-completion
assertions are executable obligations, not behavior demonstrated by those reds.
This capability ordering does not change the authoritative task dependencies.

| Group | Cases | Required later behavior |
| --- | --- | --- |
| Missing authorization/history fields | 3 | Missing rework approval, prior attempt or implementation field is rejected |
| Changed corrective scope/checks | 2 | Unrenewed context digest is rejected |
| Retry/ready assertions | 2 | Neither counter nor ready flag authorizes corrections |
| Missing retained predecessor | 1 | A schema-valid context cannot replace missing journal history |
| Unbound prior implementation | 1 | A matching Git commit alone cannot replace retained implementation binding |
| Wrong predecessor task/commit | 3 | Resolve matching retained task and exact local implementation evidence |
| Semantic drift/dirty baseline | 2 | Require renewed artifact approval and preserve uncommitted work |
| Authorized linked attempt | 1 | New immutable attempt retains checked bytes, original context and prior history |
| Corrective completion | 1 | Fresh checks/review seal correction with identical checked pre/post task bytes |
| Renewed scope/checks | 1 | Explicitly renewed corrective scope and checks bind the new attempt |

The wrong-task case completes T002 through the real CLI, using the existing valid
T001 prerequisite fixture, then requests the already checked T001 with the T002
predecessor. This isolates the task mismatch from pending markers or missing
prerequisites. Denied entry preserves product bytes/modes, index, HEAD, original
context-copy inventory, checked task bytes and immutable prior evidence. Successful
entry must preserve the same product state while adding exactly its linked attempt.
Corrective completion requires an exact durable intent whose pre/post bytes are
identical and preserves the predecessor's event prefix and context copy.

## Acceptance boundary

Actual native captures at `762e7614aa98dcc668f5d8c3c0cdda30088a6556` collected
**181 cases: 162 passes and 19 assertion failures**, with raw child exits **1, 1**.
Context contains 99 preserved passes plus 17 new capability assertions; CLI contains
63 passes plus the two retained T023 retry assertions. No case was unclassified or
missing, and both child captures emitted no stdout/stderr. Their source snapshots
were identical before and after execution. The new nine direct negatives encounter
unavailable `rework`; eight later cases encounter unavailable `record-commit`.
The deeper authorization/rework assertions remain unreached.

Focused measurement uses the unchanged native `task_outcomes.Outcomes` classifier
with absolute Python 3.14, isolated mode and disabled bytecode. It collects the
entire context and retained CLI suites; it rejects import/setup failures, missing
or extra cases, skipped tests and unclassified failures. Source snapshots must
remain unchanged during each capture. Raw suite exits and expected-outcome verifier
acceptance are separate results.

The owner explicitly approved frozen proposal
`cc698174efcbc3a08ba79814f8e0dfd54ec6e628`, its exact argv/Python/PATH and native
checks digest `sha256:54eb8d1785eeb8d8d66bc04f8398b852b606386a82f39c5055314b2e2f6869c2`,
following the combined T018/T019 package and independent review recommendation.
That current-session authorization was relayed by CrewBook dispatch; it is supplied
local authority, not independent owner identity authentication.

The approved wrapper ran **before source or checkbox edits** and returned **exit 0,
accepted=true**. Both exact inventories, fingerprints and raw child exits matched:
181 cases, 162 passes and 19 expected assertions, raw exits 1, 1. The approved input
and post-verifier snapshots were both
`sha256:7c0cf99a7594838f42ee08a35819a4c1ef1706ae4aa3bd38ead841047303d0b0`.
The full native result has SHA-256
`fef21058eff846b58ce2cba7432c8679b4cc2a3dd8282b7eb54ce70c195e3db8`.
Only after preserving that result and unchanged snapshot did bookkeeping mark T019
complete and append this evidence. The JSON keeps actual measurement source
`762e7614` distinct from approved execution source `cc698174`; later evidence edits
do not claim another execution. Its accepted record reproduces the approved matrix
by replacing only observed-check input revisions with the approved input snapshot.

The 17 new assertions still observe missing command capabilities; deeper rework
semantics remain unexecuted. Canonical repository acceptance remains a separate
required handoff gate. No full suite, authentication, platform coverage or live
runtime result is claimed here.
