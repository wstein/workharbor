# Feature 001: Create and access a Docker workspace

Status: revised proposal awaiting human spec/plan review; not implemented. Created: 2026-10-11.
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
Disconnect ends only the connection and releases its
relay; resizing and reconnect are bounded and observable.
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

## Security and operational acceptance
- Enrollment codes expire five minutes after creation. Expired, consumed and
  revoked codes fail without issuing an identity; consumption is atomic.
- Terminal grants expire 60 seconds after issuance and are consumed atomically on
  connection. An established connection lasts at most eight hours and expires after
  15 minutes without owner input; either limit ends only the connection. Reconnect
  requires a new grant. At most two connections per workspace and eight per owner
  may be active; excess requests fail with an explicit limit error. Each relay
  direction buffers at most 1 MiB per connection; overflow ends that connection
  with a diagnostic. Dimensions must be within 1–500 columns and 1–200 rows.
  Disconnect, grant expiry and idle timeout end only the connection and release
  its relay within five seconds, including when the transport disappears without a
  close frame; stop, rebuild, runtime deletion or an explicit close end the
  console session
  ([ADR 0012](../../docs/adr/0012-persistent-workspace-console.md)).
- Before any config-provided executable hook, the owner explicitly approves a
  resolved config commit and the complete set of resolved submodule commits.
  Store this approval with owner, repository identity, revisions and timestamp.
  New config/submodule commits require new approval; branch names or repository
  URLs alone never carry trust. Reject unsupported capabilities before hooks or
  provider mutations. Refusal leaves the workspace unprovisioned.
- Agents heartbeat every ten seconds. After 30 seconds without an authenticated
  heartbeat, mark the host and its workspace observations unknown; show this in
  the browser within five additional seconds. Under the supported test load,
  authenticated reconnect reconciles observed state within 30 seconds when the
  provider responds. Retry interrupted operations at most three times with delays
  of 1, 2 and 4 seconds; exhausted or timed-out work remains visibly failed or
  unknown, retains storage, and requires an explicit retry operation.
- Acceptance environment: one Linux Docker host, one control plane and one owner,
  with up to five workspaces and eight terminal connections. Record exact machine,
  OS and tool versions. Measure time from the triggering event; timing tests use
  a responsive provider and report unavailable-host/provider cases separately.
- Backup/restore must recover control-plane metadata and persistent project files
  to a clean installation from a quiesced backup, retaining workspace/owner/host
  associations and idempotency outcomes. Restore without starting runtimes, mark
  observations unknown, require authenticated host reconnect, and reject revoked
  credentials. Backup secrets remain outside Git and workspace environments.

## Resource-selection scope addition (2026-10-16)

Feature 006 [US1a and FR-039–FR-042](../006-whr-cli/spec.md) propose CPU/memory
selection at creation, identical CLI/browser validation, durable desired values and
separate observed enforcement. This is not implemented or covered by prior task
completion evidence. The accepted provider subset remains unchanged until its
contracts and affected plan/tasks are reconciled and reviewed; no arbitrary
`hostRequirements` or raw provider arguments are admitted by this proposal.

## Boundaries
Image and Dockerfile devcontainers are the initial tested matrix. Compose,
Features, nested containers, privileged configs and extra mounts must be explicitly
marked supported or rejected; no implicit full devcontainer compatibility claim.
Bundle attachments remain a contract only here. Browser editor and external IDEs
follow later. No Git server, multi-user UI or AI orchestration product endpoint.

## Success criteria
- SC-001: All US1–US4 scenarios pass on the acceptance host without AI/tracker services.
- SC-002: Two requests with one idempotency key produce one runtime; changed payload
  returns conflict. Interrupted provisioning retains files and creates no duplicate.
- SC-003: Stale detection, browser updates and reconnect meet the time limits above;
  an unavailable provider never produces a false successful or running observation.
- SC-004: Cross-owner/host commands, revoked identities, expired/reused grants,
  unapproved revisions and terminal limit violations are rejected in automated tests.
- SC-005: Uncommitted files survive stop/start and a clean-install backup/restore;
  restored file contents and idempotency outcomes match their pre-backup values.
- SC-006: Evidence includes browser walkthrough, failure-path results, host/OS/tool
  versions, exact commands and idle/load resource measurements. Mocked tests alone
  do not demonstrate the complete user journey.
