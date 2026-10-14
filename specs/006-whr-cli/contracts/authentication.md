# T006 owner authentication and browser security

Status: accepted design, implementation pending independent exact-revision review.
Prerequisite: T005 independently CLEAR at
`1189c81ba81b79a47a0485a9d5148fb8286c8eb0`.
This consolidated contract supersedes all earlier single-cookie, automatic
mode-switch/revocation and forced-reauthentication proposals. It preserves
Feature 001 T013 and Feature 006 FR-033/035–038.

## 1. Scope and authority

The pilot has one local owner authenticator. Existing Owner identities, foreign
keys and cross-owner guards remain authoritative. Trusted principals are separate
from native request bodies. Authentication does not confer host identity, trust,
provider capability or session grants. T007 and later work remain separate.

## 2. Local bootstrap

Local bootstrap generates 32 cryptographically random bytes, encoded as canonical
43-character unpadded base64url. A database singleton constraint atomically binds
one authenticator to an existing or newly created Owner. Only a domain-separated
SHA-256 verifier is stored. The secret is delivered once to a protected local
adapter, never HTTP, argv, URL, standard output or logs. If delivery fails after
initialization, preserve initialized state and fail closed for explicit local
recovery. Never silently replace an owner or verifier. Arbitrary user passwords,
email flows, cloud accounts and passkeys are not supported. Test-only delivery
artifacts do not establish an installed setup workflow.

## 3. Credentials and lifetimes

Opaque credentials are `whr1_` followed by canonical unpadded base64url of 32
random bytes: 48 ASCII characters. Store only a distinct domain-separated SHA-256
hash, owner foreign key, fixed kind/scopes, issued/expiry/revocation timestamps.
Kinds are `cli_owner` (owner read/write, 30 days), `owner_browser` (owner read/write,
eight hours) and `dashboard` (dashboard read only, at most eight hours).
Clients cannot increase expiry, choose arbitrary scopes or change owner. Expiry
at or before now rejects, with no grace or sliding renewal. Recheck the database
on every authenticated HTTP request and socket upgrade. Fresh primary login
creates a fresh credential; **no login or wrapping automatically revokes any
previous credential**. Explicit owner-write revocation and logout govern lifetime.
Status contains metadata only. Credential references never enter page scripts.

## 4. Primary login and action authorization

Anonymous `POST /api/v1/auth/login` accepts exactly `schema_version: 1`,
`owner_id` and `owner_secret`, and issues only `cli_owner`. It is a no-credential
CLI channel: no Authorization, auth cookies, Origin, Fetch-Site or context header.
A known dashboard credential calling this or any other mutating API receives an
authorization denial and safe audit before body errors can mask it. Other mixed
credential channels reject without issuance. Wrong owner/secret share one safe
401 response, including a dummy constant-time comparison for unknown owners.
Browser primary login is a separate explicit fresh-primary-proof flow.

A principal contains server-derived owner/credential IDs, kind and fixed scopes.
Body/query owner fields never override it. Explicit closed action classification
(read/write/grant) precedes lookup and side effects; HTTP method alone does not
classify a grant as read. Unknown actions/scopes fail closed. Cross-owner calls
return safe denial and no foreign data. Audit denial is security evidence, not
permission for a business mutation.

## 5. Two authentication contexts, three stores

Auth cookies are `__Host-workharbor_owner` and `__Host-workharbor_dashboard`.
A third `__Host-workharbor_login` cookie is **preauth only**, never an owner
credential. All are encrypted and signed, use distinct salts, Secure, HttpOnly,
SameSite=Strict, Path=/ and no Domain. Auth Max-Age never exceeds stored expiry.

Paths fix HTML/login/socket context. Cookie API calls require exactly one
`X-WorkHarbor-Context: owner|dashboard`. Missing/duplicate/unknown context rejects;
a supplied header on a path-fixed route must match its target context. One HTTP
session dispatcher invokes Plug.Session once for the selected store. Never union
scopes, decode another store as fallback or borrow owner authority when both
cookies are present. The database checks selected credential kind as well as scope.
This shared origin is **not** an XSS compartment; rendering and CSP remain required.

## 6. Public navigation, preauth and dashboard provisioning

Public GET `/login/owner`, `/login/dashboard` and `/login/read-token` select only
preauth state. Missing Origin is allowed even from a cross-site portal. Explicit
foreign/null/malformed Origin rejects. Public login returns only public forms,
ignores supplied auth for data and never touches either auth cookie on GET. This
preserves sign-ins withheld by SameSite on cross-site navigation.

Preauth payload contains issued_at, expires_at = issued_at + 600 seconds and
library CSRF state, no credential. POST checks interval, future/malformed timestamps
and expires_at <= now using the injected clock; Max-Age alone is not replay defense.
GET may issue fresh preauth state without persistent credential mutation.

Protected primary browser login explicitly targets owner or dashboard. Existing
cookies are not mint authority. Fresh primary proof plus preauth CSRF issues a
fixed-kind credential, writes only the target auth cookie and clears preauth state.
No existing credential is revoked. Owner-write may explicitly issue/install a
fixed dashboard credential while retaining owner context. CLI issuance returns the
secret once through HTTPS private client handling. A viewer may wrap the **same**
existing read token at `/login/read-token`; this never mints, updates, revokes or
extends a credential and may coexist with the untouched owner cookie.

Auth-cookie installation/wrapping uses no copied CSRF state and no second session
fetch. Redirect to protected HTML; its ordinary selected-session safe GET initializes
fresh library CSRF state before API/socket use. Owner/preauth CSRF is never reused,
and the owner request's CSRF process state/before_send callback remains undisturbed.
Absent/expired/revoked dashboard context never falls back to owner. GET navigation
never issues credentials or changes persistent domain state.

