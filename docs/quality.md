# Quality register

Reviewed: 2026-10-14. Owner: project maintainer.

| Area | Evidence | Status / next gate |
| --- | --- | --- |
| Spec Kit | Upstream CLI-generated assets, pinned provenance | Scaffold ready |
| Domain and ownership | Constitution, ADRs, contracts and accepted foundation/auth/protocol | Lifecycle/provider integration pending |
| Repository harness | Links/JSON/boundary checks plus negative regression tests | Executable |
| Feature 004 local automation | [T017 evidence](../specs/004-speckit-symphony-automation/quickstart.md#t017-combined-local-acceptance): 422 qualified cases at `ebe4833`; raw 41/five failures separately | T001–T017 local tooling/fixture acceptance; T018–T020 pending, live guard/provider/isolation/safe-label gates blocked |
| Product runtime | Accepted foundation/auth/protocol, HTTP1 mitigation and bounded T008 transport at64541c1; evidence below | Lifecycle/provider/browser/installable-pilot acceptance pending |
| Docker / persistence / PTY | Acceptance scenarios only | Real-host tests required |
| Local bundle reference drift (#143) | [Bounded offline evidence](exec-plans/active/package-d143-bundle-drift.md); JSONC pins and fake-Docker report tests | Reference alignment only; final qualified checks, independent review and real-host evidence remain separate gates |
| Symphony | Pinned workflow and operator runbook | Not installed or activated |
| Resource footprint | Bounded T008 loopback TLS fixtures measured below | No capacity/load acceptance |
| License | EUPL-1.2 project license; upstream MIT notice retained | Recorded |

Do not upgrade a design status to implemented without evidence. Review this register
with every release; convert recurring drift into small, targeted fixes.

## T008 genuine host transport evidence (historical predecessor)

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
candidate/Linux run and final independent acceptance were pending at this predecessor
milestone. The accepted corrected runtime is recorded below.

## T008 accepted corrected transport scope

Implementation/runtime source **64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c** is
independently Sol CLEAR with no open material code findings. The later documentation
head retains identical exercised source/config/dependency/journey/input blobs;
Linux executed 64541c1, not that later SHA. Author pristine proof:
`/private/tmp/workharbor-t008-validation-dho78ci4/evidence.json`; independent:
`/private/tmp/workharbor-t008-independent-645-kb8o8fqu/summary.json` and
`/private/tmp/workharbor-t008-independent-linux-645.json`.

| Platform / check | Actual result | Time |
| --- | --- | --- |
| macOS full native suite, author / independent | 173 / 173 passed, including 12 real TLS/WSS journeys each | 94.0s / 94.4s |
| Go application gates | 240 host / 162 API / 1 CLI events; required IDs retained | Exact command logs above |
| Isolated Ubuntu transport | 12 passed | 81.7s ExUnit, 83.573s unit |
| Isolated Ubuntu HTTP1/TLS security | 9 passed | 4.7s ExUnit, 8.33s unit |

Linux versions were Ubuntu 24.04.5 LTS aarch64, kernel
`7.2.9-orbstack-00391-g2b298d05fbd4`, systemd/libsystemd0 `255.4-1ubuntu8.17`,
OTP `29.1.1` (erts17.1), Elixir `1.20.4` compiled OTP29 and Go `1.27.2` linux/arm64.
The two exact test commands match the historical commands above, using the corrected
archive SHA256 `1ff20623b9b1bafe5adcce9487a597784f655187409a7b3c68a4727d387f012a`
and manifest `a8ebba13d392ccf85375796628d04d0456379875b23cd3b46c79c3fcbdcd60c4`.
All 352 source file hashes/modes matched before preparation and after final cleanup.
Actual assessment and all five pinned input digests:
`/private/tmp/workharbor-t008-linux-absent-pk-plan/amended-run/transport/assessment.json`.

Production Go enrollment, heartbeat/reconnect, renewal/activation/revocation and
pending crash recovery after predecessor expiry passed over genuine Elixir/Go
TLS/WSS, together with the complete refusal/expiry/wire/HTTP1 matrix recorded above.
New durability tests cover 40 Go syscall-fault cases across initial persistence and
all four active/pending transitions, with fresh-process reloads; native issuer tests
cover partial writes, sync/close/link/open failures and exclusive complete-state
recovery. Original native ignored-close failure had actual 18/19 RED before the fix;
19/19 identity tests now pass. Missing test seams are separately recorded as setup/
instrumentation absence, not executed behavior RED.

An earlier corrected-source Linux attempt stopped with pretest guard 125 because
PolicyKit was absent; zero tests ran and its owned guest was deleted. Retained
assessment: `/private/tmp/workharbor-t008-linux-rerun-plan/amended-run/transport/assessment.json`.
The successful rerun used an independently reviewed narrow absent/nonactivatable
PolicyKit proof: typed AccessDenied on empty properties of the exact owned loaded
unit, unchanged required properties, measured UID/groups/capabilities and nonwritable
cgroup ancestors. No policy installation or guard/cap relaxation occurred.

Each sequential unit bounded the CP and all Go/build descendants together to
512 MiB, one CPU, 600 seconds and control-group cleanup. Actual peaks were
269557760 and 161996800 bytes; 321 and 23 live/postexit samples plus final OOM counters
were zero. The dedicated UID/EUID was 999, sole group 990, with zero effective/
permitted/ambient capabilities; 12 ancestor paths were nonwritable. Root guest
2 CPU/2 GiB limits were measured. The configured 16 GiB disk exhaustion was not
independently measured on shared backing. Optional NoNewPrivileges and
ProtectControlGroups were actually **no** and are not claimed enabled. This is
bounded fixture evidence, not broad isolation, loaded capacity or production hardening.

Owned guest `01M4Y58JYJB5CEVCRHQD81R0ZP` was deleted at 2026-10-14 21:37:44 UTC and
inventory was empty. Outer context/config hashes and original BuildKit ID/image/
rwvolume matched and all six bounded probes passed in the observation window;
historical hang continuity remains UNVERIFIED. Already-unloaded transient-unit stop
exit 5 and UID helper broken-pipe output remain classified in the assessment.
Docker is absent/N/A to this TLS-only guest. Bandit 1.12.5 remains affected under
the S001 HTTP1-only mitigation. Full canonical Python is deferred to the combined
head; provider, browser stale-display, five-workspace/eight-attachment load, installer,
release and complete installable-pilot acceptance remain pending. Feature001 markers
and frozen history are unchanged; only Feature006 T008 is newly checked.

## W1 owner shell test-first checkpoint (2026-10-24)

The owner accepted the exact frontend plan at `7c64789c` in
[PR #242](https://github.com/wstein/workharbor/pull/242), merged at
`f2d96805383410f401826877b634d2c03780e977`, and authorized W1 after merge.
This checkpoint is based on main `54c4b271745fe78800a523607dd00a9124d22f01`;
[PR #251](https://github.com/wstein/workharbor/pull/251) records its current exact
head and CI evidence. Option A is settled: future pages and HTTP controllers share
one authorized read service. W1 has a links-only overview and no domain reads.

The test-first checkpoint binds 18 required ExUnit identities covering hostile
FR-022/HTML flash text, wrong-kind credentials inside the owner cookie, rejected
owner-ID injection, mounted expiry/revocation, first-denial stopping, preserved
controller forms, root redirect, exact route verbs, inactive future links, forged
events, unchanged database/audit, debug-level payload logs, strict headers/CSP,
narrow assets and actual generated CSS icon policy. Existing authentication
coverage follows the forms to `/owner/auth` and accepts CSRF from controller forms
or the LiveView meta element. Tests assert existing route/component behavior before
connection, without absent product modules as compile-time dependencies. No product
code has changed.

[The initial test review](https://github.com/wstein/workharbor/pull/251#pullrequestreview-5640119134)
identified assertion/coverage defects, including the LF check against structural
HEEx whitespace. Corrections inspect only unique payload spans, preserve visible
placeholder checks, and strengthen route/header/asset/logging/escaping coverage.
Source was restructured by hand to conventional formatter style. That is not a
native formatter pass: CI may still stop at `mix format --check-formatted` before
semantic tests. The review's acceptance verdict is separate from future W1 runtime
acceptance.

Actual local results: `python3 scripts/check_repo.py`, changed-test `typos` and
`git diff --check` passed. The initial scoped `MIX_ENV=test mix test` invocation for
`owner_shell_test.exs`, `owner_auth_test.exs`, `authentication_test.exs` and
`transport_security_test.exs` with `--warnings-as-errors --seed 0` exited **127**:
`mix: command not found`. Mix/Elixir/Erlang were absent from PATH and inspected
known installed-tool locations; no installation/download was attempted. Semantic
RED outcomes, compilation and formatter results remain **unverified**, with
associated PR CI as the next evidence source. Predicted assertion failures include
the missing LiveView marker, root response, unescaped payload controls, scaffold
socket path, CSP and data-image icon CSS. Existing revoked/expired admission and
foreign-query cases are regression guards expected to pass on main, not RED proof.
The database snapshot detects writes; only the static source boundary guards domain
reads. Process termination and stable audit counts check observable revocation
stopping without claiming inspection of queued state in an already-dead process.
No standalone build, browser, host/runtime operation or full `make check` was run.
Implementation waits for the separate TDD/RED-head instruction; no W1 or T012
completion is claimed.
