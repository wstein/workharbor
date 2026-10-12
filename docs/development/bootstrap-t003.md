# T003 preparation: direct state acceptance

Status: owner-authorized dispatch preparation. The owner explicitly instructed
accepting/merging T002, publishing this preparation, approving its proposed interface
and twelve green checks, then pinning and queueing only Issue 4. T002 was accepted
in [PR 31](https://github.com/wstein/workharbor-nxt/pull/31) and rebase-merged as
`029a4ec83e1240b40f72eed004f513ff8fb8b5b0`; its two blobs match original
implementation `ac8bcb6f4d7c23682c0a66d7c2d7a31fd9dcb4f2`. Exact prerequisite
evidence, scope, verifier hashes and argv are in
[bootstrap-t003.json](bootstrap-t003.json). This preparation implements no state
primitives. Queue only after publication, isolated entry verification and a
revision-bound tracker approval recording the owner's delegation.

## Acceptance gap and correction

T002's ten tests exercise the future public CLI. Initially five stop at unsupported
begin and the others at unsupported status/structured output. Implementing only
`scripts/single_task_state.py` cannot make those CLI cases green: CLI integration
is later work. A passing T002 expected-red verifier therefore cannot establish
that T003 delivers locking, transactions or immutable state.

[verify_t003_state.py](../../scripts/verify_t003_state.py) supplies twelve direct
green tests of the actual state module, independent of the public CLI. Its test
code is committed before task entry and is outside the implementation allowlist.
Missing module/API, setup/import errors, skipped cases and expected failures block.
The existing T002 expected-red verifier is a separate regression gate; its ten
remaining CLI failures must be recorded as red, not called implemented behavior.

## Proposed bounded implementation

- Feature: `003-single-task-entry`; task: T003; issue: 4.
- Prerequisite: T002 / Issue 3 only, with accepted test content, implementation
  commit, checked marker and exact expected-red verifier results/raw outcomes.
  Issue labels or a checked box alone are insufficient.
- Allowed implementation: `scripts/single_task_state.py` only.
- Additional completion permission: only T003's checkbox after all acceptance.
- No changes to CLI integration, test files/verifiers, prior features, spec/plan
  semantics, other task bytes, WORKFLOW.md or vendored skills.
- Branch: `issue-4-t003-state`; preserve retries and use operator-assisted Git
  handoff as for T002. No automatic merge or successor dispatch.

T002's ten bodies and evidence were inspected and accepted. Both corrected Linux
CI runs execute 41 tests with exactly ten expected failures and no errors; all 31
baseline tests pass. Raw full-suite CI remains red, as approved. The owner has now
authorized the interface and checks below for T003; record that delegation with
the final execution SHA in Issue 4 before applying its ready label.

## Proposed primitive interface for review

The standalone verifier imports the real module and expects:

```python
with open_store(worktree_root) as store:
    store.create_attempt(attempt_id=..., context=..., baseline=...,
                         tasks_bytes=..., entry_mode=..., predecessor=None)
    attempt = store.read_attempt(attempt_id)
    events = store.read_events(attempt_id)
    store.append_event(attempt_id, kind, payload, progress=None)
    with store.transaction():
        ...
```

`open_store` owns the nonblocking POSIX lock for its entire context lifetime,
including reads. Exceptions are `StateConflict` (lock or active-attempt conflict),
`InvalidState` (unsafe/corrupt/unsupported/legacy evidence) and `PersistenceFailure`
(SQLite transaction or durable-write failure). SQLite errors inside transaction
contexts roll back and translate to PersistenceFailure; nested append operations
must not commit through the caller's transaction.

The proposed layout is `.symphony/single-task/{lock,journal.sqlite3,contexts/}`;
the SQLite schema uses `PRAGMA user_version = 1`. Reject unsafe path components,
symlinked context copies and existing legacy `.symphony/single-task.json`; preserve
original evidence instead of adopting/resetting it. No migration is authorized.

`read_attempt` returns copied values for attempt_id, context, baseline, tasks_bytes
(raw bytes), entry_mode, predecessor, created_at, progress and
context_copy_sha256 (SHA-256 of the canonical copied context, including its existing
approval digest). This copy digest is distinct from TaskContext's approval digest,
which excludes approval.context_digest. Contexts persist as immutable JSON copies.
The initial progress is active. A second active attempt is rejected without
changing the first. Caller mutation cannot change saved input.

`read_events` returns copied, ordered event records with attempt_id, sequence,
kind, payload_digest and created_at. Append increases sequence by one and retains
all prior events byte-for-byte; progress and event append share a transaction.
Payload digests use the feature's canonical UTF-8 JSON. This API exposes no event
update/delete operation. It does not claim protection from an operator editing
SQLite directly or authenticate owner identity.

These implementation interface names/layout details are proposed acceptance
choices, not a claim that the feature specification already mandates them. Review
them before approval; do not silently adjust the verifier during implementation.

## Twelve required green cases

All methods belong to `StatePrimitiveTests` in the standalone verifier:

| Method | Required observation |
| --- | --- |
| `test_durable_immutable_attempt` | Reopen preserves context/baseline/raw task bytes/mode/predecessor/time and context digest; caller/returned-copy mutation has no effect. |
| `test_one_active_attempt` | Second active creation conflicts and retains existing events/progress. |
| `test_cross_process_lock_and_release` | Parent-held lock causes prompt child conflict; child succeeds after release, with a finite timeout. |
| `test_lock_released_after_holder_exit` | Pipe-synchronized holder is killed; lock can be reacquired without sleeps or stale ownership. |
| `test_append_only_events_and_progress` | Monotonic sequence, canonical payload digest, immutable old records and persisted progress. |
| `test_transaction_failure_rolls_back` | Injected SQLite error after event/progress mutation rolls both back, including after reopen. |
| `test_corrupt_journal_retained` | Corrupt database is rejected and its bytes retained. |
| `test_unsupported_version_retained` | Unsupported schema version is rejected without rewriting database bytes. |
| `test_legacy_evidence_retained` | Legacy state is retained; no new store is created to silently adopt it. |
| `test_unsafe_state_path_rejected` | Symlinked state path is rejected before any write outside the worktree. |
| `test_unsafe_context_copy_rejected` | Symlinked immutable copy is rejected; external target stays unchanged. |
| `test_durability_failure_blocks` | Failed fsync produces persistence failure, never successful attempt creation. |

All cases use T001's real disposable Git fixtures and preserve their task bytes.
These checks exercise the module itself, not a replacement backend or mocked
success path. SQLite rollback and fsync failure are deliberate fault injections.

## Proposed exact acceptance argv

Repository root, exit zero for all four commands, in order:

```json
[
  ["python3", "scripts/verify_t003_state.py"],
  ["python3", "scripts/verify_t002_red.py"],
  ["python3", "scripts/check_repo.py"],
  ["git", "diff", "--check"]
]
```

The direct module suite must run all twelve cases green. The second gate keeps all
31 baseline tests green and requires exactly the ten approved remaining CLI reds.
An unexpected green requires reviewing the matrix, not forcing failure. Record
raw state-suite/full-suite exits separately. Full-suite green remains mandatory
at the US3/final checkpoint; this scoped proposal is not a general green waiver.

## Dispatch verification

1. Review and accept T002's test bodies, exact red evidence and implementation
   commit; verify local prerequisite blobs and the completion marker.
2. Approve the T003 primitive API/layout and green suite above, retained planning
   artifact revisions, expected remaining CLI reds and operator handoff.
3. Commit/publish the final contract before entry, pin an execution revision that
   includes accepted T002, and verify the legacy guard in a disposable checkout.
4. Update Issue 4 with revision-bound owner approval and matching prerequisite
   evidence. Only then render its workflow and explicitly queue that issue.

Current preparation checks: source syntax and repository bootstrap remain testable;
the direct verifier must currently fail because the state module is absent. That
failure is a correct pre-implementation result, not passed T003 acceptance or a
live dispatch result. The initial ten CLI reds are the approved baseline described
above; they do not authorize changing CLI integration during T003.

## Exact legacy entry

After verifying the published execution pin, owner approval, prerequisite evidence
and a clean `issue-4-t003-state` branch, select Feature 003 and use this repository
snippet. The real issue uses the existing legacy guard; the proposed Python store
API above is exercised only by acceptance in disposable repositories.

```sh
python3 scripts/select_feature.py 003-single-task-entry
python3 - <<'PY'
import hashlib, json, subprocess
from pathlib import Path
c = json.loads(Path('docs/development/bootstrap-t003.json').read_text())
assert c['feature'] == '003-single-task-entry' and c['task'] == 'T003'
assert c['dependencies'] == ['T002']
subprocess.run(['git', 'merge-base', '--is-ancestor', c['minimum_baseline'], 'HEAD'], check=True)
for path, field in [('scripts/single_task.py', 'guard_sha256'),
                    ('scripts/verify_t003_state.py', 'primitive_verifier_sha256'),
                    ('scripts/verify_t002_red.py', 'red_verifier_sha256')]:
    assert hashlib.sha256(Path(path).read_bytes()).hexdigest() == c[field]
argv = ['python3', 'scripts/single_task.py', 'begin', '--feature', c['feature'], '--task', c['task']]
for path in c['allowed_paths']:
    argv += ['--allow', path]
for check in c['checks']:
    argv += ['--check', json.dumps(check)]
subprocess.run(argv, check=True)
PY
```

Use legacy verify/complete against the saved real-issue state; preserve retries.
Only the state module and T003 checkbox may change. Preserve original T002 evidence
and the reviewed verifier bytes. All four commands must exit zero at T003 completion;
the twelve direct green tests cannot be substituted by matching CLI reds.
