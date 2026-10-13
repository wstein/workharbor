# Feature Specification: `whr` product command line

**Feature identifier**: `006-whr-cli`
**Feature Branch**: `docs/006-whr-cli`
**Created**: 2026-10-13
**Status**: Approved for planning (owner H13:a, 2026-10-13); not implemented. Owner decisions H8, H9, H12:c and
#223 are recorded. Exit-code numbering is to be fixed by an ADR before implementation.
**Input**: Specify the product CLI `whr` for workspace lifecycle plus host and operations commands (`ws`, `open`,
`shell`, `console`, `ssh`, `setup`, `doctor`, `offboard`, `service`, `version`), release and packaging conventions,
exit codes, output formats, secret handling and terminal-text safety, plus the owner's later decisions on
in-container agent session interoperation and a read-only Spec Kit dashboard. Record unresolved legacy-versus-
rewrite conflicts as clarification questions rather than silent choices.

## Context and scope

[Product scope](../../docs/product.md) currently lists "a broader CLI" as following demonstrated demand and lists
AI features and agent management as non-goals. The owner has since stated that AI is not a second wave: in the
first wave an AI agent runs inside the workspace container, and `whr` interoperates with its sessions. The owner
decisions below are recorded here; the product document, architecture and ADRs are updated in a separate lane
after this specification, and this specification does not change them.
Feature [001](../001-workspace-lifecycle/spec.md) delivers the lifecycle through the browser. This specification
describes a command-line path to the same owner-scoped operations. The owner approved it for planning
(H13:a); that approval does not authorize implementation. The product-document update runs in the separate
documentation lane.

The legacy WorkHarbor `whr` tool (Go, single binary, Apple Container) is reference material only. Its task and
agent supervision commands are not adopted as such: session interoperation follows decision H9 below, and
`run`, `pause`, `resume`, `usage` and `purge` are deferred. Other legacy task commands (`inbox`, `approve`,
`reject`, `answer`, `kill-all`, ...) are not part of this feature. Previews, passkeys, GitHub App creation,
`serve`, tool stores and helper binaries are not part of this feature.

### Owner decisions (2026-10-13)

- **H9 - Agent session interoperation**: an agent runs inside the workspace container and exposes a small session
  interface: list, attach, status, send input, stop. `whr` uses only that interface and does not wrap, install,
  configure or authenticate the agent's native tool.
- **H8 - Spec Kit dashboard**: a read-only web dashboard shows specs, plans, tasks, review evidence and
  workspace/agent status. It shares one local API with the CLI and offers no editing or dispatch.
- **Deferred**: `run`, `pause`, `resume`, `usage` and `purge` are not part of this feature.
- **#223 - Interactive streams**: OSC, DCS, APC, PM and SOS sequences are filtered by default in `shell`,
  `console`, `ssh` and `agent attach`; unfiltered output requires an explicit per-session raw opt-in.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Manage workspace lifecycle from a terminal (Priority: P1)

An authenticated owner lists, inspects, creates, stops, starts and deletes workspaces from the command line with
the same authorization, idempotency, trust and confirmation rules as the browser dashboard.

**Why this priority**: The CLI is only useful if it reaches the core lifecycle without weakening any guarantee.
**Independent Test**: Against a control plane with one enrolled Docker host, run each `whr ws` subcommand in human
and machine-readable modes; compare resulting operations, audit records and observed state with the browser view.

**Acceptance Scenarios**:

1. **Given** an authenticated owner, **When** `whr ws ls` runs, **Then** only that owner's workspaces are shown with
   desired state, observed state, host and staleness; unknown or stale observations are labeled as such.
2. **Given** a config repository at a resolved revision that is not yet trusted, **When** `whr ws create` runs,
   **Then** the CLI shows the resolved config and submodule commits and requires explicit trust before any hook
   executes; refusal leaves the workspace unprovisioned.
3. **Given** a caller-supplied idempotency key, **When** the same create or start is repeated, **Then** one operation
   results; a changed payload with the same key fails with the conflict exit code.
4. **Given** a running workspace, **When** `whr ws stop` and then `whr ws start` run, **Then** committed and
   uncommitted project files are retained and each command reports the operation identifier and final state.
5. **Given** a request to delete project data, **When** `whr ws rm --data` runs without explicit confirmation,
   **Then** nothing is deleted; runtime deletion and data deletion are separate operations.
