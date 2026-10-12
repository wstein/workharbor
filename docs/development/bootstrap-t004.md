# T004 preparation: shared CLI outcomes and locking

Status: **proposal only; no implementation or dispatch approval**. The owner
requested preparation of T004 / Issue 5. Owner `wstein` accepted and merged T003
in [PR 32](https://github.com/wstein/workharbor-nxt/pull/32) on
2026-10-12 at 01:59:26 UTC as
`a36bf7e1fc9b09bd07226078b7d70c5d299d9324` on the authoritative source branch
`chore/issue-2-bootstrap-entry`. The original preparation base and implementation
seal `586696f7cc1956d430647b918869fbf8caedd660`, above
`74ec3d91cc313aeca4e73919c7fc271ec7f4c0c5`, remain provenance. The merged state
module and task file blobs match that seal exactly; the JSON records both blob
IDs and SHA-256 values. The accepted source tree also matches the reviewed
CI-inclusive source `0fe461ec96538a21a64064b086648fbc43cb57f6` exactly.

[PR 34](https://github.com/wstein/workharbor-nxt/pull/34) remains a draft proposal.
T004 approval and its execution/dispatch baseline remain **pending**. Neither
T003's acceptance nor this preparation authorizes T004 entry, a ready label,
queue, merge or successor task. Existing worktrees and reviewed history remain
preserved; the accepted source was integrated with a normal merge.

The current authoritative source is now
`5d1dc6563c7ae1f8479f01af0901c39792b3f8b8`, owner-merged in
[PR 33](https://github.com/wstein/workharbor-nxt/pull/33). Concurrent published
proposal head `b96421c9915bb0c69835efa8a01cf1654f1bc956` was preserved by
fast-forwarding the existing issue branch. T003's accepted merge remains `a36bf7e`
and its implementation/task seals are unchanged. Issue 5 is open, unlabelled and
has no owner approval comment; publication and "continue" do not approve entry.

[The machine contract](bootstrap-t004.json) freezes exact inputs, case IDs,
outcomes, guard identity and checks. [The executable verifier](../../scripts/verify_t004_cli.py)
exists before entry and is outside the implementation allowlist. It currently
must reject the unimplemented T004 behavior. Its self-test validates the gate,
not the proposed CLI implementation.

## Bounded implementation proposal

Only `scripts/single_task.py` may change, plus **only T004's checkbox** in
`specs/003-single-task-entry/tasks.md` after approved acceptance. Do not change
T003's state API, any tests/verifiers/contracts, planning semantics, other task
bytes, Features 001/002, WORKFLOW.md or vendored skills. The author implements no
context validator, real validated begin/status (T010), new verify/complete
(T017), retry/commit/rework migration (T023), or automatic task execution.
These boundaries retain the authoritative [tasks](../../specs/003-single-task-entry/tasks.md)
and [CLI categories](../../specs/003-single-task-entry/contracts/cli.md).

The proposed T004 parser recognizes all eight new command forms in that CLI
contract. Well-formed new forms return a stable `command_unavailable` blocker,
exit 2, pending their owning tasks. New status also rejects existing
corrupt/unsupported/legacy evidence with exit 3 before a deferred-command result.
No new form creates an attempt, context copy, journal, task marker or acceptance
result merely to satisfy this checkpoint. T010 supplies successful validated
entry/status later. A missing journal with otherwise empty state is a deferred
command, not an implicitly created store.

Proposed output for every form is one JSON object on stdout, schema_version
exactly integer 1, attempt_id/mode/progress present (null when unknown),
local_verification in `not-run`, `passed`, `failed`, both identity/live-dispatch
flags always false, and errors as a list. Each error has a stable code, fixed
safe message and affected_paths/affected_ids lists. Never derive a message from
raw argparse exception text, context, subprocess output, argv or arbitrary
exception strings. New forms have empty stderr, including parse errors. Legacy
failures may additionally print only fixed safe diagnostics needed by the
unchanged regression callers: `Out-of-scope`, `checkbox`, `rework`, `evidence`,
`Unsafe file type`, `clean checkout`. Never echo dynamic path/argv values there.
No successful legacy response may claim new journal or handoff readiness.

| Exit | Stable code and direct evidence |
| --- | --- |
| 0 | Legacy bounded begin/verify succeeds with structured output; no SQLite adoption. |
| 2 | `invalid_arguments` for malformed argv; `command_unavailable` for syntactically valid future forms, with no semantic-entry claim or side effect. |
| 3 | `scope_blocker` for actual legacy out-of-scope change; `state_blocker` for unsafe, mixed, corrupt, unsupported or legacy evidence on a new form. |
| 4 | `acceptance_failed` for an actual failing legacy acceptance subprocess; capture its output and retain only safe failure metadata, never raw output or argv in last_failure. |
| 5 | `concurrent_attempt` while another process holds the canonical worktree lock; no waiting, fallback lock or evidence mutation. |
| 6 | `persistence_failure` when real lock durability fails (in-process fsync fault injection); no raw exception text or success claim. |

These exact code choices and output values are proposed acceptance details,
requiring review and owner approval before entry. They do not change the final
feature contract's categories or imply that deferred commands are implemented.

## Temporary legacy compatibility and lock seam

Keeping all 31 implemented baseline tests requires temporary support for existing
legacy `begin --feature ... --task ... --allow ... --check ...`, bare `verify`
and bare `complete`. This is a proposed lock-only adapter in the T004 CLI, with
expiry and explicit caller migration at T010/T017/T023. It must retain the old
scope/history/checkbox/file-type/rework checks. It cannot convert, adopt or
initialize SQLite from legacy JSON. T003 `open_store` continues rejecting legacy
evidence unchanged. Mixed legacy JSON and a journal/context store is blocked,
with all original bytes retained.

All commands, including new status, legacy forms and deferred commands, use the
same `.symphony/single-task/lock` inode and nonblocking exclusive POSIX flock.
Validate unsymlinked directories and regular single-link lock/evidence files
using descriptor-relative no-follow access; reject unsafe parents/files before
outside writes. A lock-only path may create only safe directories and the lock,
not a database/contexts. Retain existing evidence. Do not remove/replace the
lock inode, follow links, choose a second lock, or bypass T003 validation.
Hold the lock through state/task reads, scope checks, subprocess execution,
post-check validation, publication and response derivation. Release it on every
success/error/exit path. Status must contend even when it would return a blocker.

T003's `open_store` cannot be nested underneath an independently opened lock
descriptor. The author must review this seam explicitly: lock-only preflight can
validate/reject legacy or empty evidence without opening a store; where a real
store validation is needed, release the preflight lock before opening the real
store, and revalidate under its own full-lifetime lock. Do not read or act on
mutable journal/task data across that transition. Another owner in the gap means
exit 5, never fallback. Existing `open_store` validation remains authoritative.
The FIFO-synchronized acceptance overlap case checks the adapter's full command
lifetime; the real T003 holder checks that all command forms use its same lock.

## Exact proposed checkpoint matrix

The T004 gate collects exactly 41 existing cases plus 12 direct cases, with no
missing/extra/duplicate/import-error cases, errors, setup/cleanup failures,
skips, expected failures or unexpected successes. It also pins the existing
test bodies/fixtures, T003 state/verifier and original T002 matrix/verifier by
SHA-256. Verifier discovery and outcome evaluation cannot be overridden by argv.

- All **31 implemented baseline cases stay green** unchanged.
- Five authored T002 cases become required green: corrupt state retained,
  unsupported state retained, legacy state retained, sanitized structured error,
  invalid entry preserves tasks. This is proposed, not measured T004 success.
- The other five authored T002 cases remain collected and red: one lock owner,
  lock released after exit, transaction rollback, append-only events, immutable
  context. Their unchanged helper calls valid `begin --context`, sees the new
  structured exit-2 blocker, then fails exactly **`'usage:' not found in ''`**.
  This is a transitional assertion in their existing helper, not a meaningful
  locking/journal test result and not the old T002 usage-failure string. The
  direct deferred-command case independently requires the precise safe blocker
  and no attempt/acceptance/task mutation. Never add usage output to force red.
- All **12 new direct cases stay green**: legacy success envelope; safe/stable
  argument errors; all deferred forms without entry/execution; no context echo;
  real scope exit 3; actual subprocess exit 4 without echo/persisted raw failure;
  injected durability exit 6; all eight forms plus legacy forms contend with
  actual T003 lock; release after CLI error; unsafe lock paths; mixed-state
  rejection; FIFO-synchronized legacy acceptance versus every new form and
  legacy verify/complete.

The JSON contract gives every exact test ID and expected assertion. The original
ten scenario bodies are preserved, never deleted/skipped/weakened. The first
five deeper scenarios must execute after T010's actual validated begin/status
integration and explicit regression migration. That checkpoint must review the
matrix against observed behavior; an unexpected green or different red blocks
this gate rather than silently revising expectations. T017 and T023 retain their
later completion/retry migration responsibilities. US3/final acceptance requires
the full green repository suite. No dependency waiver is granted here.

Run the four proposed gate commands in the JSON order: new T004 verifier, existing
direct T003 verifier, repository checker, diff check. Each must exit 0 after
approved implementation. Also run raw `make check` using supported Python and
retain its actual exit and every remaining failure separately. At the proposed
T004 checkpoint it remains **exit 2**, with raw unittest exit 1 for those five
retained failures; neither is a passing full suite. Do not run the old T002
expected-red verifier as T004 acceptance: it deliberately rejects the changed
matrix. Its frozen body remains provenance and provides the strict recorder.

## Immutable external guard, entry and recovery order

The implementation changes `scripts/single_task.py`; **never use that mutable file
to guard its own implementation**. Before entry, an operator must bind a reviewed
preparation SHA, extract the guard from immutable commit
`92471ee162599a4360436dec870b9b6ba3aa3c49`, verify SHA-256
`7c50b5170e31eed9c5c0dbeb047919c8845f6c266a32def55868a4ab9b863bc4`, and keep a
regular non-symlink copy outside the implementation worktree, in an operator-owned
private directory with no symlink parents. Recheck that digest before **every**
begin/verify/complete/recovery invocation. Keep a frozen external contract copy
from the same preparation SHA. No invocation follows from preparing these files.

1. Recheck T003's recorded owner merge and the state-module/task-file seals
   against the accepted source revision. Acceptance is recorded at
   `a36bf7e1fc9b09bd07226078b7d70c5d299d9324`; a checked marker alone remains
   insufficient evidence. Record any subsequent prerequisite/source change in
   a new reviewed preparation revision, with its actual CI gate separately.
2. Independently review this exact proposal, then obtain owner approval for its
   bounded matrix/temporary compatibility. Publish/pin only if separately
   authorized. Bind an execution SHA containing accepted prerequisites and the
   approved preparation; no checked box or label substitutes for this evidence.
3. Create/reuse an exclusive clean per-issue implementation worktree on the
   approved execution pin. Verify ancestry, task/dependency bytes, all frozen
   hashes and the verifier hash. Select Feature 003. Extract/verify the external
   guard and contract before entry; verify extraction in a disposable checkout.
4. From the implementation worktree root invoke the **external guard** with
   `begin --feature 003-single-task-entry --task T004 --allow scripts/single_task.py`
   and repeated `--check` arguments, each being the JSON serialization of one
   exact argv array from this contract. This legacy guard owns real-issue JSON
   bootstrap state. The proposed CLI runs only in synthetic fixtures during
   acceptance; its new forms must reject the real legacy state, not adopt it.
5. Implement only the CLI. Verify frozen inputs again, run all four exact gates,
   raw make check and preserve their separate outcomes. Invoke the external
   guard's `verify`, then `complete`; complete reruns the approved checks and
   changes only the T004 marker. Its ignored legacy state and ignored canonical
   lock file do not expand allowed Git changes. Keep preparation files fixed.
6. Before commit, inspect worktree/index/history/content/types/modes with the
   immutable guard and operator procedure. Commit CLI and T004 marker together
   only after approved gates. Rehash external guard/contract and rerun external
   `verify`/`complete` against the saved attempt after commit; preserve state.
   Return exact commit, raw outcomes and sealed evidence for independent review.
   No push, merge, ready label or next-task dispatch follows automatically.

On retry/interruption, reuse that issue branch, external guard/contract and saved
legacy state; do not begin again, delete state, reset tasks or adopt a journal.
If code changes after legacy completion, the immutable guard requires authorized
rework; stop for the established owner procedure, never bypass it by editing
state. Unexpected scope/evidence/guard-hash failure blocks. A lost external copy
is restored only from its pinned Git object and reverified, never from the
mutable CLI. Missing tools or forbidden Git writes remain reported blockers.

## Preparation evidence and open decisions

This proposal retains the original T003 implementation seal and now includes the
verified owner-merged source, without touching either earlier issue's checkout.
Required pre-implementation evidence is the verifier
self-test passing, the new gate rejecting current behavior, direct T003 verifier
passing, original T002 expected-red gate passing, repository check/diff check
passing, and raw make check **failing** with its actual ten existing CLI failures.
These observations are distinct from the proposed post-T004 five-red matrix.
Supported local Python 3.15 must be first on PATH for subprocess regression
callers as well as Make's interpreter; unavailable/unsupported Python is a
blocker, not passing acceptance. Exact measured results accompany the commit
handoff; no implementation result is inferred from this preparation.

Open owner decisions: approve this exact T004 contract and matrix; approve
temporary legacy lock-only support
with its staged expiry; bind/publish an execution pin and entry authorization.
Independent review remains required. No current preparation approval is a task
implementation, live dispatch or authenticated owner
identity claim.

Measured preparation results on Python 3.15, at the pinned T003 base plus these
three artifacts: verifier self-test/syntax exit 0; T003 direct suite exit 0
(12/12); original T002 gate exit 0 (31 pass, ten exact reds); new T004 gate exit 1
(53 collected; 54 outcome records: 31 pass, 13 assertions, six errors and four
failed-subtest parent records, including a duplicate parent that is rejected).
The new gate's rejection is expected before implementation, including
absent JSON responses; no error/failed subtest is accepted as a retained red.
Repository checker and staged/unstaged diff checks exit 0. Required raw make
check exits 2 (41 tests, ten failures, zero errors), preserving every original
T002 red. Raw logs remain separate from the machine acceptance report.

A disposable external-guard smoke also passed: the immutable verified guard
completed only a synthetic selected task while the mutable synthetic CLI would
exit 98, tolerated the ignored canonical lock, and verified/completed again after
the synthetic commit. This verifies the proposed guard placement/recovery order,
not actual T004 entry. PR32's subsequent CI-only commit
`0fe461ec96538a21a64064b086648fbc43cb57f6` preserves the T003 implementation
seal. Its temporary gate intentionally expires on these preparation additions
and requires strict full green; a proposal draft therefore retains red CI. No
extension of that gate or dependency waiver is included here.

The owner-merge refresh changes prerequisite facts and the verifier's reported
prerequisite status only; its 53-case matrix and implementation behavior remain
unchanged. All frozen acceptance input hashes remain valid at the accepted
source. The original full-suite results above remain applicable because the
accepted source tree is byte-identical to the reviewed CI-inclusive tree.
Refresh verification repeats contract/hash/inventory checks, verifier self-test,
syntax, repository/diff checks and the expected-reject T004 gate; it does not
report raw red checks as passing. The same independent reviewer must clear the
updated exact revision before publication; T004 approval and the dispatch pin
are still pending.

## Current CI diagnosis and approval handoff

Both actual PR34 runs ([38194960688](https://github.com/wstein/workharbor-nxt/actions/runs/38194960688)
and [38194959152](https://github.com/wstein/workharbor-nxt/actions/runs/38194959152))
selected strict full green, passed all twelve T003 state cases and repository
checks, then failed the unchanged 41-case suite with the ten known T002 CLI
failures: raw make exit 2, no import/setup/platform error. The preparation files
expire the old T003-only gate. This is a missing preparation-validation CI phase,
not a missing delivered T004 implementation or permission to waive its tests.

A separate, reviewed CI-only commit may validate exactly this immutable
three-artifact preparation seal, unchanged legacy CLI/task/state/tests and
accepted source. Its result must identify preparation validation, run the
verifier self-test plus T003's twelve greens and T002's exact 31-green/ten-red
gate, and require the current T004 verifier's precise known rejection rather
than accepting any nonzero result. Its expected preimplementation errors are
never permitted in T004 delivered-behavior acceptance. Any CLI, task marker,
test, prep artifact or other source change expires that phase and uses the
existing strict path. No T004 runtime acceptance command or matrix changes.
CI helper/harness changes require their own focused commit and independent
review; a checker cannot authenticate its own approval or hash itself circularly.

The proposed execution pin is the final independently reviewed, published PR34
head, named by exact SHA in the external approval handoff after these commits.
It contains this contract and the accepted source; recording its own SHA inside
its content would be circular. `dispatch_baseline` remains null until an owner
approves that exact pin. The implementation allowlist is only
`scripts/single_task.py`; only T004's selected completion marker may additionally
change. Use the four ordered argv arrays in the JSON contract unchanged, and
the externally copied guard pinned to SHA-256 `7c50b517…b863bc4`, never the mutable
CLI. Owner approval must cover this exact pin, matrix/temporary compatibility
and legacy bootstrap entry. Queue/merge remain separate owner actions; no new
feature, backlog fork or implicit task selection is proposed.


## Reversible proposal history reconstruction

GitHub reported PR34 mergeable but not rebaseable at published head `b96421c`.
A disposable standard rebase reproduced conflicts in the harness and CI helper
while replaying already accepted T003 CI commit `0fe461e`; the probe was aborted.
The original published head is preserved on local backup branch
`backup/5-t004-remote-b96421c`, and the tested CI candidate
`da9968c88313a80f7a04b59148b3b4b5fa24b94a` remains on the original issue branch.

The same existing proposal is reconstructed with only its preparation and CI
commits directly above accepted source `5d1dc656…`. Task state, original scenario
bodies, the 53-case matrix, guard identity, pending approvals and source content
are unchanged. The separate CI commit updates only its immutable preparation
anchor and artifact hashes to this reconstructed seal. Independent review and
actual preparation validation cover the final exact revision before replacing
the existing PR34 branch with a lease against its freshly observed predecessor.
No new feature, issue, task implementation, ready label or merge is authorized.
The scoped preparation phase intentionally expires on any later commit; a later
owner merge or execution baseline requires fresh pin validation rather than
carrying this preparation result into T004 implementation acceptance.
