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
