# Research: Local single-task entry

Status: design research complete; no Feature 003 implementation verified.
Sources are the repository spec, current `scripts/single_task.py` and its unittest
fixtures. No network/runtime upgrade or remote service is needed to make these
project-specific design decisions.

## Reuse the existing CLI
**Decision:** Extend and modularize the Python standard-library guard.
**Rationale:** Existing task/path checks, hashes and real-Git tests are useful local
baselines. Code inspection shows no prerequisite/approval contract, semantic review,
explicit rework, attempt history or lock. `verified_head` is mutable verification
metadata and cannot substitute for immutable implementation commit evidence.
**Alternatives considered:** Generic phase-oriented implementation skill (cannot
bound one task); new service/framework (unnecessary dependencies and remote coupling).

## Reviewed local context and dependencies
**Decision:** Strict versioned JSON context with operator approval/source assertions,
resolved local artifact commits and exact prerequisite evidence.
**Rationale:** Arbitrary Markdown prose is not a reliable dependency grammar; do not
infer dependencies or execute its text. Require a reviewed explicit list anchored to
source artifact identity and reject ambiguity. The authoritative declaration is the
complete tasks.md table defined by dependency-table grammar v1 in the data model;
owner review attests its completeness, while validation checks syntax and consistency. Local records establish consistency,
not owner authenticity. These trust limits are explicit in FR-002/003 assumptions.
**Alternatives considered:** Live GitHub review lookup (violates offline requirement);
checkboxes alone (not implementation/test evidence); natural-language auto-inference
(ambiguous and cannot prove completeness). Structured evidence footers provide a
narrow progress-update rule without permitting changed requirements disguised as notes.

## Exclusive durable state
**Decision:** Per-worktree POSIX nonblocking advisory lock plus SQLite journal;
immutable contexts/attempt identities, append-only events, atomic task-file replace.
**Rationale:** A process lock serializes begin/verify/complete/rework; OS release after
crash avoids PID-file stale-lock guessing. SQLite transactions protect event state;
the separate Markdown write still needs a durable prepared intent and exact-byte
recovery. Test those boundaries explicitly. Directory/file fsync is required around
new journal/context and task replacement; fail on unsupported persistence guarantees.
**Alternatives considered:** Single overwritten JSON (current code loses history and
races); stale lock-directory deletion (risks two writers); database only (cannot make
a Markdown write atomic with a database transaction). No network filesystem guarantee.

## Completion and rework
**Decision:** Keep mode separate from progress. Seal local acceptance, update checkbox,
then bind the matching committed tree with an explicit record-commit operation.
**Rationale:** A commit cannot contain its own hash. A crash before committing must
remain recoverable; it must not grant unrestricted reimplementation. Completed tasks
use handoff-only; corrective work requires a new owner-approved attempt linked to old
implementation and evidence. Retain the checked marker during rework.
**Alternatives considered:** Reopen task automatically (breaks approval/retry rules);
replace verified_head each time (destroys original implementation identity); require
commit before checkbox (contradicts desired implementation-plus-checkbox commit).

## Scope and evidence limits
**Decision:** Validate worktree, index and commit history separately; bind semantic
review and acceptance to a deterministic content manifest. Do not retain raw command
output in durable records by default; store argv, exit status and safe evidence refs.
**Rationale:** One net diff misses staged-only changes and intermediate commits that
were later reverted. Path allowlists alone cannot prove a runbook was only given a
link. Operator semantic review is therefore an explicit, content-bound prerequisite.
**Alternatives considered:** Hash files only (misses modes/types/index/history);
claim a sandbox (false: checks can have external side effects). Offline tests use
credential-free local commands and instrument the CLI to reject network attempts;
this does not sandbox arbitrary operator-approved acceptance programs.
