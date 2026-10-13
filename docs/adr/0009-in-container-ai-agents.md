# 0009-in-container-ai-agents: AI agents run inside the workspace container in wave one

Date: 2026-10-13
Status: Proposed; owner decision H7:a/H9:a recorded 2026-10-13

## Context

[ADR 0006](0006-optional-ai-execution.md) proposed a later execution board and
listed AI features as a product non-goal. The owner has decided (H7:a/H9:a) that
AI agents run inside the workspace container in wave one, while
Symphony/OpenAI-style orchestration is wave two. This ADR amends ADR 0006 and
supersedes its AI non-goal wording.

## Decision

- Wave one: an AI agent is an ordinary process in the workspace container, using
  the workspace's files, network policy and trust grant. WorkHarbor does not
  schedule, dispatch or approve agent work.
- Wave two: Symphony/OpenAI-style orchestration and the execution board of ADR 0006
  remain proposed and keep every approval and evidence gate stated there.
- Interop uses a small in-container agent session interface (list, attach,
  status, send input, stop). Owner decision H16:b: it becomes its own versioned
  contract under `contracts/`, tracked as #71 (held, depends on #66). That future
  contract, not this ADR, defines the interface details. It is exposed to the
  owner through existing authorized workspace access, never as raw host shell
  access.
- Owner decision H18:a: the console is a reattachable workspace session.
- Agents hold no host-agent credentials, control sockets or other workspaces' data
  (see the isolation section of the [architecture](../architecture.md)).
- Ordinary lifecycle and terminal access remain usable without an AI subscription
  or tracker account.

## Amendment of ADR 0006

ADR 0006 stays Proposed for the wave-two board. Its statements that AI features are
non-goals and that no AI scope exists are superseded for in-container agents only.
Nothing here authorizes orchestration, dispatch or board work.

## Consequences

[Product](../product.md) and [architecture](../architecture.md) state the wave-one
scope. The constitution still lists AI orchestration as excluded; its amendment or
scoped exception needs owner action under its governance and is not made here.
The session interface contract (#71) and its wire format, authorization binding and failure behavior are
undesigned and belong to a feature spec. Credential handling for agent
subscriptions is open. Nothing is implemented or verified.
