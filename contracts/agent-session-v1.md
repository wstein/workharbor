# In-container agent session interface, contract v1 (design baseline)

Status: proposed design baseline for wstein/workharbor-nxt#71 (owner decision
H16:b). Nothing here is implemented or verified. Like the rest of `contracts/`,
this is a reviewable boundary contract, not a live API. Machine-readable shapes:
[agent-session-v1.schema.json](agent-session-v1.schema.json).

Purpose: an AI agent runs as an ordinary process inside the workspace container
(H9, ADR 0009). It exposes this small interface so that `whr agent` (feature 006,
FR-029) and any other client can list, attach to, inspect, feed and stop its
sessions. An agent can implement this contract without the `whr` CLI. No herdr or
other protocol is assumed. `whr` never installs, configures, authenticates or wraps
the agent's native tool and never carries provider credentials (FR-030).

## Operations

| Operation | Effect | Grant | Mutating |
| --- | --- | --- | --- |
| `hello` | Version negotiation; returns supported versions only. | none | no |
| `list` | Return the agent's sessions: `id`, `status`, `started_at`, plus `limits`. | owner control grant | no |
| `status` | Return one session's `id`, `status`, `started_at`, `attachments`, `last_activity_at`, optional `detail`, and `limits`. | control grant | no |
| `attach` | Open a bidirectional byte stream to the session's terminal. `in` frames write to the session input. | attach grant, single use | yes |
| `send` | Deliver a bounded data payload to the session's input. | control grant | yes |
| `stop` | End the session. Safe to retry; stopping a stopped session succeeds. | control grant | yes |

Operations other than `hello` name the target in the request field `session`
(`status`, `attach`, `send`, `stop`); `list` takes none.
Session `status` values: `running`, `idle`, `needs-input`, `stopped`, `failed`.
Clients display an unknown status as `unknown` and never crash.

## Wire format

- JSON text, UTF-8, one object per message. Control messages use a request /
  response envelope; `attach` additionally carries a stream (below).
- Transport is outside the contract's semantics (for example a local stream
  socket inside the container, reached through the existing authorized workspace
  access path). It is never raw host shell access and never exposes host-agent
  sockets or credentials (ADR 0009).
- Request: `{"contract":"agent-session/1.0","op":"...","request_id":"...","grant":"...","session":"..."}`.
- Success response: `{"contract":"agent-session/1.0","request_id":"...","ok":true,...}`.
- Error response: `{"contract":"agent-session/1.0","request_id":"...","ok":false,"error":{"category":"...","message":"...","retryable":false}}`.
- `hello` request and response example (no grant):
  `{"contract":"agent-session/1.0","op":"hello","request_id":"r1"}` returns
  `{"contract":"agent-session/1.0","request_id":"r1","ok":true,"supported_versions":["1.0"]}`.
- Unknown fields MUST be ignored within the same major version; an unknown `op`
  yields `usage`.
- `send` payload is carried as base64 in `data_b64` so it is data, never text the
  transport or the agent's control layer interprets (FR-031).
- Invariant across all majors: the envelope fields `contract`, `request_id`, `ok`,
  `error.category`, the top-level `supported_versions` of a `hello` response and
  `error.supported_versions` keep their names and meaning, so a
  client of any version can always read an incompatibility error.

### attach stream

After a successful `attach` response the connection becomes a stream of frames:
`{"t":"out","seq":N,"data_b64":"..."}` (agent to client), `{"t":"in","data_b64":"..."}`
(client to agent), `{"t":"resize","cols":C,"rows":R}` (client to agent),
`{"t":"dropped","bytes":N}` (agent to client: retained output was lost) and
`{"t":"end","reason":"...","exit_status":S}`.

- `seq` is a per-attachment counter of `out` frames, starting at 0 and increasing
  by 1; a gap is never silent: the agent sends `dropped` instead of skipping.
- `resize` maxima are 500 columns and 200 rows (feature 001); larger values are
  not accepted: the agent ends the attachment with `end` reason `error`.
- On attach the agent MAY first send a bounded retained output tail, preceded by
  `dropped` when output was lost. The tail may begin in the middle of an escape
  sequence or UTF-8 character; clients must tolerate that.
