# Web frontend: first increment

Status: Step 0 proposal, 2026-10-24; documentation only. Sequencing condition met:
T028 [#238](https://github.com/wstein/workharbor/pull/238) and T029
[#239](https://github.com/wstein/workharbor/pull/239) merged; T030's honest-status record
was submitted as [#241](https://github.com/wstein/workharbor/pull/241).
T030/SC-018 remain open.

## Scope and approval

The owner requested this planning exception after the current T029/T030 PR:
read-only owner pages for host health, workspace status and enrollment/trust state,
using Phoenix LiveView in `apps/control_plane`, without a SPA. Automatic independent
**Opus** CLEAR for the exact plan and explicit owner approval of that reviewed
revision are prerequisites to W1. The API boundary is decided below; that decision
is not approval of the complete plan. Record review and plan approval
in the PR/tracker. W1–W4 are proposed packages, not authorized implementation.
Task dependencies, specification semantics and completion gates remain unchanged.

No lifecycle, enrollment-issuance, trust-approval, terminal, relay, deployment or
activation actions enter these pages. Local CLI use stays interactive and human
initiated on Docker Tier 1; no automatic local daemon bridge or lifecycle action.

## API and credential boundary

These owner-credential pages are distinct from the FR-032 Spec Kit dashboard
(specs/plans/tasks/review/workspace/agent evidence, read-scoped credential, T020).
`/dashboard` and its existing credential namespace/flows remain unchanged.
[H8/H24:a and FR-032/033/036](../spec.md) require one owner API for CLI/dashboard;
this increment claims neither their completion nor a second data API.

**DECIDED — owner, 2026-10-24, option A:** LiveViews and later HTTP API
controllers use the **same authorized read service in the control plane code**,
with identical scope, bounds and redaction; no LiveView-only read path. No new HTTP
endpoints are delivered here. This records the owner's decision in the #242
session; the exact reviewed plan still requires explicit owner acceptance before W1.
Option B (LiveViews consume HTTP owner API endpoints) was considered and rejected.

## Routes and authentication

| Route | Proposed destination |
| --- | --- |
| `/` | Redirect to `/owner`; test the alias. |
| `/owner` | Links-only overview throughout W1–W4; inactive/unavailable sections until pages land, no counts or data projections. |
| `/owner/hosts` | Host IDs, metadata, connection freshness and revocation. |
| `/owner/workspaces` | Workspace/host IDs, desired/observed state and recorded operation status. |
| `/owner/trust` | Enrollment metadata and exact-revision approval records. |
| `/owner/auth` | Existing `AuthController.page` owner-management forms, outside LiveView. |

Preserve existing POST `/owner/logout` and `/owner/dashboard` forms with their CSRF
checks, cookie effects and redirects at `/owner/auth`; link it from the shell.
Sign-in stays `/login/owner`. Reuse encrypted `__Host-` cookies, `RequireOwner` and
owner-kind socket sessions. Page admission is `:owner_page`, `owner_browser` only;
CLI bearer, dashboard and status-only credentials cannot admit owner pages.
Sources: [router](../../../apps/control_plane/lib/workharbor_web/router.ex),
[AuthController](../../../apps/control_plane/lib/workharbor_web/auth_controller.ex),
[Accounts](../../../apps/control_plane/lib/workharbor/accounts.ex).

Revalidate server-derived principals at disconnected/connected mount, navigation
and each refresh. Never take owner identity from browser parameters. Use one
server timer per mounted page, scheduled no later than 1 second after completion
of each refresh, including the links-only shell's authorization check. Reads have a 1-second batch
deadline, no overlapping refresh, and timeout means unknown/unavailable. No host
PubSub subscriptions: existing host topics are not owner-qualified. Read each page
through its scoped context helper; no background host commands. Preserve the
existing 30-second heartbeat threshold and 001/T015 display-within-five-seconds
of stale marking; injected-clock tests must prove this despite slow/error reads.

Run checks sequentially: page authorization, then scoped reads, short-circuiting
on the **first denial**. Cancel refresh, mark the session stopped, discard protected
assigns, disconnect/redirect to login and ignore queued ticks. Never authorize or
read again in that mounted session, including nested helpers after a denial.
`Accounts.authorize/3` audits denials: tests require at most one denial audit row
per mounted session, with no per-tick denial loop. Reconnect must authenticate
again; read errors become unavailable, not repeated authorization attempts.

## Transport and presentation security

Reuse `OwnerSocket` (`Phoenix.LiveView.Socket`) at `/owner/live`, correcting the
scaffold client's `/live`; keep long-poll disabled. W1 reconciles actual handshake
parameters with the strict query admission and adds only the shell's self-hosted
static asset paths. Retain TLS/Host/Origin, upgrade CSRF, cookie, no-store, HSTS,
frame denial, nosniff and referrer protections. Auth/API/agent routes retain their
existing policies; no credential URLs or new wire protocol.

Exact owner-LiveView-page CSP target:

```text
default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self' ${OWNER_WSS_ORIGIN}; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'
```

Expand `${OWNER_WSS_ORIGIN}` to the sole WSS origin derived from the validated
operator HTTPS origin (same host/effective port); never emit the placeholder or
request-derived authority. No wildcard or `unsafe-inline`. Verify the exact
expanded header admits only that WSS origin in the actual browser, including
browsers where `'self'` alone does not match WSS. Move inline theme logic to `app.js`. CSSOM property writes by LiveView/topbar differ from
inline `style=` attributes: forbid the latter and test both under the target CSP.
Assert exact owner-page headers, narrow asset serving and unchanged auth/API/agent
headers. Sources: [endpoint](../../../apps/control_plane/lib/workharbor_web/endpoint.ex),
[sockets](../../../apps/control_plane/lib/workharbor_web/owner_socket.ex),
[security](../../../apps/control_plane/lib/workharbor_web/security.ex),
[client](../../../apps/control_plane/assets/js/app.js).

Keep `img-src 'self'` exact: no `data:` image exception. The bundled
[heroicons plugin](../../../apps/control_plane/assets/vendor/heroicons.js) emits
`data:image/svg+xml` CSS mask URLs, incompatible with that policy. W1 adapts
`lib/workharbor_web/components/{core_components.ex,layouts.ex}`
(relative to `apps/control_plane/`, both already W1-owned): render needed icons
as static, vetted inline SVG markup, or omit decorative icons where text labels
suffice. Include theme-toggle, flash and connection-state icons; preserve accessible
names. Do not use data-image masks or fetch/inline untrusted SVG.

Data pages distinguish loading, empty, error, stale/disconnected and success.
Last-known data requires uncertainty and observation time; missing projections
never mean healthy/running or zero records. Apply [FR-022](../spec.md) visible
control escaping **and** HEEx HTML escaping to every untrusted name, identifier,
error and remote text; no raw HTML or executable Markdown.

LiveView pages contain no forms, mutating handlers or application mutation
channel pushes; standard authenticated LiveView join/diff frames remain. Their only
`handle_event` is a catch-all ignoring unknown/forged events without logging the
payload, calling authorization/read/write functions, crashing or changing DB/audit.
Theme selection uses client-only `JS.dispatch`; pagination uses plain GET links.
Existing controller authentication forms above are the explicit separate exception.

## Shared authorized reads

Views never call Repo, SQL, provider adapters or Docker. No new storage. Every
helper authorizes the named action against the principal's owner before querying;
`owner_browser` and `cli_owner` are allowed, `dashboard` denied for every new read.
Do not reuse broad `:config_read`. Page admission remains narrower than context
access so the same helpers can serve the later owner CLI API.

| Page / action | Existing source and proposed bounded helper |
| --- | --- |
| Overview / none | Links only; no domain reads or fabricated totals. |
| Hosts / existing `:host_status` | Add `Hosts` owner-host enumeration; existing `status/2` returns only IDs/revocation. Call internal unscoped `HostConnections.host_state/1` only after scoped ownership. Capabilities/last-seen are metadata, not provider-health proof. |
| Workspaces / new `:workspace_status_read` | Add owner-bound Workspace/Operation/RuntimeInstance reads in `Foundation`; no list exists. Use `HostConnections.project_runtime/2` (also requires owner-only `:host_status`) for conservative observation. Missing freshness means unknown; intent does not prove execution. |
| Enrollment / new `:enrollment_status_read` | Add scoped `Hosts` summaries of retained enrollment/identity/security rows; exclude codes, hashes, CSRs and certificates. |
| Trust / new `:revision_approval_read` | Add scoped `ConfigTrust` approval summaries. Existing `lookup/3` needs exact reference/descriptor; enumeration is absent. An approval record does not establish trust in current workspace input. |

Sources: [Hosts](../../../apps/control_plane/lib/workharbor/hosts.ex),
[HostConnections](../../../apps/control_plane/lib/workharbor/host_connections.ex),
[Foundation](../../../apps/control_plane/lib/workharbor/foundation.ex),
[records](../../../apps/control_plane/lib/workharbor/foundation/records.ex),
[ConfigTrust](../../../apps/control_plane/lib/workharbor/config_trust.ex).

Proposed bounds: default 25 rows, maximum 50 per page; stable ID ordering, positive
page numbers capped at 1,000. Permit only duplicate-free numeric `page`/`size`
queries (maximum 128 bytes) on these owner list routes only; reject unknown,
duplicate, oversized and invalid/out-of-range values before reads. Preserve fixed
auth/API/agent/socket query admission and never allow credential query parameters. Retain only the current page:
maximum 50 records for hosts/workspaces and 50 each for enrollment/approval sections,
plus fixed loading/error metadata; never accumulate streams or previous-page rows.
Read errors/unavailable helpers block the affected package or display unavailable;
never mutate backend state. Preserve authentication/denial/domain audit events;
refreshes create no lifecycle/trust mutation records or secret-bearing logs.

## Small PR sequence

Paths below are relative to `apps/control_plane/`; braces enumerate exact files.
Each package has its own issue/branch, focused Conventional Commits and one active
owner of shared files. Start dependents from merged prerequisites; stack only for
real code dependencies, at most two levels. W1 registers only implemented overview
and controller routes; future navigation stays inactive until its module lands.

| Package / dependency | Files and acceptance |
| --- | --- |
| W1 / plan gates | `lib/workharbor_web/{router.ex,endpoint.ex,security.ex,owner_socket.ex,auth_controller.ex}`, `lib/workharbor_web/live/{owner_auth.ex,overview_live.ex}`, `lib/workharbor_web/components/{layouts.ex,layouts/root.html.heex,core_components.ex}`, `assets/{css/app.css,js/app.js}`, `test/workharbor_web/{owner_auth_test.exs,owner_shell_test.exs,authentication_test.exs,transport_security_test.exs}`. Links-only shell, management forms, alias, authentication and CSP proof. |
| W2 / W1 | `lib/workharbor/hosts.ex`, `lib/workharbor_web/live/hosts_live.ex`, `lib/workharbor_web/{router.ex,security.ex,components/layouts.ex}`, `test/workharbor/host_identity_test.exs`, `test/workharbor_web/{live/hosts_live_test.exs,transport_security_test.exs}`. Activate hosts route/navigation; scoped reads, query bounds and stale timing. |
| W3 / W2 | `lib/workharbor/{foundation.ex,accounts.ex}`, `lib/workharbor_web/live/workspaces_live.ex`, `lib/workharbor_web/{router.ex,security.ex,components/layouts.ex}`, `test/workharbor/{foundation_test.exs,accounts_test.exs}`, `test/workharbor_web/{live/workspaces_live_test.exs,transport_security_test.exs}`. Activate workspace route/navigation; new read action, bounded recorded state and conservative projection. |
| W4 / W3 | `lib/workharbor/{hosts.ex,config_trust.ex,accounts.ex}`, `lib/workharbor_web/live/trust_live.ex`, `lib/workharbor_web/{router.ex,security.ex,components/layouts.ex}`, `test/workharbor/{host_identity_test.exs,config_trust_test.exs,accounts_test.exs}`, `test/workharbor_web/{live/trust_live_test.exs,transport_security_test.exs}`. Activate trust route/navigation; two new actions and secret-free metadata. |

Each package also scopes `docs/quality.md` and required test IDs in
`docs/development/ci-acceptance.json` before implementation. W1–W4 add no Hex/npm
dependencies; existing `lazy_html` supports LiveViewTest. New tools, including
axe/Playwright, require separate owner approval.

## Verification

Reuse core components and light/dark CSS variables for backgrounds, text, focus
and statuses, including system theme preference. Record the **Owner read pages
accessibility checklist** in `docs/quality.md`: semantic landmarks/headings and
labelled navigation/theme controls; keyboard order, visible focus and operability;
status text beyond color; reflow at 320/768/1280 CSS px with long hostile text and
no clipping; measured targets at least 24 × 24 CSS px. For each theme and
error/focus/status state, record foreground/background CSS variable pairs,
computed sRGB values and numeric WCAG contrast ratios using relative luminance
`(Llighter + 0.05) / (Ldarker + 0.05)`: at least 4.5:1 normal text, 3:1 large text
and 3:1 applicable non-text controls/focus. Record measurement method and actual
keyboard/theme/responsive walkthrough evidence against WCAG 2.2 AA; unmeasured
items stay unverified. These real-browser checks require the owner-authorized
host/setup, with no browser assumed in CI or new accessibility tooling implied.

Every page needs failing `Phoenix.LiveViewTest` cases before behavior, including
wrong/foreign/expired/revoked credentials, common states and navigation. W1 tests
both preserved controller POST forms, alias behavior and denial-loop bounds.
Context tests cover each named action's exact three-kind access matrix, ownership,
redaction, pagination/assign bounds and unknown projections. Hostile-text tests
start red for every FR-022 control class plus HTML/script/attribute payloads.
Forged-event probes must leave DB/audit unchanged, produce no payload log and keep
the page alive. Static checks reject Repo, context writes and `raw` in page modules.
Prove timer cancellation, queued-tick suppression and ordered first-failure stopping.
W1 adds the named `owner shell icons obey strict CSP` assertion in
`test/workharbor_web/owner_shell_test.exs`: check inline SVG/text rendering,
including the theme toggle; reject `data:image` URLs in owner page/component
sources and the actual generated `priv/static/assets/css/app.css` served to those
pages, including generated icon selectors and runtime-selected classes. Missing
bundle evidence is not a pass. This static/bundle check complements the later
owner-browser check for visible icons and CSP violations; LiveViewTest alone
cannot enforce CSP. No new dependencies or browser-in-CI assumption.

Record red/green evidence and independent exact-head review per PR.

CI proves scoped tests and required format/compile/assets/test plus `make check`,
not browser/real-host acceptance. Owner-authorized browser journeys later cover
login, all pages, empty/error/stale/reconnect/revocation, hostile text and audit
correlation on the named TLS setup. Unrun journeys remain unverified.

## Owner questions and task relationship

- Which disposable host, TLS origin and browser setup may supply live evidence,
  and which operations are authorized there?

The API boundary above is decided; routes, refresh/bounds and component choices
remain engineering proposals for approval,
not settled owner decisions. [006/T012](../tasks.md) lifecycle/retry/progress journeys
remain open. [001 tasks](../../001-workspace-lifecycle/tasks.md): T015 receives only
a read-only precursor; enrollment actions/full acceptance remain. T017 creation/
progress and T009 lifecycle/retry controls stay deferred. Remaining 006/T009–T024
and T035 retain all gates; frontend approval unblocks none of that backend work.
No task/feature/product completion follows from this plan or these packages.
