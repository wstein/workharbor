# Research: Task automation

Status: Phase 0 at approved repository source
`8f4debe5cd2ee2770f5963d292a9eb60af6270d7`; no live observation.

## Existing code and minimum guard dependencies

**Decision**: reuse Feature 003 as sole guard. `scripts/single_task.py` currently has
legacy begin/verify/complete, scope/history checks and JSON completion evidence.
`single_task_state.py` has SQLite attempts/events and POSIX lock primitives, exercised
by `verify_t003_state.py`. Context-based CLI entry, full approval validation,
completion recovery and immutable commit binding remain unimplemented. Checkmarks
and direct primitive tests do not demonstrate these missing capabilities.
**Rationale**: offline preparation is independent; entry, completion and resume
require only their relevant demonstrated safeguards. Live readiness checks the
capabilities needed by the entire approved execution to avoid knowingly stranding it.
**Alternatives considered**: blanket all-003 prerequisite delays independent work;
a second guard produces competing approval/completion rules.

## Shared validation and expected outcomes

**Decision**: one library validator serves CLI and CI, delegating context/scope/
acceptance to Feature 003. A reusable Feature 004 runner is the approved check argv:
zero means exact matrix matched, preserving Feature 003's zero-exit check contract.
Shared outcome classification derives from
`verify_t002_red.py:Outcomes/evaluate` exact inventory and pass/assertion semantics,
plus stricter subtest/setup rejection in `verify_t004_cli.py`. Preserve both frozen
scripts. The planned shared runner must demonstrate these rejection cases before use.
**Rationale**: existing `check_ci.py` T003/T004 ancestry/task seals are transitional
preparation evidence, not reusable approval authority.
**Alternatives considered**: arbitrary nonzero acceptance (unsafe); one new pinned
CI exception per task (repeated administration).

## Effective runtime

**Decision**: retain Symphony pin `be10a1b79df723d6d7612b5651c8522704dafb2e`.
Verify actual launched runtime through exact source/object identity or verified build
provenance; compare workflow bytes/effective settings before readiness and entry.
`prepare_symphony.py` currently renders source/environment settings and checks Codex
presence; it proves neither actual runtime identity, approval nor activation.
**Rationale**: pinned upstream [GitHub adapter](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/lib/symphony_elixir/github/adapter.ex)
exposes the tracker port. Its [agent tool](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/lib/symphony_elixir/github/agent_tool.ex)
accepts method/path/params/body, without arbitrary conditional headers. Its
[app-server launcher](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/lib/symphony_elixir/codex/app_server.ex)
removes tracker secret environment names from children; actual host behavior still
needs observation. No new API or runtime fork is assumed.
**Alternatives considered**: trusting configured pin text or rendered workflow as
proof of actual runtime/capabilities.

## Identity and GitHub label concurrency

**Decision**: authenticated retrieval of [comment](https://docs.github.com/en/rest/issues/comments#get-an-issue-comment)
or [review](https://docs.github.com/en/rest/pulls/reviews#get-a-review-for-a-pull-request)
objects must bind provider user ID to trusted owner configuration and exact
repository/feature/task/revision/action. Re-fetch at readiness; changed/deleted or
dismissed evidence blocks. Body text and author association are insufficient.
Provider account attribution is not proof of a fresh human interaction; separate
owner decisions and independent review remain required.
**Rationale**: comments can be edited; a review's commit ID does not identify every
requested action. Local fixtures remain supplied assertions, not authentication.
**Alternatives considered**: approve by ready label, quoted owner name or local flag.

**Decision**: current GitHub adapter reports `safe_final_label_replace: false`.
[Set labels](https://docs.github.com/en/rest/issues/labels#set-labels-for-an-issue)
replaces the full set, but [conditional request guidance](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests)
does not support conditional writes unless the endpoint says otherwise. No CAS is
documented here. Offline CAS fakes prove protocol behavior only; affected live
readiness blocks. A verified deployment serializing all possible writers or a future
supported provider operation must establish the capability before live eligibility.
**Rationale**: read/merge/PUT/read-back may silently lose another writer's label.
**Alternatives considered**: local locks, retries or separate add/remove operations;
none demonstrates preservation against concurrent external writers.

## Source identity and recovery

**Decision**: retain accepted, proposal and synthetic identities separately.
[PR Actions events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request)
normally check out a merge ref; verify event/base/head/ordered parents and complete
expected tree. [Rebase-and-merge](https://docs.github.com/en/pull-requests/reference/pull-request-merges#rebase-and-merge-your-commits)
rewrites SHAs. Validate reviewed per-commit correspondence and protected bytes;
[range-diff](https://git-scm.com/docs/git-range-diff) is human evidence, not a stable
machine-readable validator. Unexpected history blocks even if final files match.
**Rationale**: tree equality alone cannot detect an unauthorized commit/revert.
**Alternatives considered**: treat merge/rebase SHA as approved source automatically.

**Decision**: retain immutable implementation identity in Feature 003; use a separate
journal for external intents/results. Query remote state after unknown writes, reuse
matching PR/evidence and block ambiguous matches. Finish evidence before final labels;
unknown final writes need operator reconciliation, not continued agent execution.
**Rationale**: lost responses differ from failed writes. Prospective timing includes
all preparation/approval/check/configuration/handoff repair, with explicit exclusions.
**Alternatives considered**: new PR on retry, replaced history, or code-only timing.

## Bounded corrections: PR delta, prepublication and content approval

**Decision**: apply the [bounded source contract](bounded-source-contract.md).
Task head/history scope is distinct from the actual pinned integration tree. Accepted
base changes are checked against affected protected prerequisites instead of blanket
whole-tree equality. A reusable scratch fetch/merge/check gate observes both remote
pins again; publication needs its own final re-read and separate authorization.

**Rationale**: a synthetic merge includes legitimate base work; treating all of it as
task scope wrongly rejects unrelated updates. Conversely, accepting any merge tree
without independently checking the head can hide unauthorized proposal changes.
Content retention needs guard-proved semantics, while exact-candidate review stays
revision-bound. The current Feature 003 comparator/context API is unavailable; the
adapter must report that limitation instead of implementing a second guard.

**Alternatives considered**: whole repository digest as approval identity (invalidates
unrelated base changes); automatic pin refresh (hides drift); proposal-owned checker
(self-validation); a final ref read presented as atomic publication (unsupported).
