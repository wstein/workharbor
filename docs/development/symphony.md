# Symphony operator runbook

Status: prepared, not activated. No tracker, remote or credentials are supplied.

## Compatibility
The workflow targets OpenAI Symphony's Elixir reference implementation at
`be10a1b79df723d6d7612b5651c8522704dafb2e`. Its adapter uses `tracker.provider`;
older examples with flat settings may differ. JSON front matter is valid YAML and
lets bootstrap validation use Python's standard library. The reference runtime at
this commit declares Erlang 28 and Elixir 1.19.5-otp-28. Consult upstream's README
for installation; these versions are Symphony tooling, not WorkHarbor product pins.
The pinned runtime includes a GitHub Issues adapter. No extra adapter or GitHub
Projects board is required. Tracker choice concerns development only.

## Operator activation
Install the pinned Symphony separately, install/authenticate Codex CLI, and inspect
its app-server JSON schema for the workflow approval and sandbox policy. Use GitHub
Issues in the operator-selected repository. Only open issues with
`workharbor-ready` are eligible. Create the `workharbor-ready`, `human-review` and
`blocked` labels before activation. Review and label issues
explicitly. Configure environment variables in your private service environment:

- WORKHARBOR_REPO_URL: trusted remote URL or absolute local source checkout path.
- WORKHARBOR_REPO_REVISION: required full lowercase commit SHA approved for execution;
  workspace creation checks out this exact commit instead of the remote default branch.
- WORKHARBOR_FEATURE: optional existing feature directory selected after checkout;
  otherwise the agent selects the issue's validated feature before implementation.
- WORKHARBOR_ISSUE_BRANCH: optional fixed branch for a single-issue bootstrap;
  otherwise creation uses `work/<workspace-key>`. Do not use a single-issue setting
  for unrelated dispatches.
- WORKHARBOR_WORKSPACE_ROOT: absolute dedicated directory outside the source checkout.
- WORKHARBOR_GITHUB_REPO: `owner/repository`, for example `wstein/workharbor-nxt`.
- GITHUB_TOKEN: host-side token with issue read/write access to that repository;
  never commit or place in issue workspaces.

Run `python3 scripts/prepare_symphony.py` to render `WORKFLOW.local.md`. The script
fails on missing settings or Codex and does not start anything. It preserves only
the secret reference `$GITHUB_TOKEN`, not its value. Keep repository URLs free of
embedded credentials.

For the approved Issue #2 bootstrap, render with these operator-controlled settings:

```sh
export WORKHARBOR_REPO_REVISION=ac7aa0c4b50b533686c025d7231703bb5a35fd7a
export WORKHARBOR_FEATURE=003-single-task-entry
export WORKHARBOR_ISSUE_BRANCH=issue-2-t001-fixtures
python3 scripts/prepare_symphony.py
```

This pin contains the approved preparation commit `80136a7`. The generated hook
creates a branch at the exact pin, asserts HEAD and selects Feature 003; render
again when the approved execution revision changes. Re-rendering also replaces
stale local prompt/hook text from the current repository template. It does not
update an existing issue checkout: inspect its revision/state separately, retain
retry evidence and stop for operator recovery if it does not match. Never reset
existing work to retrofit a new pin. Verifying this hook locally does not prove
live Symphony dispatch. After preparation, start from the Symphony directory:

```sh
mise exec -- ./bin/symphony /absolute/path/to/workharbor/WORKFLOW.local.md
```

Follow the pinned upstream build procedure before that command. Confirm the host
implementation scrubs tracker secret environment variables from Codex children and
provides its tracker tools. Start with a harmless documentation issue and inspect
checkout path, logs, commits and label-based review handoff before product work. This
bootstrap cannot demonstrate live dispatch without your tracker and authenticated
runtime. No daemon has been started and no external messages have been sent.