## 7. Logout and clear

Owner-browser logout: **Sign out of owner management**. Revoke only the selected
owner credential and clear only owner cookie. CLI logout revokes its bearer; CLI
local deletion is T011. Dashboard credentials cannot mutate even their own token,
logout, issue credentials or mint grants. **Clear dashboard sign-in** at protected
`POST /login/dashboard/clear` selects the dashboard AUTH store (explicit override
to preauth login paths), validates own Origin and selected CSRF, and deletes only
that local cookie, with no implied server revocation or owner action. Explicit
owner-write dashboard-token revocation is separate.

## 8. Gate order

Remove generic endpoint body parsers. A stateless Endpoint.call admission guard
runs before super and hence before Phoenix socket dispatch: TLS, authority,
Origin/path/header admission only, no session/CSRF/database there. Normal HTTP
then runs request ID/telemetry, one selected Plug.Session, router/fetch_session,
credential authentication, explicit action scope, browser Origin/CSRF, bounded
body parser, handler and context object-owner check. Login credential admission
precedes parsing. No router pipeline is assumed to have run for socket dispatch.

## 9. TLS and authority

Direct HTTPS is first. Reject plaintext before body/auth; never redirect credential
requests. Active server configuration requires explicit bind, certificate/key,
allowlisted authorities/origin and a generated non-demo session key. No default
all-interface HTTP or trusted forwarded scheme/Host. Authorize effective host/port,
reject duplicate raw Host, and require any raw Host to parse/equal effective authority.
HTTP/1 origin-form requires Host; HTTP/2 authority need not have raw Host. Absolute
request-target schemes cannot establish TLS. Test authority mismatches explicitly.

## 10. Origin and anti-forgery channels

Origin must be exactly configured HTTPS scheme/host/effective port; reject null,
multiple values, userinfo/path/query/fragment and foreign origins. No permissive
CORS. Protected HTML/API/upgrades do not inherit public-login navigation exception.
Cookie API reads also require selected-session X-CSRF-Token; missing Origin is
permitted only with Sec-Fetch-Site=same-origin and valid CSRF. Browser mutations
require explicit own Origin and CSRF. Bearer-only CLI has no cookie, Origin,
Fetch-Site or context and cannot borrow browser authority. Protect login forms
with preauth CSRF. Public/sensitive HTML uses no-store, no-sniff, frame denial and CSP.

## 11. Sockets

`/owner/live` and `/dashboard/live` use constant respective session options and
check_csrf=true. Admission requires exact own Origin even for GET upgrades.
Phoenix decodes selected session/checks CSRF once; custom connect checks database
kind/scope/expiry/revocation before upgrade, never the other cookie. Disable longpoll.
Later views/attachments must reauthorize actions and enforce expiry/revocation in
their WIs; handshake protection does not claim their implementation. Browser and
actual TLS evidence is distinct from synthetic ConnTest scheme assertions.

## 12. Fixed login forms

Login pages contain no script (CSP script-src none). Isolated URLencoded parsing
accepts at most 4096 actual raw bytes and three pairs, UTF-8/absent charset only.
Owner/dashboard primary forms require exactly owner_id (canonical UUID),
owner_secret (43 characters) and _csrf_token (56 masked Plug characters).
Read-token form requires dashboard_token (48) and _csrf_token. Clear form requires
only _csrf_token. Reject missing/empty/extra, malformed percent triplets, decoded
invalid UTF-8, duplicate decoded names and unknown/nested names before map conversion.
Use stdlib component decoding only after percent validation, never collapsed query
maps. Forms are not an alternate API/lifecycle parser.

## 13. Strict raw JSON

Actual cumulative parse input is at most 1,048,576 bytes, with oversize precedence
before UTF-8/syntax. API accepts JSON only, rejects empty/nonobject/trailing input.
Preflight validates UTF-8/no BOM, at most 128 containers (root counts one), at most
4300 integer digits excluding minus and rejects fractions/exponents/nonfinite
before numeric conversion; string/escape contents do not count as containers.
Use locked Jason ordered_objects with string keys/copies, then reject decoded
duplicate keys recursively before native maps. Configure Jason integer digit limit
4301 at compile time before dependency build (its count includes minus).
No normalization, atoms or subprocess. Pass valid native bodies through T005
schema/FrozenJSON hashing, retaining scalar ordering/no LF. Safe errors contain
only code/optional offset, never Jason raw data/token.

## 14. Debug and secret boundaries

All authenticated server profiles compile debug_errors=false and disable debugger
and live-reloader bypasses. Active server rejects demo/missing session secrets.
Test actual debugger-action/exception paths. Sensitive schema fields are redacted;
SQL/log/error/audit contain no raw secret, token, cookie, body, grant or query
payload. Filter all auth parameters and suppress sensitive route logging. Audit
only bounded server-known actions/codes/outcomes and IDs/request IDs.

## 15. Evidence and completion

Write failing behavior tests before implementation. Cover singleton/races/delivery,
wrong-owner/secret, exact expiry/revocation/reopen, every mutation under read scope,
malformed login masking, both-cookie selection/no fallback, context/CSRF substitution,
public cross-site login preservation, target-only before_send cookies, independent
logout, viewer wrap without DB changes, transport/socket/debugger negatives, form
boundaries/aliases and frozen JSON plus independent byte/depth/digit/Unicode cases.
Use secret canaries through actual errors/logs/audit/database. Append meaningful
required test IDs without changing existing IDs. Run focused application/repository
and pristine exact-candidate gates, record actual results and independent review.
No synthetic test establishes real browser, transport TLS or future host capability.
