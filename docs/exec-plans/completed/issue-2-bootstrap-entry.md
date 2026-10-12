# Issue 2 / T001 bootstrap entry preparation

Scope: prepare and verify a fixed legacy-guard entry for Feature 003 T001 only.
This completed record covers entry preparation, not T001 or Feature 003 completion.

## Authorization and contract

Owner session instruction: “Bootstrap-Einstieg für genau T001 / Issue #2 verifizieren;
exakte Pfade und Check-Argumente freigeben.” This authorizes the preparation and
bounded path/check contract; no real spec/plan approval or live-dispatch result is
inferred. The agent must not create an owner-authenticated approval by writing its
own GitHub comment/body. Required tracker/PR owner review remains a dispatch gate.

Baseline inspected: `d04f711e7b43f67bd6495cc4fae5ad40e73bab4c`, including guard
correction `95b5bed`. Guard SHA-256:
`7c50b5170e31eed9c5c0dbeb047919c8845f6c266a32def55868a4ab9b863bc4`.
Preparation branch: `chore/issue-2-bootstrap-entry`.

[Exact JSON contract](../../development/bootstrap-t001.json) fixes seven files,
three ordered checks, root cwd and expected exit zero. The
[operator instructions](../../development/bootstrap-t001.md) define six named,
green-only T001 acceptance cases and the clean-entry preflight. These documents and
the replay script are preparation artifacts, outside the subsequent T001 allowlist;
commit them before taking that task's baseline. No task has been checked complete.

## Local entry evidence

Host observed: macOS/Darwin, Python 3.15.0, Git 2.56.0. No Linux run or minimum
Python-version compatibility result is claimed.

Command: `PYTHONDONTWRITEBYTECODE=1 python3 docs/exec-plans/completed/t001_bootstrap_verification.py -v`.
Exit 0; five isolated tests passed against disposable copies of the actual current
Feature 003 spec/plan/tasks and the current guard:

1. Begin stores exactly T001 and the JSON paths/argv without executing checks or
   modifying any task/artifact; checkout remains clean.
2. Missing T001 fixtures reject completion and leave the real task bytes untouched.
3. Present placeholder fixtures without the named tests cannot satisfy acceptance.
4. An extra path rejects completion before acceptance.
5. Existing state rejects re-entry and remains byte-identical.

The placeholder test exercises only negative preflight. No synthetic successful
T001 implementation or fixture acceptance is claimed. The parent environment is not
modified, test Git config/hooks/signing are isolated, and credential values are
neither copied into test environments nor printed.

The exact documented entry snippet was also run from a fresh offline local clone
of preparation commit `80136a71e9ce1d257b6b99c75894e78c27c718ab`, on branch
`issue-2-t001-fixtures`, after feature selection. The baseline/guard/branch/clean
preflight passed, begin exited 0 and saved exactly seven allowed paths and three
checks. No checks ran at entry, no task bytes changed and Git status remained clean.
A subsequent complete exited 1 with the expected missing-fixture blocker, leaving
tasks unchanged. The clone and state were temporary and removed afterwards.
This verifies the actual invocation at a committed revision, beyond the five
isolated entry tests; no successful T001 acceptance or live execution is claimed.

Repository checks: `PYTHONDONTWRITEBYTECODE=1 make check` passed with 24 tests;
`git diff --cached --check` passed. Initial make run during preparation exposed that the
harness test copies tracked files only: the newly linked untracked document was
absent in that temporary copy. Stage the complete preparation artifacts before
rerunning; no harness assertion is weakened.

## Remaining gates

T001 remains pending; fixture files and six acceptance tests still need implementation.
Obtain owner spec/plan review evidence for the selected artifact revisions before
dispatch. Make the committed preparation available in the execution checkout and
bind its actual clean HEAD. No push, ready label, PR, live tracker dispatch, Symphony
startup, merge or deployment is part of this verification. Preserve the existing
guard limits: no authenticated approval validation, concurrent lock/journal or
complete Feature 003 contract. Use one operator/agent per isolated checkout.

## Follow-up: published preparation and pinned workspace creation

