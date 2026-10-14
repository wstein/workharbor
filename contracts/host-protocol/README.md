# Host protocol v1 — consolidated T007 R2

Status: independently Sol CLEAR R2 design, consolidated with accepted T005
`1189c81ba81b79a47a0485a9d5148fb8286c8eb0` and T006
`ef399f58f2d9343109e248a519231209a707a416`. Implementation has explicit coordinator
GO; this document does not claim implementation acceptance or host capability.

## Scope and version

T007 supplies only closed contracts, independent fixtures and pure decoders,
validators and replay classification. It creates no transport, listener, provider,
trust store, dispatcher, command ledger or background work. T008 owns enrollment,
mTLS, revocation, heartbeat and reconnect; T009 owns actual trust/config/capability
and provider validation; T010 owns durable claims, reconciliation, retry and effects.
Terminal and in-container agent-session contracts are separate later work.

One UTF-8 JSON object per message. schema_version is exactly integer 1. Only known
kinds/actions/fields are admitted; objects are closed recursively. No extension
bag, executable argv, arbitrary provider options, signatures, bearer credentials,
nonce, timestamp authority or unauthenticated hello transport is added here.

Unpublished v1 is a draft. An amendment requires authoritative review, consolidated
schema/fixtures/both validators and integration acceptance before use; older draft
builds receive no compatibility claim. Once declared supported/released, incompatible
shape or meaning changes require a new version and explicit negotiation or retirement.
Never silently reinterpret or downgrade. T008 binds exact supported-version negotiation
to authenticated peers. Agent-session ignore-unknown-field semantics do not apply.

## Exact command

All fields are required, and no others are allowed:

`schema_version`, `kind`, `command_id`, `owner_id`, `host_id`, `workspace_id`,
`operation_id`, `idempotency_key`, `request_sha256`, `intent_version`, `request`,
`bindings`, `execution_config`, `approval_ref`.

Version is 1; kind is command. IDs use lowercase hyphenated UUID spelling. Key is
case-sensitive ASCII [A-Za-z0-9._:-]{1,128}; hash is lowercase SHA-256 hex.
Bindings have exactly target and candidate, each null or an exact object with
runtime_id (UUID) and generation (positive bounded integer).

Wire request has exactly schema_version, workspace_id, action, parameters, with
accepted defaults explicitly materialized; it has no owner_id. Actions are create,
start, stop, rebuild, remove_runtime; delete_data is rejected. Request and config
use the accepted [V3 schema](../../specs/006-whr-cli/contracts/owner-api.md), lexical
repository identity, full lowercase commit IDs, dot=false scalar-safe submodule
paths, duplicate-path refusal and scalar ordering. No aliases, normalization,
fetching or re-resolution occur. Wire request must equal normalized request after
removing only its server-added owner_id; omitted defaults or unsorted entries reject.

Create/rebuild use immutable request config and MUST have execution_config null
and approval_ref nonnull UUID. Start has empty parameters and nonnull frozen
existing-target execution_config, and approval_ref null. Stop/remove_runtime have
empty parameters, execution_config null and approval_ref null. A create request's
host and every repeated workspace identity must match envelope and trusted scope.
Candidate is allowed only for create/rebuild. Start requires target and no candidate;
create requires candidate and no target; rebuild requires candidate and may have
target. Stop/remove no-target no-op is allowed only by the explicit expected binding.
Compare the entire target/candidate structure to authoritative expectation.

Approval reference shape/matching against the supplied authoritative revision
expectation is validation only. T009 supplies actual approval lookup/enforcement;
config descriptors and UUIDs never themselves authorize hooks or provider calls.

## Exact result and observations

Required: schema_version, kind, command_id, owner_id, host_id, workspace_id,
operation_id, idempotency_key, request_sha256, intent_version, bindings, outcome.
Only optional field: observation. Version 1; kind result. Command-only request,
execution_config, approval_ref and all other fields reject. Identity/bindings match
immutable expected command/operation. Outcome is exactly succeeded/ok or
failed/operation_failed, in status/code fields.

