# Bounded Spec Kit task entry point

The local CLI validates one reviewed task context, checks its changes and acceptance,
and retains completion/recovery evidence. It does not implement tasks automatically.
Live Symphony dispatch remains unverified. The [Feature 003 CLI contract](../../specs/003-single-task-entry/contracts/cli.md)
and [data model](../../specs/003-single-task-entry/data-model.md) define the input formats;
[quickstart](../../specs/003-single-task-entry/quickstart.md) defines the local acceptance matrix.
The generic `speckit-implement` skill executes a feature by phases; use this entry
point when authorization covers exactly one existing task.

## Prepare the reviewed context

Use Python 3.11+, Git, Make and Bash in a clean, isolated per-issue Git checkout on
a local Linux or macOS filesystem. Preserve the checkout and ignored
`.symphony/single-task/` directory together across retries. It contains copied
contexts and the SQLite attempt/event journal; no credentials belong there.

Before `begin`, a trusted operator prepares a schema-version-1 JSON context outside
the tracked change set. The CLI does not generate approval records. Follow the
[data model](../../specs/003-single-task-entry/data-model.md) and the synthetic
[context template](../../tests/fixtures/single-task/context.json); the template has
placeholders and is not ready to run unchanged. Supply:

- One existing feature/task and `mode: "implement"`. Each spec/plan/tasks artifact
  needs its exact repository path, locally resolvable ancestor commit and raw-byte
  SHA-256. Unknown fields, unsafe paths and ambiguous task identities block entry.
- Explicit dependency IDs, including `[]` when the reviewed row says `none`.
  `source_assertion` binds the complete reviewed dependency table, its raw-byte
  digest and the supplying owner reference. No prose inference substitutes for
  that table. Every dependency needs a checked task, locally available implementation
  commit, content manifest and passing checks bound to that manifest; a checkbox
  alone is insufficient. See the synthetic [prerequisite record](../../tests/fixtures/single-task/prerequisite.json).
- Exact allowed file paths, nonempty semantic constraints and ordered acceptance
  checks with IDs, argv arrays, validated relative cwd, finite positive timeout and
  expected exit zero. Selected feature artifacts are protected; tasks.md is never
  a general allowed path. Completion has a separate selected-checkbox permission.
- `approval.owner_ref`, `approval.evidence_ref` and the canonical context digest.
  These are supplied local attestations, not authenticated owner identities or
  remote approvals. References are inert and are never fetched or executed.

The context digest uses canonical UTF-8 JSON: sorted keys, compact separators,
`ensure_ascii=False`, no nonfinite numbers or trailing newline, excluding only
`approval.context_digest`. Compute it after the other fields are finalized, then
have the operator approve that exact context. Changed scope, checks, dependencies
or requirements require renewed approval and a new attempt. The narrow permitted
progress-only artifact differences are specified in the data model; they do not
permit another task's checkbox to change during this attempt.

Acceptance argv must come from reviewed acceptance, never executable issue prose.
No shell interpolation occurs, but an approved executable can still have external
side effects. Keep command arguments and evidence references free of secrets;
raw subprocess output is not persisted by default.

## Begin, inspect and complete

These are synthetic invocation examples, not a claim that approval or a synthetic
feature exists in this checkout. Replace paths and `ATTEMPT_ID` with the reviewed
context/review files and the ID returned by `begin`. Keep those files outside the
tracked change set or in explicitly approved evidence scope.

```sh
python3 scripts/single_task.py begin --context /absolute/path/to/synthetic-context.json
python3 scripts/single_task.py status --attempt ATTEMPT_ID
```

Read the artifacts and implement only the selected task within the returned scope.
No prerequisite task or next task runs automatically. Then obtain the current
content identity without running acceptance or modifying task/journal evidence:

```sh
python3 scripts/single_task.py inspect --attempt ATTEMPT_ID
```

Review the actual changes against the semantic constraints, not just the allowed
paths. Prepare a review JSON record with exactly `schema_version: 1`, `attempt_id`,
`task_id`, `context_digest`, `content_manifest_digest`, `reviewer_ref`,
`evidence_ref` and `approved: true`, using the inspection identities. See the
[synthetic review template](../../tests/fixtures/single-task/review.json).
Inspection is a snapshot, not approval or a reservation. A later content/type/mode
change makes that review stale and requires fresh inspection and review.

