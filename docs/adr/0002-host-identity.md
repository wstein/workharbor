# 0002-host-identity: Authenticated outbound agent

Date: 2026-10-11
Status: Accepted design; implementation pending

## Context and decision
Use outbound TLS connections with mTLS peer identity and scoped application authorization. Bootstrap via short-lived, single-use enrollment token over server-authenticated TLS. Store keys outside workspaces with restrictive permissions. Define revocation, certificate rotation and offline behavior before release. Reject commands for another host or owner; persist idempotency outcomes across reconnects.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.