6. **Given** a long-running operation, **When** the owner interrupts the CLI, **Then** the operation continues or
   stops according to its durable record, and the CLI exits with the interrupted code and the operation identifier.

---

### User Story 2 - Reach a running workspace (Priority: P1)

The owner opens the workspace in the browser dashboard, starts an interactive shell inside the workspace container,
or reattaches to the workspace console session.

**Why this priority**: Terminal access is the first complete user journey; the CLI must honor the same grant rules.
**Independent Test**: Request each access path for an owned and a foreign workspace; verify grant expiry,
single use, session limits, resize bounds and disconnect cleanup match Feature 001.

**Acceptance Scenarios**:

1. **Given** a running owned workspace, **When** `whr open <ws>` runs, **Then** the default browser opens the
   authenticated dashboard page for that workspace; no credential appears in the opened address or in output.
2. **Given** a running owned workspace, **When** `whr shell <ws>` runs, **Then** an interactive shell starts inside
   the workspace container using a single-use, expiring, workspace- and owner-bound grant.
3. **Given** another owner's workspace or a revoked host, **When** any access command runs, **Then** it fails with
   the authorization exit code and no session is created.
4. **Given** the session limits of Feature 001 are reached, **When** another session is requested, **Then** the CLI
   fails with an explicit limit error and the conflict exit code.
5. **Given** an interactive session ends, **When** the remote shell exits, **Then** `whr shell` and `whr console`
   exit with the remote status (1–255), or with the interrupted code if the transport was lost.
6. **Given** any access command, **When** it succeeds, **Then** it provides no raw host shell, Docker socket or host
   secret access.

---

### User Story 3 - Prepare, diagnose and remove a host (Priority: P2)

An operator on a Docker host prepares it for WorkHarbor, checks its health, installs or removes the host agent
service and later offboards the host without leaving credentials behind.

**Why this priority**: Host enrollment is required before any workspace exists, but the browser can already
display enrollment codes; the CLI makes the host side repeatable.
**Independent Test**: On a disposable Linux Docker host, run `whr setup --dry-run`, `whr setup`, `whr doctor`,
`whr service status`, and `whr offboard host`; inspect changes, credentials on disk and the control-plane host list.

**Acceptance Scenarios**:

1. **Given** a fresh host, **When** `whr setup --dry-run` runs, **Then** every intended change is listed and nothing
   is modified.
2. **Given** a valid one-time enrollment code supplied by file or standard input, **When** `whr setup` completes,
   **Then** the host has its own identity, the agent service is installed and the host appears in the dashboard.
3. **Given** an expired, consumed or revoked enrollment code, **When** `whr setup` runs, **Then** no identity is
   issued and the CLI exits with the authorization code and a remediation hint.
4. **Given** a host with a missing prerequisite, **When** `whr doctor` runs, **Then** each check reports pass, fail,
   skipped or unknown with a remediation, and a missing tool is never reported as passing.
5. **Given** an unattended invocation, **When** a step requires a human decision (trust, destructive removal),
   **Then** the CLI refuses rather than assuming consent.
6. **Given** an enrolled host, **When** `whr offboard host` runs with confirmation, **Then** the agent service is
   removed, local host credentials are deleted, the control plane revokes the identity, and workspace data is
   deleted only with a separate explicit flag and confirmation.

---

### User Story 4 - Script WorkHarbor reliably (Priority: P2)

An owner writes scripts against `whr` using machine-readable output and stable exit codes.

**Why this priority**: A CLI that cannot be scripted safely adds little over the browser.
**Independent Test**: Run every command with `--json`, with failures injected; validate each output against the
published schema and each exit status against the published table.

**Acceptance Scenarios**:

1. **Given** `--json`, **When** a command succeeds or fails, **Then** standard output carries exactly one versioned
   envelope (or one envelope per line for streaming commands) and standard error carries only human diagnostics.
2. **Given** any failure, **When** the CLI exits, **Then** the status matches the documented exit-code table.
3. **Given** `whr version --json`, **When** it runs, **Then** it reports the release version, source revision, build
   date and supported contract versions.

---

### User Story 5 - Work with in-container agent sessions and the dashboard (Priority: P2)

The owner lists agent sessions running inside a workspace, checks their status, attaches to one, sends it input
and stops it; the owner reviews Spec Kit progress and agent status in a read-only browser dashboard.

