# Feature 001: Create and access a Docker workspace

Status: specified; not implemented. Created: 2026-10-11.
Feature identifier: 001-workspace-lifecycle.

## User scenarios and acceptance
### US1 (P1): Register a host
An authenticated owner enrolls a remote Docker host with a short-lived one-time
code. Given a valid enrollment, the host becomes visible with identity and
capabilities. Expired or reused code fails; revoked identity cannot reconnect.

### US2 (P1): Provision a configuration revision
Owner submits a Git workspace config repository. Given trusted config with a tested
image/Dockerfile devcontainer and explicitly pinned project submodules, create an
operation and a persistent workspace. Show progress and effective revision.
Unsupported config fails validation before build hooks execute. Untrusted hooks
require explicit trust. Clone/build errors expose redacted diagnostic events.

### US3 (P1): Access terminal
From the running workspace, owner opens a browser terminal inside the container.
Terminal grants are single-use, expire and cannot access another workspace/owner.
Disconnect cleans up the PTY; resizing and reconnect are bounded and observable.
Host secrets, Docker socket and host shell are inaccessible through the session.

### US4 (P1): Stop, restart and recover
Write a file without committing, stop and restart, then read exactly the same file.
Duplicate start commands with identical operation key create one runtime. Kill the
agent during provisioning, restart and reconcile partial resources. Disconnect
host: UI displays unknown/stale state, then converges after authenticated reconnect.

## Functional requirements
- FR-001: Require authenticated owner identity and owner_id for every workspace.
- FR-002: Enroll, authenticate, rotate and revoke host identity over encrypted traffic.
- FR-003: Scope all commands to assigned host, workspace and owner.
- FR-004: Resolve and record config commit and explicitly initialized submodule commits.
- FR-005: Use Dev Container CLI for supported semantics; reject unsupported capabilities.
- FR-006: Store durable idempotency records and reject changed payload for reused key.
- FR-007: Retain project storage through stop/start and failed provisioning retries.
- FR-008: Authorize expiring single-use terminal grants with bounded relay resources.
- FR-009: Show correlated, redacted progress/errors and stale host state.
- FR-010: Recover interrupted operations without duplicate runtime or storage destruction.
- FR-011: Keep secrets out of Git, logs and workspace environment unless explicitly granted.
- FR-012: Deliver browser walkthrough evidence and automated failure-path tests.

## Boundaries
Image and Dockerfile devcontainers are the initial tested matrix. Compose,
Features, nested containers, privileged configs and extra mounts must be explicitly
marked supported or rejected; no implicit full devcontainer compatibility claim.
Bundle attachments remain a contract only here. Browser editor and external IDEs
follow later. No Git server, multi-user UI or AI orchestration product endpoint.

## Success criteria
All US1–US4 scenarios pass on a clean Docker test host with no AI/tracker services.
Two requests with one idempotency key produce one runtime. Reconnect converges to
actual provider state. A cross-owner command/session and revoked agent are rejected.
Uncommitted files survive stop/start. Reports include host/OS/tool versions and
commands; mocked tests alone do not demonstrate the complete user journey.
