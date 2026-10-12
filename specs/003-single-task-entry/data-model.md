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
- `dependencies`: explicit unique task IDs including `[]`, sorted by task ID.
  `source_assertion` has exactly `path` (selected feature's tasks.md), `sha256`
  (its reviewed raw-byte digest), `excerpt` (the full dependency table below) and
  nonempty `owner_ref`. Bind these fields in the context approval. Validate the
  reviewed table and compare the selected row exactly with dependencies; no prose
  inference or standalone assertion may replace the authoritative table.
- `prerequisite_evidence`: exactly one local record per dependency; task/feature,
  completed status, content manifest, implementation commit, passing check results
  and evidence_ref. For a test-authoring prerequisite, passing results mean the
  reviewed expected-outcome verifier passed; its evidence_ref also retains the
  exact collected-case matrix and raw expected-red outcomes required by tasks.md.
  This does not waive check exit-zero validation or assert implementation success.
  Require a checked prerequisite in current tasks and locally
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

### Rework approval schema

For the strict T006 schema, `rework_approval` has exactly
`owner_ref` and `evidence_ref`, both nonempty strings. These are inert local
operator references. The existing context approval digest binds this entire record,
`prior_attempt_id`, `prior_implementation_commit`, corrective `allowed_paths`,
`semantic_constraints` and `checks`. T022 must additionally resolve the retained
prior attempt and implementation evidence; this record alone grants no rework.

The owner accepted this exact shape in the 2026-10-12 implementation session,
reviewing revision `9b7fa407dd1aebf2278a946df9ce45d02e8b775a`. T006 acceptance
requires a valid rework schema control; T008 additionally verifies that changing a
schema-valid mode/context invalidates an unrenewed approval digest.

Contexts use canonical JSON (UTF-8, sorted keys, compact separators) for digesting;
exclude the approval.context_digest field itself from the digest. Evidence references
are inert local references: never fetched, evaluated or assumed authentic.

## Dependency table grammar v1
The reviewed tasks.md must contain exactly one dependency table outside fenced code.
Its header and separator are exactly `| Task | Prerequisites |` and
`| --- | --- |`. Each following contiguous row has exactly one of these forms:

```text
| T001 | none |
| T002 | T001 |
| T003 | T001, T002 |
```

IDs match `T[0-9]{3}`. Rows are sorted by task ID and enumerate every real task
exactly once; prerequisite lists are sorted, comma-space separated, unique and
refer only to other tasks in the same feature. `none` is the sole empty-list
spelling. Reject omitted/extra/duplicate rows, duplicate headers/tables, unknown
IDs, self-dependencies, cycles, malformed separators and contradictory duplicate
rows. The excerpt contains the header, separator and all rows, with LF separators
and no trailing LF; locate it by exact lines in the reviewed artifact (CRLF line
terminators may be removed for this comparison only; its artifact digest still
hashes raw bytes). Context IDs must equal the selected row, including an explicit
`[]` for `none`; prerequisite evidence must cover exactly that set.
Other prose is not parsed for dependencies. The owner review must attest that this
table is the complete dependency declaration and that surrounding prose does not
contradict it. An artifact without this table blocks until the owner supplies and
reviews a corrected artifact revision; an operator cannot override it with an
unanchored context list. This feature changes no real Feature 001/002 artifacts.
Tests cover valid empty/nonempty rows and every rejection rule above.

## Review preparation and canonical content manifest
`inspect --attempt ID` acquires the same worktree lock, validates saved context and
scope, and returns the current manifest without executing acceptance commands,
changing task bytes or appending acceptance/completion events. It returns
`attempt_id`, `task_id`, `context_digest`, `content_manifest` and
`content_manifest_digest`; it does not approve the content or claim verification.
The reviewer examines actual content and semantic constraints before creating a
SemanticReview with these identities. Inspection is a snapshot, not a reservation:
verify/complete/resume independently recompute the manifest under lock and reject a
stale review before checks, and reject content changes during checks.

The manifest has exactly `schema_version: 1` and `entries`, an array sorted by
repository-relative path in Unicode code-point order. Include every allowed path
and the selected tasks.md path exactly once, including absent allowed files.
A regular-file entry has exactly `path`, `type: "regular"`, `mode` (Git mode string
`"100644"` or `"100755"`) and `sha256` (64 lowercase hex characters).
An absent entry has exactly `path`, `type: "absent"`, `mode: null`, `sha256: null`.
Reject directories, symlinks and gitlinks. Hash raw bytes; for tasks.md replace only
the selected checkbox byte with ASCII space before hashing, preserving all other
bytes and line endings. Exact unnormalized pre/post bytes remain recovery evidence.
Use SHA-256 over UTF-8 JSON with sorted object keys, compact separators,
`ensure_ascii=False`, no NaN/Infinity and no trailing newline. Do not normalize
Unicode or paths beyond the required canonical-path validation. This serialization
also applies to the context digest, with its documented excluded field.
Fixtures must assert identical canonical bytes/digests on Linux and macOS, including
non-ASCII paths, executable files, absence and the selected checkbox transition.

## SemanticReview (immutable per acceptance identity)
Clarification proposed for owner review: consistent with this document's versioned
evidence rule and the approved T001 review fixture, the record has exactly eight
keys: `schema_version`, `attempt_id`, `task_id`, `context_digest`,
`content_manifest_digest`, `reviewer_ref`, `evidence_ref` and `approved`.
`schema_version` must be integer `1`; reject missing or unknown keys.

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
finalizing journal progress. Recovery under the same exclusive lock distinguishes:

- Before an intent exists, a valid `active` or `acceptance-recorded` attempt may
  resume only with tasks.md byte-identical to its immutable entry snapshot. For
  `acceptance-recorded`, current content must also match the recorded acceptance
  manifest; changed content blocks without adopting a new baseline. Validate the
  saved context, scope and current content-bound semantic review, rerun every
  approved check and revalidate content. Old successful results never authorize a
  marker write. Only fresh success permits appending new acceptance evidence,
  persisting the exact pre/post intent and continuing the same attempt through the
  normal completion transition. Retain earlier events and attempt ID; do not
  reimplement, create another attempt or change the marker before durable intent.
- Once an intent exists, compare actual tasks bytes to its exact pre/post bytes,
  rerun checks against sealed content and finish only that prepared transition.
  A `completion-prepared` or later state missing its required intent is corrupt
  evidence, not the valid pre-intent case; block without reconstructing it.

Missing/stale review, failed checks, mismatched tasks/content or invalid journal
state block without changing task bytes, resetting state or deleting prior events.
The same pre-intent rule applies to rework: the entry snapshot is already checked,
and its eventual pre/post marker bytes remain identical.
After successful checkbox completion mode is handoff-only; commit binding may still
be pending. `record-commit` is permitted, implementation changes are not. Full handoff
verification requires a bound commit. Repeated complete is idempotent and never
creates another attempt or silently replaces bound evidence.
Rework creates a new linked attempt from a clean baseline with explicit authorization;
the task must already be checked. Its pre/post marker bytes are identical. It seals
corrective content with new checks/review and binds a new commit, retaining all old
attempts/implementation identities. No completed task is reopened.