**Why this priority**: Agents run inside workspaces in the first wave (H9); the dashboard gives a read-only view
(H8). Neither is needed to create or access a workspace.
**Independent Test**: With a stub agent implementing the session interface in a test workspace, exercise every
`whr agent` action for an owned and a foreign workspace; load the dashboard and confirm it exposes no mutation.

**Acceptance Scenarios**:

1. **Given** a running owned workspace with agent sessions, **When** `whr agent ls <ws>` runs, **Then** each
   session's identifier and status are shown; sessions in other workspaces or of other owners are not.
2. **Given** an owned session, **When** `whr agent attach`, `status`, `send` or `stop` runs, **Then** it acts only
   through the in-container session interface under a workspace- and owner-bound grant.
3. **Given** input for `whr agent send`, **When** it is supplied, **Then** it is read from an argument, a file or
   standard input with a bounded size, and is never interpreted by `whr`.
4. **Given** a workspace without a session interface, **When** any `whr agent` action runs, **Then** it fails with
   the not-found code and a remediation; `whr` never starts or configures the agent's native tool.
5. **Given** the dashboard, **When** the owner views specs, plans, tasks, review evidence and status, **Then** each
   item shows its Git revision or observation time, and no control edits files, approves or dispatches work.

---

### User Story 6 - Connect an external SSH client (Priority: P3)

The owner connects an SSH client or IDE to a workspace using short-lived credentials.

**Why this priority**: External IDE access is sequenced after Feature 001 (product sequence 003/004).
**Independent Test**: Connect with `whr ssh <ws>` and with a generated client configuration; verify host-key
pinning, credential lifetime and refusal for foreign workspaces.

**Acceptance Scenarios**:

1. **Given** a running owned workspace, **When** `whr ssh <ws>` runs, **Then** a short-lived client certificate is
   obtained or refreshed before connecting and the server key is pinned from the authenticated control plane,
   never trusted on first use.
2. **Given** the owner's personal SSH configuration and agent, **When** `whr ssh` runs, **Then** neither is read or
   used; WorkHarbor keeps its own isolated client state.

---

### Edge Cases

- Control plane unreachable: commands fail with a distinct error and never report cached state as current.
- Host disconnected: observed state shows unknown or stale with the last heartbeat time, matching the browser.
- Workspace names, repository URLs, error messages and remote text containing control characters, bidirectional
  controls or line separators are rendered inert in human output and escaped in JSON.
- Terminal byte streams in `shell`, `console`, `ssh` and `agent attach` may carry escape sequences that write the
  clipboard, set titles or emit device-control strings; they are filtered by default (FR-022a).
- A sequence split across transport frames, or left unterminated, is still filtered; raw opt-in applies to one
  session only and is never remembered.
- An agent session ends while attached: `whr agent attach` exits with the session's status or the interrupted code.
- Secret supplied on the command line: the CLI refuses it before any network or file action and explains the
  supported channels.
- Standard output is not a terminal: colors and progress animations are disabled by default.
- Dimensions outside 1–500 columns or 1–200 rows are clamped or rejected per Feature 001 limits.
- `whr setup` interrupted midway: rerunning resumes or reports partial state; it never reports success.
- Unknown flag or argument: usage error with a short hint, no side effects.

## Requirements *(mandatory)*

### Functional Requirements

**Command surface**

- **FR-001**: `whr` MUST provide the commands `ws` (`ls`, `show`, `create`, `start`, `stop`, `rebuild`, `rm`),
  `open`, `shell`, `console`, `ssh`, `agent` (`ls`, `attach`, `status`, `send`, `stop`), `setup`, `doctor`,
  `offboard host`, `service` (`install`, `uninstall`, `status`), `version` and shell completion.
  Bare `whr` and `--help` print help; `--version` equals `version`.
- **FR-002**: Every workspace command MUST act through the same owner-authenticated application operations as the
  browser, with identical authorization, idempotency, trust, audit and confirmation rules.
- **FR-003**: Commands that mutate workspaces MUST accept an optional idempotency key and report the durable
  operation identifier; repeating a key with a changed payload MUST fail as a conflict.
- **FR-004**: `ws rm` MUST delete runtime only by default; data deletion MUST require a separate flag and an
  explicit confirmation that cannot be implied by unattended mode.
