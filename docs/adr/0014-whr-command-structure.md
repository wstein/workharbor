# 0014-whr-command-structure: Grouped commands, short forms and a local-first increment

Date: 2026-10-16
Status: Proposed (owner decision H77, 2026-10-16). It needs an independent review CLEAR for its exact head and
then explicit owner approval of that head, evidenced by a tracker or PR link. Werner's chat direction and
implementation go-ahead of 2026-10-16 are the decision recorded here, not approval of this text. Nothing is implemented.
Amendment of 2026-10-20 (sections 3 and 4 and Consequences, for the macOS `setup` wizard and `doctor --fix` of
[Feature 009](../../specs/009-host-setup-wizard/spec.md)): the owner approved the proposals AP-6 and AP-9 in chat on 2026-10-20 and
PR #164 is merged; this amendment text also needs an independent review CLEAR for its exact head and explicit owner approval of that
head, evidenced by a tracker or PR link.

## Context

[Feature 006](../../specs/006-whr-cli/spec.md) FR-001 listed one flat set of commands
(`ws`, `open`, `shell`, `setup`, `offboard host`, ...). Two things changed on 2026-10-16:

1. The owner wants a structure that scales (`whr workspace ...`, `whr host ...`) without
   losing the short daily forms.
2. Almost every command depends on the owner API, which does not exist yet:
   `apps/control_plane/lib/workharbor_web/router.ex` serves only `/api/v1/hosts`
   (`POST /enrollments`, `POST /:id/revoke`, `GET /:id`), `/api/v1/auth` and the login
   and owner pages. There is no workspace, operation, session or agent endpoint.
   [ADR 0010](0010-go-whr-cli-layout.md) and [ADR 0013](0013-whr-exit-codes.md) are
   unaffected: the module layout, the exit-code table and the passthrough rule stay.

## Decision

### 1. Structure

Canonical form is `whr <group> <verb>`; groups are `workspace` (alias `ws`), `host`,
`auth`, `op` and `agent`. `version`, `completion` and `help` are top-level.

Short forms `whr <verb>` exist for the daily verbs:

| Canonical | Short form |
| --- | --- |
| `workspace` `ls`, `show`, `create`, `start`, `stop`, `rebuild`, `rm`, `shell`, `console`, `ssh`, `open`, `init`, `validate` (`ws` alias of the group) | `whr ls` ... `whr validate` |
| `host` `setup`, `doctor`, `offboard`, `service install|uninstall|status` | `whr setup`, `whr doctor`, `whr offboard`, `whr service ...` |
| `auth` `login`, `logout` | `whr login`, `whr logout` |
| `host ls`, `host show`, `auth status`, `op show|wait`, `agent ...` | none |

`host ls` and `host show` have no short form because `ls` and `show` belong to the
workspace. `auth status` has no short form: bare `whr status` would be ambiguous between
authentication, console state (`console --status`), `agent status` and `service status`, and its
rare use does not justify a name that would mislead; `whr auth status` stays. `offboard host`
of the earlier flat list becomes `whr host offboard` (short `whr offboard`).

### 2. Alias rules

1. One command table in code; every short form is an alias of the same command object, not a
   second implementation. The machine-readable table is
   [`contracts/cli/commands.json`](../../contracts/cli/commands.json).
2. The canonical id (`workspace.ls`, `host.service.install`) appears in `--json`
   (`command`), logs, errors and audit-facing text, whichever spelling was typed.
3. A verb that exists in more than one group gets a short form only for `workspace`.
4. `--help` shows the short form as a hint; shell completion offers both forms.
5. A new short form needs a table change, a change of the freeze test and an owner-reviewed
   spec amendment; the rules themselves change only by a superseding ADR.
6. The table is frozen by tests the way the exit-code table is (offline, in
   `apps/whr/internal/commands/table_test.go`).

### 3. Local commands and the first increment

Two commands need neither login nor control plane: `whr validate [dir]` (read-only) and `whr init [dir]`, the local
initialization wizard that Feature 006 specifies in US0 and FR-043 to FR-050 (it authors configuration only and is the
only writing command of the first increment). `whr doctor` also runs the validate checks when the current directory
contains `workharbor.json`. For `workspace create` the first increment ships only a pure CPU and memory parsing library
with shared vectors (FR-059); no flag, prompt or submission is wired until the owner API exists (wave 2).

The first implementation increment is delivered in waves (owner decision, 2026-10-17). Wave 1a: A foundation (command
table, envelope, error line, color, frozen exit codes, terminal-text escaping, `version`), then in parallel B `validate`, C
`doctor` (local checks) and the manifest-schema extension (version 2 with `start_dir`, `human_shell`, `console_mode`,
`shortcuts`, plus `name` and `project`), then D1 the non-interactive core of `workspace init`; independently the resource
library. Wave 1b: a terminal test harness, the interactive wizard D2 and devcontainer reuse D3. Wave 2: `workspace create`
wiring and the starter catalog. Work items run in parallel worktrees, one pull request per work item. `setup --dry-run` is not included (the agent
service unit and typed change plan do not exist, so a dry run would invent its plan). Everything that needs the owner
API stays specified and is blocked on it (`auth` and `host show` on the typed TLS owner client, since their endpoints exist); until then those commands are registered, listed as unavailable and exit 64
with a one-line explanation. The runtime parts of the wizard's preferences (launching fish or herdr, console grants)
are outside this ADR and the increment.

