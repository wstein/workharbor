# 0011: Read-only Spec Kit dashboard

Date: 2026-10-13
Status: Proposed; nothing is implemented
Supersedes in part: the first increment of
[ADR 0006](0006-optional-ai-execution.md#read-only-spec-kit-view-first)

## Context

ADR 0006 proposed an optional execution board whose first increment was a read-only
Spec Kit view. The owner decided (H8:a, H17:a) that the dashboard is itself the
read-only view and that it replaces that first increment. This ADR records the
dashboard decision only. It creates no approved product scope, task or
implementation, and does not change the constitution.

## Decision

- The dashboard is a read-only view over Spec Kit artifacts (specs, plans, tasks),
  review evidence and workspace/agent status.
- The Phoenix control plane serves one owner-authenticated API over TLS, shared by
  the CLI and the dashboard. The dashboard is a LiveView page of that API and the
  CLI is a client of it. "Local" means owner-scoped, not loopback. Neither
  surface owns state the other cannot see; the dashboard adds no separate data
  path.
- Git stays authoritative for specifications. The dashboard shows a derived view
  and evidence references, never a second editable copy. Unavailable or unknown
  sources are shown as unavailable or stale, not inferred.
- Console access is intended as a reattachable workspace session (H18:a), using
  the existing workspace terminal model rather than a dashboard-specific channel.
  Details are left to the feature spec.

## Non-goals

- No editing of specs, plans, tasks or other artifacts.
- No task dispatch, approval, scheduling or retry from the dashboard.
- No guarded command buttons; any later control needs a new decision.
- No assumed Symphony adapter, tracker integration or Spec Kit command execution.

## Relation to ADR 0006

ADR 0006 remains Proposed. Only its first increment (the read-only Spec Kit view)
is superseded by this ADR. Its later execution-board direction, integration
boundary and approval gates are unchanged and still unapproved.

## Consequences

Documentation only. API shape, authentication, parser capability and resource cost
are unmeasured design work for a later feature spec. Product reads still require
owner approval under the governance gates before any implementation.