- `end` reasons: `client-detached`, `grant-expired`, `idle-timeout`,
  `max-duration`, `overflow` (client too slow, attachment cut), `session-stopped`
  and `error`. `exit_status` is an optional integer 0-255, meaningful for
  `session-stopped`, so `whr agent attach` can pass the remote status through
  (FR-019, SC-012). Transport loss with no `end` frame is treated by the client
  as `interrupted`.

## Authorization and grant binding

- Each request except `hello` carries a `grant`: an opaque, short-lived credential
  issued by the control plane to the authenticated owner. Every grant is bound to
  a workspace, an owner, an expiry and the audience `agent-session`. A session
  grant is additionally bound to one session id. An agent-session grant MUST NOT
  be accepted by any other endpoint (shell, console, control plane), and grants of
  those endpoints MUST NOT be accepted here.
- Grants are operation-scoped. Two kinds exist:
  - an `attach` grant is issued for `attach` only (one session of one workspace).
    It authorizes nothing else and is consumed atomically at the moment of a
    successful attach (FR-006, ADR 0012); a replay fails. No other grant ever
    authorizes `attach`.
  - a control grant covers the four non-attach operations. It is issued either as
    an owner control grant (`list`, `status`, `send`, `stop` on any session of its
    workspace) or as a session control grant (`status`, `send`, `stop` on its named
    session only; not `list`). Control grants are reusable until expiry, which is
    short and set by the control plane.
  The grant kind and permitted operations are bound inside the grant; a request
  whose operation is not covered fails as any other grant failure.
- Author decisions, all proposals pending owner review: session control grants do
  not authorize `list`; control grants are reusable; `needs-human` is returned by
  `send`; `attach` is marked mutating; all bound values below.
- Order of checks: the grant is verified before any session lookup. Every grant
  failure (missing, invalid, expired, reused, wrong workspace, owner, session,
  operation or audience) returns identical `category` `authorization`, identical
  `message` and identical `retryable` (false), so no oracle exists.
- Grants and any provider credentials MUST NOT be written to logs, errors,
  retained output or traces; at most a grant identifier or fingerprint is
  logged. The agent holds no host-agent credentials or data of other workspaces.
- How the in-container agent verifies a grant is an open question (below).

## Bounds

All values are proposals pending owner review, not decisions. Implementations may
be stricter and advertise their limits in the `limits` object of the `list` and
`status` responses, with keys `send_max_bytes`, `frame_max_bytes`,
`idle_timeout_seconds` and `max_attachment_seconds`. Byte bounds on UTF-8 text
(`detail`, `message`) are normative in bytes; the schema `maxLength` counts
characters and only approximates them.

| Bound | Proposed value | Rationale |
| --- | --- | --- |
| `send` payload, decoded | 65,536 bytes | Covers a long prompt or pasted excerpt; small enough to deliver atomically and not to be a bulk upload channel. Larger input goes through workspace files. |
| Control message (excluding `send` data) | 16,384 bytes | Control messages are small ids and enums. |
| Stream frame, decoded data | 16,384 bytes | Bounds latency and backpressure. |
| `request_id` | 128 characters | Correlation only. |
| `grant` | 4,096 bytes | Room for a signed token. |
| Session id | 1 to 128 characters of `[A-Za-z0-9._-]` | Safe in logs, paths and arguments. |
| `list` response | 256 sessions | Bounded; extras omitted and `truncated` true. |
| `detail` | 512 bytes (UTF-8) | One-line status. |
| Error `message` | 512 bytes (UTF-8) | One-line redacted text. |
| Retained output tail on attach | 65,536 bytes | Same as the send bound; the console's tail size is a feature 006 planning decision. |
| `resize` | 500 columns, 200 rows | Feature 001. |
| Per-request deadline | 30 seconds, non-stream operations | Complete or fail with `timeout`. |

A `send` over the bound or empty is rejected with `usage` before any byte is
delivered (no partial delivery).

## Interaction with stream filtering

