# WorkHarbor control-plane scaffold

Generated with the official Phoenix 1.8.15 installer, retaining Ecto/SQLite,
HTML, LiveView and assets. No mailer, dashboard, Gettext or generated agent
instructions are included. Unused DNS clustering is omitted for the initial
single-instance SQLite control plane. This is a development scaffold, not a usable platform;
no owner API or lifecycle/provider behavior exists yet.

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
