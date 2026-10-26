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
The initial test checkpoint used main `54c4b271745fe78800a523607dd00a9124d22f01`;
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

Actual CI at `7516a5e9d994fc426de3d62970b4ed2fcffd8523` stopped at
[`mix format --check-formatted`](https://github.com/wstein/workharbor/actions/runs/40137093087/job/121001704694).
It emitted two source formatting changes: split the control corpus concatenation
and insert a blank line before the denial-count return. Both exact changes were
applied; a native formatter pass on the correction remains pending CI. Application
compilation and ExUnit semantic RED were not reached by that run.

Actual local results: `python3 scripts/check_repo.py`, changed-test `typos` and
`git diff --check` passed. The initial scoped `MIX_ENV=test mix test` invocation for
`owner_shell_test.exs`, `owner_auth_test.exs`, `authentication_test.exs` and
`transport_security_test.exs` with `--warnings-as-errors --seed 0` exited **127**:
`mix: command not found`. Mix/Elixir/Erlang were absent from PATH and inspected
known installed-tool locations; no installation/download was attempted. At that
local checkpoint, semantic RED outcomes, compilation and formatter results were
**unverified**. Predicted assertion failures included
the missing LiveView marker, root response, unescaped payload controls, scaffold
socket path, CSP and data-image icon CSS. Existing revoked/expired admission and
foreign-query cases are regression guards expected to pass on main, not RED proof.
The database snapshot detects writes; only the static source boundary guards domain
reads. Process termination and stable audit counts check observable revocation
stopping without claiming inspection of queued state in an already-dead process.

The negative asset contract requires unknown/static-adjacent requests to return a
denial without raising; literal and encoded dot segments both permit 400. During
W1 implementation, flash dismissal must use client-only `JS.hide`, or the shell
must omit `flash_group`; its existing `JS.push("lv:clear-flash")` does not qualify
for an exception to the unchanged no-push assertion.

No standalone build, browser, host/runtime operation or full `make check` was run.
Implementation waits for the separate TDD/RED-head instruction; no W1 or T012
completion is claimed.

### Native CI checkpoint and Host-fixture correction

[CI run 40138712728, bootstrap job](https://github.com/wstein/workharbor/actions/runs/40138712728/job/121006872530)
executed exact head `c798df00fa1974d9169c202b8b8c8815b136b665`. Native format,
warnings-as-errors application compilation, asset setup and asset build passed
there. ExUnit reported **222/241 passed, 19 failed**; the overall gate failed.
Reading all 19 failure records gives:

| Classification | Count | Actual evidence |
| --- | --- | --- |
| Semantic assertion RED | 3 | Client expects `/owner/live` but source uses `/live`; root expects 303 but returns 200; `/owner/auth` controller route is absent. |
| Invalid Host-header fixtures | 16 | `Plug.Conn.InvalidHeaderError`: one asset test, all nine owner-shell tests, and six owner-auth tests fail before their relevant assertions. |

The Host failures are test defects, not W1 behavior evidence. In particular,
FR-022, shell authorization, CSP, forged events and revocation assertions have not
yet produced native outcomes. The new fixture correction sets connection authority
fields and supplies the literal Host header through `conn.req_headers`, matching
the existing authentication fixture instead of Plug's protected Host setter.
Production admission and all security assertions are unchanged. Local source,
spelling, whitespace and required-inventory checks pass; native formatting,
compilation and ExUnit for the fixture correction remain pending CI. The preceding
native passes apply only to the recorded historical head. No local tool install,
standalone build or product implementation follows from these three narrow REDs;
remaining behavior waits for working fixtures and the separate implementation
instruction.

### HTTPS fixtures and authorized W1 implementation (2026-10-25)

[CI run 40141642719, bootstrap job](https://github.com/wstein/workharbor/actions/runs/40141642719/job/121015021901)
executed `ea5c97da12cb0f6530582db46f3d1e83c6f5a5b5`'s corrected Host fixtures.
Native format, warnings-as-errors compilation and asset build passed; ExUnit
reported **224/241 passed, 17 failed**. The protected Host-setter exceptions were
gone. Classification of all 17 failure records:

| Classification | Count | Evidence |
| --- | --- | --- |
| Earlier semantic RED, retained | 3 | Client socket path, root 303 response and missing `/owner/auth` controller route. |
| Additional semantic RED | 4 | Missing assets, route verbs, raw FR-022 U+0000 and absent owner-auth module. |
| HTTP fixture admission blocks | 10 | Relative ConnTest dispatch resets the scheme to HTTP; owner-positive requests receive 403, including the CSP test. These do not prove the intended shell assertions. |

The expired/revoked admission test also passed without proving its intended HTTPS
path: HTTP admission rejected the fixture first. The hostile HTML/attribute test
did execute and pass. The absolute HTTPS fixture correction now preserves the
production TLS/authority/origin guards and asserts a positive 200 response before
checking owner-page CSP. The cause follows pinned
[Phoenix ConnTest dispatch](https://github.com/phoenixframework/phoenix/blob/v1.8.15/lib/phoenix/test/conn_test.ex#L212)
and [Plug test connection construction](https://github.com/elixir-plug/plug/blob/v1.20.3/lib/plug/adapters/test/conn.ex#L7),
whose relative-URI default is HTTP.

The owner subsequently authorized W1 implementation in this same PR with new
commits only. The implementation provides the protected links-only `/owner`
LiveView, root 303 alias, separate `/owner/auth` controller forms, existing guarded
`/owner/live` transport, strict owner CSP, exact two-asset allowlist and encrypted
cookie namespace separation. Page events ignore forged payloads without logging;
the page has no application mutation handlers, domain reads or writes. Private
credential admission state is revalidated on navigation and a one-second server
timer; the first denial cancels the timer, clears the principal and redirects.
No future host/workspace/trust routes, storage, lifecycle controls or dependencies
were added. The original 18 required W1 test identities remain unchanged.

The credential-to-DOM boundary was checked against pinned LiveView 1.2.12 source:
[Static.load_session and rendering](https://github.com/phoenixframework/phoenix_live_view/blob/v1.2.12/lib/phoenix_live_view/static.ex#L43)
keep the default user mount-session map separate from the merged server cookie
session, and only the former is signed into `data-phx-session`.
[Channel mount](https://github.com/phoenixframework/phoenix_live_view/blob/v1.2.12/lib/phoenix_live_view/channel.ex#L1207)
merges the server-side transport session for mount. This implementation supplies no
explicit mount-session override and keeps the principal in socket private state.
The shell test verifies the rendered signed token and rejects a `credential` key.
The existing custom OwnerSocket and encrypted HttpOnly cookie remain unchanged.

Needed icons now use fixed inline SVG geometry; theme buttons use text labels.
The stylesheet removes both icon-mask and component generators that can emit
image data URLs. The required test inspects the actual generated CSS bundle for
`data:image`, in addition to source/markup guards; no passing bundle result on the
new implementation is claimed before CI. The root has no inline script or style.
Theme changes and flash dismissal use client-only operations under the strict
policy; the WSS source comes from the validated configured origin, never Host.

### W1 accessibility checklist and verification limits

Source review covers landmarks, one primary heading, ordered owner navigation,
skip link, named theme buttons and dismiss controls, visible keyboard focus,
responsive wrapping and system/light/dark CSS variables. Navigation and buttons
declare at least 44px target height; theme/dismiss buttons also declare 44px width.
These declarations exceed the plan's 24px minimum, but browser dimensions have
not been measured.

A Python calculation of the committed hex variables used WCAG sRGB linearization
and `(lighter luminance + 0.05) / (darker luminance + 0.05)`. Each cell below is the
minimum against both page and surface backgrounds; these are source-palette
ratios, not computed browser styles or a full accessibility audit.

| Foreground / purpose | Light | Dark | Threshold |
| --- | --- | --- | --- |
| Main text | 15.55 | 13.40 | 4.5 |
| Muted text | 7.24 | 9.89 | 4.5 |
| Links | 7.23 | 8.80 | 4.5 |
| Error text | 7.94 | 7.73 | 4.5 |
| Focus outline | 7.23 | 8.80 | 3.0 |
| Control/surface borders | 4.55 | 3.08 | 3.0 |
| Opaque dismiss X / button background | 16.27 | 13.40 | 3.0 |

Independent static QA found that the dismiss X's former `opacity-40` reduced its
light-mode contrast to 2.45:1 despite the palette's compliant opaque colors.
The icon now renders at full opacity, inheriting the button's base-content color
on its base-200 background in both themes. The existing icon test renders a flash,
requires the dismiss SVG and rejects opacity utilities or inline styling on that
control. A source calculation reproduced the former failing alpha blend and the
new opaque ratios above. These results do not replace browser measurements.

Actual local `python3 scripts/check_repo.py`, scoped `typos`, `git diff --check`,
the 18-identity inventory comparison and read-only source-boundary checks passed.
The context guard distinguishes module calls/imports from the visible `Hosts`
heading. These checks do not execute ExUnit or verify the generated bundle. Mix, Elixir,
Erlang and Node are absent from PATH and inspected known locations; `mix test`
and `node --check` attempts exit 127. No tools were installed. Native format,
warnings-as-errors compile, asset build, generated-bundle checks and ExUnit for
the new implementation remain **unverified pending CI**. Historical native passes
above apply only to those exact earlier test heads. Full `make check` and
independent exact-head review are separate coordinator checks.

The named **W1 owner browser walkthrough** remains unverified. On an independently
owner-authorized TLS host and browser, record the exact source head, browser and
origin, then check:

1. Owner login, `/` to `/owner` 303, overview and `/owner/auth` navigation; inactive
   future sections, controller-only credential/logout forms and CSRF refusal.
2. Native WSS join/reconnect and CSP console; only the two self-hosted assets,
   visible inline icons, no data-image requests or blocked inline script/styles.
3. Keyboard-only skip/navigation/theme/dismiss focus order, accessible names and
   pressed states; light, dark and system changes; computed contrast and target
   dimensions at 320px, 768px and 1280px without horizontal overflow.
4. Connection-loss/stale banner, recovery, mounted expiry and revocation redirect;
   a single denial audit without a repeating loop. Inspect no token in rendered
   signed session or application browser storage, without recording credentials.

No server, browser, host workspace, relay, local daemon or provider operation was
activated for this handoff. No W1, T012, feature or browser acceptance completion
is claimed by these source changes or historical CI evidence.

### W1 reserved-event ingress decision and test-first boundary

The owner decided on 2026-10-25 that the accepted forged-event guarantee includes
reserved `lv:*` names. There is no exception for `lv:clear-flash`: a forged event
must not clear flash, crash the page or log its name/payload. The stock pinned
[LiveView 1.2.12 dispatcher](https://github.com/phoenixframework/phoenix_live_view/blob/v1.2.12/lib/phoenix_live_view/channel.ex#L550)
handles clear-flash and raises for unknown reserved names before page hooks, so
client-only `JS.hide` alone could not meet that guarantee.

The prepared implementation was preserved at local archive ref
`archive/w1-pre-ingress-f9ee51f5` (`f9ee51f57372c4acf38ee98df9c44700649d278b`).
Published owner head `9db261f55bd6ccb11c32153ec1c56180f382a3cc`, including its
owner-authored main merge, was retained as an ancestor. The seven prepared own
commits were appended as new commits; their complete range-diff was seven `=`
entries. No owner or published commit was rewritten, and the owner's checkout
was untouched.

Test-first commits `b9824830a581ca64c3ab8f4665cfce865e9ceedc` and
`c89115932faf2776926797e8c4f8efd217aecfbd` precede the ingress implementation.
Two additional required ExUnit identities bind transport-entry coverage; the
original 18 identities are retained. One oracle feeds serialized application
envelopes to the actual `OwnerSocket.handle_in/2` callback and requires unchanged
transport state, no channel delivery, no reply, no DB/audit changes and no event
name/payload canaries in debug logs. Cases include reserved/ordinary names,
missing inner fields, component/upload metadata, arbitrary decoded payloads,
multiple topics and a stock V2 binary push frame. A second oracle admits an
HTTPS owner page, connects the actual OwnerSocket, joins its signed LiveView
session through serialized transport frames, checks heartbeat replies, preserves
a test-installed flash canary under forged frames, and permits ordinary leave.
Direct application-event page catch-all coverage remains separate because
LiveViewTest event helpers bypass transport ingress.

The scoped command `MIX_ENV=test mix test test/workharbor_web/owner_shell_test.exs
--warnings-as-errors --seed 0` exited **127**, `mix: command not found`, before
the filter was written. No executed native RED or green is claimed for these new
tests. Source inspection proves the predecessor dispatch path reaches the pinned
reserved-event mutation/raise; that evidence is distinct from executed tests.
Native formatting, compilation, generated assets and the full required ExUnit
results still require CI on the final candidate.

The filter uses the supported
[Phoenix 1.8.15 transport callback](https://github.com/phoenixframework/phoenix/blob/v1.8.15/lib/phoenix/socket.ex#L323)
and [negotiated serializer dispatch](https://github.com/phoenixframework/phoenix/blob/v1.8.15/lib/phoenix/socket.ex#L553).
It discards all valid decoded outer application `event` envelopes before the
channel sees them, returning unchanged state without acknowledgement or logging.
It delegates other protocol frames to Phoenix; dashboard and agent transports
remain unchanged. Stock decode failures for undecodable wire frames remain
protocol behavior, not a fallback that forwards forged application events.
The native test protocol join is not a browser or real-WSS claim; the existing
TLS/WSS suite, owner browser walkthrough and exact-head independent review gates
remain separate and unverified on this new candidate.

## W2 owner hosts page (2026-10-26)

The read-only hosts package uses the accepted
[frontend plan](../specs/006-whr-cli/contracts/web-frontend-first-increment.md)
and shared authorized reads. Its
[package evidence](quality/web-w2-hosts-20261026.md) distinguishes executable
source tests from owner-browser acceptance. Existing W1 authentication, strict
CSP, inline SVG and transport policy remain in force. No task completion is claimed.

Owner read pages accessibility checklist: the hosts page uses a named section,
heading, list and labeled pagination navigation, visible status words and existing
light/dark CSS variables. Real-browser keyboard order, focus, target dimensions,
320/768/1280-pixel reflow, computed contrast and stale/reconnect behavior remain
unverified until an owner-authorized browser journey. Source tests do not establish
those measurements.
