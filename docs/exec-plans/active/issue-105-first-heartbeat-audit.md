# Issue 105: first authenticated heartbeat audit

Scope: narrow maintenance of Feature 006 T008, tracked by
[issue 105](https://github.com/wstein/workharbor/issues/105) and
[PR 234](https://github.com/wstein/workharbor/pull/234). The authoritative behavior
is the [host enrollment contract](../../../specs/006-whr-cli/contracts/host-enrollment.md#live-expiry-and-reconnect).
T009–T024 dispatcher/relay implementation and broader feature acceptance remain separate.

## Decision and implementation

Socket open and hello leave liveness unknown. The first accepted heartbeat writes
one `host_connection_authenticated` row with fixed action `host_authenticate`,
outcome `connected` and channel `internal`. The existing immediate transaction
contains host timestamps, lease, connected-slot update and audit insertion.
The per-connection `seen` field controls exactly-once emission; persisted slot state
cannot cause duplicate authentication events. First-seen state and heartbeat
acknowledgement remain after successful commit. Audit failure retains the existing
fixed persistence error and conservative connection close.

Implementation source: `dc53dae623410a265030dfef5426a773529f4ac5`.
No schema, migration, public API or dispatcher change is needed.

## Executed red phase

Tests-only commit `ee5f4b442bb7cd5d3de43040788bd16ba1f09225` preceded implementation.
[CI run 40104730558](https://github.com/wstein/workharbor/actions/runs/40104730558)
stopped at `mix format --check-formatted`; it did not establish failing behavior.
Formatting-only commit `4f74d93300d66f27e8dd999fd39c926cdae40388` applied exactly
the two formatting changes printed by CI, preserving test source tokens.

[CI run 40106406359](https://github.com/wstein/workharbor/actions/runs/40106406359)
executed that exact tests-only head: ExUnit reported **219/223 passed, four failed**.
All four failures were the intended new assertions:

- First accepted heartbeat returned no authentication row instead of the fixed row.
- Rate-refusal coverage found zero initial authentication rows instead of one.
- Replacement coverage found zero initial authentication rows instead of one.
- An audit-insert failure trigger was never reached: heartbeat returned a successful
  acknowledgement instead of `{:error, :persistence_failed}`.

The remaining new tests cover unnegotiated, malformed, foreign, stale, expired and
revoked first heartbeats without authentication events. The positive tests also
exercise steady/catch-up traffic, persisted-slot recovery,
replacement fencing, rate refusal and transactional audit-failure rollback of host
last_seen/lease, conservative unknown slot, peer removal and refusal of the old ref.

## Green verification evidence

[CI run 40109341317](https://github.com/wstein/workharbor/actions/runs/40109341317)
succeeded at historical head `067ed3f8ffa08b5db53de53ae3933b7e5812e284`, including
**223/223 ExUnit tests**, application formatting/build/test and the required check
gate. This executed result follows the genuine four-assertion red phase above.

The [Opus review](https://github.com/wstein/workharbor/pull/234#pullrequestreview-5638962109)
reviewed that same historical head, accepted the implementation and identified this
stale evidence section as its sole actionable finding (F1); its verdict was
NOT CLEAR pending that documentation correction. This update records the missing
CI and review links and removes the obsolete pending-verification instructions.

The issue branch was subsequently rebased from
`c71890d27c3d415e50fe15610a6f8f044b313142` onto
`179005e6faf50aa356a38d61fd88a79121f96534`; all five issue patches compared equal
with `git range-diff` before this documentation-only correction. The linked CI and
review cover `067ed3f8`, not the rebased documentation head. Fresh candidate checks
and review remain separate evidence; historical success is not transferred to a
new SHA.

The local environment has no Mix; no local ExUnit or formatter pass is claimed.
No tool installation, standalone build, Docker, actual-host or browser run was
performed locally. Available repository static validation and `git diff --check`
passed during authoring. The initial local `make check` was interrupted during
Python checks at the coordinator's direction and is incomplete, not passing.

No real-host/browser, deployment, load or broader feature completion follows from
this bounded maintenance verification.
