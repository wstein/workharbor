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
