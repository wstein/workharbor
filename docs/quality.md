# Quality register

Reviewed: 2026-10-14. Owner: project maintainer.

| Area | Evidence | Status / next gate |
| --- | --- | --- |
| Spec Kit | Upstream CLI-generated assets, pinned provenance | Scaffold ready |
| Domain and ownership | Constitution, ADRs, contracts, feature requirements | Design only |
| Repository harness | Links/JSON/boundary checks plus negative regression tests | Executable |
| Feature 004 local automation | [T017 evidence](../specs/004-speckit-symphony-automation/quickstart.md#t017-combined-local-acceptance): 422 qualified cases at `ebe4833`; raw 41/five failures separately | T001–T017 local tooling/fixture acceptance; T018–T020 pending, live guard/provider/isolation/safe-label gates blocked |
| Product runtime | Pinned Phoenix/Go applications; accepted006 foundation/auth/protocol and HTTP1 mitigation; T008 exact candidate tests below | Lifecycle/provider/browser/installable-pilot acceptance pending |
| Docker / persistence / PTY | Acceptance scenarios only | Real-host tests required |
| Symphony | Pinned workflow and operator runbook | Not installed or activated |
| Resource footprint | Bounded T008 loopback TLS fixtures measured below | No capacity/load acceptance |
| License | EUPL-1.2 project license; upstream MIT notice retained | Recorded |

Do not upgrade a design status to implemented without evidence. Review this register
with every release; convert recurring drift into small, targeted fixes.

## T008 genuine host transport evidence (candidate, not completion)

Exact predecessor source `e42fe27ec353d262dcd2dd3496e9fbc98cc56937` has independent
fresh source/manifest/format/vet/build/compile/assets/full-test proof:
`/private/tmp/workharbor-t008-independent-zxtay0dp/summary.json`; author evidence
`/private/tmp/workharbor-t008-validation-psmexjz6/evidence.json`. Both full native
runs passed172Phoenix tests including12 genuine macOS Elixir/Go TLS/WSS journeys,
plus198host/162API/1CLI Go events. Python846collection/520required retained; full
canonical Python suite was not repeated for this WI. All prior IDs and frozen
Foundation migrations/Feature001tasks remain unchanged.

The source-only Linux archive SHA256 is
`c87eae400c6cbffe838fc19a517efd13060babde505f27ad4c244f2446694acd`;
manifest SHA256 `771b5ad98856248e37a0076b7a0b6eb2e8b2a24bf0d35f21d32b592aece6783b`.
Authorized isolated Ubuntu24.04 arm64 guest ran OTP29.1.1, Elixir1.20.4-otp29 and
Go1.27.2 linux/arm64, with both endpoints on guest loopback. Exact commands:
`MIX_ENV=test mix test test/workharbor_web/host_transport_test.exs --warnings-as-errors --seed 0`
then `mix test test/workharbor_web/transport_security_test.exs --warnings-as-errors --seed 0`.
Actual Linux results:12/12 in81.2s and9/9 in4.5s. Their512MiB aggregate control groups
peaked at216391680 and141455360bytes respectively. All349 transferred file hashes
and modes matched before preparation and after tests. Evidence:
`/private/tmp/workharbor-t008-linux-bootstrap-1qylii8h/amended-run/transport/assessment.json`.

Production Go enrollment, hello/heartbeat, worker-death reconnect with a new
non-null connected slot, stale after30s, renewal/activation/revoke socket fencing,
idle certificate expiry, and pending crash recovery after retired predecessor
expiry were exercised over real TLS/WSS. Actual negative cases cover absent,
pending, foreign, unregistered, retired, revoked and expired identities, wrong
server root/name, exact wire bounds/types/scope/version, owner-bearer refusal and
HTTP2 refusal with owner HTTP1 preserved. Explicit certificate presentation plus
actual401 or named remote TLS alert is required; generic dial failure is not
server refusal. Controlled near-expiry issuance and discarded activation ack are
labeled fixture injections; TLS peer verification is genuine. Native test P256
server trust preflight corrected OTP RSA SPKI fixture incompatibility without
weakening production trust. Recoverable masked raw bytes in OTP crash reports
were fixed with narrowly scoped primary filtering and checked after the filter
across gen_server/proc_lib events; fixed error events and UID-child broken-pipe
teardown output are retained.

The owned guest was deleted and inventory empty. Outer Engine/original BuildKit
responded afterward; historical hang continuity remains UNVERIFIED. This first
Linux run omitted effective UID/groups and ancestor cgroup privilege measurements;
no complete no-escape isolation proof is claimed. Docker is absent/N/A for TLS-only
transport; no Docker workspace/provider, browser, loaded capacity, release or pilot
acceptance follows. Independent review also required additional deterministic
filesystem durability/restart tests; those change runtime blobs, so a fresh exact
candidate/Linux run and final independent acceptance remain pending. T008 is unchecked.
