# 0014-whr-command-structure: Grouped commands, short forms and a local-first increment

Date: 2026-10-16
Status: Proposed (owner decision H77, 2026-10-16). It needs an independent review CLEAR for its exact head and
then explicit owner approval of that head, evidenced by a tracker or PR link. Werner's chat direction and
implementation go-ahead of 2026-10-16 are the decision recorded here, not approval of this text. Nothing is implemented.

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

### 4. Exit codes of the new commands

No new code: the [ADR 0013 table](0013-whr-exit-codes.md) is unchanged and
`contracts/cli/exit-codes.json` is not edited. The mapping is FR-063: success 0; bad flags or values, a target that is a
file, an unavailable command, `--devcontainer` or plain `init` in wave 1a 64; missing directory 65; a required input or overwrite permission for an existing regular file missing in unattended
`init` 70; invalid configuration, non-regular, symlinked or concurrently replaced target, file
changed since preview, pre-existing content failing validation, failed doctor check, unsupported platform for a mutating
host command 68; SIGINT, SIGTERM and wizard cancellation 72; an incomplete rollback 73, which takes precedence over 72 and
the original code; in `init` a failed self-validation of generated files or another unclassified failure 74. The
`--json` `error.code` is the exit-code table's name for the status. The ADR's note that 74 is "never used where a
category applies" is respected; a remote-unreachable category does not exist, and a superseding ADR for it is required
before owner-API client work (owner decision, 2026-10-17).

## Alternatives considered

- Flat commands only (the earlier FR-001): scales poorly, mixes host and workspace verbs.
- Groups only, no short forms: slows the daily path; rejected by the owner.
- Short form for every verb: collisions (`ls`, `show`, `status`, `stop` exist in several
  groups). Rule 3 resolves them mechanically.
- A new exit code for invalid configuration: avoided; "wrong state" (68) already covers a
  configuration that does not satisfy its contract, and ADR 0013 requires a superseding ADR for
  any table change.

## Consequences

- Spec 006 FR-001, FR-011 (`host offboard`), the stories and tasks use the new names; `ws`
  keeps working as the group alias, so existing `whr ws ...` text stays valid.
- `whr doctor` on a development macOS machine runs read-only checks even though macOS is not
  a verified host platform; mutating host commands still refuse unsupported platforms (FR-026).
- A `validate` result never claims the configuration is provisionable; admission remains the
  host agent's decision at trust time.
- Cobra registers short forms as separate root commands generated from the table (Cobra aliases
  only rename siblings), so the freeze tests also cover that generation.
