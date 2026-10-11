# Symphony operator runbook

Status: prepared, not activated. No tracker, remote or credentials are supplied.

## Compatibility
The workflow targets OpenAI Symphony's Elixir reference implementation at
`be10a1b79df723d6d7612b5651c8522704dafb2e`. Its adapter uses `tracker.provider`;
older examples with flat settings may differ. JSON front matter is valid YAML and
lets bootstrap validation use Python's standard library. The reference runtime at
this commit declares Erlang 28 and Elixir 1.19.5-otp-28. Consult upstream's README
for installation; these versions are Symphony tooling, not WorkHarbor product pins.
Linear is an initial development adapter, not a product or permanent forge choice.
GitHub/Forgejo/Gitea/Codeberg tracking needs a compatible upstream/custom adapter.

## Operator activation
Install the pinned Symphony separately, install/authenticate Codex CLI, and inspect
its app-server JSON schema for the workflow approval and sandbox policy. Create a
Linear project with Todo, In Progress, Rework, Human Review, Blocked and terminal
states. Only issues with `workharbor-ready` are eligible. Review and label issues
explicitly. Configure environment variables in your private service environment:

- WORKHARBOR_REPO_URL: trusted remote URL or absolute local source checkout path.
- WORKHARBOR_WORKSPACE_ROOT: absolute dedicated directory outside the source checkout.
- WORKHARBOR_LINEAR_PROJECT: actual Linear project slug.
- LINEAR_API_KEY: host-side secret; never commit or place in issue workspaces.

Run `python3 scripts/prepare_symphony.py` to render `WORKFLOW.local.md`. The script
fails on missing settings or Codex and does not start anything. It preserves only
the secret reference `$LINEAR_API_KEY`, not its value. Keep repository URLs free of
embedded credentials. Run from the separately installed Symphony `elixir/` directory:

```sh
mise exec -- ./bin/symphony /absolute/path/to/workharbor/WORKFLOW.local.md
```

Follow the pinned upstream build procedure before that command. Confirm the host
implementation scrubs tracker secret environment variables from Codex children and
provides its tracker tools. Start with a harmless documentation issue and inspect
checkout path, logs, commits and Human Review handoff before product work. This
bootstrap cannot demonstrate live dispatch without your tracker and authenticated
runtime. No daemon has been started and no external messages have been sent.

## Trust and recovery
Hooks clone only the operator-controlled repo into a fresh per-issue directory.
Every attempt runs bootstrap checks and retains its branch and changes on retry.
Concurrency is capped at two. Workspace-write policies are not a complete hostile
code security boundary; run Symphony under a dedicated development account/host,
without production host-agent credentials. Agents stop at Human Review; merges and
releases are owner actions. Stop the service to pause dispatch; preserve failed
checkouts for diagnosis. Do not automatically delete evidence or data.
