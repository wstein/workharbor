# Pre-ready amendment: bounded impact inventory

Owner direction, 2026-10-20: implement the new requirements without premature
historical migration/backward-compatibility obligations; schema versions remain
useful for future evolution. This inventory records the change, not review CLEAR,
full-feature approval, implemented init, provider acceptance or release evidence.

## Current validation and identity stay

The current manifest schema_version 1/2 branches, strict number/type rules, closed
field sets, examples and vectors stay. The host resolver still admits only its
current v1/four-field contract until separately authorized T035 after T009/T028.
No task dependency or admitted provider input changes. Output schema_version 1,
contract_versions reporting, profile_version and product/build identity stay.
Host/session protocols, runtime generation/intent guards, content hashes, stale
input checks and actual Git/backend capability/minimum-version checks stay.

Current contract/table/vector tests and required CI identities stay. In particular,
a freeze.test_ids inventory binds current acceptance coverage: its name does not
make it dispensable historical-version machinery. No F6 source/test change or
required-ID deletion/rename is part of this amendment. No vector count is lowered.

## Pending init and template requirements change

There is no scaffold/wizard/upgrade implementation under apps/whr at the inspected
base `db7746872b9726d06df151f2dcd201e6cad86e05`; this is a normative/test-spec
change with no runtime behavior delta.

- T030/FR-057(g): new manifests use v2. Existing valid v1/v2 inputs follow ordinary
  file preservation/proposal/edit consent: no requested edit preserves existing bytes;
  requested edits preview the complete current-format (v2) replacement and require
  per-path consent. No automatic version bump, forced default materialization,
  version-specific refusal, special defaults-only v1 mode or separate upgrade
  workflow is added. Unchanged bytes are a no-op; unwritten defaults are never
  reported as persisted. A v2 proposal/output remains not creation-ready until T035.
- T036/FR-064: keep numeric version/hash markers and strict marker integrity. Keep
  no-follow/FIFO/size/malformed checks, outside bytes, consent, preview, dry-run,
  stale-preview guards, rollback and no-op. An intact block differing in version or
  body is different_template; before readiness a higher number has no permanent
  preservation promise. Replacement still requires explicit consent. No historical
  template reader/upgrade matrix is added. SC-022 retains lower/higher/current-
  version, hash-edit, malformed, size, PTY, race and safety cases under this policy.

The output schema's optional settings_not_persisted property is reserved and is
not emitted by pre-ready init. Its validation shape remains; only its description
changes. Future T036 still owns its planned action/warning schema extension.
The output schema description no longer mandates a new number for every change;
any actual shape/semantic change still needs coherent reviewed source/contract/
test changes. Version fields enable later evolution and are not removed.

## Existing 008 and provenance boundaries

Feature 008's current profile values, numeric metadata/type checks, deterministic
rendering/hash, security canaries, refusal/capability conformance and stale-input
checks stay. Its profile rule no longer requires a new profile_version solely for
pre-ready compatibility. OD-1–OD-5 remain recorded decisions; OD-5 is resolved through Feature 009 DP-4/AP-8.
Feature 008 stays Draft and cannot start implementation without its actual
single-task/exact-head approval gates. This amendment overrides no entry guard.

Feature 006 migration.md and contracts/migration are Python-to-Go port/parity
provenance and semantic regression evidence, not a deployed-data upgrade engine.
They, ADR 0010, their parser/path/canonical-byte/hash corpus, source expectations
and independent anchors are unchanged. There is no new migration project.

ADR 0013's current codes/ranges, mapping and passthrough tests stay. Its pre-ready
change procedure removes only mandatory supersession for breaking historical
numbers; H77's separate remote-unreachable semantic ADR requirement remains.
No code, test implementation, CI policy, host/provider behavior, build, publication
or live-operation authority is changed by this documentation amendment.
