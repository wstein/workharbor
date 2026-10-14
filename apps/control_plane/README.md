# WorkHarbor control plane

The Phoenix/Ecto/SQLite control plane supplies durable metadata and an owner
authentication foundation. Lifecycle dispatch, providers, host enrollment and
product management/Spec Kit data views remain later work. The locked dependencies
and root `.tool-versions` define the tested toolchains.

Verify from this directory without starting a server:

```sh
mix deps.get --check-locked
mix format --check-formatted
mix compile --warnings-as-errors
mix test
```

Tests use file-backed databases and `server: false` by default. The bounded
transport suite starts disposable loopback TLS listeners with generated test
certificates; authentication requests use a shared sandbox transaction. Foundation tests
include independent writers and bounded SIGKILL/reopen evidence; these do not
establish hardware power-loss or live-platform acceptance.

## Authentication foundation

[The authentication contract](../../specs/006-whr-cli/contracts/authentication.md)
defines primary secret delivery, opaque scoped credentials, strict raw JSON and
browser admission. Local `WorkHarbor.Accounts.bootstrap/1` takes a protected
one-time delivery callback; it persists hashes only and stays initialized if
delivery fails. There is no HTTP bootstrap/reset flow. Do not print the delivered
secret, place it in a URL/argument or put it in repository configuration.

`/login/owner`, `/login/dashboard` and `/login/read-token` supply bounded no-script
forms. Owner and dashboard have separate selected cookies; public login uses a
third preauth cookie with server-checked 600-second expiry. Owner management can
explicitly install dashboard sign-in while retaining its owner session. Dashboard
sign-in cannot mutate, mint or revoke credentials. “Clear dashboard sign-in” only
clears that cookie; “Sign out of owner management” revokes the selected owner
credential. Raw read-token issuance is limited to the CLI bearer secret channel.

No listener starts by default. An independently authorized operator activation
requires `PHX_SERVER=true`, `WORKHARBOR_ORIGIN`, `WORKHARBOR_BIND`,
`WORKHARBOR_TLS_CERT_FILE`, `WORKHARBOR_TLS_KEY_FILE`, and a private non-demo
`SECRET_KEY_BASE`; production also requires `DATABASE_PATH`. Direct TLS origin,
authority and listener port must agree. HTTP, proxy-forwarded TLS, debugger,
reloader and demo signing secrets are rejected for an active server. Every active
environment uses the narrow explicit HTTP/1-only Bandit profile and rejects generic
listener/handler/ALPN overrides. Locked Bandit 1.12.5 remains affected by
[GHSA-69m6-wcv4-rwcf](https://github.com/mtrudel/bandit/security/advisories/GHSA-69m6-wcv4-rwcf);
[the transport contract](../../specs/006-whr-cli/contracts/transport-security.md)
records this temporary mitigation and the separate review required to re-enable
HTTP/2 after a released upstream fix. This document
records configuration requirements, not permission to activate or deploy.

The inherited root welcome test retains its required ID and now verifies the
intentional plaintext rejection. Authenticated root navigation has separate
positive coverage. ConnTest and socket adapter tests establish application gates,
not actual network TLS or browser acceptance. The separate native transport suite
proves TLS certificate/hostname verification, HTTP/2 refusal, HTTP/1 authentication
and WSS admission on ephemeral loopback listeners. It does not establish a deployed
listener, browser walkthrough, real-host enrollment or pilot completion. Assets retain the pinned Mix wrappers;
no new runtime dependencies or Python/AI validation subprocesses were added.
