# 0009-in-container-ai-agents: AI agents run inside the workspace container in wave one

Date: 2026-10-13
Status: Accepted 2026-10-16 (owner decision H69:a); owner decision H7:a/H9:a recorded 2026-10-13

## Context

[ADR 0006](0006-optional-ai-execution.md) proposed a later execution board and
framed every AI product scope as later and optional. The owner has decided (H7:a/H9:a) that
AI agents run inside the workspace container in wave one, while
Symphony/OpenAI-style orchestration is wave two. This ADR amends that framing; the
superseded non-goal wording is in [product](../product.md).

## Decision

- Wave one: an AI agent is an ordinary process in the workspace container, using
  the workspace's files, network policy and trust grant. WorkHarbor does not
  schedule, dispatch or approve agent work.
- Wave two: Symphony/OpenAI-style orchestration and the execution board of ADR 0006
  remain proposed and keep every approval and evidence gate stated there.
- Interop uses a small in-container agent session interface (list, attach,
  status, send input, stop). Owner decision H16:b: it becomes its own versioned
  contract under `contracts/`, tracked as [#71](https://github.com/wstein/workharbor-nxt/issues/71). That future
  contract, not this ADR, defines the interface details. It is exposed to the
  owner through existing authorized workspace access, never as raw host shell
  access.
- Owner decision H18:a: the workspace console is a reattachable
  session (see [ADR 0012](0012-persistent-workspace-console.md)).
- Agents hold no host-agent credentials, control sockets or other workspaces' data
  (see the isolation section of the [architecture](../architecture.md)).
- Ordinary lifecycle and terminal access remain usable without an AI subscription
  or tracker account.

## Amendment of ADR 0006

ADR 0006 stays Proposed for the wave-two board. Its framing that every AI product
scope is later and optional (lines 9-21) is amended: in-container agents are wave
one. The non-goal wording superseded is in `docs/product.md`.
Nothing here authorizes orchestration, dispatch or board work.

## Open question: agent credentials (design input, not a decision)

Recorded 2026-10-15 as input for the credential design; nothing here is decided.

- Subscription CLIs (Claude Code, Codex, Antigravity) authenticate with an
  interactive login, not an API key. A model gateway does not cover them; their
  guardrails need a separate answer.
- For API-key use, a candidate pattern is an owner-run model gateway outside the
  workspace. The gateway holds the provider keys; the workspace receives only a
  base URL and a per-workspace virtual key, which gives budgets and
  per-workspace attribution without provider keys in the container.
- A gateway only enforces this if workspace egress to the providers is
  restricted to the gateway. That needs a network policy in the Docker
  provider, which is undesigned; no workspace egress policy exists yet.
- WorkHarbor would expose a provider-neutral seam (gateway URL and key
  reference) and would not bundle, deploy or configure a gateway.
- Candidates to evaluate, unverified:
  [Busbar](https://github.com/GetBusbar/busbar) (self-hosted single binary,
  six model protocols; its MCP/A2A governance is not yet demonstrated and its
  config format is outside semver), [LiteLLM Proxy](https://github.com/BerriAI/litellm) and
  [agentgateway](https://github.com/agentgateway/agentgateway).

## Consequences

[Product](../product.md) and [architecture](../architecture.md) state the wave-one
scope. Constitution v1.1.0 (2026-10-14, [#75](https://github.com/wstein/workharbor-nxt/issues/75))
allows in-container agents (Principle I) and keeps control-plane orchestration
outside wave one.
The session interface contract ([#71](https://github.com/wstein/workharbor-nxt/issues/71)) and its wire format,
authorization binding and failure behavior are undesigned and belong to a
feature spec. Credential handling for agent
subscriptions is open; see the open question above. The session interface and
agent integration are not implemented or verified.
