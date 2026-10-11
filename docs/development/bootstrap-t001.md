# Bootstrap entry for Feature 003 T001 / GitHub issue 2

Status: locally verified entry preparation, not T001 implementation or live dispatch.
The owner's session request authorizes preparing/verifying this bootstrap and fixing
its exact scope/checks. It supplies no tracker-authenticated spec/plan review and
does not request a ready label, task implementation, push or Symphony startup.

## Fixed contract

[bootstrap-t001.json](bootstrap-t001.json) is the exact local contract. It selects
only `003-single-task-entry` / `T001`, with explicit empty prerequisites. Execute
from a clean, isolated branch `issue-2-t001-fixtures` after this preparation is
committed and available in that checkout. The execution baseline must include
`d04f711e7b43f67bd6495cc4fae5ad40e73bab4c` (including the guard correction
`95b5bed`); the guard SHA-256 is fixed in the contract. Review current Feature 003
spec/plan/tasks at that revision; the original issue source pin is not evidence of
approval for later artifact changes. Capture the actual clean HEAD at entry.

Exactly these implementation files are permitted:

- `tests/fixtures/single-task/spec.md`
- `tests/fixtures/single-task/plan.md`
- `tests/fixtures/single-task/tasks.md`
- `tests/fixtures/single-task/context.json`
- `tests/fixtures/single-task/prerequisite.json`
- `tests/fixtures/single-task/review.json`
- `tests/test_single_task.py`

The six fixture files are inert synthetic templates, not real approval or prior
completion evidence. Generate commits, hashes and linked records in disposable
local repositories at runtime. No literal real credentials, live API calls or
mutation of Features 001/002. Keep existing test assertions intact. No new production
CLI modules, journal implementation or subsequent task work is authorized.
The selected T001 checkbox has the guard's separate completion-only permission;
tasks.md is not an implementation allowlist entry. Every other task byte is protected.

## Exact checks and T001 acceptance

The contract contains three ordered argv arrays, cwd repository root, expected exit
zero. First, `python3 -c` runs the exact code stored in `checks[0][2]`: require all
six regular, unsymlinked fixture files and run these six methods directly from
`tests/test_single_task.py` class `SingleTaskFixtureTests`:

| Method | Required observation |
| --- | --- |
| `test_two_task_feature` | Synthetic spec/plan/tasks load; exactly two unique tasks and explicit empty/nonempty dependency rows exist. |
| `test_runtime_context_prerequisite_review` | Generate local baseline/implementation commits and current hashes; build linked implement, satisfied-prerequisite and review examples with the planned identities. No static real-owner evidence or assumption that the enhanced CLI exists. |
| `test_git_configuration_hooks_and_signing_are_isolated` | Hostile temporary Git config, a failing hook and signing setting cannot affect helper-created repositories or commits; global/system configuration is isolated. |
| `test_tracker_credentials_are_removed_without_logging` | Sentinel tracker credentials are absent from fixture subprocess environments and captured output/evidence; the parent environment is unchanged. |
| `test_fixture_creation_rejects_network` | Deny/instrument Python socket and network-capable subprocess entry points while exercising fixture creation. A deliberately attempted network operation fails; normal creation uses local Git only. This verifies the helper, not OS-wide network sandboxing. |
| `test_protected_features_remain_unchanged` | Fixture operations touch only disposable repositories; actual Feature 001/002 tree identities and bytes remain unchanged against the entry baseline. |

All six must run and pass. Missing methods/imports, skips, expected failures,
unexpected success and other errors reject the check; passing empty discovery cannot
satisfy it. Test names alone do not prove these semantics: inspect the assertions
and helper implementation before completion.

Second check: `["make", "check"]`. Third: `["git", "diff", "--check"]`.
These are green-test acceptance, not a test-authoring red matrix. Missing fixture
files/tests currently block acceptance as intended; verification of entry is not
evidence that T001 has met acceptance.

## Entry after required owner reviews

Obtain tracker/PR-verifiable owner spec/plan approval and exact bootstrap scope/check
authorization before Symphony dispatch. This session's scope request is recorded
here without impersonating an authenticated GitHub owner approval. Never add ready
automatically. Ensure the preparation commit is present before taking the guard
baseline; adding these preparation documents during the T001 attempt is out of scope.

After creating the isolated branch and selecting Feature 003, run this trusted
repository snippet (it constructs argv, never executes issue shell text):

```sh
python3 scripts/select_feature.py 003-single-task-entry
python3 - <<'PY'
import hashlib
import json
import subprocess
from pathlib import Path

contract = json.loads(Path('docs/development/bootstrap-t001.json').read_text())
assert contract['feature'] == '003-single-task-entry' and contract['task'] == 'T001'
assert contract['dependencies'] == []
subprocess.run(['git', 'merge-base', '--is-ancestor', contract['minimum_baseline'], 'HEAD'], check=True)
assert hashlib.sha256(Path('scripts/single_task.py').read_bytes()).hexdigest() == contract['guard_sha256']
assert subprocess.check_output(['git', 'branch', '--show-current'], text=True).strip() == 'issue-2-t001-fixtures'
assert not subprocess.check_output(['git', 'status', '--porcelain'], text=True).strip()
print('Entry HEAD:', subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip())
argv = ['python3', 'scripts/single_task.py', 'begin', '--feature', contract['feature'], '--task', contract['task']]
for path in contract['allowed_paths']:
    argv.extend(['--allow', path])
for check in contract['checks']:
    argv.extend(['--check', json.dumps(check)])
subprocess.run(argv, check=True)
PY
```

The existing guard validates pending T001, clean entry, paths and argv; it does not
authenticate approvals, enforce semantic review, lock concurrent processes or
implement Feature 003's future journal/TaskContext API. Use one operator/agent for
this checkout. Verify contract bytes against the approved preparation commit;
do not edit scope/checks, reconstruct a baseline or delete state on failure.

Implement T001 only after its gates pass; use the existing `verify` and `complete`
commands. Preserve `.symphony/single-task.json` on retry. After completion, verify
and finish handoff only; failures require operator review, never a new baseline.
Commit only implementation plus T001 checkbox, then record review evidence and any
authorized PR before the final atomic label handoff. No merge or deployment.

## Verification evidence

[Preparation record](../exec-plans/completed/issue-2-bootstrap-entry.md) records
the actual local checks and remaining blockers. Replay the isolated entry tests:

```sh
python3 docs/exec-plans/completed/t001_bootstrap_verification.py -v
```

The replay uses disposable copies of the actual feature artifacts and the current
guard. It does not implement T001, supply owner approvals, start Symphony or change
real task checkboxes. Reverify if the guard/contract changes. This narrowly scoped
bootstrap is not proof of unattended operation or of completed Feature 003.