- **FR-005**: `ws create` MUST display resolved config and submodule commits and obtain explicit owner trust before
  any repository-provided hook executes; changed commits require new trust.
- **FR-006**: `shell`, `console` and `ssh` MUST use single-use, expiring, workspace- and owner-bound grants and the
  session limits of Feature 001; they MUST NOT provide raw host shell, Docker socket or host-secret access.
- **FR-007**: `whr console <ws>` MUST attach to the workspace's persistent console session and support status
  and close actions; `whr shell <ws>` MUST start a fresh login shell inside the workspace.
- **FR-008**: `whr ssh` MUST pin the workspace server key from the authenticated control plane, use short-lived
  client credentials refreshed before each connection, and ignore the user's personal SSH configuration and agent.
- **FR-009**: `whr setup` MUST support dry-run, list every intended change, consume a one-time enrollment code and
  install the host agent service; it MUST be resumable after interruption.
- **FR-010**: `whr doctor` MUST report each check as pass, fail, skipped or unknown with a remediation, support a
  shareable redacted report, and never report an absent tool or unmeasured check as passing.
- **FR-011**: `whr offboard host` MUST remove the service and local host credentials, request identity revocation,
  and delete workspace data only with a separate flag and confirmation. Unattended mode MUST be refused.
- **FR-012**: `whr service` MUST install, uninstall and report the host agent service state without exposing
  credentials in output.
- **FR-013**: Unattended or answer-file modes MUST NOT approve trust, destructive deletion or offboarding.

**Output formats**

- **FR-014**: Standard output MUST carry command data only; human progress, prompts and diagnostics MUST go to
  standard error.
- **FR-015**: `--json` MUST emit one versioned envelope (schema version, result or error code and message,
  operation identifier where applicable); streaming commands MUST emit one envelope per line.
- **FR-016**: Color MUST follow `--color auto|always|never` and `--no-color`, defaulting to off when output is not
  a terminal or when the conventional no-color environment setting is present.
- **FR-017**: Errors in human mode MUST be one line prefixed `whr:` followed by an optional remediation line.

**Exit codes**

- **FR-018**: `whr` MUST publish one exit-code table, freeze it with automated tests and document it with the
  release. Usage errors, not-found, authorization failure, conflict or wrong state, timeout and interruption MUST
  have distinct codes. The numeric assignment MUST be a new exit-code table recorded in an ADR (owner decision
  H12:c; the ADR is tracked in a new issue) and fixed before implementation. The legacy `whr` table (0 OK,
  1 error, 2 usage, 3 not found, 4 auth, 5 conflict, 6 needs-human, 7 timeout, 8 quit, 9 interrupted, 10 task
  failed) and the development-script table of Features 003/004 (0, 2–6, with 2 meaning blocker) are reference
  material only.
- **FR-019**: Interactive access commands MUST pass through the remote shell's exit status (1–255) and use the
  interrupted code when the transport is lost.

**Secret handling**

- **FR-020**: Credentials and enrollment codes MUST be accepted only through a file readable solely by the owner
  or through standard input or an inherited pipe; they MUST be refused in arguments, URLs and command history,
  and never written to output, logs, reports or Git.
- **FR-021**: Locally stored CLI and host credentials MUST be kept outside workspaces with owner-only permissions;
  `doctor` MUST flag wider permissions.

**Terminal-text safety**

- **FR-022**: Human output that includes untrusted text MUST replace C0 controls except tab, DEL, C1 controls,
  bidirectional controls and Unicode line/paragraph separators with a visible placeholder; JSON output MUST escape
  them.
