# T012 preparation: acceptance and completion CLI cases

Status: **design proposal only; T010 acceptance and owner matrix approval pending**.
[Issue 13](https://github.com/wstein/workharbor-nxt/issues/13) selects
[Feature 003 T012](../../specs/003-single-task-entry/tasks.md). The
[JSON proposal](feature003-acceptance-matrix.json) freezes 40 proposed public CLI
case IDs and final contract assertions. It creates no tests, implementation,
SingleTask entry, completion marker or execution approval.

The preparation base is `3cc1350a952b7dfed633d3ff03c4082ee54e7be8`.
T012 depends on accepted T010. T010's author reports its migration is pending;
its current helper names and `command_unavailable` response are informative,
not acceptance evidence. No T010 implementation pin or measured T012 outcome
is supplied. The remote URL identifies `wstein/workharbor-nxt`; live remote-main
verification was unavailable because the native read failed DNS resolution.

## Cases and fixtures

Append each JSON suffix to `test_single_task.SingleTaskAcceptanceTests.test_`.
The 40 cases cover seven inspect cases, ten acceptance execution/evidence cases,
nine review-binding cases, five mutations during checks, and nine verification/
completion cases. Their exits describe the final
[CLI contract](../../specs/003-single-task-entry/contracts/cli.md), not measured
checkpoint results. T014–T017 implement those behaviors; T013 supplies shared scope
inventory needed at the CLI boundary. T011 owns direct scope/manifest/review cases
in its separate test file.

Use the accepted T010 `SingleTaskFixture` and migrated CLI helpers once available.
Every case begins with a real disposable Git repository, clean approved baseline,
valid locally bound context, valid explicit dependency declaration, and successful
`begin --context PATH`. Introduce only the named variation afterward. Context
checks, output files and fixture scripts must be reviewed and committed before
begin; reseal approval digests when changing reviewed checks. Keep context/review
files, execution trace and synthetic control inputs in fixture-private locations
outside repository scope; never copy live credentials or operate on real Features
001/002. All actual implementation changes remain in `tests/test_single_task.py`.

Do not import future acceptance/scope modules at collection time. Invoke the real
CLI from test bodies, with an outer finite deadline longer than the reviewed check
limit. The timeout case tests a returned CLI acceptance failure, not a test-runner
timeout. A pipe/barrier establishes the inspect lock conflict; timing-only sleeps
are insufficient. Do not repair T013–T017 behavior from this test-authoring task.

Independent expected bytes and canonical serialization establish manifest identity;
do not merely repeat the implementation's digest helper. Include Unicode paths,
raw line endings, executable modes and typed absence. Review preparation uses actual
attempt IDs from begin and manifests from inspect where available, with independent
expected identities so an incorrect inspect result cannot make a wrong review pass.
Against unsupported inspect behavior, a valid fixture still reaches a specified
public behavior assertion; no synthetic review approval bypass is permitted.

Failure cases require a safe versioned envelope and false identity/live-dispatch
flags. Assert marker/content preservation before the final contract assertion where
possible. For a check that itself changes tasks, preserve that actual mutation as
failure evidence and assert no tool-applied selected completion; automatic rollback
of the check's changes is not promised by the guard. Missing review CLI input uses
exit 2; mismatched supplied review uses exit 3; returned acceptance failures,
including timeout and post-check mutation, use exit 4. Error-code spellings beyond
the existing envelope are not invented here.

## Retained regressions and exact runner

The JSON lists all 16 existing `SingleTaskTests` IDs and retains all six fixture
cases. T010 migrates entry and shared helpers; T017 migrates verification/completion;
T023 migrates retry, commit and recovery scenarios. Preserve every assertion and
collected case. Their accepted T010 outcomes remain explicit checkpoint blockers
until the owning successors deliver them. Never delete, skip or weaken them to
make this proposal green. Reconcile names/counts against accepted T010 instead of
assuming the preparation-base inventory is still the full collection.

Reuse unchanged [task_outcomes.py](../../scripts/task_outcomes.py), with its native
exact schema and three proposed check IDs/argv in the JSON. The future runner checks
the full CLI file and retained context/state suites; it does not silently accept
T011's independently authored expected reds. Previously implemented cases must pass.
Any accepted unfinished T017/T023 regression outcomes need their own explicit
retained entries, distinguished from T012's newly authored missing behavior.

Each executable check needs exact ordered collected IDs, `pass` with null fingerprint
or `assertion` with its canonical assertion fingerprint, raw suite exit, current tool
revision and immutable input snapshot. Unsupported commands qualify only when a
collected test invokes the CLI and fails its intended test-body behavior assertion.
Reject import/setup/cleanup errors, skips, failed subtests, extra/missing/duplicate
IDs, unexpected green or red outcomes, unfinished subprocesses and source mutation.
Do not add another verifier, utility copy, custom stamp or repository dependency.

The JSON wrapper argv is a proposal with unresolved immutable paths and digest.
It deliberately omits the matrix approval flag. Its placeholder binding and null
observations prevent treating it as executable acceptance. No runner invocation,
owner approval flag or acceptance claim is made during preparation.

## Next gate and verification limits

Accept T010 and record its exact implementation/check evidence first. Reconcile this
design with that accepted API and baseline collection, then obtain the owner's
explicit bounded test-authoring decision. Before SingleTask entry/completion,
the exact authored test content, collected IDs, meaningful assertion outcomes,
raw exits, full runner checks digest and executable argv must receive the owner's
revision-bound approval. Design review alone cannot supply that approval.
If behavior is already implemented, retain the actual green outcome and revise the
proposed matrix through review; do not manufacture red assertions.

This preparation is checked only for JSON/design consistency, repository document
checks and whitespace. Canonical `make check`, raw suites, actual T012 assertions,
platform behavior and live dispatch are unperformed here; combined canonical
acceptance remains coordinator-owned. T012 remains pending, and T013 implementation
has not been entered. No provider/runtime, tracker, publication or activation action
follows from this proposal.
