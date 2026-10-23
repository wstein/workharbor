# Source references

Checked: 2026-10-11. These are design sources, not a parity or compliance certification.

- https://openai.com/index/harness-engineering/ — repository knowledge, concise agent navigation and executable feedback informed the development harness.
- https://openai.com/index/open-source-codex-orchestration-symphony/ — informed development-only issue execution and review handoff.
- https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/SPEC.md — workflow contract; implementation runbook pins the same revision.
- https://github.com/github/spec-kit/tree/0443760542e7e5269724744a2ac55c4bd386d095 — actual scaffold source, CLI 1.1.4.dev0.
- https://github.com/portainer/portainer — host management inspiration.
- https://github.com/loft-sh/devpod — portable workspace inspiration.
- https://github.com/okteto/okteto — remote development inspiration.
- https://containers.dev/ — canonical devcontainer specification; version/capability validation is feature work.
- https://github.com/devcontainers/cli — intended semantic implementation boundary.
- https://github.com/apple/container — planned provider; not implemented.
- https://github.com/wstein/bundledock — owner-provided bundle concept; upstream format not verified in this bootstrap.

Product documents express WorkHarbor decisions independently; they do not reproduce
upstream articles or claim those sources require a particular backend or provider.

## Optional execution proposal sources

Checked: 2026-10-12. These primary sources support the
[concept paper](papers/optional-ai-execution.md); they establish no WorkHarbor
product approval or measured integration capability. The existing Symphony and
Spec Kit pins above are retained.

- [Pinned Symphony Elixir README](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/README.md) — prototype status, tracker adapters and observability surface.
- [Pinned Symphony orchestrator](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/lib/symphony_elixir/orchestrator.ex) — process naming/configuration evidence; not a same-BEAM compatibility certification.
- [Pinned Spec Kit README](https://github.com/github/spec-kit/blob/0443760542e7e5269724744a2ac55c4bd386d095/README.md) — repository-centered development artifacts and workflow.
- [GitHub Projects](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects) — external planning views over issues and pull requests; separate from the proposed internal board.
- [Linear API documentation](https://linear.app/developers/graphql) — hosted GraphQL endpoint and authentication; no self-hosted server or Elixir SDK claim.

## Future orchestrator adapter design inputs

Inspected: 2026-10-15. These mutable upstream sources inform the
[proposed adapter design](design/orchestrator-adapters.md); they are not product
dependencies, verified integration profiles or permission to activate anything.
The pinned Symphony/Spec Kit development sources above remain unchanged.

- [Gas Town README](https://github.com/gastownhall/gastown#readme) — team roles,
  Beads, mailboxes, Git worktrees and Refinery merge queue; WorkHarbor human merge
  policy still governs any future use.
- [Symphony specification, main](https://github.com/openai/symphony/blob/main/SPEC.md) —
  issue polling, dispatch, retries and per-issue workspace model.
- [Symphony Elixir README, main](https://github.com/openai/symphony/blob/main/elixir/README.md) —
  Codex app-server execution and repository workflow configuration.

## Agent sandbox comparison: AgentBox

Inspected: 2026-10-23 at
[`605ee75`](https://github.com/madarco/agentbox/tree/605ee75d63319003c188bc532d4e5b2ba36a51d6)
(MIT, pre-1.0, CLI 0.33.0, one maintainer). The sources are read for comparison only.
They are not a product dependency, a parity or compliance claim, or permission to
integrate anything; WorkHarbor decisions stay in its own specifications.

- [Architecture](https://github.com/madarco/agentbox/blob/605ee75d63319003c188bc532d4e5b2ba36a51d6/docs/architecture.md)
  — disposable per-agent boxes, a git worktree per box, `docker pause` for idle boxes,
  `docker commit` checkpoints, a host relay that holds credentials.
- [Security policy](https://github.com/madarco/agentbox/blob/605ee75d63319003c188bc532d4e5b2ba36a51d6/SECURITY.md)
  — the stated isolation boundary: a box must not reach host files, credentials or other boxes.
- [Provider plugins](https://github.com/madarco/agentbox/blob/605ee75d63319003c188bc532d4e5b2ba36a51d6/docs/provider-plugins.md)
  — a provider contract (`CloudBackend`) behind a versioned SDK.

Different problem: AgentBox runs disposable boxes for coding agents and orchestrates
them (queue, wait, send). WorkHarbor prepares the person's persistent, `.devcontainer`
native workspace and keeps runs human-initiated, with no agent queue or scheduler in the
runtime (decision D57 in the archived
[requirements](archive/workharbor-old/docs/content/docs/requirements.md)).
The overlap is isolated, reproducible environments on the owner's own hardware.
