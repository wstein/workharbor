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
- WORKHARBOR_WORKSPACE_ROOT: absolute dedicated directory outside the source checkout.
- WORKHARBOR_GITHUB_REPO: `owner/repository`, for example `wstein/workharbor-nxt`.
- GITHUB_TOKEN: host-side token with issue read/write access to that repository;
  never commit or place in issue workspaces.

Run `python3 scripts/prepare_symphony.py` to render `WORKFLOW.local.md`. The script
fails on missing settings or Codex and does not start anything. It preserves only
the secret reference `$GITHUB_TOKEN`, not its value. Keep repository URLs free of
embedded credentials. Run from the separately installed Symphony `elixir/` directory:

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
provide diagnostics without blocking repairs; passing checks remain mandatory at
handoff. Invalid checkout/branch prerequisites still stop the hook.
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
