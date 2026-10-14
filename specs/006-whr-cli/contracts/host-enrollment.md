# T008 host enrollment and connection contract

Status: consolidated V2 independently Sol DESIGN CLEAR, including the final
empty-subject enrollment CSR addendum. Implementation GO follows accepted
T005 1189c81, T006 ef399f5, T007 e10c6997 and S001 c88f339. This document
freezes one proposal before tests/behavior; it claims no implementation evidence.
Author: assigned Sol author; independent reviewer: sol_enrollment_review.

## Authority and scope

Feature006 T008 retains Feature001 T004/T014: five-minute atomic one-time
enrollment, encrypted scoped identity, rotation/revocation, ten-second heartbeats,
monotonic unknown at30seconds, real-host handshake/reconnect and later browser
stale display within five seconds. Feature006 FR020/021 and FR035-038 apply.
T009 trust/provider and T010 durable effects/reconciliation remain separate.

Reuse the explicitly configured direct HTTPS listener/server identity. Owner API
/api/v1 keeps login as its only owner-authentication exemption. Separate agent
protocol paths are POST /agent/v1/enroll, POST /agent/v1/identity/renew,
POST /agent/v1/identity/activate and GET /agent/v1/connect (WSS). First enrollment
uses code+CSR; renewal uses current active mTLS; activation admits only its exact
pending/current replacement certificate; connect requires active mTLS. No owner
bearer, browser cookie, body ID, authenticated flag or forwarded certificate is
host authority. Compare actual TLS peer certificate against persisted assignment.
Reject Cookie, Authorization, Origin, Sec-Fetch-* and X-WorkHarbor-Context on agent
paths. Require direct TLS, configured effective/raw authority, canonical exact
path, no query, bounded upgrade headers and no redirects. Run agent ingress before
BrowserSession; preserve owner/browser Origin/CSRF/store behavior and safe errors.

Owner host operations use explicit Accounts actions host_enrollment_issue,
host_revoke and host_status with target-owner association checks before disclosure;
never alias credential revoke/status. Routine renewal needs no owner bearer.
Owner-triggered rotation is optional presentation; automatic scoped renewal is the
required rotation path. Closed API routes: POST /api/v1/hosts/enrollments accepts
schema_version=1 and host_id null-or-UUID; POST /api/v1/hosts/:id/revoke accepts
schema_version=1; GET /api/v1/hosts/:id returns owner-scoped status. Mutations use
owner-write, status uses read; dashboard cannot mutate. Secret code delivery is
private HTTPS response/no-store, never diagnostic/log/CLI output.

## Narrow S001 extension

The accepted Bandit HTTP1-only mitigation remains mandatory: Bandit.PhoenixAdapter,
exact http_1_options=[enabled:true], http_2_options=[enabled:false], explicit
boolean activation, no HTTP/redirect, demo secret, debug or reloader. Existing
HTTPS allowlist ip/port/certfile/keyfile/http_1_options/http_2_options may gain only
cacerts (one validated internal issuer DER), verify=:verify_peer and
fail_if_no_peer_cert=false as one complete optional-peer profile. Reject partial,
duplicate, malformed or different values and all generic handler, transport,
ALPN, plug or arbitrary overrides. Client roots are loaded from validated native
issuer state, never token-selected input. No HTTP2 re-enable/dependency clearance
is claimed. Existing owner-only profile remains valid when identity is inactive.

## Closed wire and decoding

Enrollment JSON has exactly schema_version=1, code and csr. Code is whe1_ plus
43 canonical unpadded base64url characters representing32random bytes. csr is
canonical standard-base64 DER, at most4096decoded bytes. Renewal contains exactly
schema_version=1 and csr; activation exactly schema_version=1. Raw bodies<=16384
bytes, enforced before shared strict decoding. JSON rejects duplicates, unknown
fields, malformed UTF8/surrogates, floats, wrong types, trailing input and bad
base64 before mutation. Certificate/issuer DER each<=4096bytes; whole response
<=16384bytes. Response fields exactly schema_version,owner_id,host_id,identity_id,
certificate,issuer_certificate,expires_at (integer UTC seconds equal leaf NotAfter).

