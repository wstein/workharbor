# S001 authenticated transport mitigation

Status: finite design independently Sol CLEAR on 2026-10-14 before tests/behavior;
implementation candidate complete; exact-candidate acceptance pending. This follow-up blocks T008.
It preserves the owner API, HTTPS origin/authority, browser sessions, WSS and CLI
contracts. It changes the current listener protocol profile only.

## Advisory and boundary

Locked Bandit 1.12.5 is affected by
[GHSA-69m6-wcv4-rwcf / CVE-2026-105659](https://github.com/mtrudel/bandit/security/advisories/GHSA-69m6-wcv4-rwcf)
(the dependency warning identifies EEF-CVE-2026-105659). Verified on 2026-10-14:
the advisory lists patched version TODO, [Hex](https://hex.pm/packages/bandit)
still lists 1.12.5 as latest, and the upstream
[changelog](https://github.com/mtrudel/bandit/blob/main/CHANGELOG.md) puts the fix
under Unreleased. The official mitigation disables HTTP/2 using
`http_2_options: [enabled: false]`. The locked dependency remains affected; this is
a deployment-profile mitigation, not a dependency security clearance.

No unreleased dependency pin, upstream source copy, reverse proxy, host enrollment,
listener deployment or persistent service is introduced. Native tests may start
only disposable loopback TLS listeners with generated test certificates and cleanup.
Real host/browser pilot acceptance and signed-release SC-006 remain separate gates.

## Current authenticated profile

Every actual PHX_SERVER profile (dev, test and prod) uses Bandit.PhoenixAdapter,
HTTPS only, and explicit `http_1_options: [enabled: true]` plus
`http_2_options: [enabled: false]`. Exactly one explicit `server: false` is
inert; exactly one `server: true` requires the complete secure profile. Missing,
nonboolean or duplicate activation settings are rejected because Phoenix treats
truthy values as active and missing values can inherit `serve_endpoints`.
RuntimeSecurity rejects active configurations that omit or alter these exact lists,
use another adapter, contain duplicate HTTPS keys, or contain HTTPS keys outside
`ip`, `port`, `certfile`, `keyfile`, `http_1_options`, `http_2_options`.
The existing secret/debug/reloader/plaintext/origin/private-key checks remain.
The production port remains 1..65535; port 0 is exclusively a test adapter fixture.

Phoenix passes its HTTPS options directly to Bandit. Low-level ThousandIsland
handler, transport and ALPN overrides can bypass the ordinary protocol switches;
the narrow current profile therefore refuses them rather than trusting defaults.
Future T008 may extend the HTTPS allowlist only with separately reviewed narrow
client-CA / optional peer-certificate verification options. It must preserve this
HTTP/1-only protocol restriction and cannot allow generic handler/ALPN overrides.
No mTLS behavior is implemented by S001.

## Required failure-first evidence

Independently constructed valid active configuration and each real runtime env
must pass. Missing/nonboolean/duplicate activation, missing/enabled/malformed/duplicated protocol settings, wrong
adapter, and handler/transport/ALPN override attempts must fail closed. Preserve
all prior required Phoenix, Go and Python IDs; append meaningful transport IDs.

Actual Phoenix-adapter TLS on an OS-assigned loopback port must prove dual ALPN
selects HTTP/1.1, h2-only never negotiates h2, and HTTP/2 prior-knowledge prefaces
close both without ALPN and with HTTP/1.1 ALPN. The effective bound production
configuration is validated before traffic probes. Verified certificate/hostname
HTTP/1 requests must preserve CLI bearer login/status, browser form sign-in and
owner/dashboard WSS upgrades; missing/foreign origin, wrong CSRF, wrong selected
cookie and revoked credentials remain denied. No synthetic ConnTest result is
reported as actual TLS evidence.

Run focused and full application gates, source/manifest checks and pristine exact
candidate verification, then obtain independent Sol review for that exact SHA.
The root reserves full canonical Python execution for combined acceptance unless
a changed runner/policy or concrete regression concern requires an earlier rerun.

## Re-enable conditions

HTTP/2 remains disabled until a separately scoped change pins a released upstream
version documented as fixing this exact advisory, reconciles current advisories,
updates the contract and protocol tests, passes exact-candidate application / real
TLS checks, and receives independent review. Publication or deployment still needs
its own existing authorization. A TODO patched-version field or an unreleased main
commit does not satisfy this condition.