The contract moves bytes; it does not filter them. FR-022a (owner decision on
wstein/workharbor#223, closed) filters OSC, DCS, APC, PM and SOS only in streams
that `whr` itself relays, including `whr agent attach`. The agent MUST NOT assume
the client filtered, and a client that connects directly is outside FR-022a's
reach. Clients treat all `out` data, `detail` and error text as untrusted
terminal-capable text (FR-022).

## Versioning and incompatibility

- `contract` is `agent-session/<major>.<minor>`; this document is `1.0`.
- Same major is compatible: unknown fields are ignored and minor additions are
  optional. A different major is incompatible.
- A client MAY send `hello` first (no grant) and read `supported_versions`.
  Every other request also states `contract`; an agent that does not support the
  major returns `incompatible-version` with `error.supported_versions` (the agent's) and
  `client_version`, so the client can name both versions and a remediation.
  Incompatible requests are rejected, never handled best effort; no silent
  downgrade.
- A workspace with no listening interface is reported by the client as `not-found`;
  the client does not start or configure the agent's native tool.

## Error categories

Category names only. The numeric exit-code table is decided separately in
wstein/workharbor-nxt#70 and is deliberately not duplicated here.

| Category | Used when |
| --- | --- |
| `usage` | Malformed request, unknown op, bound exceeded, empty payload. |
| `not-found` | Unknown session id (after grant check), or no interface present (client-side). |
| `authorization` | Any grant failure, always identical in message. |
| `conflict` | Operation invalid in the session's state, for example `send` to a stopped session. |
| `timeout` | Deadline exceeded. |
| `interrupted` | Operation cut short by the agent shutting down; client-side also for transport loss without `end`. |
| `needs-human` | Returned by `send` when the session is blocked on a decision only a human may take (approval prompt) and input cannot satisfy it; `detail` of `status` says what. |
| `incompatible-version` | Major version unsupported. |

Messages are redacted: no grants, credentials or session content.

## Reattachable semantics (ADR 0012)

A session's lifetime is independent of its attachments. Client disconnect, grant
expiry, idle timeout and the maximum attachment duration end only the
attachment; resources are released promptly (five seconds, consistent with ADR
0012), including when the transport vanishes without `end`, and the session keeps
running. Workspace stop, rebuild, runtime deletion and an explicit `stop` end the
session. Reattachment needs a new single-use grant and may show the retained tail
with a `dropped` note. After a host disconnect a client reports session state as
unknown until it can query again.

## Stub-agent conformance test plan (text only)

A stub agent implementing this contract lets feature 006 and other clients be
tested offline (ADR 0007: no network, provider account or real agent). Planned
file: `tests/test_agent_session_contract.py`, added to the CI acceptance manifest
by the implementing task; the stub lives beside it as test data. Cases:

1. `list`/`status`/`hello` shapes, bounds and `truncated` against the schema.
2. `send` at, one over and zero bytes; binary and control-character payloads
   unmodified; no partial delivery on rejection.
3. Grant matrix: valid owner control grant, valid session control grant, wrong workspace, owner,
   session, operation and audience, expired, reused `attach` grant, a control grant
   presented for `attach`, an `attach` grant presented for any other operation, a
   shell or console grant; all yield identical `authorization` responses, checked before session
   lookup, with no grant in any log capture; control grants reusable until expiry.
4. `stop` idempotence; `send` after stop gives `conflict`; `needs-human` case.
5. Attach: disconnect, transport drop without `end`, grant expiry, idle timeout,
   max duration and overflow end only the attachment (session still `running`,
   resources released within five seconds); explicit stop ends the session with
   `exit_status`; workspace stop, rebuild and runtime deletion are simulated
   events in the stub; reattach needs a new grant and reports `dropped`; `seq`
   gaps never occur silently.
6. Version: same major with unknown fields tolerated; different major yields
   `incompatible-version` with both versions; absent interface yields `not-found`.
7. Each error category is produced at least once; names only, no exit-code
   numbers (mapping is tested in feature 006, SC-012).

## Open questions (undecided)

- How the in-container agent verifies a grant (provisioned key versus callback to
  the control plane) without holding host-agent credentials.
- Concrete transport and path inside the container.
- Whether `send` needs an idempotency key; none is defined in v1.
- Whether the console is itself listed as a session (left to feature 006).