WSS raw/canonical messages<=4096bytes; one strict JSON text object per message,
no compression. Shapes are closed/all-required:
- hello: schema_version=1,kind=hello,owner_id,host_id,capabilities.
- hello_ack: schema_version=1,kind=hello_ack,owner_id,host_id.
- heartbeat: schema_version=1,kind=heartbeat,owner_id,host_id.
- heartbeat_ack: schema_version=1,kind=heartbeat_ack,owner_id,host_id.
IDs are canonical lowercase UUIDs. Capabilities are unique [] or [docker]; T008
emits [] until T009 measured integration. Intervals are fixed contract constants.
Compare asserted scope against enrolled peer assignment before acknowledgement.
Negotiate exact version1 only after authentication; no unauthenticated hello or
downgrade. Unknown/unsupported fields/versions reject in both languages. Add a
separate connection schema/fixture; existing T007 lifecycle validators stay pure
and unchanged. Use StrictJSON.decode_object and Go wirejson behind smaller bounds.

## Native issuer and installation

Use OTP public_key/crypto Ed25519 issuer and Go crypto/ed25519,x509,TLS, with no
OpenSSL runtime or signing-service dependency. An explicit future T023 installer
calls the native initializer once with an operator-selected private directory.
Create one exclusive atomic closed versioned issuer-state file with self-signed
CA certificate and PKCS8 private key, service-owned0600 in0700 directory. CA=true,
certSign usage, validity5*365days. Restart auto-loads that exact state; never create
or replace it implicitly. Missing/malformed/unsafe/invalid profile/signature,
key mismatch, not-yet-valid, expired or <24h remaining issuer blocks active identity
startup with fixed remediation. Native initializer tests use temporary fixtures;
no deployed initialization is performed by this implementation task.

Server TLS public/OS roots or explicit private SERVER CA are separate operator
inputs. Agents must verify configured CP hostname and roots before secrets and
never learn server trust from enrollment response/internal client issuer.

Leaf: Ed25519, CA=false, digitalSignature and clientAuth only, issuer-generated
serial, CN host UUID. Authority remains exact fingerprint+database status. Fixed
24h leaf: NotBefore=floor(UTC issuance seconds), NotAfter=NotBefore+86400; require
issuer>=24h remaining, never silently shorten. No expiry grace or backdating;
now>=NotAfter rejects. Require synchronized host/CP clocks; validity skew fails
closed with fixed clock/identity remediation, no insecure override. Response leaf
must match saved key, issuer/profile/validity, expected known assignment and host
CN, with expires_at exactly leaf NotAfter, before local acceptance.

Initial CSR has EMPTY subject, version0, Ed25519 key/signature and no requested
extensions. Verify possession; derive host/owner only from code-bound record.
Initial pending host/owner stay null until verified HTTPS response; compare leaf
CN to returned host_id. Renewal CSR uses known host UUID subject and no requested
extensions; compare to current mTLS assignment. Unknown CSR attributes/extensions
are diagnosed, never silently admitted. No enrollment descriptor is used.

## Atomic enrollment and revocation

Persist only domain-separated SHA256 code hash, owner/host binding, issued_at,
expires_at=issued_at+300, consumed_at/revoked_at. At exact expiry reject. Owner nil
target allocates new stable host metadata and audit in the same immediate code
transaction; existing target must belong to owner and be non-revoked. Do not nest
Foundation.create_host (it rejects nested transactions); use its existing record
constraints inside the T008 top-level transaction.

Agent saves pending key+CSR before redemption. One short top-level immediate
transaction checks code AND host revocation, consumes it, persists active
fingerprint/assignment, retires any prior/pending identity on explicit same-host
re-enrollment, and appends fixed audit. Commit before response; one independent
writer wins. Expired/consumed/revoked codes issue/replay nothing. Lost response
preserves pending key and reports incomplete; explicit owner newcode for SAME
non-revoked host recovers. Revoked host is retained tombstone: issuance/redemption
cannot resurrect it; explicit new enrollment has a new host ID.

Issue/redeem/renew/first activation/revoke use serialized immediate transactions.
Revoke atomically tombstones host, invalidates all codes/current/pending identities,
and audits. Revoke first prevents later issuance/activation; issuance first is
invalidated by later revoke. Delayed response confers no continuing authority.
Database negatives enforce no reactivation after revocation. Fence active/retired
sockets and recheck DB identity on every incoming/outgoing application message and
upgrade. No claim to reverse an already executed provider effect.

