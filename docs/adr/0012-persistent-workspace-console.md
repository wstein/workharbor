# 0012-persistent-workspace-console: The console is a reattachable workspace session

Date: 2026-10-13
Status: Accepted 2026-10-16 (owner decision H69:a); owner decisions H18:a and H22:a recorded 2026-10-13

## Context

[ADR 0009](0009-in-container-ai-agents.md) records H18:a: the console is a
reattachable workspace session. Feature 001 said disconnect cleans up the PTY,
which conflicts with a console that outlives its connections.

## Decision

- Each workspace has one persistent console session whose lifetime is independent
  of its connections.
- Disconnect, grant expiry and idle timeout end only the connection. Its relay
  resources are released within five seconds, including when the transport
  vanishes without a close frame.
- Workspace stop, rebuild, runtime deletion or an explicit close end the session.
- Reattachment requires a new single-use grant; other grant and relay bounds of
  feature 001 are unchanged.

## Consequences

The feature 001 [spec](../../specs/001-workspace-lifecycle/spec.md),
[tasks](../../specs/001-workspace-lifecycle/tasks.md) and
[data model](../../specs/001-workspace-lifecycle/data-model.md) are reconciled
to this rule. The wording of the feature 001 plan (lines 61-62) is flagged for
the plan's next revision. The persistent console-session state model (entity and
end events: stop, rebuild, deletion, explicit close) is follow-up work for the
spec that uses it; the existing Session entity and migration naming refers to the
single-use grant. Retained output, host-disconnect state and CLI behavior belong to
the specs that use the console. No persistent console is implemented or verified; only the shell policy in
`apps/host_agent/internal/workspaceshell` exists.
