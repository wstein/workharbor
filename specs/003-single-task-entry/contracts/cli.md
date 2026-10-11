# Local CLI contract v1

Status: proposed interface. These commands extend the existing script and are not
currently implemented in this form. No tracker or Symphony protocol is exposed.

## Commands
Run from a Git worktree; paths supplied as arguments are never shell-generated from
issue prose. Context/review records follow [data-model.md](../data-model.md).

| Command | Inputs | Outcome |
| --- | --- | --- |
| `python3 scripts/single_task.py begin --context PATH` | Reviewed local context with mode implement, pending task and clean baseline | Validate artifacts/approvals/dependencies; create immutable attempt; return attempt ID |
| `python3 scripts/single_task.py rework --context PATH` | Rework context, checked task, complete prior evidence, new owner authorization, clean baseline | Create linked attempt in owner-authorized rework; preserve old state/marker |
| `python3 scripts/single_task.py verify --attempt ID --review PATH` | Current semantic review and existing attempt | Scope and acceptance checks; no task-marker mutation |
| `python3 scripts/single_task.py complete --attempt ID --review PATH` | Same as verify | Rerun checks; journal and apply only selected marker; enter handoff-only, awaiting commit |
| `python3 scripts/single_task.py record-commit --attempt ID --commit SHA` | Existing matching local commit | Bind immutable implementation identity; no commit creation or file modification |
| `python3 scripts/single_task.py resume --attempt ID --review PATH` | Saved state and current content-bound review | Recover prepared completion if needed, rerun acceptance; handoff readiness requires bound commit |
| `python3 scripts/single_task.py status --attempt ID` | Existing journal | Read-only mode/progress/evidence/blocker summary |

No command executes task implementation automatically. Begin returns a bounded
context; the operator/agent implements that selected task only, then uses verify and
complete. Commands execute only explicitly reviewed acceptance argv without a shell,
from validated cwd with finite timeout. They never run prerequisite tasks or select
a next task. Legacy ad hoc begin arguments cannot bypass context/evidence validation;
reject legacy state with migration instructions rather than overwriting it.

## Validation and outcome
Acquire the worktree lock before reading mutable task/journal state, including status
so it never displays a partial transition. Validate inputs before invoking any
acceptance command. Record failed check outcomes without leaking raw command output;
subprocess output is not persisted by default. Operators must keep commands/output
free of secrets. No automatic credential environment inspection or remote requests.

Structured output contains schema_version, attempt_id when known, mode, progress,
local_verification, owner_identity_authenticated=false, live_dispatch_verified=false,
and errors with stable code, safe message and affected paths/IDs. Never echo an
entire malformed context or secret-bearing argument back in an error.

| Exit | Category | Examples |
| --- | --- | --- |
| 0 | Successful operation | Valid entry; verified completion; read-only status |
| 2 | Context/approval/prerequisite blocker | Missing field, unknown task, semantic drift, missing approval/rework |
| 3 | Scope/evidence blocker | Changed unrelated task, stale review, missing commit, corrupt state |
| 4 | Acceptance failure | Nonzero exit, missing executable, timeout, post-check mutation |
| 5 | Concurrent attempt | Another process owns the worktree lock |
| 6 | Persistence/platform failure | Cannot safely lock, journal, flush or recover |

A successful begin/status/verify does not claim task completion. Complete may report
handoff-only with completed-awaiting-commit; it is not ready for tracker handoff until
record-commit and fresh resume verification succeed. Failures never mutate task
markers except recovery of an already prepared, valid completion transition.

## Protected scope and approval rules
Features 001 and 002 are forbidden implementation/test mutation targets for this
feature. All acceptance examples use synthetic repositories. Reviewed scope and
execution scope differ as described in the data model; no checkbox normalization
permits another task's marker to change during this attempt. Rework uses new approved
scope and prior-implementation evidence, never an edited old attempt.
The guard is local consistency tooling, not owner authentication or a sandbox.
No PR, label change, tracker write or live-runtime result follows from exit zero.
