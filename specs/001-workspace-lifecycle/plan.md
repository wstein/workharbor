# Feature 001 implementation plan

Status: revised proposed implementation plan awaiting human review; product work not started.
Branch for implementation: `001-workspace-lifecycle`.

## Technical context
Phoenix/Elixir LiveView control plane, Ecto + SQLite; Go host agent; official Dev
Container CLI; Docker Engine on Linux. Exact runtime and dependency versions must
be selected, pinned and locked in T001, never guessed in bootstrap documentation.
One control plane instance, one local owner; authenticated outbound remote agent.

## Constitution check
Scope excludes product AI. Provider port is independent of Docker. Ownership is
mandatory; host operations and PTYs are scoped. Storage is not a runtime side effect.
Spec is in repo; implementation needs targeted evidence and human review.
No exceptions proposed. Repeat this check after design and before handoff.

## Implementation sequence
1. Pin toolchain and create applications with local format/build/test commands.
2. Implement domain migrations, owner authentication and durable operations.
3. Implement enrollment, mTLS, authorization, reconnect and contract validation.
4. Implement Docker adapter using tested Dev Container CLI semantics and storage.
5. Implement lifecycle reconciliation and explicit retry behavior.
6. Implement browser UI and terminal relay with authorization and bounded buffers.
7. Demonstrate end-to-end acceptance on clean host and record evidence.

## Verification layers
Domain unit tests for transitions/idempotency; protocol tests for invalid versions,
replay and scopes; real Docker integration tests for provisioning/recovery/volumes;
browser walkthrough for enrollment → provision → PTY → stop → restart. Security
negative tests exercise wrong owner/host, revoked cert, expired/consumed grant,
unsafe submodule path, secret redaction and untrusted hooks.

## Operational evidence
Correlate operation_id/workspace_id/host_id; report state transitions, retries,
reconciliation latency, PTY lifetimes and buffer rejections. Sensitive payloads are
excluded. Capture actual idle/load resource measurements rather than stack estimates.

## Release gates
Pin versions and publish the supported config matrix; all FR requirements need
evidence; backup/restore and certificate rotation runbooks must be tested. Do not
label a scaffold or mocked provider as a shipped feature.

## Components and validation commands
Control-plane modules live in `apps/control_plane/lib/workharbor/`; LiveViews in
`apps/control_plane/lib/workharbor_web/live/`. Domain migrations live in
`apps/control_plane/priv/repo/migrations/`, tests in `apps/control_plane/test/`.
Agent ports, services and Docker adapter live respectively in
`apps/host_agent/internal/ports/`, `apps/host_agent/internal/service/` and
`apps/host_agent/internal/providers/docker/`; Go tests sit beside their packages.
Protocol schemas and negative fixtures live in `contracts/host-protocol/`.
T001 and T011 establish the actual project commands: `mix format --check-formatted`,
`mix compile --warnings-as-errors`, `mix test`, `gofmt -l .`, `go build ./...` and
`go test ./...`, each run from its application directory. `make check` remains the
repository gate. Real-host/browser commands and results are added to quickstart
and evidence by T010; absent application tools or hosts never count as passing.

## Security and recovery design
Persist revision-scoped trust approvals in the control plane and include the
approved revision set in authorized provisioning commands. Resolve and validate
before executing hooks. Enrollment and session consumption use transactional
compare-and-set semantics. Implement the spec's exact TTLs, session quotas,
dimensions, buffer limits and cleanup deadlines; test boundary and concurrent use.
Use monotonic timers for heartbeat expiry, operation deadlines and relay cleanup.
Each provider mutation has a 120-second deadline, except provisioning (20 minutes).
A timeout triggers inspection before any retry; retries never run overlapping
mutations. After the spec's three retries, require explicit operator retry.
Use generation-checked reports and reconcile after authenticated reconnect.

## Backup and restore gate
T019 supplies `scripts/backup-workharbor.sh`, `scripts/restore-workharbor.sh` and
`docs/development/backup-restore.md`. Quiesce operations and stop workspaces before
copying metadata and project storage; abort if quiescence cannot be confirmed.
Back up identity/revocation state securely outside Git, with restricted access.
Restore into an empty installation, retain ownership and operation outcomes,
mark observations unknown, and reconcile only after authenticated reconnect.
T019 proves this with uncommitted files and a revoked identity on a clean host.

## Review and execution gates
The revised spec and plan require separate human review before implementation
issues receive `workharbor-ready`. An issue names the feature, one task, its
prerequisites, authoritative documents and acceptance commands. Agents select that
feature, read those documents and implement only the approved task. Missing gates,
unclear scope or unmet dependencies block execution. Analyze before implementation;
converge after an implementation pass, never as an automatic planning substitute.
