# Web frontend: first increment

Status: Step 0 proposal, 2026-10-24; documentation only. The owner's sequencing
condition is met: T028 [#238](https://github.com/wstein/workharbor/pull/238) and
T029 [#239](https://github.com/wstein/workharbor/pull/239) merged; the T030 honest-status
record was submitted as [PR #241](https://github.com/wstein/workharbor/pull/241).
T030/SC-018 remain open.

## Scope and approval

The owner requested this bounded planning exception after the current T029/T030 PR.
Deliver a read-only owner dashboard for host health, workspace status and
enrollment/trust state, using Phoenix LiveView in `apps/control_plane`; no SPA or
second frontend stack. Automatic independent **Opus** review of this exact plan
and explicit owner approval of its exact reviewed revision are required before W1.
Approval must identify the revision in a durable PR/tracker record; it does not
approve deferred backend work. This proposal changes no task dependencies,
checkboxes or specification semantics. W1–W4 below are proposed packages, not
newly authorized tasks.

No provision/start/stop/rebuild/delete/retry, enrollment issuance, trust approval,
terminal, relay, deployment or activation controls enter this increment. Existing
sign-in/sign-out and dashboard-credential flows remain separate authentication
functions. Local CLI use stays interactive and human initiated on Docker Tier 1;
no automatic local daemon bridge, relay or lifecycle action is introduced.

## Pages and authentication

Proposed information architecture:

| Route | View |
| --- | --- |
| `/owner` | Overview: available host/workspace summaries and links, with uncertainty visible. |
| `/owner/hosts` | Host IDs, reported capabilities, connection freshness and revocation. |
| `/owner/workspaces` | Workspace/host IDs, desired state, observed state and recorded operation status. |
| `/owner/trust` | Read-only enrollment metadata and exact-revision approval records. |

Keep `/` as the owner entry alias and `/dashboard` in its existing read-credential
namespace. Reuse `/login/owner`, encrypted `__Host-` owner cookies and
`RequireOwner`; dashboard credentials, status-only authorization and CLI bearer
credentials must never become owner-page admission. Owner/dashboard cookies and
socket sessions remain kind separated. Sources:
[router](../../../apps/control_plane/lib/workharbor_web/router.ex),
[authentication](../../../apps/control_plane/lib/workharbor_web/auth_controller.ex),
[Accounts](../../../apps/control_plane/lib/workharbor/accounts.ex).

Revalidate server-derived owner scope during disconnected and connected mount,
before navigation, reads and events. Existing `Accounts.authorize/3` reloads
credential validity; W1 must implement and test connected-session expiry/revocation
handling so a mounted page cannot continue serving protected data after loss of
authorization. Never trust owner IDs or principals from browser parameters.

Reuse the authenticated `OwnerSocket` (which uses `Phoenix.LiveView.Socket`) at
`/owner/live`, not the scaffold client's `/live`. Its connect guard alone does not
prove page lifetime authorization. W1 must reconcile actual LiveView handshake
parameters with the strict query allowlist, add narrowly scoped static asset
serving, and replace the current `script-src 'none'` only where required for the
approved shell. Propose self-hosted scripts/styles and same-origin connections;
remove or nonce the scaffold's inline theme script. Preserve TLS/Host/Origin,
CSRF, cookie protections, no-store, frame denial, nosniff and referrer protections;
keep auth/API denial defaults. No wildcard CSP, credential URL, long-poll bypass
or new wire protocol. Test actual upgrade admission rather than weakening checks
until the client connects. Sources:
[endpoint](../../../apps/control_plane/lib/workharbor_web/endpoint.ex),
[sockets](../../../apps/control_plane/lib/workharbor_web/owner_socket.ex),
[security/session](../../../apps/control_plane/lib/workharbor_web/security.ex),
[client](../../../apps/control_plane/assets/js/app.js).

Every page distinguishes loading, empty, error, stale/disconnected and successful
reads. Retain last-known data only with explicit uncertainty and read time; absent
or failed projections never imply healthy/running or zero records. Host stale
marking becomes visible within the existing 001/T015 five-second bound. Errors
use redacted actionable text. Apply [FR-022](../spec.md)
visible control escaping **and** HEEx HTML escaping to every untrusted name,
identifier, error and remote text; never use raw HTML or executable Markdown.

## Data boundary

Views call application contexts, never provider adapters, Docker, raw SQL or Repo.
No new storage or network API is required. The following reads are available only
as stated; bounded owner-authorized list/projection helpers are proposed work.

| Page | Existing source and bounded missing work |
| --- | --- |
| Overview | Compose the accepted reads below; no synthetic totals when a read fails. |
| Hosts | `Hosts.status/2` returns scoped identity/revocation only. Add bounded owner-host enumeration in `Hosts`; wrap internal unscoped `HostConnections.host_state/1` behind that check. Capabilities/last-seen fields are metadata, not proof of provider health. |
| Workspaces | `Foundation` stores Workspace/Operation/RuntimeInstance, but exposes no workspace list. Add owner-bound bounded reads there. Use `HostConnections.project_runtime/2` for conservative observed state; missing freshness stays unknown. Do not infer lifecycle execution from durable intent. |
| Enrollment/trust | `Hosts` retains enrollment/identity/security rows; add a scoped metadata projection excluding codes, hashes, CSRs and certificates. `ConfigTrust.lookup/3` checks an exact reference/descriptor, not enumeration. Add bounded owner approval summaries there; an approval record does not prove the workspace's current input is trusted. |

Sources: [Hosts](../../../apps/control_plane/lib/workharbor/hosts.ex),
[HostConnections](../../../apps/control_plane/lib/workharbor/host_connections.ex),
[Foundation](../../../apps/control_plane/lib/workharbor/foundation.ex),
[records](../../../apps/control_plane/lib/workharbor/foundation/records.ex),
[ConfigTrust](../../../apps/control_plane/lib/workharbor/config_trust.ex).
Use deterministic bounded pagination; missing projections block their package or
show unavailable, never trigger backend mutation. Preserve existing authentication,
denial and domain audit events; read refreshes must not create lifecycle/trust
mutation audit records. Correlate browser evidence with existing audit records
without recording secrets or claiming an audit viewer.

## Presentation and verification

Reuse core components and layout first. Proposed engineering choices: semantic
landmarks/headings, labelled navigation and theme controls, visible keyboard focus,
status text beyond color, WCAG 2.2 AA contrast and 24 CSS px minimum targets.
Use existing light/dark CSS variables, including error/focus/status colors; support
system preference without inline-script CSP exceptions. Verify keyboard use,
320/768/1280 CSS px layouts, long hostile text and both themes; reflow must preserve
labels and state without clipping.

For every page, write failing `Phoenix.LiveViewTest` cases before behavior:
unauthenticated/foreign owner/wrong credential kind, expired/revoked connected
session, populated/empty/loading/error/stale states, and safe navigation. Write the
hostile-text corpus red first: every FR-022 control class plus HTML/script and
attribute payloads must become visible inert text. Context tests prove bounded
owner filtering, redaction and unknown projections. W1 additionally exercises
CSRF/origin/Host/CSP/assets and the real socket handshake; W2 tests the five-second
stale bound. Record red/green evidence and exact-head independent review per PR.

CI proves the scoped tests and required format/compile/assets/test plus `make check`
gates, not a real browser or Docker host. Later owner-authorized browser journeys
cover login, each page, empty/error/stale/reconnect, revocation, keyboard/themes and
hostile text on the named TLS setup; retain versions, screenshots and correlated
redacted audit evidence in `docs/quality.md`. Actual disposable host, TLS origin
and live-operation scope need owner input/authorization. Unrun journeys remain
unverified; documentation approval claims no product acceptance.

## Small PR sequence

Paths below are exact proposed files relative to `apps/control_plane/`; shared
files have one active author. Each package gets its own issue/branch and focused
Conventional Commits. Dependents use merged prerequisites; only a real code
dependency justifies a stack, at most two levels deep. W1 registers only the
implemented overview; future-page routes remain unregistered and navigation items
inactive until their page package supplies the module. No missing-module routes
or dummy data make W1 appear complete.

| Package / dependency | Files and acceptance |
| --- | --- |
| W1 shell, after plan gates | `lib/workharbor_web/{router.ex,endpoint.ex,security.ex,owner_socket.ex}`, `lib/workharbor_web/live/{owner_auth.ex,overview_live.ex}`, `lib/workharbor_web/components/{layouts.ex,layouts/root.html.heex,core_components.ex}`, `assets/{css/app.css,js/app.js}`, `test/workharbor_web/{owner_auth_test.exs,owner_shell_test.exs,transport_security_test.exs}`. Shell/navigation, all common states, auth/security and theme proof first. |
| W2 hosts, after W1 | `lib/workharbor/hosts.ex`, `lib/workharbor_web/live/hosts_live.ex`, `lib/workharbor_web/{router.ex,components/layouts.ex}`, `test/workharbor/host_identity_test.exs`, `test/workharbor_web/live/hosts_live_test.exs`. Activate the hosts route/navigation; scoped enumeration, metadata and truthful liveness. |
| W3 workspaces, after W2 | `lib/workharbor/foundation.ex`, `lib/workharbor_web/live/workspaces_live.ex`, `lib/workharbor_web/{router.ex,components/layouts.ex}`, `test/workharbor/foundation_test.exs`, `test/workharbor_web/live/workspaces_live_test.exs`. Activate the workspaces route/navigation; scoped recorded state and conservative runtime projection. |
| W4 trust/enrollment, after W3 | `lib/workharbor/{hosts.ex,config_trust.ex}`, `lib/workharbor_web/live/trust_live.ex`, `lib/workharbor_web/{router.ex,components/layouts.ex}`, `test/workharbor/{host_identity_test.exs,config_trust_test.exs}`, `test/workharbor_web/live/trust_live_test.exs`. Activate the trust route/navigation; secret-free read-only metadata and exact-revision distinctions. |

Each package also scopes `docs/quality.md` and required test IDs in
`docs/development/ci-acceptance.json` explicitly before implementation. Paths with
braces enumerate regular files; they do not grant directory-wide scope.

## Remaining questions and task relationship

Owner input still needed: which disposable host/TLS origin and browser setup may
supply live evidence, and what operations are authorized there? Routes, refresh
mechanism, pagination bounds and component treatment above are proposed engineering
choices for exact-plan approval, not previously settled owner decisions.

This is a partial path toward [006/T012](../tasks.md), whose lifecycle, retry and
progress journeys remain open. [001 tasks](../../001-workspace-lifecycle/tasks.md):
T015 gains only a read-only hosts precursor; enrollment actions and full acceptance
remain. T017 creation/progress and T009 lifecycle/retry controls stay deferred.
Remaining 006/T009–T024 control-plane/relay work and T035 retain all original gates;
frontend plan approval unblocks none of that work. No checkbox or feature/product
completion claim follows from this plan or these read-only packages.
