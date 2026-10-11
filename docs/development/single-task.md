# Bounded Spec Kit task entry point

Status: local CLI and integration tests implemented; live Symphony unverified.
The generic `speckit-implement` runs a feature by phases. For an approved single-task
issue use `scripts/single_task.py` instead, while retaining the existing Spec Kit
spec, plan, tasks and read-only analysis as authority.

## Entry and execution
Verify owner approvals, reviewed revisions, dependencies, exact permitted paths and
acceptance commands first, as required by [Symphony policy](symphony.md). This CLI
does not authenticate reviewers or parse issue authorization. It checks an explicitly
supplied local scope contract. Commands must come from the reviewed task acceptance;
argv arrays avoid shell interpolation but do not make an untrusted executable safe.

From a clean, isolated per-issue checkout, select the feature and begin. Example for
Feature 002 T001 after its approvals and other dispatch prerequisites are satisfied:

```sh
python3 scripts/select_feature.py 002-symphony-doc-smoke
python3 scripts/single_task.py begin \
  --feature 002-symphony-doc-smoke --task T001 \
  --allow docs/development/symphony-smoke-test.md \
  --allow docs/development/symphony.md \
  --check '["make", "check"]' \
  --check '["git", "diff", "--check"]'
```

Read the artifacts and implement only T001. Do not run the generic feature-wide
implementation skill. Manually inspect semantic restrictions too: for example,
Feature 002 permits only a link addition in the existing runbook. Exact-file scope
alone cannot enforce that wording constraint. Record this review in the workpad.

```sh
python3 scripts/single_task.py verify
python3 scripts/single_task.py complete
```

`verify` checks all tracked changes since the baseline commit, staged/unstaged
changes and nonignored untracked files, before and after running the approved checks.
Only exact allowed files may change. Spec/plan are protected; tasks.md may differ only
in the selected checkbox. Other task descriptions, order and checkbox states must
stay unchanged. Path traversal and changed symlinks are rejected. `complete` reruns
acceptance, records file hashes and marks only that checkbox. Commit the resulting
implementation and checkbox, capture evidence, then perform the final tracker handoff.

## Retry and failure
State and check outcomes live in ignored `.symphony/single-task.json`. Preserve this
file with the checkout on retries; it contains the baseline, commands, task, scope
and hashes, never tokens. Existing state cannot be overwritten by `begin`.
For completed tasks run `verify` and resume handoff only. File hashes must still
match even after implementation is committed. A crash between checkbox update and
state-phase update is recoverable using the recorded pre-checkbox acceptance hashes.
Failed checks never mark completion. Missing evidence, changed completed content,
out-of-scope changes or lost state require operator investigation/authorized rework;
do not delete state, widen scope or reconstruct a baseline to bypass a failure.
Owner-authorized rework uses a fresh reviewed checkout and corrective task, retaining
old evidence. No automatic rollback, reset, tracker write or subsequent-task dispatch
is performed. Stop on failure and keep evidence.

## Verification and limits
`python3 -m unittest discover -s tests -p test_single_task.py -v` exercises the CLI in
isolated Git repositories. `make check` includes those tests. Local evidence covers
completion, interruption recovery, committed/untracked scope violations, other-task
mutation, failed checks, early completion and unsafe paths/symlinks.

This is a post-change guard, not a filesystem sandbox: it cannot prevent arbitrary
code execution or irreversible effects before verification. Ignored files, external
side effects, semantic correctness and owner identity remain outside its checks.
State is local and mutable, not cryptographically authenticated. Maintain Symphony's
sandbox, trust boundaries and human reviews. Live dispatch and interrupted-handoff
behavior must still pass the operator smoke test before unattended operation.