Observation omission means no observation (native nil); explicit wire null rejects.
A present closed object requires runtime_id, generation, observed_state. Allowed
states: pending, provisioning, running, stopping, stopped, failed, unknown. Optional
provider_instance_id is string/null, effective_config is strict normalized config/null,
and deleted is exact boolean. Omitted optionals normalize null/null/false before
terminal replay equality. Explicit deleted:null, false provider IDs, malformed config,
unknown fields, improper lists or float generations reject, without truthiness fallback.

Observed runtime must equal an exact bound target/candidate. A known provider ID
cannot change. Null provider/config preserve stored values, never clear them.
Only candidate may first populate effective config, matching immutable requested
revisions; existing target config cannot change or be populated by stop/remove.
Start preserves the frozen snapshot. deleted=true requires succeeded remove_runtime,
bound target and stopped observation. It is confirmation data requiring later
provider/reconciliation evidence; missing/disconnected/failed observation never implies
a tombstone. No standalone repeatable nonterminal observation API is added.

## Bounded native and raw values

Strict raw limits remain 1,048,576 bytes, 128 containers (root counts one), 4,300
integer magnitude digits, scalar UTF-8, no BOM/duplicates/surrogates/floats/exponents
or trailing JSON. Native validation independently checks exact closed types, proper
lists, UTF-8 and lexical domains. Counters are exact integers 1..9223372036854775807
before conversion, agreeing with accepted foundation allocation bounds. Booleans,
float 1.0, loose comparison and unchecked big-integer narrowing are forbidden.

Variable strings and array counts have only the whole canonical envelope <=1 MiB
bound; no speculative per-string/submodule caps or nonempty provider-ID restriction.
Perform bounded native canonical-size preflight BEFORE unbounded canonical allocation
or emission, using accepted escaping and depth semantics. Then reuse canonical bytes.
Schema annotations alone cannot establish raw/native byte/type semantics.

An admitted operation whose envelope cannot fit receives fixed transport_refused,
with no partial emission/provider call. Preserve request bytes/hash/key/op/versions;
never truncate, renormalize, weaken limits or automatically retry changed payload/key.
The operation remains retrievable. T010 MUST atomically record current nonterminal
operation failed/operation_failed under exact scope/current-intent/binding/status guards,
with a fixed visible refusal reason in a reviewed typed diagnostic/audit projection.
Do not overwrite terminal results or silently queue forever. Admission should apply
dispatch limits where knowable; envelope overhead still needs this refusal path.

## Trusted contexts, identity and replay

Order: strict decode; version/kind/shape/types/budget; trusted peer/scope admission;
scope-authorized expected operation lookup; immutable identity/replay classification;
current intent/bindings/status for new work; typed disposition only. Wrong scope has
one fixed authorization diagnostic before existence/replay disclosure. Missing trusted
context rejects. Context is a separate native typed argument, never a decoded body.

Control plane derives peer host from future verified mTLS and authoritative host
ownership/operation/workspace relations. Agent derives verified control-plane peer
and enrolled local host/owner assignment. Owner credentials/principals, body IDs and
JSON authenticated flags are not host authority. Revoked peer rejects even historical
lookup. T007 receives only a caller-supplied trusted Peer plus an Operation snapshot;
these types are not cryptographic enforcement and have no public body decoder.

First create uses scoped accepted operation/request and candidate reservation. Agent
needs an appropriate trusted create-admission expectation from the future authenticated
admission layer, not copied body IDs. No existing local workspace is assumed. Missing
expectation fails closed. T008/T010 must review its actual production and atomically
establish the local ledger before effects. Fixtures inject expectations and prove no
mTLS. T007 creates no workspace/ledger/enrollment, and mutates no foundation records.

Owner hash is normalized request plus separately verified owner; compare canonical
request bytes AND digest against immutable operation. Owner hash excludes generated
command/intent/runtime/approval/execution context per V3. Separate canonical WHOLE
command bytes include immutable scope/request/binding/execution/approval context for
replay equality. Never confuse these identities.

