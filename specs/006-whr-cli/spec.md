# Feature Specification: `whr` product command line

**Feature identifier**: `006-whr-cli`
**Feature Branch**: `docs/006-whr-cli`
**Created**: 2026-10-13
**Status**: Revised after review; awaiting independent re-review. Owner approval for planning (H13:a, 2026-10-13)
covers this revised text only once that re-review is CLEAR for its exact head. Not implemented. Owner decisions
are recorded below; implementation prerequisites are listed under "Prerequisites".
**Input**: Specify the product CLI `whr` for workspace lifecycle plus host and operations commands (`ws`, `open`,
`shell`, `console`, `ssh`, `setup`, `doctor`, `offboard`, `service`, `version`), release and packaging conventions,
exit codes, output formats, secret handling and terminal-text safety, plus the owner's later decisions on
in-container agent session interoperation and a read-only Spec Kit dashboard. Record unresolved legacy-versus-
rewrite conflicts as clarification questions rather than silent choices.

## Context and scope

[Product scope](../../docs/product.md) currently lists "a broader CLI" as following demonstrated demand and defers
AI agent orchestration to wave two. The owner has stated that AI is not entirely a second wave: in the first
wave an AI agent runs inside the workspace container as an ordinary process, and `whr` interoperates with its
sessions through the in-container session interface; orchestrating agents from the control plane stays in
wave two. The owner decisions below are recorded here; the constitution (v1.1.0, issue #75) and the product and
architecture documents (issue #66) are reconciled with them.
Feature [001](../001-workspace-lifecycle/spec.md) delivers the lifecycle through the browser. This specification
describes a command-line path to the same owner-scoped operations. Planning approval (H13:a) does not authorize
implementation.

Future Gas Town or Symphony integration is documented only in the
[proposed external orchestrator adapter design](../../docs/design/orchestrator-adapters.md).
It is outside this feature and the installable pilot: no scheduler, adapter, native-tool launch,
provider-credential relay or automated merge capability is added. FR-029/FR-030 retain the in-container
session boundary. Any future integration needs separate scope/governance and feature spec/plan approval;
this contextual proposal changes no current execution approval or implementation task.

The legacy WorkHarbor `whr` tool (Go, single binary, Apple Container) is reference material only. Its task and
agent supervision commands are not adopted as such: session interoperation follows decision H9 below, and
`run`, `pause`, `resume`, `usage` and `purge` are deferred. Other legacy task commands (`inbox`, `approve`,
`reject`, `answer`, `kill-all`, ...) are not part of this feature. Previews, passkeys, GitHub App creation,
`serve`, tool stores and helper binaries are not part of this feature.

### Owner decisions (2026-10-13)

- **H8 - Spec Kit dashboard**: a read-only web dashboard shows specs, plans, tasks, review evidence and
  workspace/agent status. It shares one API with the CLI and offers no editing or dispatch.
- **H24:a - Owner API host**: the control plane serves one owner-authenticated API over TLS. The dashboard is a
  page of the control plane's web interface and the CLI is a client of the same API. "Local" in H8 means
  owner-scoped, not loopback-only.
- **H9 - Agent session interoperation**: an agent runs inside the workspace container and exposes a small session
  interface: list, attach, status, send input, stop. `whr` uses only that interface and does not wrap, install,
  configure or authenticate the agent's native tool.
- **H14:a - Documentation reconciliation first**: constitution, product and architecture documents are reconciled
  with this feature in the documentation lane (constitution via issue #75, product and architecture via
  issue #66) before implementation starts.
- **H15:b - Exit codes**: a fresh exit-code table is recorded in an ADR (issue #70). This supersedes the earlier
  H12:c wording. The legacy `whr` table and the development-script tables of Features 003/004 are reference
  material only.
- **H16:b - Session interface contract**: the in-container session interface is a separate, versioned contract
  under `contracts/` (issue #71). This specification references it and does not define its wire format.
- **H17:a - Dashboard and ADR 0006**: the read-only dashboard supersedes the first increment proposed in ADR 0006;
  the documentation lane records this (issue #68).
- **H18:a - Console**: `whr console` is a reattachable workspace session, not the legacy host-VM console.
- **H10:a, H11:a - CLI implementation language**: `whr` is implemented in Go, which requires an amendment of
  ADR 0001 (issue #67).
- **H23:a - Code layout**: the host agent and `whr` are separate Go modules under `apps/` (`apps/host_agent`,
  `apps/whr`) with a shared API client package.
- **Deferred**: `run`, `pause`, `resume`, `usage` and `purge` are not part of this feature.
- **wstein/workharbor#223 (closed) - Interactive streams**: OSC, DCS, APC, PM and SOS sequences are filtered by
  default in `shell`, `console`, `agent attach` and sessions that `whr ssh` relays itself; unfiltered output
  requires an explicit per-session raw opt-in.

### Prerequisites

Implementation MUST NOT start before all of the following exist:

- Constitution amendment (issue #75) and product/architecture reconciliation (issue #66, merged), including the
  ADR 0006 supersession note (#68).
- The ADR 0001 amendment for the Go CLI and the `apps/` module layout (issue #67, H10:a, H11:a, H23:a).
- The exit-code ADR with the fresh numeric table (issue #70).
- The versioned in-container session interface contract under `contracts/` (issue #71).
- A reconciliation of Feature 001's terminal cleanup rule with the console persistence model of FR-007a, recorded
  in the Feature 001 spec or an ADR.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Sign in and manage workspace lifecycle from a terminal (Priority: P1)

An owner configures `whr` for a WorkHarbor endpoint, signs in, and then lists, inspects, creates, stops, starts,
rebuilds and deletes workspaces with the same authorization, idempotency, trust and confirmation rules as the
browser dashboard.

**Why this priority**: The CLI is only useful if it reaches the core lifecycle without weakening any guarantee.
**Independent Test**: Against a control plane with one enrolled Docker host, start from an empty client profile,
sign in, run each `whr ws` subcommand in human and machine-readable modes, and compare resulting operations,
audit records and observed state with the browser view.

**Acceptance Scenarios**:

1. **Given** no client profile, **When** any owner command runs, **Then** it fails with the authentication code
   and points to `whr auth login`; no request carries an owner credential.
2. **Given** `whr auth login <endpoint>`, **When** the endpoint's server identity cannot be verified, **Then** login
   stops before sending any credential; **Given** a verified endpoint, **Then** the owner authenticates through a
   flow that never places a credential in arguments, URLs or output, and the profile is stored per FR-021.
3. **Given** a signed-in profile, **When** `whr auth status` runs, **Then** it shows endpoint, server identity,
   owner identity, credential scope and expiry, and no secret.
4. **Given** an expired or revoked credential, **When** any owner command runs, **Then** it fails with the
   authentication code and a re-login hint; `whr auth login` renews it without losing profile settings, and
   unattended mode never prompts for a secret.
5. **Given** an authenticated owner, **When** `whr ws ls` runs, **Then** only that owner's workspaces are shown with
   desired state, observed state, host and staleness; unknown or stale observations are labeled as such.
6. **Given** a config repository at a resolved revision that is not yet trusted, **When** `whr ws create` runs,
   **Then** the CLI shows the resolved config and submodule commits and requires explicit trust before any hook
   executes; refusal leaves the workspace unprovisioned.
7. **Given** no caller-supplied idempotency key, **When** a mutating command runs, **Then** the CLI generates one,
   reports it before sending, and reuses it for automatic retries; **Given** a caller-supplied key reused with the
   same payload, **Then** one operation results; with a changed payload, the conflict code results.
8. **Given** the response to a mutating request is lost, **When** the CLI retries or the owner runs
   `whr op show --key <key>`, **Then** the existing operation is returned and no duplicate is created.
9. **Given** a running workspace, **When** `whr ws stop` and then `whr ws start` run, **Then** committed and
   uncommitted project files are retained and each command reports the operation identifier and final state.
10. **Given** a running workspace with uncommitted files, **When** `whr ws rebuild` runs at the already trusted
    revision, **Then** project files are retained and a new runtime starts; **Given** the config or submodule
    commits changed since trust, **Then** rebuild requires new trust before any hook runs.
11. **Given** a rebuild that fails, **When** the owner inspects the workspace, **Then** project storage is intact,
    the workspace is reported failed (never running), and an explicit retry or a rebuild at the previously trusted
    revision recovers it.
12. **Given** a request to delete project data, **When** `whr ws rm --data` runs without explicit confirmation,
    **Then** nothing is deleted; runtime deletion and data deletion are separate operations.
13. **Given** a long-running operation, **When** the owner interrupts the CLI or `--timeout` elapses, **Then** the
    operation is neither cancelled nor reported complete; the CLI exits with the interrupted or timeout code and
    the operation identifier, and `whr op wait <id>` resumes waiting.

---

### User Story 2 - Reach a running workspace (Priority: P1)

The owner opens the workspace in the browser dashboard, starts an interactive shell inside the workspace container,
or attaches and reattaches to the workspace's persistent console session.

**Why this priority**: Terminal access is the first complete user journey; the CLI must honor the same grant rules.
**Independent Test**: Request each access path for an owned and a foreign workspace; verify grant expiry,
single use, session limits, resize bounds, disconnect cleanup and console reattachment.

**Acceptance Scenarios**:

1. **Given** a running owned workspace, **When** `whr open <ws>` runs, **Then** the default browser opens the
   authenticated dashboard page for that workspace; no credential appears in the opened address or in output.
2. **Given** a running owned workspace, **When** `whr shell <ws>` runs, **Then** an interactive shell starts inside
   the workspace container using a single-use, expiring, workspace- and owner-bound grant, and the shell ends
   when the attachment ends.
3. **Given** another owner's workspace or a revoked host, **When** any access command runs, **Then** it fails with
   the authorization code and no session is created.
4. **Given** the session limits of Feature 001 are reached, **When** another attachment is requested, **Then** the
   CLI fails with an explicit limit error and the conflict code.
5. **Given** a console with a long-running command, **When** the client disconnects, **Then** the attachment's
   relay resources are released within five seconds, the console process keeps running, and a later
   `whr console <ws>` with a new grant reattaches and shows the retained output tail.
6. **Given** a console, **When** the workspace is stopped or rebuilt or `whr console <ws> --close` runs, **Then**
   the console session ends; a later `whr console <ws>` starts a new session and says so.
7. **Given** a workspace with and without a console session, **When** `whr console <ws> --status` runs, **Then** it
   reports whether a session exists, its start time and current attachments, without creating or attaching to a
   session; for a disconnected host it reports the state as unknown.
8. **Given** a remote shell or console that exits with status 0–255, **When** `whr shell` or `whr console` ends,
   **Then** the CLI exits with that status; with `--json`, the exit event marks it as a remote status, distinct
   from any CLI failure.
9. **Given** any access command, **When** it succeeds, **Then** it provides no raw host shell, Docker socket or host
   secret access.

---

### User Story 3 - Prepare, diagnose and remove a host (Priority: P2)

An operator on a Docker host prepares it for WorkHarbor, checks its health, installs or removes the host agent
service and later offboards the host without leaving credentials behind.

**Why this priority**: Host enrollment is required before any workspace exists, but the browser can already
display enrollment codes; the CLI makes the host side repeatable.
**Independent Test**: On a disposable Linux Docker host, run `whr setup --dry-run`, `whr setup`, `whr doctor`,
`whr service status` and `whr offboard host`, including injected failures between offboarding steps; inspect
changes, credentials on disk and the control-plane host list.

**Acceptance Scenarios**:

1. **Given** a fresh host, **When** `whr setup --dry-run` runs, **Then** every intended change is listed and nothing
   is modified.
2. **Given** a valid one-time enrollment code supplied by file or standard input, **When** `whr setup` completes,
   **Then** the host has its own identity, the agent service is installed and the host appears in the dashboard.
3. **Given** an expired, consumed or revoked enrollment code, **When** `whr setup` runs, **Then** no identity is
   issued and the CLI exits with the authentication code and a remediation hint.
4. **Given** a host with a missing prerequisite, **When** `whr doctor` runs, **Then** each check reports pass, fail,
   skipped or unknown with a remediation, and a missing tool is never reported as passing.
5. **Given** an unattended invocation, **When** a step requires a human decision (trust, destructive removal,
   offboarding), **Then** the CLI refuses rather than assuming consent and exits with the needs-human code.
6. **Given** an enrolled host and confirmation, **When** `whr offboard host` runs and every step succeeds,
   **Then** it reports success only after the control plane confirms revocation and the service and local host
   credentials are removed; workspace data is deleted only with a separate flag and confirmation.
7. **Given** the control plane is unreachable or a step fails, **When** `whr offboard host` stops, **Then** it
   exits with a partial-failure status listing completed and remaining steps, keeps the agent stopped and keeps
   the credentials needed to request revocation; rerunning resumes from the first incomplete step.
8. **Given** the owner revoked the host from the browser while offboarding was incomplete, **When** offboarding is
   rerun, **Then** it detects the confirmed revocation and completes local cleanup.

---

### User Story 4 - Script WorkHarbor reliably (Priority: P2)

An owner writes scripts against `whr` using machine-readable output and stable exit codes.

**Why this priority**: A CLI that cannot be scripted safely adds little over the browser.
**Independent Test**: Run every command with `--json`, with failures injected and with hostile binary terminal
output; validate each output against the published schema and each exit status against the published table.

**Acceptance Scenarios**:

1. **Given** `--json`, **When** a non-interactive command succeeds or fails, **Then** standard output carries exactly
   one versioned envelope and standard error carries only human diagnostics.
2. **Given** `--json` with `shell`, `console` or `agent attach`, **When** remote output contains arbitrary bytes,
   **Then** standard output carries one typed event per line with the bytes encoded binary-safely, and the stream
   always parses as valid JSON lines.
3. **Given** `--json` with a command that cannot provide a typed event stream, **When** it is invoked, **Then** it
   is rejected before connecting with a structured usage error.
4. **Given** any failure, **When** the CLI exits, **Then** the status matches the published exit-code table.
5. **Given** `whr version --json`, **When** it runs, **Then** it reports the release version, source revision, build
   date, client platform and supported contract versions.

---

### User Story 5 - Work with in-container agent sessions and the dashboard (Priority: P2)

The owner lists agent sessions running inside a workspace, checks their status, attaches to one, sends it input
and stops it; the owner reviews Spec Kit progress and agent status in a read-only browser dashboard.

**Why this priority**: Agents run inside workspaces in the first wave (H9); the dashboard gives a read-only view
(H8). Neither is needed to create or access a workspace.
**Independent Test**: With a stub agent implementing the session interface contract in a test workspace, exercise
every `whr agent` action for an owned and a foreign workspace; call every mutating API endpoint directly with the
dashboard's credential; load a hostile web page in the same browser against the owner API.

**Acceptance Scenarios**:

1. **Given** a running owned workspace with agent sessions, **When** `whr agent ls <ws>` runs, **Then** each
   session's identifier and status are shown; sessions in other workspaces or of other owners are not.
2. **Given** an owned session, **When** `whr agent attach`, `status`, `send` or `stop` runs, **Then** it acts only
   through the session interface contract under a workspace- and owner-bound grant.
3. **Given** input for `whr agent send`, **When** it is supplied, **Then** it is read from an argument, a file or
   standard input with a bounded size, and is never interpreted by `whr`.
4. **Given** a workspace without a session interface, **When** any `whr agent` action runs, **Then** it fails with
   the not-found code and a remediation; **Given** a session interface with an incompatible contract version,
   **Then** it fails with the incompatible-version code and names both versions. In neither case does `whr` start
   or configure the agent's native tool.
5. **Given** the dashboard, **When** the owner views specs, plans, tasks, review evidence and status, **Then** each
   item shows its Git revision or observation time, and no control edits files, approves or dispatches work.
6. **Given** the dashboard's read-scoped credential, **When** any mutating API endpoint is called directly, **Then**
   the API rejects it with an authorization error, changes no state and records the denial.
7. **Given** a hostile website open in the owner's browser, **When** it attempts cross-origin reads, form posts,
   DNS-rebinding requests or streaming connections to the owner API, **Then** each is rejected and no state or
   data is exposed.

---

### User Story 6 - Connect an external SSH client (Priority: P3)

The owner connects an SSH client or IDE to a workspace using short-lived credentials.

**Why this priority**: External IDE access is sequenced after Feature 001 (product sequence 003/004).
**Independent Test**: Connect with `whr ssh <ws>` and with a configuration from `whr ssh <ws> --print-config`;
verify host-key pinning, credential lifetime, exit status and refusal for foreign workspaces.

**Acceptance Scenarios**:

1. **Given** a running owned workspace, **When** `whr ssh <ws>` runs, **Then** a short-lived client certificate is
   obtained or refreshed before connecting and the server key is pinned from the authenticated control plane,
   never trusted on first use.
2. **Given** the owner's personal SSH configuration and agent, **When** `whr ssh` runs, **Then** neither is read or
   used; WorkHarbor keeps its own isolated client state.
3. **Given** `whr ssh <ws> --print-config`, **When** an external client or IDE uses the generated configuration,
   **Then** it pins the same server key and uses short-lived credentials; the output states that such
   connections are not relayed or filtered by `whr`.
4. **Given** a remote command or shell started by `whr ssh` that exits with status 0–255, **When** the connection
   ends, **Then** `whr ssh` exits with that status; on transport loss it exits with the interrupted code.

---

### Edge Cases

- Control plane unreachable: commands fail with a distinct error and never report cached state as current.
- Host disconnected: observed state shows unknown or stale with the last heartbeat time, matching the browser;
  console state is unknown until reconciliation.
- Workspace names, repository URLs, error messages and remote text containing control characters, bidirectional
  controls or line separators are rendered inert in human output and escaped in JSON.
- Terminal byte streams in `shell`, `console`, `ssh` and `agent attach` may carry escape sequences that write the
  clipboard, set titles or emit device-control strings; they are filtered by default (FR-022a).
- A sequence split across transport frames, or left unterminated, is still filtered; raw opt-in applies to one
  session only and is never remembered. In `--json` mode bytes are encoded, never written raw.
- An agent session ends while attached: `whr agent attach` exits with the session's status or the interrupted code.
- Console output produced while detached exceeds the retained tail: the reattach notes that output was dropped.
- Secret supplied on the command line: the CLI refuses it before any network or file action and explains the
  supported channels.
- Standard output is not a terminal: colors and progress animations are disabled by default.
- Dimensions outside 1–500 columns or 1–200 rows are clamped or rejected per Feature 001 limits.
- `whr setup` or `whr offboard host` interrupted midway: rerunning resumes or reports partial state; neither ever
  reports success for incomplete work.
- Host commands on an unsupported host platform fail with an unsupported-platform error before any change.
- Unknown flag or argument: usage error with a short hint, no side effects.

## Requirements *(mandatory)*

### Functional Requirements

**Command surface**

- **FR-001**: `whr` MUST provide `auth` (`login`, `status`, `logout`), `ws` (`ls`, `show`, `create`, `start`,
  `stop`, `rebuild`, `rm`), `op` (`show`, `wait`), `open`, `shell`, `console`, `ssh`, `agent` (`ls`, `attach`,
  `status`, `send`, `stop`), `setup`, `doctor`, `offboard host`, `service` (`install`, `uninstall`, `status`),
  `version` and shell completion. Bare `whr` and `--help` print help; `--version` equals `version`.
- **FR-002**: Every workspace command MUST act through the same owner-authenticated application operations as the
  browser, with identical authorization, idempotency, trust, audit and confirmation rules.
- **FR-003**: Every mutating command MUST send an idempotency key: the caller's, or else one the CLI generates and
  reports before sending. The CLI MUST reuse that key for its own bounded retries, report the durable operation
  identifier, and fail as a conflict when a key is reused with a changed payload.
- **FR-003a**: Mutating commands MUST wait for a terminal operation state by default, support `--no-wait` (return
  the operation identifier immediately) and `--timeout`. Interruption or timeout MUST NOT cancel the operation
  or report it complete; the CLI exits with the interrupted or timeout code and the operation identifier.
- **FR-003b**: `whr op show` and `whr op wait` MUST retrieve an operation by identifier or idempotency key, so
  that an operation whose response was lost can be found without creating a duplicate.
- **FR-004**: `ws rm` MUST delete runtime only by default; data deletion MUST require a separate flag and an
  explicit confirmation that cannot be implied by unattended mode.
- **FR-005**: `ws create` and `ws rebuild` MUST display resolved config and submodule commits and obtain explicit
  owner trust before any repository-provided hook executes; changed commits require new trust.
- **FR-005a**: `ws rebuild` MUST retain project storage, start a new runtime at a trusted revision, and on failure
  leave storage intact, report the workspace failed (never running), and allow recovery by explicit retry or by
  rebuilding at the previously trusted revision.
- **FR-006**: `shell`, `console`, `ssh` and `agent attach` MUST use single-use, expiring, workspace- and
  owner-bound grants and the attachment limits of Feature 001; they MUST NOT provide raw host shell, Docker socket
  or host-secret access.
- **FR-007**: `whr shell <ws>` MUST start a fresh login shell inside the workspace that ends with its attachment.
- **FR-007a**: `whr console <ws>` MUST attach to the workspace's single persistent console session, creating it if
  absent and saying so. The console session's lifetime MUST be independent of its attachments:
  - client disconnect, grant expiry, idle timeout and the maximum attachment duration end only the attachment;
    its relay resources are released within Feature 001's five seconds and the console process keeps running;
  - workspace stop, workspace rebuild, runtime deletion and `whr console <ws> --close` end the console session;
  - host disconnect ends the attachment and leaves console state unknown until reconciliation;
  - reattachment always requires a new grant and shows a bounded retained output tail, noting any dropped output;
  - `whr console <ws> --status` reports whether a session exists, its start time and current attachments.
- **FR-008**: `whr ssh` MUST pin the workspace server key from the authenticated control plane, use short-lived
  client credentials refreshed before each connection, and ignore the user's personal SSH configuration and agent.
- **FR-008a**: `whr ssh <ws> --print-config` MUST generate a client configuration for external SSH clients and IDEs
  with the pinned server key and a short-lived credential reference, and MUST state that connections made with it
  are neither relayed nor filtered by `whr` (FR-022a does not apply to them).
- **FR-009**: `whr setup` MUST support dry-run, list every intended change, consume a one-time enrollment code and
  install the host agent service; it MUST be resumable after interruption.
- **FR-010**: `whr doctor` MUST report each check as pass, fail, skipped or unknown with a remediation, support a
  shareable redacted report, and never report an absent tool or unmeasured check as passing.
- **FR-011**: `whr offboard host` MUST be confirmed interactively, refuse unattended mode, and proceed in resumable
  steps: stop the agent, request revocation, confirm revocation with the control plane, delete local host
  credentials, uninstall the service. It MUST report success only after revocation is confirmed and local
  cleanup is complete. On any failure it MUST exit with a partial-failure status listing completed and remaining
  steps, keep the agent stopped and keep the credentials needed for revocation; a rerun MUST resume from the
  first incomplete step and MUST accept a revocation confirmed through the browser. A local-only cleanup for a
  permanently unreachable control plane MAY be offered behind explicit confirmation, but MUST report revocation
  as unconfirmed and exit non-zero. Workspace data is deleted only with a separate flag and confirmation.
- **FR-012**: `whr service` MUST install, uninstall and report the host agent service state without exposing
  credentials in output.
- **FR-013**: Unattended or answer-file modes MUST NOT approve trust, destructive deletion or offboarding; such
  refusals MUST exit with the needs-human code.

**Authentication and owner API**

- **FR-035**: `whr auth login` MUST select a control-plane endpoint, verify its server identity before sending any
  credential, authenticate the owner without placing a credential in arguments, URLs or output, and store the
  result per FR-021. `whr auth status` MUST show endpoint, server identity, owner identity, scope and expiry
  without secrets. Expired or revoked credentials MUST yield the authentication code with a re-login hint;
  `whr auth logout` MUST delete local credentials and request server-side invalidation.
- **FR-036**: The control plane MUST serve one owner API over TLS for both the CLI and the dashboard (H24:a). Every
  endpoint except login MUST require owner authentication and scope requests to the owner; "local" means
  owner-scoped, not loopback-only. The listening address and network exposure are explicit operator
  configuration, and the CLI MUST refuse plaintext endpoints.
- **FR-037**: The owner API MUST reject requests whose Host is not a configured name (DNS-rebinding defense) and
  browser requests whose Origin is not the control plane's own origin, MUST NOT enable permissive cross-origin
  access, and MUST require an anti-forgery token on every state-changing browser request, including streaming
  upgrades.
- **FR-038**: Browser session credentials for the owner API MUST be inaccessible to page scripts and not sent on
  cross-site requests.

**Output formats**

- **FR-014**: Standard output MUST carry command data only; human progress, prompts and diagnostics MUST go to
  standard error.
- **FR-015**: `--json` MUST emit one versioned envelope (schema version, result or error code and message,
  operation identifier where applicable) for non-interactive commands. For `shell`, `console` and `agent attach`
  it MUST emit one typed event per line (start, output with stream name and binary-safe encoded bytes, notice,
  exit, error). Commands that cannot provide such events, including `ssh`, MUST reject `--json` with a structured
  usage error before connecting. Terminal bytes MUST never appear raw on standard output in JSON mode.
- **FR-016**: Color MUST follow `--color auto|always|never` and `--no-color`, defaulting to off when output is not
  a terminal or when the conventional no-color environment setting is present.
- **FR-017**: Errors in human mode MUST be one line prefixed `whr:` followed by an optional remediation line.

**Exit codes**

- **FR-018**: `whr` MUST publish one exit-code table, freeze it with automated tests and document it with the
  release. Usage errors, not-found, authentication failure, authorization failure, conflict or wrong state,
  incompatible contract version, needs-human (a required human decision was refused in unattended mode or not
  given), timeout, interruption and partial failure MUST have distinct codes. The numbers, and how remote
  statuses 0–255 passed through by FR-019 relate to them, MUST come from the fresh table recorded in the
  exit-code ADR (H15:b, issue #70). The legacy `whr` table (0 OK, 1 error, 2 usage, 3 not found,
  4 auth, 5 conflict, 6 needs-human, 7 timeout, 8 quit, 9 interrupted, 10 task failed) and the development-script
  tables of Features 003/004 are reference material only.
- **FR-019**: `shell`, `console`, `ssh` and `agent attach` MUST exit with the remote status 0–255 when the remote
  side exits, and with the interrupted code when the transport is lost. Because remote statuses can overlap CLI codes,
  CLI failures MUST also be announced on standard error with the `whr:` prefix, and the JSON exit event MUST
  distinguish a remote status from a CLI failure.

**Secret handling**

- **FR-020**: Credentials and enrollment codes MUST be accepted only through a file readable solely by the owner,
  standard input, an inherited pipe or the interactive login flow; they MUST be refused in arguments, URLs and
  command history, and never written to output, logs, reports or Git.
- **FR-021**: Locally stored CLI and host credentials MUST be kept outside workspaces with owner-only permissions.
  `whr auth status` MUST flag wider permissions on client credentials on every client platform; `whr doctor`
  MUST flag them on host credentials on supported host platforms.

**Terminal-text safety**

- **FR-022**: Human output that includes untrusted text MUST replace C0 controls except tab, DEL, C1 controls,
  bidirectional controls and Unicode line/paragraph separators with a visible placeholder; JSON output MUST escape
  them.
- **FR-022a**: Interactive streams relayed by `whr` (`shell`, `console`, `agent attach`, and `ssh` sessions that
  `whr ssh` relays itself) MUST filter OSC, DCS, APC, PM and SOS sequences by default, including sequences split
  across frames or left unterminated. Unfiltered output MUST require an explicit per-session raw opt-in that is
  shown when the session starts and never persisted (owner decision on wstein/workharbor#223, closed).
  Connections made by external clients with FR-008a configuration are out of this requirement's reach.

**Release, packaging and platforms**

- **FR-023**: Releases MUST originate from a human-signed annotated `vX.Y.Z` tag on `main` with green required
  checks; the signer MUST be on a reviewed allow list.
- **FR-024**: Each release MUST provide per-platform archives named with version, OS and architecture, a checksum
  file, a software bill of materials and build-provenance attestation that users can verify.
- **FR-025**: Automated release jobs MUST produce a draft only; publication and package-channel updates require a
  human; prereleases MUST NOT be published to stable package channels.
- **FR-026**: Each release MUST publish two separate matrices with verified and unverified entries: client
  platforms (where `whr` runs owner, workspace, access and agent commands) and host platforms (where `setup`,
  `doctor`, `service` and `offboard` are supported). Host commands MUST refuse unsupported host platforms.
- **FR-027**: `whr version` MUST report version, source revision, build date, client platform and supported
  contract versions, in human and JSON form.

**Agent sessions and dashboard**

- **FR-028**: Workspace lifecycle, access and host commands MUST work without an AI provider, tracker or cloud
  account.
- **FR-029**: `whr agent` MUST support list, attach, status, send input and stop through the versioned session
  interface contract (H16:b, issue #71) only, under the owner, workspace and grant authorization of FR-002 and
  FR-006. A missing interface MUST yield the not-found code; an incompatible contract version MUST yield the
  incompatible-version code with both versions and a remediation.
- **FR-030**: `whr` MUST NOT install, configure, authenticate, launch or wrap an agent's native tool, and MUST NOT
  receive or relay provider credentials.
- **FR-031**: `whr agent send` input MUST be size-bounded and passed as data; `whr` MUST NOT interpret it.
- **FR-032**: A read-only web dashboard MUST show specs, plans, tasks, review evidence and workspace/agent status,
  each with its Git revision or observation time, and MUST offer no editing, approval or dispatch (H8). It
  supersedes the first increment proposed in ADR 0006 (H17:a).
- **FR-033**: The CLI and the dashboard MUST share the owner API of FR-036. The dashboard MUST use a
  read-scoped credential, and the API MUST enforce that scope server-side on every endpoint, so that a direct
  call to any mutating endpoint with the dashboard credential is rejected, changes no state and is audited.
- **FR-034**: `run`, `pause`, `resume`, `usage` and `purge` are deferred and MUST NOT be added by this feature.

### Key Entities

- **Client profile**: endpoint, verified server identity, owner identity, credential scope, expiry and local
  credential location for one owner.
- **Operation reference**: durable operation identifier, idempotency key (caller- or CLI-generated) and status.
- **Access grant**: single-use, expiring authorization binding owner, workspace and attachment type.
- **Console session**: the single persistent terminal session of a workspace, with start time, retained output tail
  and current attachments; ends on stop, rebuild, runtime deletion or explicit close.
- **Attachment**: one grant-bound relay connection to a shell, console or agent session; bounded by Feature 001.
- **Host enrollment**: one-time code, resulting host identity, service state and revocation status.
- **Offboarding record**: ordered offboarding steps with completed, failed and remaining status.
- **Agent session**: an agent process inside one workspace, reached only through the session interface contract.
- **API credential scope**: owner (full) or dashboard (read-only), enforced by the control plane's owner API.
- **Dashboard view**: a read-only projection of Git-revisioned Spec Kit artifacts, review evidence and status.
- **Doctor report**: ordered checks with status, evidence and remediation, redacted for sharing.
- **Output envelope / stream event**: schema version, typed data or error, exit kind and code, operation reference.
- **Platform matrix**: client and host platforms, each marked verified or unverified per release.
- **Release artifact set**: archives, checksums, bill of materials, provenance and release notes for one tag.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Starting from an empty client profile, the Feature 001 lifecycle (sign in, create, inspect, stop,
  start, rebuild, delete runtime) completes using only `whr` on the acceptance host, with operations and audit
  records equivalent to browser-initiated ones except for the recorded client channel.
- **SC-002**: Automated tests prove that cross-owner commands, reused or expired grants, unapproved revisions,
  secrets in arguments, expired credentials and unattended destructive actions are rejected with the published
  exit codes.
- **SC-003**: Every command's `--json` output validates against the published schema in success and failure cases,
  including interactive streams fed a corpus of arbitrary binary and escape-sequence output.
- **SC-004**: A corpus of hostile names and messages containing every control class in FR-022 renders without
  emitting any of those characters to a terminal, and a hostile stream corpus (including split and unterminated
  OSC, DCS, APC, PM and SOS sequences) reaches the terminal filtered unless raw opt-in was given.
- **SC-005**: With failures injected after each offboarding step, every run either reports success with confirmed
  revocation and no host credential on disk, or reports partial failure; a rerun completes, and the control plane
  rejects the revoked identity.
- **SC-006**: A release can be verified end to end (signature, checksum, bill of materials, provenance) by a user
  following the published instructions in under ten minutes.
- **SC-007**: Evidence records exact client and host platforms, OS, tool versions and commands; mocked tests alone
  do not demonstrate sign-in, host setup, access, console reattachment or offboarding.
- **SC-008**: Every `whr agent` action and every dashboard page is rejected for foreign workspaces, and every
  mutating API endpoint called directly with the dashboard credential is rejected with no state change, in
  automated tests.
- **SC-009**: A hostile web page test (cross-origin read, form post, DNS rebinding, streaming upgrade) against the
  owner API produces no data exposure and no state change.
- **SC-010**: A console reattach test (attach, start a long-running command, disconnect, verify relay release within
  five seconds, reattach with a new grant) shows the command still running and its retained output; stop, rebuild
  and `--close` each end the session, and `--status` reports existence, start time and attachments correctly
  before, during and after each step without creating a session.
- **SC-012**: Automated tests map every exit path (missing and incompatible session interface, unattended refusals,
  remote statuses 0, 1 and 255 for `shell`, `console`, `ssh` and `agent attach`) to the code the exit-code ADR
  assigns.
- **SC-011**: A lost-response test (drop the reply to a mutating request) yields exactly one operation, retrievable
  with `whr op show --key`.

## Assumptions

- The CLI talks to the control plane's owner API over TLS for every workspace, access, agent and operation
  command; it never contacts a host agent or provider directly. Host commands (`setup`, `doctor`, `service`,
  `offboard`) run on the host itself with local privileges.
- Linux Docker is the first supported host platform; Apple container and other providers follow the product
  sequence. The initial client platform matrix is settled in planning and published per FR-026.
- The exact login flow is a planning decision within FR-020 and FR-035.
- The size of the retained console output tail and the CLI's automatic retry bounds are planning decisions.
- Legacy `whr` behavior is reference only; no legacy code or binary is a dependency.
- Development scripts of Features 003–005 keep their own exit codes and are not the product CLI.
- The session interface is comparable to herdr in purpose only; no herdr protocol, behavior or dependency is
  assumed. Its wire format belongs to the contract of issue #71.
- The dashboard reads Spec Kit artifacts at an identified Git revision.