```sh
python3 scripts/single_task.py verify --attempt ATTEMPT_ID --review /absolute/path/to/synthetic-review.json
python3 scripts/single_task.py complete --attempt ATTEMPT_ID --review /absolute/path/to/synthetic-review.json
```

Both commands check worktree, index, committed-since-baseline history and nonignored
untracked changes before and after acceptance. Restoring a worktree or reverting a
commit cannot hide an unauthorized earlier change. Unsafe symlinks/gitlinks and
nonregular outputs block. Only the selected checkbox may change in tasks.md;
all other bytes, descriptions, ordering and markers must match entry.

`verify` runs the checks without marking completion. `complete` reruns them,
durably records acceptance and exact completion intent, then atomically publishes
only the selected marker. Success enters `handoff-only` with
`completed-awaiting-commit`; it does not yet establish handoff readiness.
Missing tools, nonzero exits, timeouts, stale reviews and check-induced mutations
block completion. Retain the failed evidence and affected files for review.

## Commit, resume and rework

After completion, commit the approved implementation and selected checkbox using
the repository's focused Conventional Commit procedure. Do not include a commit's
own hash in a file that forms part of that commit. Bind the resulting local commit,
then freshly verify the handoff:

```sh
python3 scripts/single_task.py record-commit --attempt ATTEMPT_ID --commit IMPLEMENTATION_SHA
python3 scripts/single_task.py resume --attempt ATTEMPT_ID --review /absolute/path/to/synthetic-review.json
python3 scripts/single_task.py status --attempt ATTEMPT_ID
```

`record-commit` creates no commit. It validates the selected completed marker and
sealed output bytes/types/modes against a locally reachable commit and retains the
original implementation identity. `resume` reruns acceptance and binds readiness
to fresh evidence and the current verification HEAD. Only then can progress be
`handoff-ready`. Repeated verification does not replace the implementation commit.
Record that SHA and actual command/exit/platform evidence in the subsequent review
handoff; PR/tracker actions follow [WORKFLOW.md](../../WORKFLOW.md) separately.

For interruption before a durable intent, `resume` requires exact entry task bytes
and fresh review/check validation; recorded acceptance also requires unchanged
sealed content. After intent preparation, it accepts only the recorded exact
pre/post task bytes and recovers that same transition under the lock. It never
reimplements the task, selects a new baseline or reconstructs missing evidence.
A later completion state without its required intent is corrupt, not recoverable
from the checkbox alone. Failed recovery preserves evidence for investigation.

Corrections after completion require a new reviewed context with
`mode: "owner-authorized rework"`, retained `prior_attempt_id`, exact
`prior_implementation_commit` and explicit `rework_approval` owner/evidence
references. The approval digest binds the corrective scope and checks. Start from
a clean baseline with the task already checked:

```sh
python3 scripts/single_task.py rework --context /absolute/path/to/synthetic-rework-context.json
```

Use the new attempt ID for inspect/review/verify/complete/record-commit/resume.
The old attempt/events/commit identity remain; the checked task is never reopened.
A retry counter, ready label or completed checkbox cannot authorize rework.

## Blockers and limits

Legacy `begin --feature/--task/--allow/--check` entry is rejected. A retained
`.symphony/single-task.json` is an explicit migration blocker; it is not imported
as approved journal state. Preserve the legacy file and checkout for operator
investigation. Prepare a separately reviewed clean checkout/context if migration
is authorized; do not delete, overwrite or fabricate state to bypass the blocker.
Missing, corrupt or inconsistent journal/context evidence likewise blocks.

Every command, including status, uses the nonblocking per-worktree advisory lock.
A competing process returns conflict; process exit releases the lock. This is local
POSIX filesystem behavior, not a distributed lease. Windows and network-filesystem
locking are outside the initial supported matrix. A local run does not substitute
for the required Linux/macOS acceptance evidence in the quickstart.

Responses are safe versioned JSON, with `owner_identity_authenticated: false` and
`live_dispatch_verified: false`. Exit categories are 0 success, 2 context/approval/
prerequisite blocker, 3 scope/evidence blocker, 4 acceptance failure, 5 concurrency
conflict and 6 persistence/platform failure; see the CLI contract for details.
Begin, status and verify success do not claim completion.

The guard detects repository changes after they occur; it is not a filesystem
sandbox. Ignored files, host/tracker state, external side effects and owner identity
remain outside its enforcement. Local state is not cryptographically authenticated.
Maintain existing isolation and human review. Local completion creates no PR,
changes no label and proves neither live dispatch nor unattended operation.