## Trust and recovery
Hooks clone only the operator-controlled repo into a fresh per-issue directory.
Every attempt retains its branch and changes on retry. Pre-run bootstrap checks
run without blocking repairs. Symphony may discard successful hook output, so
the agent runs `make check` itself at the beginning of each attempt and records the
results in its workpad. Passing checks remain mandatory at handoff. Invalid checkout/branch prerequisites still stop the hook.
Concurrency is capped at two. Workspace-write policies are not a complete hostile
code security boundary; run Symphony under a dedicated development account/host,
without production host-agent credentials. Before handoff, agents finish checks, commits, any authorized PR and tracker
evidence. The final tracker action replaces the issue's label set in one GitHub API
operation: preserve unrelated labels, remove `workharbor-ready`, and add
`human-review` (or `blocked` with a recorded explanation). The issue stays open.
Reconciliation can stop the agent immediately after the ready label disappears;
no remaining work may depend on that agent continuing. If the available tracker
tool cannot perform this replacement or the write fails, the handoff is incomplete
and an operator performs the final switch. An owner explicitly restores the ready
label to resume work. Merges and
releases are owner actions. Stop the service to pause dispatch; preserve failed
checkouts for diagnosis. Do not automatically delete evidence or data.

## Spec Kit and bounded implementation issues
Symphony dispatches issues; it does not automatically execute the Spec Kit workflow.
For a new feature, run `$speckit-specify`, optionally `$speckit-clarify`, then obtain
human spec review. Run `$speckit-plan` and obtain separate human plan review before
`$speckit-tasks` and `$speckit-analyze`. The repository's Spec Kit workflow contains
these human gates; a ready label does not replace them.

Feature 001 already has spec, plan and tasks. Reuse and review their current
revisions. Use the [implementation issue template](../../.github/ISSUE_TEMPLATE/implementation.md)
to name one task, prerequisites, document paths, approved scope, acceptance results
and commands. Approval evidence must identify the owner review and reviewed commit;
requirements, task descriptions, acceptance, dependencies or ordering changes require
renewed approval. Pure checkbox updates and appended evidence references retain
approval if their diff changes none of those semantics; record this comparison
against the approved commit. Ambiguous changes block execution. Add `workharbor-ready` manually only
after review and prerequisite completion. The template does not apply it automatically.

At each attempt the agent validates the issue fields, selects the existing feature
with `scripts/select_feature.py`, reads its artifacts, runs initial checks and
`$speckit-analyze`, then uses the [single-task entry point](single-task.md)
for the selected task. The generic `$speckit-implement` remains feature-wide.
Missing approval, invalid context, unmet dependencies or a skill that cannot honor
that scope result in a blocked handoff. Initial failures outside the task's repair
scope are blockers. Planning artifacts are not regenerated during implementation.
Run `$speckit-converge` for the whole feature only in an explicitly authorized
closure pass after implementation; review its remaining work before dispatch.
This is a prompt/issue contract, not a mechanical tracker gate. Live dispatch and
skill-scope behavior require the documented smoke test before unattended operation.

## Retry and rework states
A pending approved task uses `implement`. After acceptance passes and its checkbox
is checked, a retry uses `handoff-only` if matching implementation commits and test
evidence exist for that issue/task/revision. Verify the commits and rerun acceptance
against the current tree, then finish only missing PR/workpad/evidence/label steps.
Reuse existing handoff artifacts. Do not uncheck or reimplement the task, execute
other tasks, or run planning/convergence skills during a handoff-only retry.

Missing evidence or failing revalidation blocks handoff and requires explicit owner
rework authorization. `rework` must identify the previous implementation, corrective
scope and acceptance. It permits only those corrections; preserve previous evidence
and completion history. Changed requirements/design/dependencies need renewed
spec/plan approval. Neither an attempt number nor restoring the ready label alone
is evidence of rework approval.

Before unattended operation, smoke-test a normal handoff and interruption after the
task checkbox/commit but before PR/workpad/labels. Resume with matching evidence and
verify no task is reimplemented and no duplicate PR is created. Also test missing
completion evidence and a semantic artifact change: both must block; a checkbox-only
progress change must retain approval. These are required scenarios, not recorded
live results. The single-task guard has local integration tests; live orchestration remains
unverified until these scenarios run against Symphony.