- **FR-022a**: Interactive streams (`shell`, `console`, `ssh`, `agent attach`) MUST filter OSC, DCS, APC, PM and
  SOS sequences by default, including sequences split across frames or left unterminated. Unfiltered output
  MUST require an explicit per-session raw opt-in that is shown when the session starts and never persisted
  (owner decision on #223).

**Release and packaging**

- **FR-023**: Releases MUST originate from a human-signed annotated `vX.Y.Z` tag on `main` with green required
  checks; the signer MUST be on a reviewed allow list.
- **FR-024**: Each release MUST provide per-platform archives named with version, OS and architecture, a checksum
  file, a software bill of materials and build-provenance attestation that users can verify.
- **FR-025**: Automated release jobs MUST produce a draft only; publication and package-channel updates require a
  human; prereleases MUST NOT be published to stable package channels.
- **FR-026**: Each release MUST have release notes stating verified and unverified platforms.
- **FR-027**: `whr version` MUST report version, source revision, build date and supported contract versions, in
  human and JSON form.

**Scope and governance**

- **FR-028**: Workspace lifecycle, access and host commands MUST work without an AI provider, tracker or cloud
  account.
- **FR-029**: `whr agent` MUST support list, attach, status, send input and stop through the in-container session
  interface only, under the owner, workspace and grant authorization of FR-002 and FR-006 (decision H9).
- **FR-030**: `whr` MUST NOT install, configure, authenticate, launch or wrap an agent's native tool, and MUST NOT
  receive or relay provider credentials.
- **FR-031**: `whr agent send` input MUST be size-bounded and passed as data; `whr` MUST NOT interpret it.
- **FR-032**: A read-only web dashboard MUST show specs, plans, tasks, review evidence and workspace/agent status,
  each with its Git revision or observation time, and MUST offer no editing, approval or dispatch (decision H8).
- **FR-033**: The CLI and the dashboard MUST share one local, owner-authenticated API; the dashboard MUST NOT gain
  any capability the API does not grant the CLI.
- **FR-034**: `run`, `pause`, `resume`, `usage` and `purge` are deferred and MUST NOT be added by this feature.

### Key Entities

- **CLI profile**: control-plane address, owner identity reference and local credential location for one owner.
- **Operation reference**: durable operation identifier, idempotency key and final or current status.
- **Access grant**: single-use, expiring authorization binding owner, workspace and session type.
- **Host enrollment**: one-time code, resulting host identity, service state and revocation status.
- **Agent session**: an agent process inside one workspace, with identifier, status and the five interface actions.
- **Dashboard view**: a read-only projection of Git-revisioned Spec Kit artifacts, review evidence and status.
- **Doctor report**: ordered checks with status, evidence and remediation, redacted for sharing.
- **Output envelope**: schema version, data or error, exit code and operation reference.
- **Release artifact set**: archives, checksums, bill of materials, provenance and release notes for one tag.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The Feature 001 lifecycle (create, inspect, stop, start, delete runtime) completes using only `whr`
  on the acceptance host, with operations and audit records indistinguishable from browser-initiated ones.
- **SC-002**: Automated tests prove that cross-owner commands, reused or expired grants, unapproved revisions,
  secrets in arguments and unattended destructive actions are rejected with the documented exit codes.
- **SC-003**: Every command's `--json` output validates against the published schema in success and failure cases.
- **SC-004**: A corpus of hostile names and messages containing every control class in FR-022 renders without
  emitting any of those characters to a terminal, and a hostile stream corpus (including split and unterminated
  OSC, DCS, APC, PM and SOS sequences) reaches the terminal filtered unless raw opt-in was given.
- **SC-008**: Every `whr agent` action and every dashboard page is rejected for foreign workspaces in automated
  tests, and a review of the shared API shows no dashboard route that mutates state.
- **SC-005**: Offboarding leaves no host credential on disk and the control plane rejects the revoked identity.
- **SC-006**: A release can be verified end to end (signature, checksum, bill of materials, provenance) by a user
  following the published instructions in under ten minutes.
- **SC-007**: Evidence records exact host, OS, tool versions and commands; mocked tests alone do not demonstrate
  host setup, access or offboarding.

## Assumptions

- The CLI talks to the control plane for every workspace operation; it never contacts a host agent or provider
  directly. Host commands (`setup`, `doctor`, `service`, `offboard`) run on the host itself with local privileges.
- Linux Docker is the first supported host; Apple container and other providers follow the product sequence.
- `console` is redefined from the legacy host-VM console to a reattachable workspace session, because the
  architecture forbids raw host shell access through the relay.
- Owner authentication for the CLI reuses the control-plane owner login; the exact login flow is a planning
  decision within the constraints of FR-020.
- Legacy `whr` behavior is reference only; no legacy code or binary is a dependency.
- Development scripts of Features 003–005 keep their own exit codes and are not the product CLI.
- The session interface is comparable to herdr in purpose only; no herdr protocol, behavior or dependency is
  assumed. Its wire format is a planning decision.
- The dashboard reads Spec Kit artifacts at an identified Git revision; how it relates to the ADR 0006 board is
  settled in the documentation lane.
