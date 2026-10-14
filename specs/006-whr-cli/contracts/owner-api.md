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

## Accepted T005 foundation contract

Independent Sol design review accepted the consolidated V3 foundation and final
observation-API clarification before implementation. This section defines native
application operations, not an authenticated/live HTTP endpoint. Authentication,
strict raw HTTP decoding, protocol, trust/provider work and lifecycle dispatch
remain T006–T010. T005 has no provider effects, dispatcher, retries or activation.

### Native lifecycle identity

`accept(owner_id, key, request, channel)` receives a string-keyed native map.
Owner is separately scoped, not an authentication proof. T006 must derive it from
verified credentials. Exactly these request fields are admitted:

```json
{"schema_version":1,"workspace_id":"<UUID>","action":"create","parameters":{"host_id":"<UUID>","config":{"repository_identity":"git@example.test:team/workspace.git","commit":"<full commit>","submodules":[{"path":"project","commit":"<full commit>"}]}}}
```

| Action | Exact parameters |
| --- | --- |
| create | host_id, config |
| start, stop, remove_runtime | empty object |
| rebuild | config |
| delete_data | reserved separate action; rejected in T005 |

Version is mandatory, exactly integer 1. Parameters may be omitted only for an
empty-parameter action and become `{}`. Config requires repository_identity and
commit; omitted submodules become `[]`. Explicit null, unknown fields and wrong
types fail recursively. UUIDs use lowercase hyphenated spelling. Keys are required,
case-sensitive ASCII `[A-Za-z0-9._:-]{1,128}`; no server default. Commits are full
lowercase 40- or 64-hex object IDs; existence is not checked. Submodule entries
contain exactly path/commit, use the accepted lexical path predicate with dot=false,
reject duplicate paths and sort by Unicode scalar path order. No other defaults,
names or arbitrary provider options exist. Never fetch or re-resolve on replay.

The normalized hash object adds owner_id from the scope argument. Store schema,
canonical bytes and SHA-256 digest; compare bytes and digest on replay. Accepted
T003 canonical UTF-8/scalar-key/no-normalization/no-LF semantics apply, including
128-container depth and 4300-digit integers. Canonicalization has no parse-input
byte cap. Exclude key, operation ID, channel, credentials/confirmation/trust grants,
tracing, timestamps, retry counts, wait/no-wait, timeout, output/color and generated
intent/runtime versions. Exclusions are separate context, never silently dropped
request fields. Native maps cannot prove duplicate-free raw input. Future HTTP
integration must reject duplicates before map collapse, malformed UTF-8,
surrogates, floats and raw byte-limit violations; T005 claims no raw decoder.

### Literal repository identity

Admit HTTPS URL, SSH URL and scp-form spellings without fetching or rewriting.
These are lexical storage identities, not a fetch support matrix or canonical
remote/trust identity. Alias spellings remain distinct and never inherit trust.
Do not trim, case-fold, Unicode-normalize, percent-decode, strip .git/default ports,
follow redirects or resolve aliases. Actual fetch support and origin/trust
resolution remain T009.

Reject empty strings, backslash, C0, DEL, `?`, `#` and Unicode White_Space:
U+0009–000D, U+0020, U+0085, U+00A0, U+1680, U+2000–200A, U+2028–2029,
U+202F, U+205F, U+3000. Every percent sign forms a two-ASCII-hex-digit triplet,
retained unchanged. Unbracketed hosts are nonempty dot-separated labels matching
`[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?`, optionally one trailing dot; no
underscores or interior empty labels. Numeric labels do not prove IPv4 validity;
there is no DNS length/network validity claim. Bracketed hosts are locally parsed
IPv6 literals yielding eight components, without zone IDs, preserving spelling.

Recognize HTTPS:// and SSH:// ASCII-case-insensitively and retain scheme spelling.
Reject another scheme:// prefix rather than treating it as scp. URL shapes are
`https://host[:port]/suffix` (no userinfo) and
`ssh://[username@]host[:port]/suffix` (no password). Suffix may be empty. Username
matches `[A-Za-z0-9._-]+`, without colon or percent escapes. Port is nonempty
ASCII digits with value 1–65535; leading zeros remain distinct. Scp shape is
`[username@]host:nonempty-path`; delimiter is the first colon outside brackets,
there is no port syntax, and its path may be relative/absolute with later colons/@.

### Atomic acceptance and metadata guards

Use a short immediate SQLite transaction. Scope-authorized owner/workspace/key
lookup precedes busy, tombstone and current-state preconditions or mutations.
Equal normalized identity returns the same operation/status/outcome. Changed
identity conflicts. New-key acceptance enforces owner/host scope, stable create
identity, action preconditions and one unfinished operation/workspace. Busy
refusal consumes no version. Acceptance CAS-increments intent_version, inserts
queued operation, desired-state projection, required create storage metadata,
runtime bindings/reservations and audit in the same transaction. Any failure rolls
back all rows. Create/start/rebuild desire running; stop/remove_runtime desire
stopped. Commit precedes acceptance response or any later external effect. The
operation is durable reconciliation intent; T005 installs no effect consumer.

Claim CAS allows queued->executing only with matching scope/current intent and
atomically audits. Repeated executing claim reads never authorize another effect.
Candidate reservation permits create/rebuild while queued/executing, derives
config only from immutable operation request, and reuses a matching reservation.
One candidate/operation; conflicting reservation is rejected. Initial reservations
and their audits are part of acceptance; any later reservation is independently
atomic before an external call.

Terminal results require scoped operation, intent_version and exact target/candidate
runtime ID/generation bindings. Nonterminal writes also require workspace current
intent and expected status; matching runtime generation alone is insufficient.
Allow queued->failed, executing->succeeded/failed; forbid queued->succeeded and
terminal->nonterminal. Foundation outcomes are exactly status/code pairs:
`{"status":"succeeded","code":"ok"}` or
`{"status":"failed","code":"operation_failed"}`. Optional typed observation
metadata, terminal result/outcome and audit commit together. Provider ID cannot
change once set. Effective config must match candidate requested revisions;
existing-target start cannot change effective config. Terminal equal result replay
checks scope/bindings and returns immutable recorded outcome even after newer
intent; conflicting status/code/observation cannot overwrite or append audit.
No separate repeatable nonterminal observation API exists in T005.

### Audit and later boundaries

One-time accepted, claimed, terminal and runtime_reserved events contain generated
ID, owner/host/workspace/operation IDs, action, intent version, fixed channel enum
(cli/browser/internal), outcome/code and UTC time. Runtime events include runtime
ID/generation. Accepted event records created storage metadata IDs. Every new
candidate emits runtime_reserved atomically with allocator/runtime/binding writes,
including initial acceptance; equal replay emits none. Unique operation/event
applies only to these one-time events. Terminal audit covers its observation
updates. Exclude request contents, URLs, handles, raw names/messages/provider
payloads, input, tokens, grants and credentials. Admin metadata creation is audited.

Runtime/data deletion have separate operation identities/keys. T005 refuses
reserved delete_data until later explicit confirmation/authorization. Runtime and
storage tombstones represent confirmed outcomes only, never queued removal.
Preserve workspace identity, keys, outcomes and audit indefinitely in the pilot;
no destructive cascades/identity reuse. T010/T011 define rm --data coordination.
