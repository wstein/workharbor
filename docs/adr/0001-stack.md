# 0001-stack: Control plane and host agent

Date: 2026-10-11
Status: Accepted bootstrap baseline

## Context and decision
Use Elixir/Phoenix/LiveView for orchestration and UI, Go for the host agent. SQLite supports the first single-instance control plane. Go-only would simplify deployment, but Phoenix fits the owner preference and long-lived session management. No memory or CPU superiority is asserted. Measure idle RSS, 8 active workspaces, PTYs, reconnect load and operation latency on the target homeserver before setting resource budgets. There is no product stack code in this bundle.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.