The original publication/approval gates above were subsequently satisfied:
the preparation branch is published at `ac7aa0c4b50b533686c025d7231703bb5a35fd7a`,
and the repository owner approved spec/plan revision `d04f711` and the exact T001
contract in [Issue 2's owner comment](https://github.com/wstein/workharbor-nxt/issues/2#issuecomment-6115295473).
T001 is still pending. GitHub main remains at `d04f711`; cloning its default HEAD
would omit the approved preparation.

The renderer now requires `WORKHARBOR_REPO_REVISION` as a full commit SHA and emits
clone-without-checkout, branch creation at that SHA and an exact HEAD assertion.
Optional operator feature/branch settings select the bounded T001 entry. A real
temporary-Git regression test checks a pinned commit different from default HEAD;
invalid revisions fail rendering. No existing checkout is reset or silently updated.
Local `make check` passes all 25 tests; `git diff --check` passes.

`WORKFLOW.local.md` was regenerated from the current template with revision
`ac7aa0c4b50b533686c025d7231703bb5a35fd7a`, feature `003-single-task-entry` and
branch `issue-2-t001-fixtures`. This replaced the stale prompt and Feature 001
selection hook. The token remains an environment reference, not a stored value.

The configured workspace root `/Users/werner/workharbor-issue-workspaces` was
created. The actual newly rendered after_create hook was executed in a temporary
child of that root against GitHub. It exited zero and produced the exact pin,
branch and selected feature, with clean Git status. Five bootstrap tests and all
24 repository tests at the pinned revision passed, as did `git diff --check`.
No real task state or checkbox was created/modified. The temporary child was removed
after verification; the configured root remains. This is workspace-hook verification,
not a Symphony dispatch or completed T001 acceptance.

No ready label, agent startup, merge, push or deployment was performed. The renderer
fix and this record require separate publication if they must be recreated on another
host; the approved T001 checkout continues to use its published preparation pin.

## Live startup blocker: Codex approval-policy protocol

The first GH-2 attempts reached the correct clean `ac7aa0c` checkout and branch,
but failed before an agent session with JSON-RPC code `-32600`: unknown approval
variant `reject`. Installed Codex CLI 0.162.1 accepts `granular` instead. Its generated
ThreadStartParams/TurnStartParams schemas and the official configuration reference
confirm category booleans control whether prompts are allowed. The workflow now
uses all five granular categories set to false, preserving auto-denial rather than
loosening approvals; workspace-write sandboxing remains unchanged. The rendered
local workflow was regenerated with the same approved pin, branch and feature.

A direct local stdio app-server probe reproduced rejection of the old policy and
accepted the corrected policy with `thread/start`, returning a thread identity.
The probe used a temporary CODEX_HOME/cwd and started no model turn. This verifies
protocol acceptance, not successful T001 execution. The renderer regression checks
the exact five false categories; `make check` passes 25 tests and diff check passes.
Existing GH-2 checkout/state was not reset or modified by the repair. Live retry
and task handoff still require runtime evidence.

At `2026-10-12T02:13:16+02:00`, the existing Symphony process picked up the rendered
fix on GH-2 attempt 4 without restart. Its log recorded `thread/settings/updated`
with all five granular categories false and then `Codex session started` for GH-2,
thread `01a12d74-df99-77b0-8140-891a4455217d`. The previous protocol startup failure
is resolved in the live process. This establishes session startup only; T001
acceptance, normal handoff and interrupted-handoff recovery are not yet verified.

## T001 acceptance and operator-assisted commit recovery

Attempt 4 implemented the seven approved T001 paths and completed only its checkbox.
It passed acceptance but could not create `.git/index.lock`; escalation was denied
by the configured policy. The issue was handed off as blocked, preserving its
checkout, handoff-only state and original evidence. No sandbox relaxation occurred.

On the owner's subsequent repair request, the operator independently reran the
three approved checks and verified staged paths, content, type and mode against
sealed evidence, protected feature artifacts and the original test class. The
eight-file implementation was committed on `issue-2-t001-fixtures` as
`962129de1acd43b4db8502bd9125fab5918edb0e`
(`test(speckit): add isolated single-task fixtures for T001`). The source checkout
does not contain that implementation; it remains in the preserved issue checkout.

Post-commit `single_task.py verify` passed the exact three ordered checks: all six
named fixture tests, bootstrap validation with all 30 repository tests, and diff
validation. The issue checkout is clean and retains handoff-only state. Original
`.symphony/issue-2-t001-evidence.json` remains byte-for-byte intact, SHA-256
`7c87697e3a8428d90906bd959b27db419260e6e016aff84740d0d2aedd665342`.
The appended `.symphony/issue-2-operator-handoff.json` binds the implementation
commit, post-commit results and original evidence; SHA-256
`ed186882f51300de0c79d76a90d8bc92eeced6a006ab8c3a61665e600f7846df`.

No push, PR, merge, release or deployment was performed. This proves local T001
acceptance and operator-assisted recovery of an interrupted Git handoff. It does
not prove an unattended commit/PR/label handoff, a normal end-to-end Symphony run,
or readiness for T002. Keep the issue out of dispatch while publication and owner
review remain outstanding.
