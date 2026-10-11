# Feature 001 implementation plan

Status: proposed implementation plan; product work not started.
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
