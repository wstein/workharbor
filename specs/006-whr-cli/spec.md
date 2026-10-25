# Feature Specification: `whr` product command line

## Pre-ready compatibility amendment (2026-10-20)

Owner direction: implement the new requirements without premature historical
migration/backward-compatibility obligations; schema versions remain useful for
future evolution. Keep current `schema_version` values, strict numeric/type checks,
closed v1/v2 shapes and all safety/conformance acceptance. Do not infer a schema
merge, host-resolver input change, task-graph change or permission to run providers.
Current `contract_versions` reporting, product/build identity and host/session
protocols remain. "Freeze" tests check the current contract and required coverage;
they do not freeze every historical accepted set or require new-version machinery
solely for pre-ready compatibility. See the [bounded impact inventory](pre-ready-policy.md).
This amendment changes requirements for pending init/template work; it claims no
init implementation, prior review CLEAR, or approval of the full feature.


## Amendment (2026-10-23): host session and local shell

Owner request: work from one persistent herdr session on the host with host panes and workspace panes
([Feature 010](../010-host-and-workspace-panes/spec.md), [ADR 0015](../../docs/adr/0015-host-session-and-local-shell.md)).
This amendment adds `shell --local`, makes `plain` the default console mode (FR-048) and states what does not apply to a local
pty. It is proposed text: it needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced
by a tracker or PR link. Implementation record: the separate owner-gated work items have landed in PRs
[#220](https://github.com/wstein/workharbor/pull/220), [#221](https://github.com/wstein/workharbor/pull/221) and
[#222](https://github.com/wstein/workharbor/pull/222), reconciling the local command contracts and the `plain` configuration default
(Feature 010 FR-020). Remote console session creation and real-host acceptance remain separate.

## Amendment (2026-10-24): selected local stage/promote interaction

Owner direction [LS-1](#ls-1-owner-direction-2026-10-24) selects a primary by
container name/full ID plus an explicit source directory, fingerprint trust, and
Docker healthcheck or the existing workspace smoke script over stdin. This proposed amendment records that direction
in FR-068, FR-063 and SC-024 with [the local contract](contracts/local-stage-promote.md).
The approval record is an owner-authored approval or owner merge of
[PR #229](https://github.com/wstein/workharbor/pull/229), covering this interaction
amendment only. Before that record it is proposed; afterwards S0 still gates code
and live operations. The D1/D2 decision record does not approve the full execution profile. The plan explicitly retains an
execution-profile gate before product implementation. No command is activated,
T038 stays open, and the dependency of T039 on full T038 is unchanged.

The owner decision on #226 keeps the whole-prefix home protection and excludes
existing Anchorage staging/temp cleanup from that task. This amendment neither
changes FR-067(e) nor claims a cleanup path for those protected containers.
New candidate-home naming and a compatible promote deletion-consent surface are
reserved for a separate owner decision and task, not folded into T038 by this
amendment or inferred as approved by LS-1.

### LS-1: owner direction (2026-10-24)

Source: the foreman's [Owner decision record for D1 and D2 on PR #229](https://github.com/wstein/workharbor/pull/229#issuecomment-6385032899),
dated 2026-10-24, records the owner's choices after discussing both options.
D1/D2 were decided by the owner on 2026-10-24; that comment is the decision record.

- **D1 selection B:** exact primary container name or full container ID
  (`<id|name>`) with explicit `--source <dir>`; no directory selector.
- **D1 trust:** the previewed source fingerprint requires explicit trust separately
  from general confirmation. Unattended application supplies the exact
  `--trust <digest>` plus `--yes`; changed inputs invalidate trust. This is not
  the committed-revisions-only alternative.
- **D2 verification:** promotion requires a running candidate with matching verified
  image and mount/user configuration, and a passing Docker healthcheck when one
  is declared. Otherwise require an explicit successful smoke result using
  Anchorage's existing `.devcontainer/smoke-container.sh`: fingerprint-bound host
  bytes over stdin to fixed argv `bash -s -- <container-workspace-path>`, with
  displayed output escaped per FR-022. Neither the built-in-only nor file-entrypoint
  alternative is selected. No host `smoke.sh` orchestrator or Anchorage file changes
  are authorized.

This decision does not select new home names or promote deletion flags; those
remain a separate owner decision and task. It does not approve the execution
profile or authorize provider operations. The interaction amendment's exact-text
approval record remains the owner approval/merge of [PR #229](https://github.com/wstein/workharbor/pull/229);
S0 remains a separate engineering and approval gate.

**Feature identifier**: `006-whr-cli`
**Feature Branch**: `docs/006-whr-cli`
**Created**: 2026-10-13
**Status**: Amended 2026-10-16/17 (decision H77: grouped command structure, short-form aliases, local `validate`
and `doctor`, and a first increment that also implements the `ws init` core and wizard and a pure resource-parsing library) on top of the
workspace initialization and resource-selection revision, which is proposed for owner review (2026-10-16); not implemented.
The H77 amendment text is proposed: it needs an independent review CLEAR for its exact head and then explicit owner
approval of that head, evidenced by a tracker or PR link (WORKFLOW.md). Werner's chat decisions of 2026-10-16/17 (including his chat approval of `tasks.md` from T009 onward, H77 (5)) are
the owner decisions recorded below, not approval of this text, and merging this amendment needs renewed owner approval of the spec, the plan and the whole `tasks.md` at one exact head, evidenced by a tracker or PR link, before T009 or any of T025 to T035 can start, and the owner's request for human review of the plan,
task scope, configuration schema and launcher contracts of the initialization revision is not satisfied by
this document. Earlier scope status: Revised after review; awaiting independent re-review. Owner approval for planning (H13:a, 2026-10-13)
covers only the pre-amendment revised text, and only once that re-review is CLEAR for its exact head; it does not cover the H77 text or the 2026-10-16 revisions. Not implemented. Owner decisions
are recorded below; implementation prerequisites are listed under "Prerequisites".
**Input**: Specify the product CLI `whr` for workspace lifecycle plus host and operations commands (groups
`workspace`, `host`, `auth`, `op`, `agent` with daily short forms such as `open`, `shell`, `console`, `ssh`, `setup`,
`doctor`, `offboard`, `service`, plus `init`, `validate` and `version`), release and packaging conventions,
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

### Owner decisions (2026-10-16): H77

Numbering: the highest decision number recorded in the repository is H76:a, so this amendment is H77. The owner
(Werner, 2026-10-16) decided:

- **H77 (1) - Command structure**: long form `whr <group> <verb>` for the groups `workspace` and `host`; short
  form `whr <verb>` for the daily verbs. The canonical group name is `workspace` and `ws` is an alias. Workspace
  verbs `ls`, `show`, `create`, `start`, `stop`, `rebuild`, `rm`, the interactive `shell`, `console`, `ssh`, `open`
  and the new `init` and `validate` have short forms. Host verbs `setup`, `doctor`, `offboard`, `service` have short
  forms; `host ls` and `host show` have none because `ls` and `show` belong to the workspace. Authentication is
  `whr auth login|status|logout` with the short forms `whr login` and `whr logout`. Decided here: `auth status`
  keeps no short form, because bare `whr status` is ambiguous between authentication, workspace and console state
  (ADR 0014).
- **H77 (2) - Local commands**: `whr validate [dir]` (read-only check of `workharbor.json` against its schema, of the
  devcontainer configuration and of pinned submodules, with `--json`) and `whr init [dir]` (the local initialization
  wizard that US0 and FR-043 to FR-050 specify, which validates its output with `validate`'s code and is the only
  writing command of the first increment) need neither control plane nor login. `whr doctor` additionally runs the
  validate checks when the current directory contains a `workharbor.json`.
- **H77 (3) - Alias rules**: one command table in code; short forms are aliases of the same command; the table is
  frozen by a test like the exit-code table; the canonical name appears in `--json` (`command: "workspace.ls"`),
  logs and errors; a verb that exists in two groups gets the short form only for `workspace`; `--help` shows the
  short form as a hint; shell completion knows both forms; a new short form needs a table change.
- **H77 (4) - First increment**: implementation order A foundation, B `validate`, C `doctor` (local checks), D
  `init`; `setup --dry-run` only if cheap and consistent with FR-009. Everything that needs the owner API stays
  specified but is out of the first increment and blocked on it. Verified on 2026-10-16:
  `apps/control_plane/lib/workharbor_web/router.ex` serves only `/api/v1/hosts` (enrollment issue, revoke, status
  by id), `/api/v1/auth` (login, status, logout, revoke, dashboard credentials) and the login and owner pages; no
  workspace, operation, session, agent, host-list or dashboard API exists.
- **H77 (5) - Placement and scope (chat 2026-10-16/17)**: the first-increment work items run on their own branch and
  pull requests, not in the T001-T024 chain (Werner's chat answer "5approved" on 2026-10-17 to the question of renewed approval of
  `tasks.md` from T009 onward is recorded here as decision H77 (5); it is not approval of any head's text, per WORKFLOW.md
  the approval must refer to one exact head with a tracker or PR link, to be added when the specification pull request
  exists, and until then no work item of T009 onward or T025 to T035 may start on the strength of this amendment);
  `setup --dry-run` stays out of the first increment (FR-061); `ADR 0013` gets a superseding ADR for "remote
  unreachable" before owner-API client work, and `init`'s unclassified code stays until then; `doctor` exits 0 with the
  verdict `incomplete` when only skipped or unknown checks remain and runs read-only on macOS. Scope extension of
  2026-10-17: wave 1a is the foundation, `validate`, local `doctor`, the manifest-schema extension (FR-058) and then the
  non-interactive core of `ws init`, plus a pure resource parsing library with shared vectors and no `ws create`
  wiring; wave 1b is the interactive wizard and devcontainer reuse; the starter catalog stays unsupported and `ws
  create` wiring comes with the owner API in wave 2 (FR-061). The work items run in separate worktrees, branches and pull
  requests.
- **H77 (7) - Answers to the open questions (chat 2026-10-17, "1j 2j 3j 4j 5approved")**: (1) `name` and `project` are
  confirmed in manifest version 2, `name` being the suggested workspace name for `ws create` (its grammar matches the
  future owner-API name rule) and `project` a desired input only; (2) the exit codes are confirmed: 64 for `--template`,
  68 for invalid configuration and a failed doctor check, 72 for wizard cancellation, 73 taking precedence over 72 and
  the original code on an incomplete rollback, and 70 only in unattended mode; (3) the host-agent resolver work item
  that accepts manifest version 2 is added now (T035); (4) the 5 s value is the per-Git-call deadline and the per-run
  bound of the hostile corpus, as interpreted in H77 (6), (5) the chat answer "5approved" (2026-10-17), recorded as decision H77 (5), not approval of any head's text. Implementation note, not
  part of the answers: the 5 s bound is realised as one shared per-run budget (FR-055). None of this approves the amendment text of a head; that needs an independent review CLEAR for its
  exact head and the owner's approval with a tracker or PR link.
- **H77 (6) - Thresholds (owner confirmation, chat 2026-10-17)**: "Git >= 2.39.5, 2 GiB, 64/256/64 KiB, 5 s,
  256 MiB seems to be fine from my side." These are the minimum Git version, the free-disk threshold, the manifest,
  devcontainer and `.gitmodules` size limits, the time bound (applied both to each Git call and to each hostile-corpus
  run) and the memory bound of FR-054 to FR-060; the owner confirmed the interpretation of "5 s" (H77 (7)). A change
  needs a spec amendment and a test change.

These decisions are recorded in [ADR 0014](../../docs/adr/0014-whr-command-structure.md) and the contracts
[`commands.json`](../../contracts/cli/commands.json), [`output.schema.json`](../../contracts/cli/output.schema.json),
[`doctor-checks.json`](../../contracts/cli/doctor-checks.json), [`validate-findings.json`](../../contracts/cli/validate-findings.json),
[`workharbor.schema.json`](../../contracts/workspace/workharbor.schema.json), [`vectors.json`](../../contracts/workspace/vectors.json), [`resources-vectors.json`](../../contracts/cli/resources-vectors.json) and [`starters.json`](../../contracts/workspace/starters.json). Throughout this specification a
short form such as `whr ws create`, `whr open` or `whr offboard` names the same command as its canonical spelling
`whr workspace create`, `whr workspace open` or `whr host offboard`.

### Prerequisites

Implementation MUST NOT start before all of the following exist:

- Constitution amendment (issue #75) and product/architecture reconciliation (issue #66, merged), including the
  ADR 0006 supersession note (#68).
- The ADR 0001 amendment for the Go CLI and the `apps/` module layout (issue #67, H10:a, H11:a, H23:a).
- The exit-code ADR with the fresh numeric table (issue #70).
- The versioned in-container session interface contract under `contracts/` (issue #71).
- A reconciliation of Feature 001's terminal cleanup rule with the console persistence model of FR-007a, recorded
  in the Feature 001 spec or an ADR.

The first increment (FR-061, H77) needs only the constitution (v1.1.0), [ADR 0010](../../docs/adr/0010-go-whr-cli-layout.md)
and [ADR 0013](../../docs/adr/0013-whr-exit-codes.md) (both Accepted 2026-10-16, H69:a) and the proposed
[ADR 0014](../../docs/adr/0014-whr-command-structure.md); it uses neither the session interface contract (#71) nor
the console reconciliation, which gate the interactive and agent commands. This amendment does not verify the state
of the documentation reconciliation issues (#66, #68, #75); the workpad of the foundation work item records it.

### Local workspace initialization (owner request, 2026-10-16)

`ws init` gains the local wizard specified in US0 and FR-043–FR-050. The owner
requested separate human/agent shells, file preview, existing devcontainer reuse,
unattended equivalents, a fish default and a console mode choice (herdr or plain; the default is plain since 2026-10-23), workspace/project/directory
inputs, starter templates, shared shortcuts and later optional bundle selection.
This revision specifies those outcomes; it does not implement them. It requires
human spec review and reconciliation/review of affected plans, task scope,
configuration schema and launcher/console contracts before implementation. No
previous implementation approval or completed task implicitly covers this scope.

### Workspace creation resources (owner request, 2026-10-16)

`ws create` gains CPU and memory selection under FR-039–FR-042. This is proposed
product scope, not a claim that the accepted persistence, owner API or Docker
provider contracts already accept or enforce resource fields. Before implementation,
reconcile and review those contracts, the browser path and the affected plan/tasks.
Existing approvals and completion evidence retain their original source/scope;
this revision does not expand them or authorize runtime operations.

### Agent workflow block in `AGENTS.md` (owner request, 2026-10-17)

The owner decided that the owner-workflow rules for agents (own-branch pushes and
rewrites, Ready as mergeable, delegation, stack handling) belong to the workspace
repository, not to every project repository, because contributors need not use
WorkHarbor. `ws init` therefore authors that block in the workspace `AGENTS.md`
(FR-064, US0 scenario 10). This revision specifies the outcome; it does not implement
it, and it needs the same human spec review as the revision "Local workspace initialization" above before
implementation. It changes no accepted task and approves no runtime operation.

## User Scenarios & Testing *(mandatory)*

### User Story 0 - Initialize workspace configuration locally (Priority: P1)

An owner runs `whr ws init` in a local directory, answers a short wizard, reviews
its proposed files and writes a configuration repository ready for later revision
resolution and trusted creation. It works before sign-in and without a host.

**Why this priority**: Owners can prepare a repeatable workspace without manually
learning every configuration field or replacing an existing project environment.
**Independent Test**: Initialize empty and existing configuration directories,
compare interactive and unattended results, cancel and inject write failures,
then validate shell/session preferences without provisioning a workspace.

**Acceptance Scenarios**:

1. **Given** a terminal and no existing configuration, **When** `whr ws init` runs,
   **Then** it asks for workspace name, optional project repository, starting
   directory, existing devcontainer or starter, human shell, terminal mode and
   shortcuts; it offers fish, plain and enabled shortcuts as defaults for missing
   settings, and shows bash as the fixed WorkHarbor agent-command shell policy.
2. **Given** an existing `.devcontainer` and WorkHarbor settings, **When** init runs,
   **Then** it retains existing explicit values, asks only for missing settings,
   and offers explicit edits; it never replaces the selected environment implicitly.
3. **Given** the chosen answers, **When** review opens, **Then** every proposed file
   is listed with its contents or diff, and the owner can go back, write or cancel;
   cancellation and pre-write validation failure leave original bytes unchanged.
4. **Given** complete equivalent flags and `--non-interactive --yes`, **When** init
   runs in a new directory, **Then** it produces the same configuration as the
   wizard without prompts; missing required values yield needs-human and the
   missing flag names. `--json` and nonterminal use never prompt.
5. **Given** existing files that need edits, **When** unattended init lacks explicit
   per-file overwrite permission, **Then** it refuses without changing any files;
   a file changed since preview also refuses before any write (exit 68); the only
   exceptions to the missing-permission refusal are `AGENTS.md` (FR-064) and `.claude/settings.json` (FR-065),
   which without the flag are left unchanged with exit 0 (`AGENTS.md` with a warning).
6. **Given** fish for humans, **When** a new human shell or console starts after
   trusted creation, **Then** it runs fish; WorkHarbor-controlled noninteractive
   agent commands use bash and existing script interpreters remain unchanged.
   No agent-native tool is installed, authenticated or launched, and none is configured except by the authored,
   credential-free `.claude/settings.json` (FR-065, owner decision of 2026-10-17) and `AGENTS.md` (FR-064).
7. **Given** a persistent console (plain by default, or herdr when selected), **When** its owner disconnects and reattaches,
   **Then** the same console survives with the same shell/environment and a fresh
   grant; stop/rebuild/runtime removal/explicit close terminate it. A fresh `shell`
   still ends on disconnect. Missing selected capabilities refuse explicitly.
8. **Given** a project repository, **When** a revision is resolved for creation,
   **Then** the generated project declaration must become a pinned project commit
   and the exact config/submodule revisions are presented for separate owner trust;
   a generated URL or floating branch never constitutes a pin or approval.
9. **Given** an injected failure while writing several files, **When** init exits,
   **Then** it restores original files and removes newly generated files, or reports
   partial failure with exact remaining paths and recovery steps if restoration fails;
   it never reports a partially written configuration as successful.
10. **Given** a workspace without or with an existing `AGENTS.md`, **When** init
   runs (the block is on by default), **Then** an absent file is created without a flag;
   for an existing file the block is appended or replaced only when an edit is requested
   (`--overwrite AGENTS.md`, plus `--yes` unattended, or the owner's confirmation of that
   path in the wizard), otherwise the file is left unchanged with a warning and exit 0;
   the precedence is size (a file already over 64 KiB: `.too_large`), malformed
   (`.malformed`), modified, current (no-op, no warning), different_template,
   missing; a result over 64 KiB of a modified, different-template or missing edit gives `.too_large`
   (68 with the flag); an edit requested by the flag that would be refused is 68 in every mode, and a
   wizard edit without the flag that would be refused is not offered (warning printed);
   every byte outside the markers is preserved; `--no-agents-block` skips the file; a
   rerun is a no-op.
11. **Given** a workspace without or with an existing `.claude/settings.json`, **When** init runs (on by default,
   FR-065), **Then** an absent file is created with `{"attribution": {"sessionUrl": false}}`; an existing file is left
   unchanged with exit 0 and a next step, unless `--overwrite .claude/settings.json` (plus `--yes` unattended; the flag
   pre-confirms it in the wizard) replaces it; with the flag a symbolic link or special file is refused (68), without
   it the path is left unchanged; `--dry-run` previews the file; a rerun is a no-op.

---

### User Story 1 - Sign in and manage workspace lifecycle from a terminal (Priority: P1)

An owner configures `whr` for a WorkHarbor endpoint, signs in, and then lists, inspects, creates, stops, starts,
rebuilds and deletes workspaces with the same authorization, idempotency, trust and confirmation rules as the
browser dashboard.

**Why this priority**: The CLI is only useful if it reaches the core lifecycle without weakening any guarantee.
**Independent Test**: Against a control plane with one enrolled Docker host, start from an empty client profile,
sign in, run each `whr ws` subcommand in human and machine-readable modes, and compare resulting operations,
audit records and observed state with the browser view.

**Acceptance Scenarios**:

1. **Given** no client profile, **When** any owner API command runs, **Then** it fails with the authentication code
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

### User Story 1a - Choose workspace CPU and memory (Priority: P1)

The owner chooses runtime CPU and memory limits while creating a workspace, sees
exact effective values and host support, and can inspect the same values later.

**Why this priority**: Owners need predictable workspace limits without changing
project images or manually editing provider arguments.
**Independent Test**: Create through CLI and browser with supported limits, omitted
values, invalid values and an unsupported host; inspect desired and observed limits,
retry a lost response, then stop/start and rebuild the supported workspace.

**Acceptance Scenarios**:

1. **Given** an interactive create on a supported host, **When** resource selection
   opens, **Then** CPU and memory show the defaults and accept explicit values;
   the final summary shows effective limits and their origin before submission.
2. **Given** `--cpus 2 --memory 4GiB`, **When** create succeeds, **Then** inspect shows
   desired limits of two CPU equivalents and 4 GiB, with observed enforcement or
   an explicit unknown state; the browser reports the same effective limits.
3. **Given** omitted resource flags in unattended mode, **When** create runs, **Then**
   it uses and reports explicit host-profile defaults; absent defaults refuse with
   the needs-human code and guidance to supply both values, without provisioning.
4. **Given** zero, negative, malformed, excessive-precision or out-of-range values,
   **When** create is requested, **Then** it refuses before hooks or provider mutation;
   an unsupported limit capability also refuses rather than silently ignoring it.
5. **Given** a submitted creation request, **When** its response is lost, **Then** a
   same-key retry retains the frozen resource values despite profile changes;
   changing either limit with that key yields conflict.
6. **Given** a created workspace, **When** stop/start or rebuild occurs, **Then** the
   recorded limits persist; this feature provides no implicit live resize.

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
service and later offboards the host without leaving credentials behind. The scenarios below describe the Linux/systemd
enrollment; on macOS `whr setup` is the host setup wizard of [Feature 009](../009-host-setup-wizard/spec.md) (FR-009).

**Why this priority**: Host enrollment is required before any workspace exists, but the browser can already
display enrollment codes; the CLI makes the host side repeatable.
**Independent Test**: On a disposable Linux Docker host, run `whr setup --dry-run`, `whr setup`, `whr doctor`,
`whr service status` and `whr offboard`, including injected failures between offboarding steps; inspect
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
6. **Given** an enrolled host and confirmation, **When** `whr offboard` (canonically `whr host offboard`) runs and every step succeeds,
   **Then** it reports success only after the control plane confirms revocation and the service and local host
   credentials are removed; workspace data is deleted only with a separate flag and confirmation.
7. **Given** the control plane is unreachable or a step fails, **When** `whr offboard` stops, **Then** it
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
6. **Given** a command typed as a short form (`whr validate`), a group alias (`whr ws validate`) or canonically
   (`whr workspace validate`), **When** each runs with `--json`, **Then** all three execute the same command and
   report `command` as the canonical `workspace.validate`; logs and error lines use the canonical name.
7. **Given** `whr --help` and `whr host --help`, **When** they print, **Then** each command with a short form shows
   it as a hint, `host ls` and `host show` show none, and completion scripts for bash, zsh and fish offer both
   spellings of every available command.
8. **Given** the command table, **When** the freeze tests run, **Then** a changed, added, removed or conflicting
   command, group alias or short form fails them, including a short form for a verb that exists in two groups.
9. **Given** a command that is specified but unavailable in this release (for example `whr ls`), **When** it runs,
   **Then** it exits with the usage code and one line naming the dependency it waits for, performs no network
   access and creates no profile or file; with `--json` the same appears as the envelope error.

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

### User Story 7 - Validate a workspace configuration repository locally (Priority: P1, first increment)

A developer or a CI job checks a workspace configuration repository on their own machine, before any control plane,
login or Docker daemon exists, and learns what is wrong and how to fix it.

**Why this priority**: It is the first command that works without the owner API (H77) and it protects the later
trust and provisioning path from malformed or hostile configuration.
**Independent Test**: Run `whr validate` on the admitted image and Dockerfile fixtures, on the historical example,
and on a corpus of hostile directories (below); check exit status, `--json` against the published schema,
that the directory tree (names, size, mode, inode, mtime and ctime; not atime) is unchanged, that no marker file from a hostile hook was
created and that no network connection was attempted.

**Acceptance Scenarios**:

1. **Given** a directory with a valid `workharbor.json`, a regular devcontainer file inside it and a `projects`
   directory, **When** `whr validate <dir>` runs, **Then** it exits 0, reports `valid: true`, lists warnings (for
   example `git.not_repository`) and states `provider_admission: not_evaluated`; it never claims the configuration
   is provisionable.
2. **Given** a directory without `workharbor.json`, **When** it runs, **Then** it exits with the conflict/wrong-state
   code and one `manifest.missing` finding with a remediation naming `whr init`; **Given** a path that does not
   exist, **Then** it exits with the not-found code; **Given** a path that is a file, **Then** it exits with the
   usage code.
3. **Given** a manifest with an unknown or missing field, a `schema_version` other than 1 or 2, a version 1 manifest carrying a version 2 field, a non-empty `bundles`
   list, or an absolute, traversing, backslash-bearing, glob-bearing, `.git`-containing or empty path, **When** it
   runs, **Then** every such defect is reported as its own error finding in one run (bounded to 200 findings), and
   the exit code is the conflict/wrong-state code.
4. **Given** a manifest or devcontainer file that is larger than its limit, not UTF-8, begins with a byte-order
   mark, contains NUL, has a duplicate key at any depth, is nested deeper than the limit, or is not valid JSON
   (JSONC for the devcontainer file), **When** it runs, **Then** it reports a finding without reading beyond the
   limit, without echoing file content other than a truncated, escaped key or path, and without hanging.
5. **Given** `workharbor.json`, the devcontainer file, any directory on the way to them, or `projects_path` is a
   symbolic link, a FIFO, a socket, a device or a directory where a file is expected, **When** it runs, **Then** it
   reports an unsafe-path error, never opens or follows the link or special file and does not disclose the link
   target.
6. **Given** a devcontainer file that uses features outside the admitted provider subset (Features, Compose,
   `initializeCommand`, `postAttachCommand`, `mounts`, `runArgs`, a tag instead of a digest, non-numeric users),
   **When** it runs, **Then** each is a warning that names the capability matrix and the result remains valid.
7. **Given** a Git repository root with `.gitmodules` and gitlinks, **When** it runs, **Then** each submodule is
   reported with its pinned commit; a `.gitmodules` entry with credentials in the URL (reported redacted), a
   non-HTTPS/SSH or relative or `ext::`/`file:` URL, an `update = !command` or a path outside `projects_path`, and
   a gitlink with no `.gitmodules` entry are errors; an entry without a gitlink is an error (the resolver rejects it) and a gitlink that differs from
   `HEAD` is a warning.
8. **Given** a hostile repository whose configuration would run code when processed by an unhardened Git or
   container tool (`core.fsmonitor`, hooks, filters, aliases, `.gitmodules` update commands, devcontainer
   commands), **When** `whr validate` runs, **Then** no repository-provided code executes (a marker file is not
   created) and nothing in the directory, including `.git`, is modified.
9. **Given** `--json`, **When** it runs in any case above, **Then** standard output carries exactly one envelope that
   validates against the published schema and the result carries counts and findings; in human mode every
   untrusted name, path or URL is rendered inert (FR-022).
10. **Given** a version 2 manifest with valid or invalid settings (unknown enum, traversing `start_dir`, credential in
    `project.url`, bad `name`) and a version 1 manifest carrying a version 2 field, **When** it runs, **Then** each defect
    is its own error finding without echoing the credential, a valid declaration without a pinned gitlink is the
    warning `project.declared_not_pinned`, and the shared vectors give the same codes.
11. **Given** a repository whose HEAD is unborn with a staged submodule, **When** it runs, **Then** it reports
    `git.head_missing`, reads `.gitmodules` from the index and validates the staged submodule; any other Git failure
    is `git.listing_failed`.
12. **Given** interruption (SIGINT or SIGTERM) while it runs, **When** it stops, **Then** it exits with the interrupted code and
    has changed nothing.

---

### User Story 8 - Diagnose the local environment (Priority: P1, first increment)

An operator or developer runs `whr doctor` to see whether this machine has what WorkHarbor needs, with checks that
require the control plane clearly marked as not run.

**Why this priority**: Host diagnostics are useful before enrollment and give `validate` a natural place in daily
use; the control-plane checks cannot run until the owner API exists.
**Independent Test**: Run `whr doctor` on a Linux Docker host, on macOS with OrbStack, on a host without a
Docker-compatible socket, with an old or missing Git, with a nearly full filesystem (injected measurement port) and
in a directory with valid and invalid `workharbor.json`; compare status, evidence redaction and exit status.

**Acceptance Scenarios**:

1. **Given** a host with Git at or above the minimum, a connectable Docker-compatible socket and enough free disk,
   **When** `whr doctor` runs, **Then** `platform.host`, `container.socket`, `git.version` and `disk.working_directory`
   report `pass` with measured evidence (`disk.container_storage` is `pass` only where the storage path is readable and
   known, otherwise `unknown`), the control-plane and host-credential checks report `skipped` with a reason,
   the verdict is `incomplete` and the exit status is 0.
2. **Given** no socket among the documented candidates, a refused or permission-denied connection, an absent or old
   Git, or free space below the threshold, **When** it runs, **Then** each is `fail` with a remediation (the socket
   remediation links the container-runtime page), the verdict is `failed` and the exit status is the
   conflict/wrong-state code; an absent tool is never `pass`.
3. **Given** `DOCKER_HOST` with a non-`unix` scheme, **When** it runs, **Then** `container.socket` is `unknown` and the
   remote daemon is not contacted.
4. **Given** the current directory contains `workharbor.json`, **When** it runs, **Then** `config.valid` equals the
   validate result for that directory (`fail` on any error finding); **Given** no `workharbor.json`, **Then** it is
   `skipped` with the reason `precondition_absent`. Parent directories are not searched.
5. **Given** a platform that is not in the host-platform matrix, **When** it runs, **Then** `platform.host` is
   `unknown`, the other read-only checks still run, and the mutating host commands (`setup`, `service`,
   `offboard`) still refuse that platform.
6. **Given** `--json`, **When** it runs, **Then** the output is one envelope that validates against the schema and
   contains no home directory path (shown as `~`), user name, host name, environment value or credential, so that
   it can be shared.
7. **Given** any run, **When** it finishes, **Then** (plain `doctor`, without `--fix`) it has created no file, started no process other than the fixed
   `git --version` and, when `workharbor.json` is present, the fixed read-only invocations of FR-055, installed or configured no container runtime, and, for the checks of FR-060, made no network connection other than
   connecting to the discovered local socket (the host checks of [Feature 009](../009-host-setup-wizard/spec.md) may use the network, FR-015).

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
- `whr setup` or `whr offboard` interrupted midway: rerunning resumes or reports partial state; neither ever
  reports success for incomplete work.
- Mutating host commands (`setup`, `service`, `offboard`) on an unsupported host platform fail with an unsupported-platform error before any change; `doctor` runs read-only and reports the platform `unknown`.
- Unknown flag or argument: usage error with a short hint, no side effects.

- Init on a nonterminal never prompts; JSON mode also suppresses prompts on a terminal.
- Existing explicit shell/session choices survive init; new defaults fill missing fields only.
- Previewed files change, a target path escapes through a symlink, or rollback fails:
  refuse/restore as FR-046 specifies and report partial failure honestly.
- A selected shell/herdr is unavailable, a starting directory is missing, or bundle
  attachment is unimplemented: report the specific missing capability without substitution.

- `validate` or `doctor` pointed at a hostile directory (FR-055): a symlinked or special `workharbor.json`, a FIFO
  that would block a read, a sparse file of enormous apparent size, non-UTF-8 or NUL bytes, a byte-order mark,
  duplicate or very deeply nested keys, very many `.gitmodules` entries or index paths, a `.git` file that
  redirects elsewhere, Unicode control or bidirectional characters in names, and repository configuration that would
  execute code (fsmonitor, hooks, filters, update commands) are bounded findings; nothing is executed, followed,
  echoed raw or modified.
- `doctor` without any Docker-compatible socket (a fresh machine) fails `container.socket` with a link to the
  container-runtime page and installs nothing; with a remote `DOCKER_HOST` it is `unknown`.
- A command that is specified but unavailable in the first increment (FR-061) exits with the usage code and names what it waits for.
- `init` write races and partial states (FR-057): a path created or replaced by another process after the preview, a full disk and
  interruption at every step leave the complete valid result or exactly the previous state, or report a partial failure
  with the remaining paths.

## Requirements *(mandatory)*

### Functional Requirements

**Command surface**

- **FR-001**: `whr` MUST provide the commands of the command table (FR-051): the group `workspace` (alias `ws`) with
  `init`, `validate`, `ls`, `show`, `create`, `start`, `stop`, `rebuild`, `rm`, `shell`, `console`, `ssh` and `open`;
  the group `host` with `setup`, `doctor`, `offboard`, `service` (`install`, `uninstall`, `status`), `ls` and `show`;
  the group `auth` with `login`, `status` and `logout`; the group `op` with `show` and `wait`; the group `agent` with
  `ls`, `attach`, `status`, `send` and `stop`; and the top-level `version`, `completion` and `help`. Short forms
  follow FR-052. Bare `whr` and `--help` print help; `--version` equals `version`. Only the commands and parts marked
  as first increment in the table are available before the owner API exists (FR-061); the others are specified by
  this feature and registered, but unavailable.
- **FR-002**: Every remote workspace command MUST act through the same owner-authenticated application operations as the
  browser, with identical authorization, idempotency, trust, audit and confirmation rules.
  `ws init` is a local configuration-authoring exception: it requires no client
  profile, owner API request, host enrollment or provisioning operation.
- **FR-003**: Every workspace lifecycle mutation (`ws create`, `start`, `stop`, `rebuild`, `rm`) MUST send an idempotency key: the caller's, or else one the CLI generates and
  reports before sending. The CLI MUST reuse that key for its own bounded retries, report the durable operation
  identifier, and fail as a conflict when a key is reused with a changed payload.
- **FR-003a**: Workspace lifecycle mutations covered by FR-003 MUST wait for a terminal operation state by default, support `--no-wait` (return
  the operation identifier immediately) and `--timeout`. Interruption or timeout MUST NOT cancel the operation
  or report it complete; the CLI exits with the interrupted or timeout code and the operation identifier.
- **FR-003c**: Interactive streams and agent controls follow their session contracts, not lifecycle-operation waiting. `agent send` MUST NOT be retried automatically after uncertain delivery; report delivery as unknown and require explicit owner resend. Host-local setup, service and offboarding use resumable local checkpoints rather than workspace operation records.
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
  or host-secret access. The one exception is `shell --local` of Feature 010 (FR-013 there): run on the Docker host with `--local`,
  it needs no grant because its caller already uses the host's Docker socket; it is never an automatic fallback.
- **FR-007**: `whr shell <ws>` MUST start a fresh login shell inside the workspace that ends with its attachment.
  With `--local` on the Docker host, `whr shell --local <ws>` starts a fresh login shell through the local Docker daemon and the FR-066 discovery,
  without control plane or grant; its lifetime is governed by Feature 010 FR-007, not by this sentence.
- **FR-007a**: `whr console <ws>` MUST attach to the workspace's single persistent console session, creating it if
  absent and saying so. The console session's lifetime MUST be independent of its attachments:
  - client disconnect, grant expiry, idle timeout and the maximum attachment duration end only the attachment;
    its relay resources are released within Feature 001's five seconds and the console process keeps running;
  - workspace stop, workspace rebuild, runtime deletion and `whr console <ws> --close` end the console session;
  - host disconnect ends the attachment and leaves console state unknown until reconciliation;
  - reattachment always requires a new grant and shows a bounded retained output tail, noting any dropped output;
  - `whr console <ws> --status` reports whether a session exists, its start time and current attachments.
  - The console has no local mode: `whr console <ws> --local` exits 64 (Feature 010 FR-011).
- **FR-008**: `whr ssh` MUST pin the workspace server key from the authenticated control plane, use short-lived
  client credentials refreshed before each connection, and ignore the user's personal SSH configuration and agent.
- **FR-008a**: `whr ssh <ws> --print-config` MUST generate a client configuration for external SSH clients and IDEs
  with the pinned server key and a short-lived credential reference, and MUST state that connections made with it
  are neither relayed nor filtered by `whr` (FR-022a does not apply to them).
- **FR-009**: `whr setup` MUST support dry-run, list every intended change, consume a one-time enrollment code and
  install the host agent service; it MUST be resumable after interruption. The dry run is not part of the first
  increment (FR-061) and arrives with `setup` itself. On macOS `whr setup` is the host setup wizard of
  [Feature 009](../009-host-setup-wizard/spec.md): it consumes no enrollment code, installs no host agent service and has its own dry run
  (Feature 009 FR-018). On Linux this requirement is unchanged.
- **FR-010**: `whr doctor` MUST report each check as pass, fail, skipped or unknown with a remediation, support a
  shareable redacted report, and never report an absent tool or unmeasured check as passing. The local subset available in the first increment
  is defined by FR-060.
- **FR-011**: `whr host offboard` (short form `whr offboard`) MUST be confirmed interactively, refuse unattended mode, and proceed in resumable
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

**Workspace creation resources**

- **FR-039**: `ws create` MUST accept `--cpus <value>` and `--memory <size>` and offer
  the same choices interactively when input and diagnostics are terminals and
  unattended/JSON mode is not selected. CPU is a positive base-10 number of CPU
  equivalents with at most three fractional digits; memory is a positive whole
  number suffixed `MiB` or `GiB`. Reject signs, exponent notation, nonfinite values,
  missing units and overflow. Canonical values are integer thousandths of a CPU
  and integer bytes, so equivalent spellings have identical request identity.
- **FR-040**: Effective values MUST resolve independently per field from explicit
  flags, then the selected host's explicit default resource profile. Show both
  effective values, their origins and host support before submission; unattended
  output MUST also report them. Missing defaults in unattended mode yield needs-human;
  interactive callers MUST supply the missing values. Never interpret omission as
  unlimited resources. These are runtime limits, not guaranteed reservations or
  build-process limits, and do not change the selected language image.
- **FR-041**: The owner API and provider MUST validate both effective limits against
  the selected host's supported range and enforcement capabilities before hooks or
  provider mutation. Unsupported or unavailable capability evidence MUST refuse
  without silent fallback, clamping or dropping a limit. Resource limits are not
  Dev Container `hostRequirements`; this revision does not admit that field or raw
  provider arguments. Browser creation MUST use the same validation/default rules.
- **FR-042**: Freeze normalized limits in the creation payload before the first
  submission and include them in idempotency conflict detection, durable desired
  state and audit. Retries MUST reuse them, not recompute defaults. `ws show` and
  the browser MUST distinguish desired limits from observed enforcement with its
  observation time, and label absent/stale evidence unknown. Stop/start and rebuild
  MUST retain the values. Resizing existing workspaces is outside this revision.

**Local workspace initialization**

- **FR-043**: `ws init [directory]` MUST author local configuration only. It MUST NOT
  create/start a workspace, build or attach artifacts, run repository hooks, fetch
  project code, install tools, change account login shells, commit/push Git changes
  or grant execution trust. It MUST show next steps to commit configuration, resolve
  and pin project revisions, then use `ws create` with separate owner trust.
- **FR-044**: With terminal input and diagnostics, absent unattended/JSON mode,
  init MUST start a wizard with back/review/cancel. Offer workspace name, optional
  project URL, workspace-relative starting directory, reuse of an existing
  devcontainer or a small supported starter catalog, human shell (`fish`, `bash`,
  `zsh`, `nu`), persistent console mode (`herdr`, `plain`) and shared shortcuts
  (`on`, `off`). Existing explicit settings take precedence over wizard defaults;
  prompt only for missing values unless the owner requests edits. Validate names,
  credential-free URLs and paths; reject traversal and symlink escapes. Starting
  directory defaults to the workspace root and must exist in the runtime before
  spawning; never silently launch in a different directory.
- **FR-045**: Every wizard choice MUST have a flag: `--name`, `--project-url`,
  `--start-dir`, mutually exclusive `--devcontainer`/`--template`, `--human-shell`,
  `--console-mode`, and `--shortcuts`. `--non-interactive`, nonterminal input or
  diagnostics, and `--json` MUST suppress all prompts. Missing required inputs
  after existing settings and documented defaults yield needs-human with the
  relevant flags. `--yes` confirms writing new files only; it never approves trust,
  destruction or overwriting. The same inputs MUST generate equivalent configuration
  across wizard and unattended modes. Prompts/previews go to stderr under FR-014;
  JSON results/errors follow FR-015 with no raw prompt text on stdout.
- **FR-046**: Before writing, init MUST validate the full proposal and show every
  proposed file and contents/diff. `--dry-run` MUST return the proposal without any
  writes. Existing-file edits require explicit confirmation of each affected path;
  unattended use requires repeatable `--overwrite <relative-path>` for each such
  file plus `--yes`. Preserve unrelated fields/files, detect changed bytes since
  preview, and refuse unsafe paths before writing. Cancel/pre-write refusal MUST
  make no changes. Write failures MUST restore the prior state; failed restoration
  MUST return partial failure and exact remaining paths/recovery guidance. A rerun
  with identical settings MUST be a no-op. No hidden bootstrap side effects.
- **FR-046a**: Any future init proposal that generates or edits installation-PATH
  setup MUST use one shared, configurable installation-directory policy across
  the supported shells (FR-047), with adapters in their existing startup files.
  Absolute `$HOME/bin` MUST precede `$HOME/.local/bin`, each exactly once, and
  both MUST precede every BundleDock executable entry, including
  `/opt/bundledock/toolbox/codex-path`. Go's actual install directory (`GOBIN`,
  otherwise the first `GOPATH` entry's `bin`) and explicitly configured absolute
  executable directories MUST also precede all BundleDock entries. Preserve the
  user's explicit directory order where compatible with these constraints; do
  not prescribe an additional fixed ordering of toolchains. Remove duplicate,
  empty and relative entries before tool discovery as well as in the resulting
  PATH, handle spaces in HOME and prefixes, and do not add the working directory.
  Startup adapters MUST invoke their helper through a known absolute interpreter
  path; Go settings lookup MUST use the sanitized absolute-only PATH. Neither
  lookup nor adapter startup may execute a project-local `go` or `sh` through an
  empty or relative PATH entry. The shared configuration accepts absolute bin
  directories, including a tool's actual installation root plus `bin`; it MUST
  NOT require separate configuration mechanisms for each language. An existing
  absolute `CARGO_INSTALL_ROOT`, otherwise `CARGO_HOME`, MUST automatically add
  that root's `bin` before all BundleDock entries without redundant shared-config
  opt-in. A relative selected root MUST be ignored without resolving it against
  CWD or guessing a lower-priority root. `CARGO_HOME` can be `/usr/local/cargo`;
  do not assume it is home-relative. A selected `--root` or `install.root` uses
  the shared explicit-bin mechanism ahead of environment-derived entries; no
  Cargo invocation or parsing of flags/project configuration is required. With
  neither environment variable set, the actual root (Cargo's default `~/.cargo`
  unless configured otherwise) uses the same explicit mechanism. Crystal shard
  `./bin` and Zig `zig-out/bin` MUST NOT
  be added globally: only an explicit trusted project or user opt-in may add
  their absolute output directory. No toolchain installation is part of init.
  All affected files and exact contents/diffs remain inspectable in the FR-046
  proposal, with per-path overwrite consent, unrelated settings preserved and
  no silent rewriting of existing JSONC. Wave 1a T030 and wave 1b T033 continue
  to reuse existing devcontainers byte-for-byte; this requirement does not
  enable starter generation, startup-file editing or runtime activation in
  either task. They may offer next-step guidance through existing output fields.
  Generation/editing and its acceptance remain future work requiring owner
  approval of the exact amended spec, plan and tasks; this text grants none.
- **FR-047**: Default missing human-shell preference to fish, and retain bash for
  WorkHarbor-controlled noninteractive agent command execution. These are separate
  policies: human preferences MUST NOT change script shebangs, hook interpreters,
  native agent settings or noninteractive command behavior. Do not redirect bash
  startup into fish or rely on `$SHELL` to enforce command execution. Report external
  agent executor shell choice as unmanaged unless independently established; `whr`
  MUST NOT configure native agents to make that claim. Apply human-shell policy to
  new `shell` and `console` sessions; existing console reattachment retains the old
  shell/environment until explicit close/new session. Validate executable availability
  at trusted creation/session launch; missing/unknown support MUST refuse with
  remediation, never silently substitute or install a shell.
- **FR-048**: Default missing console mode to plain, with explicit herdr as an optional alternative (amended 2026-10-23: the
  owner's herdr session runs on the host, [ADR 0015](../../docs/adr/0015-host-session-and-local-shell.md)).
  Either mode MUST preserve FR-007a's one owner-scoped persistent console, bounded
  output tail, grant/attachment limits and end events. herdr in the container is an optional
  human-console backend, never required by any `whr` command, not an agent launcher or an assumed agent-session protocol, and `whr`
  gives no nesting guidance and does not detect or avoid a multiplexer on the host.
  When herdr is selected, backend availability/version compatibility MUST be established before launch;
  otherwise refuse with remediation to supply it through a separately approved
  environment or explicitly select plain. `whr shell` remains a fresh shell ending
  with its attachment, and `shell --local` never starts a console or a multiplexer. No backend may expose host sessions/sockets or
  widen access.
- **FR-049**: Reuse existing devcontainer contents by default. Starters MUST use
  only admitted capabilities and immutable image inputs, preserve language-image
  choice, and expose their supported/unsupported status. Init MUST NOT execute a
  template to validate it or claim runtime compatibility from static validation.
  Optional project URLs are desired inputs only: no fetching during init. The
  separate resolver MUST pin the exact project commits before creation and trust;
  unresolved declarations MUST NOT be presented as creation-ready. Default shortcuts
  on when missing, with a complete off option under Feature 007; absent toolbox
  means unavailable, not active. BundleDock-managed Starship stays disabled.
- **FR-050**: Optional bundle selection is a later capability. Until a separately
  reviewed resolver/attachment contract and runtime evidence exist, init MUST emit
  an empty bundle list for new manifests and label selection unavailable. Preserve
  existing bundle declarations unchanged without claiming attachment support; refuse
  requested bundle edits until that capability is reviewed. It MUST NOT offer
  actionable selections or fabricate attachment success. Later selection MUST offer an explicit
  none option and record immutable artifact/source identities subject to independent
  trust/attachment verification. CPU/memory and host selection belong to `ws create`,
  not this local wizard.

**Command table, local commands and the first increment (H77)**

- **FR-051**: `whr` MUST define one command table in code, mirrored by the language-neutral contract
  [`contracts/cli/commands.json`](../../contracts/cli/commands.json), from which command registration, help, shell
  completion and the `--json` `command` field are generated. The table lists for every command its canonical id,
  group path, optional short form, increment, dependency (none, control plane or host-local), whether it writes,
  its `--json` mode and its flags where this specification fixes them (`workspace.init`, `workspace.create`).
  Automated offline tests MUST freeze the table against the contract file, as the exit-code table is frozen
  (FR-018), so that any difference fails.
- **FR-052**: Short forms follow these rules, enforced by the table test: (a) a short form is an alias of the same
  command object, never a second implementation, and behaves identically including flags, output and exit
  status; (b) short forms exist only for the verbs listed in H77 (1), which is an allowlist; as a constraint on it, a
  verb that exists in more than one group (`ls`, `show`, `status`, `stop`) has a short form only for the `workspace`
  group, so `host ls`, `host show`, `auth status`, `op show` and every `agent` verb have none (`whr service
  install|uninstall|status` keeps its `service` prefix, so it is not a bare `status`); (c) short forms are unique and
  do not equal a group name or a top-level command, except the `service` prefix itself; (d) `ws` is an alias of the
  group `workspace` only, so upstream text such as `ws init` and `ws create` names `workspace init` and
  `workspace create`; (e) `--help` of a command shows its short form as a hint, and shell completion offers both
  spellings of every available command; (f) adding or changing a short form needs a table change, a freeze-test
  change and an owner-reviewed amendment of this specification.
- **FR-053**: The canonical command id (`workspace.ls`, `workspace.init`, `host.service.install`, `auth.login`) MUST
  be the value of the `--json` `command` field, and MUST be the name used in logs and error messages, whichever
  spelling the caller typed. An unknown command or alias fails with the usage code and a one-line hint naming the
  nearest valid spelling.
- **FR-054**: `whr workspace validate [dir]` (short form `whr validate`) MUST read a workspace configuration
  directory (default: the current directory) without writing, without network access and without credentials, and
  report findings, each with a stable code, a severity (`error`, `warning`, `info`), an optional subject path
  relative to the directory, a message and a remediation. The result is `valid` exactly when no finding has
  severity `error`. The authoritative, fully qualified list of codes and severities is
  [`contracts/cli/validate-findings.json`](../../contracts/cli/validate-findings.json); the checks are:
  - the manifest `workharbor.json` against [its schema](../../contracts/workspace/workharbor.schema.json) with the
    strict-JSON profile of ADR 0010 (`manifest.*`: `missing`, `unsafe_path`, `too_large`, `encoding`, `syntax`,
    `duplicate_key`, `schema`, `path_invalid`, `bundles_deferred`; an unreadable manifest is `manifest.unsafe_path`);
  - the devcontainer file named by the manifest (`devcontainer.*`): errors `missing`, `unsafe_path`,
    `too_large`, `encoding`, `syntax`, `duplicate_key`; warnings `unsupported_key`, `image_not_digest_pinned`,
    `users_not_numeric`, `host_command` for content outside the provider subset of the
    [capability matrix](../../docs/development/devcontainer-matrix.md);
  - the version 2 settings of the manifest (FR-058) as `manifest.schema`, `manifest.path_invalid` (`start_dir`) and
    `manifest.url_credentials` (`project.url`, never echoed); a project declaration without a pinned gitlink is the
    warning `project.declared_not_pinned`;
  - `projects_path` (`projects.*`): errors `missing`, `unsafe_path`; warning `not_a_submodule` for committed
    regular files under it other than `.gitkeep`;
  - Git state, only when the directory itself contains `.git` (`git.*`, `gitmodules.*`, `submodule.*`): an unsafe
    `.git` (symbolic link, gitfile, `commondir`, `objects/info/alternates`, or a non-regular `.git/HEAD`, `.git/index` or `.git/config`), a failed, timed-out or unparsable Git call (an error, so Git failure never
    hides submodule errors), a listing beyond its bound (an error), non-canonical or case-folded `.git` components in
    the `HEAD` tree, the committed `.gitmodules` blob (mode, size, syntax, duplicate fields, keys other than `path`
    and `url`, which the resolver rejects), and each submodule: URL form (`https://`, `ssh://` or the scp-like
    `user@host:path` the resolver admits; relative, `file:` and `ext::` refused), credentials (any userinfo in an
    `https` URL or a password, never echoed), an `update` value that is a command, a gitlink without an entry, an
    entry without a gitlink (the resolver rejects it), a path outside `projects_path`, nested or overlapping paths,
    an index gitlink that differs from `HEAD` (warning) and the pinned commit (info). Git-derived checks read the
    index, the `HEAD` tree and the committed blob, as the resolver does; the other checks read the working tree.
    HEAD is unborn exactly when `git rev-parse --verify --quiet HEAD` exits 1 with empty standard error and `.git/HEAD`
    is a symbolic reference to a missing ref; then `git.head_missing` is reported, `ls-tree` and `cat-file HEAD:` are
    skipped, `.gitmodules` is read from the index (`cat-file blob :.gitmodules`, only when the index lists it) and the
    submodule checks run against the index. Every other non-zero exit, a timeout or unparsable output is
    `git.listing_failed`. With a `HEAD`, `unlisted` and `not_pinned` compare `HEAD`-tree gitlinks with the committed
    `.gitmodules` blob (read only when the `HEAD` tree lists it); staged-only differences are the warnings
    `submodule.not_committed` and `gitmodules.uncommitted_change`.
  Path rules refuse any case variant of `.git`, as the resolver does, and `schema_version` must be the integer 1 or 2 (not
  `1.0`). The result states `provider_admission: not_evaluated`: validation never claims a configuration is
  provisionable, because admission is the host agent's decision at trust time. Findings beyond 200 are counted but
  not listed (`truncated`). All independent defects are reported in one run. `--json` follows the
  [output schema](../../contracts/cli/output.schema.json).
- **FR-055**: `validate` MUST treat the directory as hostile. It MUST NOT execute, source, interpolate or follow
  any content of the directory: no repository hook, filter, alias, `core.fsmonitor`, `.gitmodules` update command or
  devcontainer command runs, and no devcontainer or `.gitmodules` value is expanded. It MUST open only
  the manifest, the devcontainer file, the `.gitmodules` file and the directory entries named by
  the manifest, walking each path component without following symbolic links, requiring a regular file (or
  directory) by `fstat` on the opened descriptor, never blocking on a FIFO, socket or device, and reading at most
  64 KiB (manifest), 256 KiB (devcontainer file) and 64 KiB (`.gitmodules`, at most 256 entries) plus
  bounded depth, key counts and index listing size, all within one shared 5 second wall-clock budget per run (file reads
  and every Git call draw on the remaining budget; exhaustion during Git work is `git.listing_failed`, elsewhere the error `findings.time_budget_exhausted`). Before any Git call it
  checks by `lstat` that `.git/HEAD`, `.git/index` (if present) and `.git/config` are regular files, else `git.unsafe_path`.
  It MUST NOT reveal a link target, a file's content beyond a
  truncated escaped key, path or redacted URL, or a credential contained in a URL. Git is used only through a fixed,
  read-only invocation with a freshly built environment (no inherited `GIT_*` variables, no system or global
  configuration, no trace output, no optional locks, no pager, no prompts, neutral working directory, empty
  standard input) and `-c` overrides of `core.fsmonitor`, `core.hooksPath` and `protocol.allow` plus `protocol.<name>.allow=never` for
  `file`, `git`, `ssh`, `http`, `https` and `ext`, with `GIT_ALLOW_PROTOCOL` set to a name that matches no protocol and
  `GIT_NO_LAZY_FETCH=1` (the repository's own `.git/config`, including includes, is still parsed; a per-protocol
  setting overrides `protocol.allow`, so a partial clone or promisor remote must never trigger a fetch, an
  `uploadpack` or `sshCommand`, or a credential helper; the override list and the minimum Git version carry the
  protection; `safe.directory` is never set). A `.git` containing `commondir` or `objects/info/alternates` is
  `git.unsafe_path` that lists the index and `HEAD` tree; an absent, too old or unusable Git
  while `.git` is a real directory yields the error `git.unavailable` (the Git-derived checks did not run, so the result
  is never `valid`); without a `.git` it is not reported. A `.git` that is a symbolic link or a file (gitfile) yields `git.unsafe_path`
  and no Git call. On macOS a Git that would open the Command Line Tools installer is detected without executing it
  and reported as unavailable. It MUST NOT modify any file or directory, including `.git`.
- **FR-056**: `whr workspace init` (short form `whr init`) implements FR-043 to FR-050 as specified by the owner,
  as configuration authoring only, in two waves (owner decision of 2026-10-17). Wave 1a (D1, the non-interactive core):
  the proposal model, the preview of every file with contents or diff, `--dry-run`, `--non-interactive`, `--yes`,
  `--overwrite <relative-path>`, the flags `--name`, `--project-url`, `--start-dir`, `--human-shell`, `--console-mode` and
  `--shortcuts`, detection of changes since the preview, restore-or-report-partial rollback, the no-op rerun, preserved
  existing settings, validation with the code of `validate` (FR-054), and reuse of an existing devcontainer at the
  manifest's `devcontainer_path` (default `.devcontainer/devcontainer.json`); without one, unattended init exits
  needs-human (70) whose remediation names `.devcontainer/devcontainer.json`. By the owner's decision of 2026-10-17 (starters
  unsupported) US0 scenario 4 ("in a new directory") is accepted in wave 1a only for a directory that already contains a
  devcontainer file; this narrows the scenario until wave 1b/2, it does not change it. `--non-interactive` without `--yes` and
  without `--dry-run` exits 70 naming `--yes`; `--devcontainer` in wave 1a exits 64 ("not in this release"). Without `--non-interactive` and without `--dry-run`, wave 1a (no wizard yet) exits 64 ("not in this release"; the interactive case is the wave 1b wizard), on a terminal or not. The placeholder file
  is `<projects_path>/.gitkeep`. Wave 1b: the interactive wizard (D2: back, review, cancel; its terminal test
  harness is its own work item) and `--devcontainer <path>` selection with devcontainer reuse in the wizard (D3).
  Wave 2: `--template` and the starter catalog, which stay listed as unsupported (FR-058) until the provider admits
  them; `--template` refuses with that explanation. The settings, including `--name`, are written to the manifest (FR-058). Because init only authors configuration (FR-043), the runtime parts of FR-047 and FR-048
  (launching fish or herdr, console grants, availability checks at creation or session launch) and every bundle
  selection (FR-050: empty list only) are not in the first increment and are labelled so in the output
  (`runtime_enforcement: not_in_this_release`). The agent workflow block of FR-064 is on by default and is delivered by T036 in wave 1b after D1: it adds `AGENTS.md` to the D1 proposal output and changes the no-op-rerun baseline, so T036 updates the D1 (T030) tests and vectors, and until it lands D1 proposes no `AGENTS.md`. Likewise FR-065 (T037, after T036) adds `.claude/settings.json` and `.claude/`; until it lands, D1 and T036 propose neither. init runs no Git command, makes no network connection and never fetches a
  project: `--project-url` (`https://` or `ssh://`, no `https` userinfo, no query or fragment, no control or bidirectional characters, well-formed
  percent escapes) is written as the manifest's `project` declaration with the path `<projects_path>/<name>`, where
  `<name>` is the last path component of the URL with one trailing `.git` removed and must match
  `[A-Za-z0-9][A-Za-z0-9._-]*` (otherwise a usage error), and the next steps tell the owner to
  `git init -b main`, add and pin the project as a submodule and commit; until pinned the project is not creation-ready
  (validate warns `project.declared_not_pinned`). Exit codes follow FR-063. This reconciliation is the human review
  input that the owner's text requires for the plan, task scope, configuration schema and launcher contracts; it is not
  their approval.
- **FR-057**: `init` MUST add these safety properties to FR-046: (a) it validates the complete proposal with the code
  of `validate` (FR-054) in its Git-free profile (manifest, devcontainer and projects checks; no Git command runs, the
  Git-derived checks are marked `git_checks: not_run` and the next steps tell the owner to run `whr validate` after
  `git init`; `project.declared_not_pinned` is reported whenever a project is declared) in memory before the first write
  and on the written result after the last write, and reuses the same findings, computed in memory, for `--dry-run`; an error finding in generated files rolls back and exits with the
  unclassified code, an error caused by content it preserved is the conflict code, detected before the first write
  where possible; (b) targets are created with exclusive no-follow creation relative to a no-follow-opened target
  directory, so a path created or replaced by another process after the preview is a conflict; existing real
  directories (`.devcontainer`, `projects`, `.claude`) are reused and never removed by rollback; a symbolic link,
  file or special file at a target path is refused unless the file is named by `--overwrite` and is a regular file, except that a regular file whose bytes equal the
  proposal is `skip_existing` and needs no flag, except the paths of FR-064 and FR-065, whose outcomes those requirements define;
  (c) an overwrite writes a temporary file with the deterministic prefix `.whr-init-tmp-` in the same directory with the
  old file's mode, re-checks the identity (`fstat` device and inode) and content hash of the opened target immediately
  before the rename, keeps the old bytes until then and restores them on failure; a change in the remaining window
  between that check and the rename is documented, not eliminated, and leftover temporary files are detected by the next
  run like other leftover paths; (d) rollback removes exactly the paths this run created, and restores
  overwritten files, verifying device and inode before each removal, never removes the target directory unless this
  run created it, and an incomplete rollback is a partial failure (73) listing the remaining paths and recovery steps,
  taking precedence over the original failure code; (e) SIGINT and SIGTERM trigger the rollback and exit 72; SIGKILL or a
  crash is not rolled back and the next run refuses on the leftover paths with the conflict code; (f) wizard
  cancellation changes nothing; (g) new manifests use schema_version 2. Existing manifests that pass the current
  v1/v2 validator use the same proposal, preservation, preview and per-path overwrite rules as
  any existing file: keep their bytes when no edit is requested, and never rewrite merely to
  match a newer version or materialize missing defaults. An explicit requested change proposes
  replacement using the current authoring format (v2), showing the entire diff and retaining
  existing explicit settings; it requires ordinary per-path overwrite consent before any write.
  A v2 proposal or written replacement MUST warn that it is not creation-ready until T035
  makes the resolver accept version 2 (FR-058), in both preview and the reported result.
  There is no separate defaults-only v1 workflow, automatic version bump, migration/upgrade mode
  or `settings_not_persisted` emission. Do not claim missing/default settings were persisted when
  their fields were not written. Explicit flags equal to defaults still count as explicit;
  identical proposed bytes are a no-op; dry-run reports the same proposal/consent outcome without
  writing. Both versions require `bundles: []`, so unsupported bundle declarations remain
  byte-unchanged and refuse with 68 under FR-050, never rewritten; (h) files are created 0644 and directories 0755 subject to the umask; no secret is
  accepted, written or printed.
- **FR-064**: `ws init` MUST propose the workspace `AGENTS.md` through the proposal model of FR-046 and
  FR-057 unless `--no-agents-block` is given; the block is on by default (owner decision of 2026-10-17). The
  agent workflow block comes from one template shipped with `whr`. It covers: pushes of topic branches and PRs by
  agents within the requested task; rewrites of agent-authored PR branches only after checks and an exact-SHA review
  of the rewritten head, then only with an explicit expected-head lease; Ready meaning the owner can merge and requiring
  independent CLEAR review of the exact current head; Draft only with a named blocker; owner-only merge, tag, release,
  publication and settings; an approval covering one named operation with no standing approval; submodule gitlinks
  belonging to the owner; running source checks with `umask 022` (avoiding sandbox `umask 0000` permission breakages);
  stacked PRs targeting their parent branch, linked as a stack, and retargeted to `main` once the parent merges; never
  setting per-repo git identity; bot updater commits and PR titles naming the changed versions; and delegation by model
  tier with one worktree per parallel agent. It embeds no stack facts, names no Anchorage path, repository, remote,
  gitlink, credential or vendor-specific model, and adds no automation mode (D57 unchanged). The Anchorage umbrella
  `AGENTS.md` (sections "Pull requests and history" and "Delegation") is the source text from which the template is
  derived, not its first instance: it has no versioned markers and holds Anchorage-specific text. The template is
  authored configuration, not project policy.

  *Markers.* The block is delimited by the exact LF-terminated lines `<!-- whr:agents-block begin v=<N> sha256=<hex> -->`
  and `<!-- whr:agents-block end -->` at column 0; `<N>` is the template version and `<hex>` the SHA-256 of the body,
  the bytes after the LF of the begin line up to and including the LF before the end line. Any other line that,
  after removing a trailing CR, leading whitespace and a leading byte order mark, begins with `<!-- whr:agents-block`
  is marker-like and makes the file malformed, wherever it is (including inside a code fence); quoting a marker
  outside a block is unsupported. Malformed also covers a begin without an end, an end without a begin, an end before
  its begin, a nested begin and more than one block. Bytes outside the markers (including CRLF and a BOM) are
  preserved; the inserted block uses LF and an appended block follows one blank LF line. The shipped template body
  contains no marker-like line.

  *Classification, one predicate, one precedence.* An edit is requested when `--overwrite AGENTS.md` is given
  (plus `--yes` unattended) or, in the wizard, the owner confirms that path; for `AGENTS.md` and `.claude/settings.json` (FR-065) only, the flag also
  pre-confirms the path in the wizard (no generalisation of FR-046). The file is opened without following links,
  checked by `fstat` on the descriptor and read with a 64 KiB bound, never blocking on a FIFO; a symbolic link or
  special file is refused (68). Classes, first match wins: size (the file is over 64 KiB by `fstat`; it is not
  read), malformed, modified (body hash differs from the recorded one), current (version and intact
  body equal the shipped template), different_template (an intact block differs from the shipped version
  or body), and missing (no block). Keep the numeric template marker and validate its syntax; no
  historical templates or older/newer negotiation are required. Only for an edit of a
  modified, different-template or missing file, a result that would exceed 64 KiB replaces that class's warning with
  `agents_block.too_large` (no edit requested) or is refused (68, edit requested). Outcomes, each with exactly one
  action in the `--json` `actions`: an absent file is created (`create`); a current block is `skip_existing` (a rerun
  is a no-op); every other class without a
  requested edit, or with a wizard edit declined, is `left_unchanged` with `agents_block.too_large`, `.malformed`,
  `.modified`, `.different_template` or `.missing` and exit 0, so a rerun after a template bump or on a pre-existing `AGENTS.md`
  never fails; with a requested edit, size and malformed are refused (68, nothing written, rerun a no-op) and
  modified, different-template and missing are `replace_block` or `append_block`. A numeric version above
  the shipped one has no special permanent-preservation promise before readiness; it still requires
  explicit edit consent, full preview, stale-preview checks and all FR-057 rollback safeguards. For `AGENTS.md` only, declining the wizard edit
  is not a cancellation (72), and the wizard does not offer an edit without the flag that would be refused (it
  prints the warning instead); an edit pre-confirmed by the flag that would be refused is 68 in every mode. The
  general rule of FR-046 and FR-063 (a declined path is a cancellation, a refused edit is 68) stays unchanged for
  every file other than `AGENTS.md` and `.claude/settings.json` (FR-065). No "changed since preview" re-check applies to an `AGENTS.md` that is `left_unchanged` or `skip_existing`, since nothing is written. `--no-agents-block` adds no `AGENTS.md` entry and leaves the file untouched; with
  `--overwrite AGENTS.md` it is a usage error (64). `validate` never opens `AGENTS.md` (FR-055), so init checks
  marker integrity itself, in memory before the first write and on the written result.

  *Result.* T036 extends `contracts/cli/output.schema.json`: the `actions` enum gains `append_block`, `replace_block`
  and `left_unchanged` (`applied` is false for `left_unchanged` and for the `skip_existing` of a current block, as for every action of a dry run), and `initResult`
  gains `agents_block_warnings`, an always-present array (`maxItems` 1) of `{code, path}` with the codes
  `agents_block.missing|different_template|modified|too_large|malformed`; `validation` keeps its fields.
  The reserved optional `settings_not_persisted` schema property imposes no emission obligation;
  pre-ready init does not emit it. Exit codes follow FR-063.
- **FR-058**: The settings that FR-044 collects extend the manifest `workharbor.json`, as the owner decided on
  2026-10-17. [`workharbor.schema.json`](../../contracts/workspace/workharbor.schema.json) defines the accepted
  `schema_version` 1 (exactly four fields, unchanged) and the proposed `schema_version` 2: the same four fields plus
  optional `name` (lowercase letters, digits and hyphens, 1-64 characters, no leading or trailing hyphen), `start_dir`, `human_shell` (`fish`, `bash`, `zsh`, `nu`), `console_mode` (`herdr`, `plain`), `shortcuts`
  (`on`, `off`) and `project` (`url`, `path`), with the defaults of FR-047 to FR-049 when a field is absent (`start_dir`
  `.`, fish, plain, on; amended 2026-10-23, the schema and vectors follow in a separate work item); the agent-command shell is fixed to bash by policy and is not a field. Version 2 is
  fail-closed: the host-agent resolver rejects any manifest whose field set is not exactly version 1's until a separate,
  reviewed resolver change (work item T035, after T009) accepts version 2, so an init-authored manifest is not
  creation-ready on its own. The manifest vectors ([`vectors.json`](../../contracts/workspace/vectors.json)) are shared by
  `validate`, `init`, `scripts/check_repo.py` and, after that change, the resolver. The starter catalog
  [`starters.json`](../../contracts/workspace/starters.json) lists starters with an id, kind, immutable digest and status;
  every starter is `unsupported` until the provider admits it (owner decision of 2026-10-17), and init never executes a
  starter. Reading and enforcing the settings at creation and session launch is later work (Feature 007 and the
  launcher contracts).
- **FR-059**: For `workspace create` resources the first increment ships only a pure library and shared vectors, as the
  owner decided on 2026-10-17: strict parsing and canonicalization of CPU (integer thousandths of a CPU equivalent) and
  memory (integer bytes) per FR-039, the rejection classes (empty, sign, exponent, nonfinite, syntax, excess precision,
  missing or bad unit, not positive, overflow above 2^63-1) and the per-field resolution of FR-040 (explicit flag, then
  host profile, else needs-human or prompt), with the vectors in
  [`resources-vectors.json`](../../contracts/cli/resources-vectors.json), written so that the Elixir control plane can use
  the same file later. There is no command wiring, flag, prompt, summary output or submission in the first increment:
  `--cpus` and `--memory`, the interactive prompts, the creation request, the idempotency freeze of FR-042, observed
  versus desired display and the server-side validation of FR-041 are wave 2, blocked on the typed TLS owner client and
  the owner API's resource fields.
- **FR-060**: `whr host doctor` (short form `whr doctor`) in the first increment MUST run these local checks, each
  reported `pass`, `fail`, `skipped` or `unknown` with measured evidence and, for `fail`, a remediation
  (FR-010; catalogue and thresholds in [`doctor-checks.json`](../../contracts/cli/doctor-checks.json)):
  `platform.host`; `container.socket` (when `DOCKER_HOST` is set it is the only candidate, as the Docker CLI would use
  it; otherwise it discovers an existing Docker-compatible socket from the documented candidates, connects and sends
  nothing; plain `doctor` never installs or configures a container runtime (on macOS the [Feature 009](../009-host-setup-wizard/spec.md) setup wizard and `doctor --fix`, for a selected catalogue item, may install one); the failure remediation links the
  [container-runtime page](../../docs/container-runtime.md), which recommends OrbStack on macOS); `git.version` (on
  macOS a Git shim that would open the Command Line Tools installer is detected without being executed and reported
  `fail` with a remediation); `disk.working_directory`; `disk.container_storage` (measured on `/var/lib/docker` only if
  readable and the runtime's data root is known to be the default, otherwise `unknown` with the measured path or the
  reason); and `config.valid`, which equals the `validate` result when the current directory (not a parent) contains
  `workharbor.json` and is otherwise `skipped`. Checks that need the control plane or host credentials
  (`control_plane.reachable`, `host.identity`, `host.credentials_permissions`, `service.agent`) MUST report `skipped`
  with a reason. The verdict is `failed` when any check fails, `incomplete` when none fails but some are skipped or
  unknown, and `ok` only when all pass. Exit status is 0 unless a check failed (FR-063); owner decision of 2026-10-17:
  `incomplete` exits 0. Doctor has the same 5 second wall-clock budget per run (each socket dial, `git --version` and `daemon.json` read draws on it). Evidence is redacted (home directory as `~`; the user name and host name replaced wherever
  they occur in a path; no environment value or credential), so `--json` output is the shareable report of FR-010; a
  separate report file option is not part of the first increment. Plain `doctor` is read-only for the host on every
  platform (no install, no write, no `sudo`, no protocol; it may use the network for the host checks of
  [Feature 009](../009-host-setup-wizard/spec.md) FR-015, while the checks above make no network connection other than the local socket);
  `doctor --fix` on macOS is the mutating real run of that feature. FR-026's
  refusal of unsupported host platforms applies to the mutating host commands, and doctor reports an unlisted
  platform as `unknown` and continues (owner decision of 2026-10-17: allowed). `platform.host` is `pass` for Linux on
  amd64 or arm64 and `unknown` otherwise.
- **FR-061**: The first increment (H77 (4), extended by the owner on 2026-10-17) is delivered in waves. Wave 1a: A
  foundation (the command table of FR-051 with aliases, the `--json` envelope of FR-015, the `whr:` error line of FR-017,
  color of FR-016, the exit codes of ADR 0013 frozen by tests, terminal-text escaping of FR-022, `version` per FR-027,
  `completion` and `help`), then in parallel B `validate`, C local `doctor` and the manifest-schema extension of FR-058
  (checker rules, vectors, `scripts/check_repo.py`), then D1 `workspace init` non-interactive core; independently the
  resource library of FR-059. Wave 1b: the terminal test harness, the interactive wizard D2, devcontainer reuse D3 and the agent workflow block of FR-064 (T036, after D1 and the wizard D2, which owns the per-path confirmation), and the Claude Code settings default of FR-065 (T037, after D1 and T036).
  Wave 2: `workspace create` wiring and persistence with the owner API, and the starter catalog after provider
  admission. Each part is a work item that can run in its own worktree, branch and pull request (tasks). It needs no
  control plane, login or Docker daemon. `setup --dry-run` is not included for Linux (owner decision of 2026-10-16; the macOS wizard has its own dry run, [Feature 009](../009-host-setup-wizard/spec.md) FR-018): FR-009
  requires the dry run to list every intended change and neither an agent service unit nor a typed change plan exists
  before T015. Excluded, specified and blocked on the owner API (verified 2026-10-16: the router serves only
  `/api/v1/hosts` with enrollment issue, revoke and `GET /:id` status, and `/api/v1/auth` login, status, logout,
  revoke and dashboard credentials): `workspace ls|show|create|start|stop|rebuild|rm|open|shell|console|ssh`, `host ls`,
  `op` and `agent`; `host show` and `auth login|status|logout` are blocked on the typed TLS owner client and credential
  storage work item (T011), not on missing endpoints. Registered but unavailable commands exit with the usage code
  (FR-063) until their work items are accepted; they never fake a result or contact an endpoint.
- **FR-065**: `ws init` MUST propose `.claude/settings.json` in the workspace configuration directory through the
  proposal model of FR-046 and FR-057 (owner decision of 2026-10-17). Its bytes are exactly these five LF-terminated
  lines, and no others:
  `{`, `  "attribution": {`, `    "sessionUrl": false`, `  }`, `}`.
  Claude Code appends the claude.ai session link (a `Claude-Session` trailer on commits, a link in pull request
  descriptions) when it commits or opens a pull request from a cloud or Remote Control session; the default is true.
  The file turns that off for sessions whose working directory is the workspace configuration directory. It sets neither
  `commit` nor `pr`; their values come from other scopes or Claude Code's defaults; whether Claude Code merges the nested `attribution`
  object across settings scopes is unconfirmed, and its own scope precedence is unchanged. The file holds no credential,
  is read only when the owner uses Claude Code there, is harmless for workspaces that do not, and writing it installs,
  authenticates and launches nothing (scenario 6). `~/.claude/settings.json` is the owner's decision and is never read
  or written. `.claude` is proposed as a directory (`create`, or `reuse` of a real directory). A symbolic-link `settings.json` under a real `.claude` directory still yields the `.claude` `reuse` row; only a `.claude` that is not a directory has no `.claude` row.

  *Outcomes, one action each.* An absent file is created (`create`); a regular file with equal bytes is `skip_existing`
  (a rerun is a no-op). Any other existing regular file is never merged or edited by default: it is `left_unchanged`,
  exit 0, no warning; as for any existing file without a requested edit, its bytes are kept (FR-057(g)). Without `--overwrite .claude/settings.json`,
  a non-regular target (symbolic link, FIFO, special file) or a `.claude` that is not a real directory is also
  `left_unchanged` (exit 0; classified by no-follow `fstatat`, only a regular file is opened, nothing is followed, no
  `.claude` or file is created, and the only row is the one `left_unchanged` for `.claude/settings.json`, with no
  `.claude` row). Every `left_unchanged` of this
  path adds one `next_steps` entry naming `attribution.sessionUrl: false` and `--overwrite .claude/settings.json`, with
  the note that overwrite replaces the whole file, and a human output line. `applied` is false for `skip_existing` and
  `left_unchanged`. With `--overwrite .claude/settings.json` (plus `--yes` unattended; the flag pre-confirms the path in
  the wizard, which offers the replacement only then and otherwise asks nothing) the file is replaced entirely
  (`overwrite`) under FR-046; then a symbolic link, special file or non-directory `.claude` is refused (68). With or
  without the flag, a file that appears or changes after the preview is 68 (FR-057(b)), except that no re-check applies
  to a path that is `left_unchanged`. A regular file is opened no-follow and non-blocking; its `fstat` size is compared
  first and at most 64 KiB (the bound of FR-064) is read; a larger file is additionally fingerprinted by size and
  modification time, and equality is decided by size and bytes, which the content-hash re-check of FR-057(c) before
  the rename uses. An overwrite of an existing file larger than 64 MiB is refused (68) because it could not be
  restored. A rerun after an upgrade creates these
  new files once. No flag, schema field or warning code is added; T037 uses the `left_unchanged` action of T036.
  Exit codes follow FR-063.
- **FR-066**: `whr workspace ls --local` (short form `whr ls --local`) MUST inspect the local Docker daemon
  (following the socket discovery rules of `whr doctor`, FR-060) within a 5-second total wall-clock budget and fixed argument
  vectors, without evaluating shell commands or contacting any remote network endpoint (owner decision of 2026-10-19/20).
  It discovers running and stopped containers bearing recognized workspace or devcontainer labels (`anchorage-staging`,
  `anchorage-temp`, `devcontainer.local_folder`, `devcontainer.config_file`, or a WorkHarbor workspace label). Passing
  `--role <primary|staging|temp>` or `--label <key[=value]>` filters discovery accordingly; foreign, unlabelled containers
  are never returned. For each matched container, discovery reports: `id` (64-hex container ID; 12-char prefix in human
  output), `name` (without leading slash), `role` (`primary`, `staging`, `temp`), `image` reference, `state` and `status`,
  `workspace_folder` (from labels or bind mounts), `labels`, `home_volume` (named volume mounted at `/home/captain` or legacy
  `/home/vscode`, or null), and `protected_volumes` (read-only volumes, mounts under `/opt/bdock/*` and `bundledock-*` tool
  volumes). Terminal output is escaped per FR-022; `--json` emits a validated `localWorkspaceListResult`. Omitting `--local`
  preserves the remote control-plane contract (exit 64). `shell --local` (Feature 010) reuses this discovery, label rules and socket
  rules; it does not copy them.
- **FR-067**: `whr workspace rm --local` (short form `whr rm --local`) MUST stop and remove local workspace containers subject to
  hard home volume protection invariants (owner decision of 2026-10-19/20, #142):
  (a) Target selection requires an explicit container `<id|name>`, `--role <staging|temp>`, or `--label <key[=value]>`;
  omitting selectors or providing conflicting selectors is a usage error (64).
  (b) Pre-mutation ownership check: before stopping or removing any container, `whr` verifies that the target carries a
  recognized workspace/devcontainer label; foreign unlabelled containers are refused with exit 68 (`conflict`) with zero mutations.
  (c) Hard volume invariant 1 (Default preservation): removing or recreating a container preserves its `/home/captain` (or
  legacy `/home/vscode`) named home volume by default and states `preserve_home_volume: true` in the preview,
  except the receipt-bound new whr-managed staging candidates in the owner
  [throwaway amendment of 2026-10-25](contracts/throwaway-staging-amendment-20261025.md).
  Their normal confirmed removal deletes all candidate artifacts, including home,
  private clone/result tree and candidate receipts/journals/metadata.
  (d) Hard volume invariant 2 (Force requirement): deleting a home volume requires both an explicit volume flag (`--volumes`)
  and an explicit force flag (`--force`), accompanied by confirmation (`--yes` in non-interactive/scripted mode); the preview
  MUST explicitly name the target volume and all its mounts. Unattended or scripted deletion without both `--force` and `--yes`
  is refused (70); in automated execution mode, force volume deletion is strictly prohibited without explicit human confirmation.
  For those new staging candidates only, normal confirmation/`--yes` covers
  complete disposal without separate `--volumes`/`--force` or a keep mode.
  Do not manufacture those flags internally; other home removals retain this rule.
  (e) Hard volume invariant 3 (Label scoping & primary volume protection): cleanup of temporary or staging containers (targeted by
  `--role staging`, `--role temp`, or labels `anchorage-temp=1`, `anchorage-staging=1`) strictly refuses to resolve to, unlink, or
  delete the primary workspace volume (`anchorage-home-*` or the volume mounted to the primary devcontainer), even if an argument,
  flag, or environment variable targets it (refusal with exit 68 and zero mutations).
  (f) Hard volume invariant 4 (Multi-container concurrency check): if a home volume selected for deletion is currently mounted by
  any other running container, volume deletion is refused before any container is stopped or removed (exit 68).
  (g) Hard volume invariant 5 (Tool volume protection): read-only volumes, mounts under `/opt/bdock/*`, and all `bundledock-*` tool
  volumes are strictly protected and never removed under any flag combination.
  (h) Execution order: `whr` stops and removes the verified container (`docker rm -f -- <id>`), then removes the selected home
  volume (`docker volume rm -- <name>`). If volume deletion fails after container removal, `whr` exits 73 (`partial_failure`)
  naming the residual volume and recovery instructions. `--dry-run` previews all container and volume actions with zero mutations
  (`applied: false`). For new whr-managed staging, successful disposal also
  removes the candidate clone/result tree and candidate metadata, leaving no
  separate cleanup. Its normal preview discloses loss of unfetched/uncommitted
  clone work. Omitting `--local` preserves the remote control-plane contract (exit 64).
- **FR-068**: `whr workspace stage --local --source <dir> <id|name>` and
  `whr workspace promote --local <id|name>` implement side-by-side devcontainer
  verification and promotion per [the local interaction contract](contracts/local-stage-promote.md).
  `--local` is boolean; the primary selector is an exact container name or full
  64-hex ID, with no new bare short form. Stage resolves the explicit source
  directory against that primary's workspace-folder label; promote uses the bound
  candidate receipt, not an inferred workspace or mutable image tag.
  Stage builds/starts candidate `whr-anchorage-staging`, label `anchorage-staging=1`,
  alongside the running primary with an isolated staging home. Trust in the exact
  previewed source fingerprint is separate from mutation consent; unattended/JSON
  stage application requires both `--trust <digest>` and `--yes`. Dry-run needs
  neither source trust nor mutation consent and executes no source
  hook, smoke, pull, build or resource mutation. Changed inputs require fresh trust.
  The candidate must be running with expected image/mount/user configuration and
  a passing declared Docker healthcheck, or a successful workspace smoke when no
  healthcheck exists. `--smoke` explicitly requests the existing workspace smoke
  contract even with a healthcheck: trusted host bytes of
  `.devcontainer/smoke-container.sh` over stdin to fixed `bash -s -- <workspace-path>`
  in the exact candidate. Displayed output is escaped per FR-022; do not execute
  Anchorage's host smoke orchestrator or retain raw output/environment in receipts.
  Promote runs no host hook. Any container lifecycle hook it runs must be bound
  to the receipt-trusted fingerprint after a byte recheck. A missing binding
  refuses 68 before effects, or 73 after partial effects; never run the unverified
  hook. S0 must prove this. Promote revalidates the candidate and
  recreates the primary from the exact
  immutable verified image, preserving its original home. Preview warns that
  primary exec/shell panes end; require confirmation (`--yes` unattended/JSON).
  After verifying the resulting primary and the required result refs in the
  original repository, remove all new candidate artifacts: container, fresh
  `whr-stage-home-<id>` home, private clone/result tree and candidate records.
  Normal confirmation/`--yes` covers this disposal, without promote-specific
  `--volumes`/`--force`, a keep flag, backup or later clone cleanup. The owner's
  [2026-10-25 amendment](contracts/throwaway-staging-amendment-20261025.md)
  supersedes retained-clone/separate-home-consent rules for new whr-managed
  candidates only; the accepted execution-profile body stays unchanged.
  Failed stage and post-transfer promotion rollback clean their owned artifacts
  under that amendment; incomplete/uncertain cleanup reports actual residuals,
  not success. The primary and original home are never cleanup targets.
  Protected/shared/foreign homes refuse 68 before effects. Current Anchorage
  candidates with `anchorage-home-*` homes remain refused even container-only;
  no recovery cleanup path for those excluded candidates is defined here.
  The fixed name allows one candidate per Docker daemon; collisions/abandoned
  attempts refuse, never adopt/delete by name or retry uncertain effects.
  Full source capture/argv/capability/receipt proofs are the plan's execution gate;
  this amendment does not claim working-tree support or implementation readiness.

  Proposed execution-profile revision, owner session direction 2026-10-24:
  independent writable real Git clones with full history/dirty-index preservation,
  normal owner Git work, non-overwriting result fetch and retained project container
  hooks replace the unaccepted read-only/synthetic/hook-free proposal. See
  [the explicit clone/result amendment](contracts/local-stage-promote.md#proposed-cloneresult-revision-owner-session-direction-2026-10-24)
  and [technical proposal](contracts/local-stage-execution-profile.md). Fetch adds
  objects/new operation refs, not checkout/merge/current-branch changes; source
  worktree preservation does not imply all original Git bytes remain unchanged.
  The explicit cooperative phase-binding amendment (pause ordinary input writers,
  pre/post actual-byte checks, honest race limits) requires exact artifact approval
  before code. No existing-candidate rerun-verification path is added: refuse stale
  verification, then use the current throwaway cleanup/fresh-stage contract.
  Ordinary removal loses unfetched clone work; it is not retained for recovery.
  Result-only candidate commits/ref moves do not invalidate unchanged actual
  execution verification; Git identities/state consumed by execution remain bound
  and refuse on change. Original-source correspondence and D1/D2 stay unchanged.
  This note registers no flag
  or new task
  and preserves D1/D2, no-host-hook promote and FR-067.

- **FR-069**: `whr workspace drift --local` (or `whr workspace ls --local --drift`) MUST perform a read-only comparison between
  the bundle references pinned in the workspace configuration (`.devcontainer/bundle-pins.json`, `devcontainer.json`, or bundle locks)
  and the actual mounted image/volume references and version identities on the running container (#143). It lists, per bundle:
  bundle name, pinned reference, mounted reference/identity, and status (`in_sync`, `drift`, or `cannot_determine`). Output is stable,
  redacted (no credentials), and available as `--json` (`bundleDriftResult`). It NEVER pulls images, recreates containers, or mutates
  any resource. Exit status is 0 when all bundles are in sync, a distinct exit code (exit 1 or 68) when drift is detected, and exit 68
  when configuration or runtime mounts cannot be determined.
  The initial bounded Package D delivery selects `workspace drift --local --source <dir> <exact-name|full-id>`,
  running primary selection and the Anchorage JSONC pin protocol in
  [the local bundle drift contract](contracts/local-bundle-drift.md). Both drift and unknown use exit 68,
  distinguished by report status; unknown dominates mixed evidence. Canonical volume names establish only
  declared reference alignment, never content attestation. Other pin protocols remain unsupported.
- **FR-062**: The commands of the first increment MUST work offline, unattended where their flags allow and without a
  credential or provider account (FR-028). No secret is accepted, read, stored or printed by them: a
  `--project-url` with `https` userinfo or any password or token (`ssh://git@host/...` is admitted) and a `.gitmodules`
  URL containing credentials are refused or reported redacted (FR-020), and no credential is written to generated files, output or logs.
- **FR-063**: The new failures map to the codes of ADR 0013 without changing that table: success, including warnings
  only, 0; unknown flag, invalid or missing flag value, mutually exclusive flags, a traversing or credential-bearing
  input, `--template` while starters are unsupported, `--devcontainer` or plain `init` (no `--non-interactive`, no `--dry-run`) in wave 1a, a `dir` that is a file, an unavailable command, `--json` where
  unsupported, `--no-agents-block` together with `--overwrite AGENTS.md`, missing or conflicting targets in local `rm` (FR-067) 64; a `dir` or `init` parent that does not exist 65; a missing required input in unattended `init`
  (including a missing devcontainer), `--non-interactive` without `--yes`, flags that need an existing manifest
  edited, a regular file that existed at preview time without `--overwrite` (except `AGENTS.md`, FR-064: left unchanged with a warning, exit 0, and `.claude/settings.json`, FR-065: left unchanged, exit 0), and deleting a home volume without both `--force` and `--yes` (FR-067, except
  new throwaway staging whose normal confirmation covers disposal) 70, in unattended mode only (needs-human,
  with the flag names; the interactive wizard instead asks per path, and a refused path is a cancellation, 72, except `AGENTS.md`, where a declined edit is not a cancellation (FR-064), and `.claude/settings.json`, which the wizard offers to replace only when `--overwrite .claude/settings.json` names it (FR-065));
  invalid configuration (`validate`), a target path that is not a regular file or is a symbolic link, a path created or replaced after the preview, a file changed since preview (no such re-check applies to a path that is left unchanged and not written), pre-existing
  content that fails validation, with an edit requested (FR-064) an `AGENTS.md` with malformed or marker-like lines, larger than 64 KiB or whose result would exceed it, a foreign unlabelled container or attempt to delete a volume mounted by another container or resolving staging/temp cleanup to the primary volume (FR-067), unreadable bundle pins or mounts (FR-069), a `doctor` run with a failed check, an unsupported platform for a mutating host
  command, permission denied creating the target (path named), an unreadable `dir` or manifest (a finding) 68; bundle drift detected (FR-069) 1 (or 68); SIGINT
  or SIGTERM and wizard cancellation (wave 1b) 72; an incomplete rollback or volume removal failure after container deletion (FR-067) 73, which takes precedence over 72 and over
  the original failure code; in `init` a failed self-validation of generated files and any other unclassified failure
  74. An existing `AGENTS.md` left unchanged (no edit requested or declined; `agents_block.*` warnings), and `--no-agents-block` are not failures: exit 0, nothing written for that file; the same holds for an existing `.claude/settings.json` or a non-regular target left unchanged without `--overwrite` (FR-065); with the flag, a symbolic link, special file or non-directory `.claude`, is 68; a file that appears after the preview is 68 with or without the flag.
  Unauthenticated (66), forbidden (67) and incompatible-contract (69) do not occur
  in the first increment. Timeout (71) does not occur there except for the proposed
  local stage/promote amendment (FR-068): a deadline with nothing applied is 71,
  reporting the local phase/target and existing receipt when available, with no
  remote operation ID. Partial/uncertain effects instead return 73, which takes
  precedence over 68/71/72. Stage/promote uses 64 for bad arguments/ambiguity, 65
  for no labelled primary match, 68 for wrong role/state or invalid/unresolved
  source/label/receipt bindings, and 70 for missing required trust/confirmation or
  explicit candidate-home deletion consent once defined by its separate owner
  decision/task; FR-067 removal's mapping is unchanged.
  The macOS `setup` wizard and `doctor --fix` of [Feature 009](../009-host-setup-wizard/spec.md) use the same table without a new code: 0 success or an answer `no`; 64 usage; 68 refusal
  (unsupported platform, root start, run lock, blocked or failed with nothing applied, failed doctor check); 70 needs-human (unattended without
  confirmation, privileged step without terminal); 71 deadline with nothing applied; 72 interrupt or cancellation; 73 partial
  failure, which takes precedence over 72, 71 and 68; 74 internal (Feature 009 FR-020). Every CLI failure also writes the `whr:` line (FR-017). The `--json` `error.code` is the name of the
  `exit_code` in the exit-code table (`conflict` for 68, `needs_human` for 70, and so on).

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
- **FR-019**: `shell` (including `shell --local`), `console`, `ssh` and `agent attach` MUST exit with the remote status 0–255 when the remote
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
  Connections made by external clients with FR-008a configuration are out of this requirement's reach, and so is `shell --local`:
  `whr` does not relay that stream, the child owns the terminal directly (Feature 010 FR-014); FR-022 still applies to `whr`'s own lines.

**Release, packaging and platforms**

- **FR-023**: Releases MUST originate from a human-signed annotated `vX.Y.Z` tag on `main` with green required
  checks; the signer MUST be on a reviewed allow list.
- **FR-024**: Each release MUST provide per-platform archives named with version, OS and architecture, a checksum
  file, a software bill of materials and build-provenance attestation that users can verify.
- **FR-025**: Automated release jobs MUST produce a draft only; publication and package-channel updates require a
  human; prereleases MUST NOT be published to stable package channels.
- **FR-026**: Each release MUST publish two separate matrices with verified and unverified entries: client
  platforms (where `whr` runs owner, workspace, access and agent commands) and host platforms (where `setup`,
  `doctor`, `service` and `offboard` are supported). The mutating host commands (`setup`, `service`, `offboard`) MUST
  refuse unsupported host platforms; plain `doctor` is read-only for the host, runs on any platform and reports an unlisted
  one as `unknown` (FR-060). macOS is a host platform for `setup` (the wizard of [Feature 009](../009-host-setup-wizard/spec.md)) and `doctor --fix`,
  unverified until owner-run evidence exists; `service` and `offboard` still refuse macOS.
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

- **Initialization proposal**: target directory, workspace/project declarations,
  devcontainer source, starting directory, human-shell and console preferences,
  shortcuts policy, proposed file changes and explicit write permissions; no credentials.

- **Workspace resource selection**: desired CPU thousandths and memory bytes,
  value origins, selected host/profile, and separately timestamped observed limits.

- **Command table**: the single list of canonical commands, group aliases, short forms, increments, flags and `--json`
  modes (FR-051), mirrored by `contracts/cli/commands.json`.
- **Validation result**: valid flag, counts, bounded findings (code, severity, subject, message, remediation) and
  `provider_admission: not_evaluated` for one configuration directory.
- **Manifest settings**: the version 2 fields of `workharbor.json` (FR-058) authored by `init`; read by no runtime
  component in the first increment, except `human_shell`, which `shell --local` reads (Feature 010 FR-005) once that command exists.
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
- **Doctor report**: ordered checks with status, evidence and remediation, redacted for sharing; verdict ok,
  failed or incomplete.
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
- **SC-016**: The command-table freeze tests (FR-051/FR-052) fail on any difference from
  `contracts/cli/commands.json`; a table-driven test proves that every available short form and group alias
  executes the same command and yields identical output, exit status and canonical `command` value as the
  canonical spelling, and that no verb present in two groups has more than one short form.
- **SC-017**: A hostile corpus for `validate` and `doctor` (FR-055 and the edge case above, including FIFO, sparse
  10 GiB file, symlinks at every path position, invalid UTF-8, duplicate keys, depth bombs, 100,000-entry
  `.gitmodules` and index, credential-bearing URLs, partial-clone repositories, and fsmonitor, hook, filter and
  update-command repositories) completes within the single 5 second budget and 256 MiB resident memory per run, including runs whose Git calls hit the deadline, executes nothing (no
  marker file), modifies nothing (tree compared before and after by names, size, mode, inode, mtime and ctime,
  including `.git`), makes no network connection (for the checks of FR-060 and `validate`) and yields the documented findings and exit codes; every `--json`
  output validates against the schema. Device-node cases and the bounds are verified on a real host only.
- **SC-018**: For `init`, in addition to SC-014, an exhaustive failure-injection and race test (a failure, SIGINT or
  SIGTERM after each step, a full disk, a path appearing or being replaced after the preview, an overwrite target that
  is a symbolic link) ends in exactly one of three outcomes: the complete result whose `validate` result has no error,
  exactly the pre-run tree, or a partial failure (73) listing every remaining path; pre-existing files not named by
  `--overwrite` are byte-identical in every run; no run follows a symbolic link, runs Git, touches the network or
  leaves a credential; `--dry-run` changes nothing; wizard (wave 1b) and unattended runs with equivalent inputs produce
  byte-identical files.
- **SC-019**: For `doctor`, injected measurement ports prove that every check yields the documented status for
  present, absent, old, denied and unmeasurable inputs, that an absent tool or an unmeasured value is never
  `pass`, that every control-plane check is `skipped` with a reason, and that no home path, user name, host name or
  environment value appears in the output; real-host runs on a Linux Docker host and on macOS with OrbStack record
  exact versions and results (SC-007).
- **SC-020**: The first increment passes its tests with the network disabled and with no profile, credential or
  Docker daemon present, and the same binary reports every owner-API command as unavailable without a connection attempt.
- **SC-021**: The resource library and its shared vectors (FR-059) pass offline: every CPU and memory case of
  `resources-vectors.json` yields the listed canonical value or error class, equivalent spellings share canonical
  values, per-field resolution gives the listed origin or needs-human, and the same file is consumable by a second
  implementation; SC-013 remains unverified until the owner API and real-host enforcement evidence exist.
- **SC-022**: Init with the agent workflow block (FR-064) passes offline on a table of cases, each with and without
  an edit requested (`--overwrite AGENTS.md`) and for every class of the precedence list: absent file; file without the
  block; current, modified and different-template blocks (lower and higher numeric versions, same
  version with a different intact body, each with and without edit consent); a file over 64 KiB and a result over 64 KiB
  (`.too_large` with exit 0, or 68 with the edit; a file over 64 KiB that is also malformed is `.too_large`); duplicate, unclosed, nested and orphan markers and CRLF-, indented-
  and BOM-prefixed marker-like lines, including inside a code fence (`.malformed` with exit 0, or 68; no second block
  on a CRLF-converted file); CRLF and BOM files without markers; a symbolic-link and a FIFO target; `--no-agents-block`
  (alone and with `--overwrite AGENTS.md`, 64); the single action per class and `agents_block_warnings` in the
  `--json` result; an interactive PTY run (with T032) that declines only the `AGENTS.md` edit one where a wizard edit without the flag
  would be refused (not offered, warning printed) and one where the flag pre-confirms an edit that would be
  refused (68); the template body free of marker-like lines; and a rerun. Bytes outside the markers are identical, the exit code
  is the one FR-063 lists, and the preview matches what is written.
- **SC-023**: Init with the Claude Code settings default (FR-065) passes offline, under umask 022, on a table of cases:
  absent file (exact bytes, 0644, `.claude` created or reused); existing differing and invalid-JSON files
  (`left_unchanged`, exit 0, the `next_steps` entry and output line, `applied` false); identical file (`skip_existing`);
  `--overwrite .claude/settings.json` (with `--yes` unattended; in the wizard, pre-confirmed and not offered without the
  flag, with T032); `--dry-run` previews the file and writes nothing; a rerun is a no-op; a symbolic-link, FIFO and
  `.claude`-is-a-file or symlinked `.claude` target (`left_unchanged`, exit 0, nothing opened, without the flag; 68 with
  it); and a file appearing after the preview (68 with or without the flag; for the no-flag case, absent at preview and created by another
  process before the exclusive create), with exactly one `left_unchanged` row and no `.claude` row for a non-directory `.claude`.
- **SC-024**: Local container lifecycle and home volume protection (FR-066, FR-067, FR-068; #142) pass offline unit and fake-Docker
  integration tests covering: (1) `whr ls --local` role and label filtering and FR-022 escaping; (2) container removal preserving the
  home volume by default; (3) volume deletion requiring both `--volumes` and `--force`; (4) force deletion without `--yes` unattended
  exiting 70; (5) staging/temp cleanup refusing to touch the primary home volume (68); (6) volume shared with another running container
  refusing deletion (68); (7) read-only, `/opt/bdock/*`, and `bundledock-*` tool volumes never deleted; (8) foreign unlabelled containers
  refused (68); (9) `--dry-run` parity with zero mutations; (10) partial failure exiting 73; and (11) invocation without `--local`
  still refusing (64). The FR-068 amendment additionally requires fake-Docker/CLI
  tests for (12) exact primary/source selection and no-effects refusals;
  (13) fingerprint trust/consent and changed-byte refusal; (14) stage leaving the
  primary running with a distinct admissible new candidate home; (15) health/smoke
  success/failure, exact stdin bytes and FR-022 output; (16) immutable verified-image
  promotion, original primary home retained and pane-loss confirmation;
  (17) candidate-home cleanup under its separately approved consent contract and
  protected/shared-home refusal;
  (18) collision, stale receipt, interrupted/uncertain effects and honest recovery,
  with dry-run zero effects, exact exit codes and JSON conformance throughout.
  Existing protected Anchorage staging/temp cleanup is excluded by the recorded
  owner decision, never counted as passing through an invented recovery path.
  The owner-decided throwaway-staging amendment additionally requires complete
  candidate cleanup on confirmed removal, successful promotion and the specified
  failed-stage/rollback paths, result refs verified before promote clone deletion,
  primary/original-home preservation, no extra deletion consent and truthful
  residual reporting. See its [planned acceptance matrix](contracts/throwaway-staging-amendment-20261025.md#planned-implementation-and-acceptance).
- **SC-025**: Bundle mount drift reporting (FR-069; #143) passes offline unit and fake-Docker integration tests covering:
  (1) all bundles in sync (exit 0); (2) one or more bundles drifted (exit 1 or 68, reporting pinned vs mounted identities); (3) missing mount
  or unreadable pin file (status `cannot_determine`, exit 68); (4) stable, parseable `--json` output (`bundleDriftResult`) with no secrets;
  and (5) verified zero mutations (no pull, rebuild, or restart).
- **SC-011**: A lost-response test (drop the reply to a workspace lifecycle mutation) yields exactly one operation, retrievable
  with `whr op show --key`. A lost `agent send` response reports unknown delivery without an automatic resend.

- **SC-013**: Every US1a case passes for CLI and browser: accepted limits agree in
  desired state and observed enforcement on a supported real host, invalid/unsupported
  requests produce zero hooks/provider mutations, equivalent values share request
  identity, same-key changed limits conflict, and restart/rebuild retain limits.
  Missing real enforcement evidence remains unverified rather than a passing claim.

- **SC-014**: Every US0 case passes: wizard/flag parity, preservation of existing
  configuration, no-op rerun, zero changes on cancel/refusal/dry-run, and truthful
  recovery after injected write/restore failures. No init path provisions, runs
  hooks, fetches projects or grants trust.
- **SC-014a**: Before any future installation-PATH authoring is accepted, execute
  colliding temporary commands through each offered shell's actual startup and
  the remote environment before startup. Prove the FR-046a ordering, command
  selection and deduplication after repeated startup, including HOME with spaces,
  custom GOBIN, GOPATH fallback, automatic absolute `CARGO_INSTALL_ROOT` and
  `CARGO_HOME` roots (including their precedence, deduplication and rejection of
  relative roots without a CWD fallback), explicit install roots and inherited
  `codex-path`. Marker-based fixtures MUST prove no project-local `go` or `sh`
  executes through dot/empty PATH entries during lookup or startup, across every
  offered shell; isolate Go lookup with GOBIN/GOPATH unset and adapter lookup
  with a safe explicit GOBIN. Reject empty/relative/CWD entries and verify trusted project
  outputs appear only after explicit opt-in. Proposal tests MUST show every
  changed path, unchanged unrelated fields, per-path overwrite refusal,
  no-write dry-run and byte-identical devcontainer reuse. These are future
  acceptance requirements, not evidence of implemented generation or live
  activation; T030/T033's existing reuse acceptance stays in force.
- **SC-015**: On supported real environments, every offered human shell works for
  new sessions while noninteractive agent commands retain their declared interpreter;
  unsupported environments refuse. Both console modes (plain by default) pass FR-007a reconnect/end
  cases without duplicate consoles, and changed preferences never alter a live console.
  Unmanaged native-agent execution is explicitly identified, not claimed verified.

## Assumptions

- The CLI talks to the control plane's owner API over TLS for every remote workspace, access, agent and operation
  command; local `ws init` authors configuration without API access or sign-in.
  Remote commands never contact a host agent or provider directly. Host commands (`setup`, `doctor`, `service`,
  `offboard`) run on the host itself with local privileges.
- Linux Docker is the first supported host platform; Apple container and other providers follow the product
  sequence. The initial client platform matrix is settled in planning and published per FR-026.
- The exact login flow is a planning decision within FR-020 and FR-035.
- The size of the retained console output tail and the CLI's automatic retry bounds are planning decisions.
- Legacy `whr` behavior is reference only; no legacy code or binary is a dependency.
- Development scripts of Features 003–005 keep their own exit codes and are not the product CLI.
- The agent session interface assumes no herdr protocol or dependency; its wire
  format belongs to the contract of issue #71. FR-048 makes plain the default human-console backend and herdr an optional one,
  subject to reviewed integration and availability (amended 2026-10-23).
- The first increment (FR-061) runs entirely on the user's machine. `validate` and `doctor` read local files and
  discover a local socket; plain `doctor` reads the machine's state read-only (`doctor --fix` is [Feature 009](../009-host-setup-wizard/spec.md)) and its thresholds are not host policy; `init` writes a local directory and runs no Git command. Thresholds of `doctor` and the
  limits of FR-055 are owner-confirmed values (H77 (6), 2026-10-17).
- The `provider_admission` of a configuration is decided only by the host agent's resolver at trust time (Feature
  006 T009); `validate` mirrors a subset as warnings and is not an authority.
- Version 1 manifests and the examples keep their accepted current schema and provider admission.
  New init manifests use version 2; existing v1/v2 manifests follow ordinary file proposal/consent
  semantics (FR-057(g)), with no automatic version rewrite. The resolver still rejects version 2
  until separately reviewed T035 after T009/T028; a v2 proposal/output is labelled not creation-ready
  until then. No admitted provider input is widened; starters remain unsupported until admission.
- The dashboard reads Spec Kit artifacts at an identified Git revision.