Amendment (2026-10-20, [Feature 009](../../specs/009-host-setup-wizard/spec.md)): `host.setup` and `host.doctor` stay one command each. `whr setup`
selects its fixed step list by the detected platform, not by flag: on Linux it is the Feature 006 enrollment (unchanged), on macOS it is
the host setup wizard (no enrollment code, no host agent service, its own dry run), other platforms are refused before any change.
`whr doctor` stays read-only for the host; `whr doctor --fix` is the mutating real run on macOS only, so `host.doctor` declares
writes `none`, and `host` with `--fix`. The doctor catalogue gains the checks `host.profile`, `host.tool.<id>`, `host.dotfile.<id>`
and `git.global_config`. The registration of this in `contracts/cli/commands.json` (a `host.setup` note and `blocked_on`),
`doctor-checks.json` and `output.schema.json` (result definitions, and a place for the stable reason code of the wizard, either
`result.reason` or Feature 008's `error.refusal`) is owner-gated work item T011 of Feature 009 and is not part of this amendment;
no new exit code is introduced.

### 4. Exit codes of the new commands

No new code: the [ADR 0013 table](0013-whr-exit-codes.md) is unchanged and
`contracts/cli/exit-codes.json` is not edited. The mapping is FR-063: success 0; bad flags or values, a target that is a
file, an unavailable command, `--devcontainer` or plain `init` in wave 1a 64; missing directory 65; a required input or overwrite permission for an existing regular file missing in unattended
`init` 70; invalid configuration, non-regular, symlinked or concurrently replaced target, file
changed since preview, pre-existing content failing validation, failed doctor check, unsupported platform for a mutating
host command 68; SIGINT, SIGTERM and wizard cancellation 72; an incomplete rollback 73, which takes precedence over 72 and
the original code; in `init` a failed self-validation of generated files or another unclassified failure 74. The
macOS `setup` wizard and `doctor --fix` ([Feature 009](../../specs/009-host-setup-wizard/spec.md) FR-020) use the same table: success or an answer `no` 0; usage 64; refusal
(unsupported platform, root start, run lock, blocked or failed with nothing applied, failed doctor check) 68; needs-human (unattended
without confirmation, privileged step without terminal) 70; deadline with nothing applied 71; interrupt or cancellation 72; partial
failure (something applied, something not) 73, which takes precedence over 72, 71 and 68; internal, including an invalid catalogue, 74.
The `--json` `error.code` is the exit-code table's name for the status. The ADR's note that 74 is "never used where a
category applies" is respected; a remote-unreachable category does not exist, and a superseding ADR for it is required
before owner-API client work (owner decision, 2026-10-17).

## Alternatives considered

- Flat commands only (the earlier FR-001): scales poorly, mixes host and workspace verbs.
- Groups only, no short forms: slows the daily path; rejected by the owner.
- Short form for every verb: collisions (`ls`, `show`, `status`, `stop` exist in several
  groups). Rule 3 resolves them mechanically.
- A new exit code for invalid configuration: avoided; "wrong state" (68) already covers a
  configuration that does not satisfy its current contract. The pre-ready ADR 0013 change
  procedure permits coherent reviewed changes without preserving old numbers; H77's
  semantic remote-unreachable ADR requirement remains.

## Consequences

- Spec 006 FR-001, FR-011 (`host offboard`), the stories and tasks use the new names; `ws`
  keeps working as the group alias, so existing `whr ws ...` text stays valid.
- Plain `whr doctor` on a development macOS machine runs read-only checks even though macOS is not
  a verified host platform; mutating host commands still refuse unsupported platforms (FR-026, as amended by
  Feature 009: macOS is a host platform for `setup` and `doctor --fix`, unverified until owner-run evidence; `service` and
  `offboard` still refuse it). `doctor --fix` is the only mutating form of `doctor`.
- The command table and `contracts/cli/` change only when Feature 009's T011 is approved and merged; until then they describe the Linux
  `setup` and a read-only `doctor` only.
- A `validate` result never claims the configuration is provisionable; admission remains the
  host agent's decision at trust time.
- Cobra registers short forms as separate root commands generated from the table (Cobra aliases
  only rename siblings), so the freeze tests also cover that generation.

## Local stage/promote amendment (2026-10-24; approval record #229)

The [FR-068 amendment](../../specs/006-whr-cli/contracts/local-stage-promote.md)
selects canonical `workspace stage` / `workspace promote`, retaining group alias
`ws` and adding no bare short forms. `--local` is boolean; stage takes a primary
name/full ID plus explicit `--source`, fingerprint trust and optional smoke;
promote uses the bound receipt. Compatible candidate-home naming and deletion
consent require their separate owner decision/task; they are not added here.
These local mutations extend the historical first-increment writing-command
scope without changing remote commands or creating a control-plane dependency.

Only the proposed interaction is recorded here. Exact-head owner approval and
the plan's completed execution-profile gate precede product registration/code.
The authoritative command JSON, its source mirror/freeze tests and closed result
schemas must change together in the later implementation slice, not by claiming
these commands already exist. No new alias or numeric exit code is introduced.

The owner approval/merge of [PR #229](https://github.com/wstein/workharbor/pull/229)
is the approval record for this interaction clarification only; before that record
it is proposed, and S0 execution-profile approval remains separate afterwards.

## Home clone/backup amendment (2026-10-29)

The [home clone/backup amendment](../../specs/006-whr-cli/contracts/home-clone-backup-amendment-20261029.md)
adds the three-word local commands `workspace home clone` and `workspace home backup`
(IDs `workspace.home.clone`, `workspace.home.backup`) below a new `home` subgroup of
`workspace`, reachable through the `ws` group alias. They get no short form: `clone`
and `backup` are not daily verbs, and a bare `whr clone` would be ambiguous with Git.
Both are first-increment local commands (`needs` none, `writes` local, JSON envelope)
that change only together with `contracts/cli/commands.json`, the closed
`localHomeCopyResult` schema and the freeze tests. No numeric exit code is added; the
mapping is the stage table (64/65/68/70/71/72/73/74). Stage, promote and `rm` are
unchanged.
