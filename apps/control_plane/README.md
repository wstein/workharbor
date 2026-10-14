# WorkHarbor control-plane scaffold

Generated with the official Phoenix 1.8.15 installer, retaining Ecto/SQLite,
HTML, LiveView and assets. No mailer, dashboard, Gettext or generated agent
instructions are included. Unused DNS clustering is omitted for the initial
single-instance SQLite control plane. This is a development scaffold, not a usable platform;
no authenticated HTTP owner API or lifecycle/provider execution exists yet.

The root `.tool-versions` pins the tested toolchains. `mix.lock` pins every
resolved Hex dependency and the heroicons/daisyUI Git revisions. The esbuild and
Tailwind executable versions are pinned in `config/config.exs`; assets use Mix
wrappers rather than an npm dependency graph.

From this directory, verify without starting a server:

```sh
mix deps.get --check-locked
mix format --check-formatted
mix compile --warnings-as-errors
mix test
mix assets.setup
mix assets.build
```

Tests use a local ignored SQLite database and `server: false`. Generated
development secrets are public local defaults, not deployment credentials.
Production authentication, TLS policy and installation remain later work items.
The generated Phoenix welcome page is retained unchanged; no browser acceptance
of product UI is claimed.

`assets/tsconfig.json` supplies editor completion with Mix dependency paths; its
generated comments were removed to comply with the repository's strict JSON gate.


## T005 metadata foundation

Native contexts under lib/workharbor/foundation/ enforce scoped SQLite records,
canonical request identity, separate intent/runtime counters, atomic audit and
metadata result guards. The accepted contract is
[Feature 006 owner API](../../specs/006-whr-cli/contracts/owner-api.md).
There is no dispatcher/provider or raw HTTP decoder. Start requires an existing
current runtime with known effective config; replacement-start remains T010.

Foundation tests use separate private file-backed databases and independent writer
connections, not wrapping Sandbox transactions. They include bounded subprocess
SIGKILL/reopen evidence and effective per-writer durability pragmas. These tests
are native persistence evidence, not live platform or hardware power-loss acceptance.
The existing scoped migration formatter covers priv/repo/migrations/*.exs.
