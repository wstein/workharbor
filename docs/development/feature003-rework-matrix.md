# T019 rework test proposal

Status: **preparation only; exact owner matrix approval remains pending**.
[Feature 003 T019](../../specs/003-single-task-entry/tasks.md) adds tests only in
`tests/test_single_task_context.py`. Its implementing successors are T022/T023.
The original 99 context cases and every existing CLI regression remain unchanged.
No implementation, completion checkbox, dispatch or expected-red acceptance is
authorized by this proposal.

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

The executable matrix is generated outside the checkout after the final proposal
commit, binding its new input snapshot while retaining the actual capture identities.
The exact wrapper argv and checks digest accompany that final binding. They require
independent review and a distinct
owner approval before any `--matrix-approved` invocation or T019 completion.
Canonical repository acceptance remains a separate required handoff gate. No full
suite, authentication, platform coverage or live runtime result is claimed here.
