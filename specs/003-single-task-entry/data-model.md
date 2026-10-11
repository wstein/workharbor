# Data model: Local single-task entry

Status: proposed; formats are specified, not implemented.
All context/evidence formats have `schema_version: 1`, reject unknown/missing fields,
and use repository-relative canonical paths with no escapes or symlink components.
Persist only safe references and identifiers; no authentication tokens or raw output.

## TaskContext (immutable)
- `feature`: existing direct child of specs/; `task_id`: one unique `TNNN` outside
  fenced code in tasks.md, not a list or wildcard. Required regular spec/plan/tasks.
- `mode`: `implement` or `owner-authorized rework` at entry; handoff-only resumes state.
- `reviewed_artifacts`: exact spec/plan/tasks paths, locally resolvable commit and
  SHA-256 bytes identity for each artifact. Reviewed commit must be an ancestor of
  the clean entry HEAD; no network lookup or implicit checkout is permitted.
- `approval`: nonempty owner_ref/evidence_ref and context_digest binding feature,
  task, reviewed artifacts, dependency assertion, allowed paths, semantic constraints,
  checks and requested mode. This is an operator assertion, not authenticated identity.
- `dependencies`: explicit unique task IDs including `[]`, plus `source_assertion`
  naming artifact path, reviewed digest, exact dependency excerpt (or explicit
  declaration of no dependencies), and owner review reference. Validate against
  corresponding source bytes and prerequisite records; no prose inference.
  If the excerpt cannot unambiguously enumerate those IDs, block and request a
  reviewed structured declaration rather than treating it as complete.
- `prerequisite_evidence`: exactly one local record per dependency; task/feature,
  completed status, content manifest, implementation commit, passing check results
  and evidence_ref. Require a checked prerequisite in current tasks and locally
  resolvable implementation commits whose recorded tree paths match the manifest;
  all check records bind to that manifest. Reject extra, missing or stale records.
- `allowed_paths`: nonempty unique exact paths; no globs/directories, .git or local
  state paths. Selected feature artifacts are protected. tasks.md is never a general
  allowed path; the selected marker has its own constrained completion rule.
- `semantic_constraints`: nonempty reviewed descriptions (for example link-only edits).
- `checks`: nonempty ordered records with ID, argv array of nonempty strings,
  repository-relative cwd (or `.`), positive timeout_seconds, and expected exit 0.
  No shell evaluation of task text; operator approves executables/argv explicitly.
- `prior_attempt_id`, `prior_implementation_commit`, `rework_approval`: all required
  only for rework; approval binds exact corrective scope and checks to prior evidence.

Contexts use canonical JSON (UTF-8, sorted keys, compact separators) for digesting;
exclude the approval.context_digest field itself from the digest. Evidence references
are inert local references: never fetched, evaluated or assumed authentic.

## SemanticReview (immutable per acceptance identity)
`attempt_id`, `task_id`, `context_digest`, `content_manifest_digest`, `reviewer_ref`,
`evidence_ref`, `approved: true`. All required; a changed content manifest invalidates
review. Review attests semantic constraints, not just path permission.

## Attempt and Event journal
Each attempt has immutable ID, context digest/copy, baseline commit, baseline tasks
bytes, entry_mode, predecessor attempt and creation time. Effective execution mode
is derived: entry_mode while active, handoff-only after completion. The immutable
entry_mode is never overwritten by progress transitions. Each event has attempt ID,
monotonic per-attempt sequence, kind, payload digest and timestamp. Never edit/delete
old events; append failure and recovery events. SQLite transactions update event log
and derived progress together under the per-worktree exclusive lock.

Derived progress: `active` → `acceptance-recorded` → `completion-prepared` →
`checkbox-applied` → `completed-awaiting-commit` → `handoff-ready`.
A failed operation appends a blocker event; it does not authorize a mode change or
alter sealed input. Only one attempt is active per worktree. Lock acquisition is
nonblocking; concurrent commands report conflict without claiming completion.
Missing/corrupt/unsupported state blocks; do not import legacy single-task.json as
approved state. Retain legacy evidence and explain explicit operator migration.

## Acceptance and implementation evidence
Content manifests record each allowed path and selected tasks snapshot: file
existence, type, mode and SHA-256 bytes (use distinct typed absent values, not a
sentinel string that could match real file contents). Normalize only selected task
checkbox for comparison across completion. Retain exact before/after tasks digests
for recovery; all other bytes must match. Reject symlink/gitlink output changes.
Acceptance events bind context and content manifests to semantic review and ordered
check IDs, argv, cwd, exit status, timeout/error category and safe evidence references.
The latest verification HEAD is separate from the immutable bound implementation
commit. `record-commit` verifies the commit is locally reachable, contains the
selected completed marker and exact sealed allowed-path bytes/types/modes, and no
unauthorized baseline-to-commit/index/worktree/history changes exist.

## Approval comparison versus execution scope
Approval comparison may normalize all existing task checkbox markers while retaining
task identities/descriptions/order. It may ignore only appended EOF evidence footers
introduced by `## Execution evidence`, containing exclusively lines of the form
`- Evidence: TNNN PATH SHA256` with valid existing task, canonical local reference and
64 lowercase hex digest. Existing footer records cannot be changed/removed; referenced
content must match the digest. Spec/plan prose outside such footers is byte-exact.
No broad section stripping, whitespace normalization or semantic guessing is allowed.
Execution checks are stricter: all tasks.md bytes except the selected marker match
entry baseline; evidence for this attempt lives in the ignored journal or an explicitly
allowed evidence document. Approval normalization never grants execution permission.

## Completion and recovery transitions
Under the lock, validate context/scope and semantic review, execute checks, then
revalidate content. Persist acceptance and exact pre/post marker intent durably
before atomic tasks.md replacement. Persist replacement directory durability before
finalizing journal progress. A restart at any boundary compares the actual tasks
file to exact intent pre/post bytes, reruns checks against the sealed content and
finishes only that same transition. Anything else blocks without rollback.
After successful checkbox completion mode is handoff-only; commit binding may still
be pending. `record-commit` is permitted, implementation changes are not. Full handoff
verification requires a bound commit. Repeated complete is idempotent and never
creates another attempt or silently replaces bound evidence.
Rework creates a new linked attempt from a clean baseline with explicit authorization;
the task must already be checked. Its pre/post marker bytes are identical. It seals
corrective content with new checks/review and binds a new commit, retaining all old
attempts/implementation identities. No completed task is reopened.