One immutable lifecycle directive per operation. T010 generates/stores stable command_id
before transmission and preserves it across retry/reconnect. Enforce host+command and
operation-to-command uniqueness plus owner/workspace/key association. A different
command ID cannot bypass dedup. T007 consumes supplied snapshots, not a new store.
Dispositions: new_eligible, pending_lookup, completed_lookup, reconciliation_required.
New_eligible is NOT effect permission: durable unique claim and trust/capability gates
remain T009/T010. Exact pending replay only reads status/requires reconciliation;
completed replay returns immutable result, with no provider or new audit. Changed
identity conflicts. Old same-runtime intent cannot execute/update; exact historical
terminal replay remains lookup after newer intent. New explicit op/key/intent can
legitimately reuse runtime generation. Unknown lost response requires inspection,
not blind resend. T007 makes no atomic/restart-safe ledger claim.

New result writes permit queued->failed, executing->succeeded/failed; queued success
and terminal overwrite reject. Exact terminal outcome/observation replay ignores newer
workspace intent after current peer authorization and exact operation/binding checks.
Expected status comes from trusted snapshot, never untrusted result fields.

Fresh authenticated connection/certificate rotation never resets application histories,
keys or counters. T008 supplies version/identity/revocation/staleness; T010 reconciles
before bounded nonoverlapping retries. Restore retains identities/outcomes and resets
observations unknown. No added wire sequencing/signature/expiry mechanism.

## Accepted implementation alignment

Elixir reuses WorkHarbor.StrictJSON.decode_object/1 and Foundation.Request.normalize/3,
normalize_config/1 and Foundation.Canonical.bytes/1. Go reuses wirejson.Parse and
CanonicalBytes plus accepted config.CanonicalPath; independently validate V3 request
and lexical repository behavior. Both perform bounded native sizing before canonical
serialization. Wire runtime_id maps explicitly to native bindings id, preserving both
roles and null slots. Request.normalize adds owner_id only from trusted scope for hash
comparison. Native create/rebuild execution_config_snapshot equals request config while
wire execution_config remains null; start uses native frozen snapshot. Absent observation
maps to native nil; optional defaults match Foundation.finish, preserving exact nil/type
strictness. Terminal result comparison uses outcome plus normalized native observation.

T006 Accounts.Principal is separately credential-derived; Accounts.authorize currently
has an auth-only closed registry. Do not widen it or alias lifecycle mutation to read
status. T007 takes a distinct trusted peer/context, performs no authentication/Repo
lookup/mutation, and does not call accept/claim/reserve_candidate/finish. command_id and
approval expectations are supplied, not persisted or manufactured from T005 rows.

## Diagnostics and evidence

Fixed codes/messages only: invalid_json, input_limit, unsupported_version,
invalid_message, unauthorized_scope, identity_conflict, stale_intent, stale_runtime,
invalid_status, transport_refused. No raw input/field values/URL/handle/credential/grant/
provider exception/decoder message. Correlation logs require validated authorized context;
validators emit no logs. Protocol rejection is not fabricated provider completion.

Independent shared raw fixtures/expected dispositions and canonical/hash anchors run
in both suites, alongside native tests: closed shape/null/version/action; escaped nested
duplicates, UTF-8/surrogates/floats/trailing/depth/digit limits; counter narrowing, improper
lists/types/cycles/native budget; all scope/op/key/hash/intent/runtime/role/status mismatches;
same-generation stale results, borrowed candidates, changed config/provider, queued
success/terminal overwrite; pending/completed/historical retries and alternate IDs/new
intent same generation; observation defaults/removal rules; admitted-envelope overflow
without identity change/partial emission; secret canaries. A test provider sentinel must
remain untouched for rejected/replayed/refused inputs. It proves pure gate order, not a
real dispatcher, durable restart/race, TLS, browser or Docker capability.
