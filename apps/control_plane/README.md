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

Tests use private file-backed databases and `server: false`. Foundation tests
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
reloader and demo signing secrets are rejected for an active server. This document
records configuration requirements, not permission to activate or deploy.

The inherited root welcome test retains its required ID and now verifies the
intentional plaintext rejection. Authenticated root navigation has separate
positive coverage. ConnTest and socket adapter tests establish application gates,
not actual network TLS or browser acceptance. Assets retain the pinned Mix wrappers;
no new runtime dependencies or Python/AI validation subprocesses were added.