## Renewal and crash recovery

Automatic current-active-mTLS renewal begins with<=6h remaining under enrollment's
standing policy, without owner credentials or repeated approval. Persist one pending
replacement linked to exact predecessor and CSR SHA256. Exact staging retry under
valid active old cert returns same public certificate; different CSR conflicts.
Expired/retired/revoked old cert cannot stage, including historical result lookup.

First activation through pending mTLS checks non-revoked host, exact pending/current
predecessor and BOTH old/new cert valid; atomically promotes new, retires old and
audits. Pending cert authorizes only activation, never connect/heartbeat/renew or
history. Retry with now-current valid new cert returns immutable already-activated
success even after old expiry. It cannot revive expired/revoked new identity.

One atomic host identity.json contains closed version, endpoint/server-trust
reference, known owner/host assignment, active key/cert/issuer, pending null or
key/CSR/cert/issuer/predecessor identity. Initial assignment may be null and cannot
connect. Persist pending key/CSR before stage and returned cert/issuer before first
activation. Retain BOTH old/new until activation acknowledgement, then atomically
promote pending. Startup pending cert first attempts activation/replay; pending
CSR-only retries stage under valid active cert. Thus CP promotion followed by lost
ack/local crash recovers via saved new key. First staging/activation after old expiry
fails and requires explicit owner newcode recovery. Preserve all state and report
failure; never claim success from a local write alone.

Installed Linux path /var/lib/workharbor/identity/identity.json, service-owned0600
in0700 directory outside workspaces/Git/containerenv. Atomic writes require
restrictive create, file fsync, rename and parent fsync. Reject symlinks, unsafe
ownership/mode, malformed keys/certs and unknown private schemas; never reset or
regenerate silently. No private key leaves host.

## Live expiry and reconnect

Every admission/incoming/outgoing message checks min(leaf,issuer) expiry. Compute
monotonic close deadline at authentication from remaining validity; backwards UTC
change cannot extend it. Force close at deadline; <=1s watchdog catches forward
clock jumps on idle sockets. No message at/after wall expiry is admitted before
watchdog close. Agent mirrors expiry and stops expired active/pending use.

Heartbeat immediately after hello then every10s; acknowledge each heartbeat.
Without authenticated heartbeat for>=30s mark unknown and close stale socket.
Only transport reconnect uses bounded1/2/4/8/16/30s backoff then30s maximum; no blind
provider resends or command-history resets. Health requires authenticated hello
and heartbeat acknowledgement. UTC last_seen/lease are display only.

## Fenced conservative liveness overlay

Preserve Foundation terminal results/runtime history, desired state, counters,
provider IDs/config and its no-repeatable-observation-API scope. Add host connection
slot (scoped host, current server-generated connection UUID or null, unknown/connected)
and runtime freshness annotation (runtime ID+immutable generation, workspace intent,
connection UUID). Connection UUID is internal CAS fencing, never a wire token,
cryptographic authority or lifecycle identity.

Native HostConnections open(verified_peer_context), receive_hello(peer_ref,hello),
heartbeat(peer_ref,heartbeat), mark_stale(peer_ref,lease_ref), project_runtime(owner,
runtime), record_fresh_observation(peer_ref,runtime,generation,intent). Actual TLS
admission creates opaque context; no body IDs manufacture it. One worker serializes
current socket per host; fresh connection UUID is persisted before heartbeat.
Each authenticated heartbeat replaces opaque ephemeral timer lease_ref.

Projection is unknown unless authorized association, current live worker/socket,
same persisted connection UUID, active non-revoked valid identity, heartbeat age<30s,
and annotation matching connection+current runtime generation+workspace current
intent all hold. Missing worker/persistence failure/annotation gives unknown, never
historical running. Heartbeat/reconnect cannot freshen runtimes. Startup invalidates
slots unknown/null before admission; worker restart invalidates its old slot. Old
annotations cannot match after restart/reconnect; no history is overwritten.

