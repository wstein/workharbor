# Owner API design

Status: proposed, not live. Prefix `/api/v1`, verified TLS, versioned payloads.
CLI connects only to the control plane; provider handles stay opaque.

| Group | Capability |
| --- | --- |
| auth | secret-channel login, own credential status/revoke; no secret URL/argument |
| workspaces | owner-scoped read and lifecycle, exact revision trust, separate destructive confirmation |
| operations | own ID or workspace/key lookup and waiting; preallocated identity supports lost create reply |
| hosts | owner enrollment/rotation/revoke/confirmation; agent outbound mTLS and assigned scope |
| access | fresh scoped grants, bounded shell/console/SSH, console status/close |
| agent-sessions | versioned contract v1 forwarding; authorization before lookup; no native-tool launch |
| spec-kit | immutable read projection only; read-scoped dashboard cannot mutate any endpoint |

Every workspace lifecycle request carries owner-scoped stable workspace identity,
key and canonical normalized body hash. Durability precedes provider mutation.
Transient retries retain identity/key. Host-local checkpoint commands and session
controls do not use lifecycle waiting. Lost agent send response is unknown and
never automatically resent.

Publish root `contracts/cli/output.schema.json` in the output WI. Nonstream stdout:
one versioned envelope; stream stdout: typed JSON events with base64 bytes and a
final CLI/remote exit event when transport permits. Human progress goes to stderr.
Decoded stream frames <=16 KiB; encoded limits include base64/envelope overhead.
Agent send <=65,536 decoded bytes, checked before partial delivery. No compression.
Server enforces Host/Origin/anti-forgery, scope, runtime generation and attachment
bounds. Stateful terminal filtering spans chunks and bounds unterminated control
strings; C1 introducers and arbitrary bytes are covered by negative fixtures.
Audit identities/action/outcome, never raw input, grants or credential payloads.
