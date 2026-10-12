# T002 expected-red bootstrap and operator handoff

This contract authorizes only test authorship for Feature 003 T002 / Issue 3.
It does not implement the journal or expanded CLI. Prerequisite T001 was accepted
in [PR 30](https://github.com/wstein/workharbor-nxt/pull/30), merged by the owner
as `64951c8bb39ba53bede043c70b01c2e01b267f01`. Its reviewed implementation
was `962129de1acd43b4db8502bd9125fab5918edb0e`; the merged eight-file content
matches. [T001's review](https://github.com/wstein/workharbor-nxt/pull/30#pullrequestreview-5485696751)
and [workpad](https://github.com/wstein/workharbor-nxt/issues/2#issuecomment-6115479258)
bind six named passing tests and all 30 tests at its implementation revision.

## Scope and approval

The owner requested the exact red matrix, bounded approval, operator-assisted Git
handoff and readiness for Issue 3 in the current session. Record that delegation
in an owner-authenticated tracker comment binding this preparation's full commit
before dispatch. Compare spec/plan against the previously approved `d04f711` and
tasks against it with checkbox-only normalization; no semantic planning change is
authorized. T001 is checked, T002 is pending, dependency row is `T002 | T001`.

The implementation allowlist contains only `tests/test_single_task_state.py`.
Only T002's completion marker may additionally change, after acceptance. Do not
modify existing tests, scripts, planning prose, other tasks, Feature 001/002 or
WORKFLOW.md. The preparation and verifier must be committed before guard entry.
Use the legacy guard with the SHA-256 in [bootstrap-t002.json](bootstrap-t002.json),
not the planned `begin --context` interface for the real issue checkout.

## Exact collected cases and raw expected outcomes

Author class `SingleTaskStateTests` in `tests/test_single_task_state.py`. All ten
methods below must import and execute; no skips, xfail, missing imports or setup
errors. Reuse T001's disposable fixtures. Build a valid synthetic TaskContext and
invoke the proposed CLI in the disposable repository with bounded timeouts.
Never invoke proposed begin/status on the actual issue checkout.

| Method | Required scenario and assertions after implementation | Initial raw red observation | Successor |
| --- | --- | --- | --- |
| `test_one_lock_owner` | Begin valid context; synchronize an independent lock holder using pipe/barrier, then status exits 5 without altering tasks/events; no timing-only sleeps. | begin exits 2 instead of 0 | T003/T004 |
| `test_lock_released_after_exit` | Begin valid context; holder exits, a fresh process acquires lock and status exits 0; preserve attempt identity and tasks. | begin exits 2 instead of 0 | T003/T004 |
| `test_transaction_rollback` | Begin valid context; inject a SQLite transaction failure in-process, assert persistence exit 6 and unchanged events/progress/tasks; no test-only production switches. | begin exits 2 instead of 0 | T003/T004 |
| `test_append_only_events` | Begin valid context; cause a safe failed operation; previous event bytes remain, sequence increases, progress/events update transactionally. | begin exits 2 instead of 0 | T003/T004 |
| `test_immutable_context` | Begin valid context; change the caller's context, assert retained context bytes/digest/baseline/entry mode stay immutable and mismatched reuse is blocked. | begin exits 2 instead of 0 | T003/T004 |
| `test_corrupt_state_retained` | Write deliberately corrupt journal in disposable state directory; status exits 3, retains corrupt bytes and task bytes. | status exits 2 instead of 3 | T003/T004 |
| `test_unsupported_state_retained` | Seed unsupported persistence version; status exits 3 without deleting/migrating state or tasks. | status exits 2 instead of 3 | T003/T004 |
| `test_legacy_state_retained` | Create actual legacy guard state in disposable fixture; proposed status exits 3 with migration explanation, preserving old evidence and tasks. | status exits 2 instead of 3 | T003/T004 |
| `test_sanitized_structured_error` | Pass malformed synthetic context containing secret sentinels; exit 2 with required structured fields/stable safe code and both identity flags false; neither stream leaks context, argv or sentinel. | structured JSON response absent | T004 |
| `test_invalid_entry_preserves_tasks` | Use valid fixture and unknown selected task; exit 2 with structured blocker; task bytes/index and all prior state remain unchanged. | structured JSON response absent | T004 |

The first five initially fail at the public CLI's missing valid entry, before
their deeper scenario assertions can execute. This is explicit missing-CLI red
evidence, **not evidence that locking/rollback/event invariants were exercised**.
The test bodies must contain the full subsequent scenario assertions for T003/T004;
review those semantics before accepting the authored tests. Do not stop each test
after an unconditional failure, catch import/setup failures as expected red, or
replace missing behavior with a simulated passing backend.

Use `self.fail` only after observing the exact expected discrepancy, with precisely
the corresponding `failure` string in bootstrap-t002.json. This yields raw
AssertionError failures inside the collected methods; no preemptive failure based
solely on a missing module. Preserve raw CLI argv, exit, stdout/stderr and assertion
outcomes locally, with synthetic sentinels redacted from any published evidence.
If behavior is already implemented or a different failure occurs, stop for review
of this matrix; never force the expected red.

## Acceptance and initial diagnosis

The three exact ordered acceptance argv are:

```json
[
  ["python3", "scripts/verify_t002_red.py"],
  ["python3", "scripts/check_repo.py"],
  ["git", "diff", "--check"]
]
```

All run at repository root and must exit zero. The trusted verifier requires
exactly the 31 pinned baseline tests green and ten precise AssertionError outcomes.
It rejects missing/extra/duplicate cases, errors, setup failures, skips, xfail,
unexpected green and baseline regressions. Its zero exit means the reviewed red
matrix matched; `implementation_verified` remains false. Human review of test
semantics remains mandatory; a marker string alone cannot prove a good test.

Also run the raw state suite and `make check` for diagnosis and record their actual
nonzero exits and ten individual red failures. Do not report them as passing.
The T002 verifier supersedes the all-green handoff gate only for this explicitly
approved test-authoring issue; baseline failures never qualify. Successors consume
the content/commit-bound verifier record. US3/final acceptance still require green
full suites. At first entry, before tests exist, `make check` must pass, while the
T002 verifier must reject the missing matrix without marking T002 complete.

## Bounded entry and recovery

From a clean per-issue branch at the approved preparation SHA:

```sh
python3 scripts/select_feature.py 003-single-task-entry
python3 - <<'PY'
import hashlib, json, subprocess
from pathlib import Path
c = json.loads(Path('docs/development/bootstrap-t002.json').read_text())
assert c['feature'] == '003-single-task-entry' and c['task'] == 'T002'
assert c['dependencies'] == ['T001']
assert hashlib.sha256(Path('scripts/single_task.py').read_bytes()).hexdigest() == c['guard_sha256']
argv = ['python3', 'scripts/single_task.py', 'begin', '--feature', c['feature'], '--task', c['task']]
for path in c['allowed_paths']:
    argv += ['--allow', path]
for check in c['checks']:
    argv += ['--check', json.dumps(check)]
subprocess.run(argv, check=True)
PY
```

Use `verify` and `complete` only against that saved real-issue state. Preserve
prior state on retries; one operator/agent per checkout. Keep raw red outcomes
separate from the verifier's passing record and bind both to current file hashes.
Use operator-assisted Git commits: the agent may implement/verify/complete, then
records sealed evidence and hands off as blocked if `.git` writes are denied.
The operator reviews staged paths/index/history/content/types/modes, commits the
test file and only T002's checkbox together, reruns the exact gate and publishes
the review evidence. No sandbox relaxation, state deletion or reimplementation.
No automatic merge or T003 dispatch. The full unattended handoff remains unverified.