Stale requires matching current socket/peer_ref/lease_ref and age>=30s, then
immediate CAS current connection UUID, unknown liveness and fixed audit. Close
stale socket, reject late heartbeat, require new authenticated connection. Old timer
cannot overwrite fresh reconnect. Persistence failure keeps worker projection
unknown and reports fixed failure, never claims durable audit success.

record_fresh_observation annotates only: immediate transaction checks current
live valid peer, fresh heartbeat, authoritative owner/host/workspace/current runtime,
exact generation and workspace intent, then writes annotation+fixed audit. It
accepts no state/provider/config payload. T010 invokes ONLY after actual inspected/
current guarded observation commits, never heartbeat/cached terminal replay/copied
body IDs. Crash before annotation remains unknown; intervening intent/connection
change rejects. T008 tests explicit metadata fixtures, not provider reconciliation.
T010 must review first-create admission/local ledger and actual observation path.

T007 Peer/Snapshot remain separate: Elixir kinds :control_plane/:host, Go strings;
T008 constructs only authenticated peer scope. Missing authoritative operation
snapshot continues fail closed. Never create Snapshot by copying incoming command.
Reconnect/rotation preserve command IDs, op/key identity, intent and generations.

## Offboard and redaction

T015 confirms revocation through T006 owner-authenticated host status/idempotent
host revoke, including rerun after browser revocation. Revoked cert gains no broad
confirmation bypass. CLI owner credential remains separate private CLI profile,
never agent identity state/environment. Without confirmation keep agent stopped,
retain recoverable credentials/data and report incomplete/nonzero; absence is not
successful offboarding. Default project data retention remains unchanged.

Audit every persisted connection/heartbeat/identity/freshness mutation with fixed
server-known IDs/action/outcome/code only; respect accepted scope constraints and
one-time operation audit uniqueness. Never raw codes, keys, CSR/certs, requests,
URLs, names, provider payloads, SQL or exceptions in logs/reports/audit. Secret input
is private file/stdin/pipe; T015 owns CLI integration. Error bodies are fixed.

## Verification and resource boundary

Failure-first tests: independent SQLite writer single-use and issue/redeem/renew/
activation-vs-revoke races; expiry300s and cert/issuer boundaries; direct SQL no
resurrection; issuer init exclusivity/restart and bad/near-expiry startup; real CSR
key/host/root/EKU/algorithm negatives; file mode/owner/symlink/schema/fsync/rename
failures; every active/pending crash boundary and lost activation acknowledgement;
new-current replay after old expiry versus first activation after old expiry;
open socket expiry/clock changes; monotonic29.999/30/30.001s stale, replaced sockets/
timers, restart, fresh annotations and wrong intent/generation; strict shared
fixtures/bounds; credential sentinel redaction. No test can treat fixtures as
cryptographic/provider proof.

Genuine ephemeral macOS Elixir HTTPS/WSS + Go client tests must exercise enrollment,
hello/heartbeat, socket loss/reconnect, rotation, old/pending/revoked/expired refusal,
expiry/fencing and server root/name failure while preserving S001 HTTP1-only ALPN.
Separately recreate an authorized isolated Ubuntu OrbStack machine, both genuine
Elixir and Go endpoints INSIDE guest, no host-network isolation bypass, explicit
source inputs/pinned Linux toolchain/private fixtures, actual real Linux handshake/
reconnect/rotation/revoke and full cleanup. Generic prior smoke/teardown is evidence
at /private/tmp/workharbor-systemd-fixture-iyZ4ILpH, not T008 acceptance. Review
resource plan before activation;2CPU/2GiB/16GiB was smoke sizing, not load capacity.
Systemd is for the Linux host, never workspace images. No persistent/deployed runtime
is authorized. Later browser<=5s, provider<=30s and five-workspace/eight-attachment
load remain mandatory; mocks/macOS loopback never replace Linux evidence.

Run all scoped Go/Phoenix application gates, unchanged required-ID inventory,
source/manifest/repository/diff checks and fresh pristine exact-candidate checks,
then same independent Sol reviewer fixes/CLEAR. T008 stays unchecked until exact
acceptance; Feature001 markers/frozen migration remain unchanged. Full canonical
Python is reserved for final combined head unless helper/policy concern arises.
No push/PR/release/merge/deploy in this WI.
