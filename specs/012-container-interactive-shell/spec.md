# Feature Specification: Interactive shell for the container provider

**Feature identifier**: `012-container-interactive-shell`
**Feature Branch**: `docs/spec012-container-interactive-shell`
**Created**: 2026-10-31
**Input**: Owner decision 1 of 2026-10-31, recorded in the merged PR #290 (`docs(spec): record owner decisions for
Feature 011`): the container provider should get an interactive shell in the future; until a separate feature builds it,
the container provider keeps `interactive_shell=none`. This feature is that separate feature.
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md).
**Related**: [Feature 008](../008-workspace-sandbox/spec.md) (RuntimeProvider contract, FR-005, FR-012 to FR-015, FR-024,
FR-028 to FR-030, FR-034, FR-035, [refusal codes](../008-workspace-sandbox/contracts/refusal-codes.json),
[runtime provider contract](../008-workspace-sandbox/contracts/runtime-provider.md),
[command surface](../008-workspace-sandbox/contracts/command-surface.md)),
[Feature 009](../009-host-setup-wizard/spec.md) (endpoint selection FR-029, FR-030, FR-032 and the `--confirm <id>` pattern, cited by D9, FR-005, FR-007),
[Feature 011](../011-vm-provider/spec.md) (capability field `interactive_shell`, FR-003, FR-004, FR-016, refusal code
`refused.interactive_unavailable`; a VM backend is expected to use the same `whr sandbox shell` command under that rule,
and a backend without an enforced `pty` is refused with that code; this is owner decision 14 (with 9, "one shared set of
core commands for all backends") in the merged PR #290, now in `specs/011-vm-provider/spec.md` on `main`),
[Feature 010](../010-host-and-workspace-panes/spec.md) (`shell --local`, FR-001 to
FR-015), [Feature 006](../006-whr-cli/spec.md) (FR-007, FR-019, FR-022, FR-022a, FR-030),
[ADR 0013](../../docs/adr/0013-whr-exit-codes.md), [ADR 0014](../../docs/adr/0014-whr-command-structure.md).

## Context and scope

Feature 008 defines Execution Instances (sandboxes) of a RuntimeProvider and the container provider. Its contract has
no interactive shell: the operations are `Create`, `Exec` (an opaque argv, FR-029), `Export`, `Cancel`, `Remove` and
`List`, and the proposed `exec` command is itself deferred (owner decision OD-1 of Feature 008, command-surface.md).
Feature 011 FR-004 adds a capability field `interactive_shell` (`pty` or `none`) and FR-016 states that an interactive
shell is offered only where it is `pty` with status `enforced`, otherwise refused with `refused.interactive_unavailable`.
Feature 011 FR-003 sets the container provider to `interactive_shell=none` because no feature defined the behavior.

This feature defines that behavior for the container provider:

1. a command to open an interactive shell inside an existing, owned, ready instance (FR-001, FR-002);
2. the capability change and the admission path (FR-003 to FR-006);
3. terminal, resize, signal, exit-status and session-loss behavior (FR-007 to FR-017);
4. the security statements, including what is not enforced (FR-018 to FR-022, "Not enforced"); and
5. conformance cases, success criteria and the list of existing documents to change.

### Terms

- **Instance**: a Feature 008 Execution Instance of the container provider, identified by `sbx-` plus 16 base32
  characters (FR-002 there).
- **Shell session**: one interactive login shell started by this feature inside an instance, with a pty allocated by the
  container runtime.
- **Commit point**: the move of the instance record from `ready` to `running` (FR-014), made as the last action before
  the container runtime client is launched.
- **Hand-over**: begins at the commit point. While the container runtime client runs, it owns the terminal directly and
  `whr` writes nothing to the terminal stream, as for `whr shell --local` (Feature 010 FR-010, FR-014). Once the client has
  ended, or on a signal, `whr` may write `whr:` lines to standard error (FR-015, FR-016). Feature 010 has the same tension
  between its "nothing after the hand-over" sentence (FR-010) and its transport-loss `whr:` line (FR-008); this text states
  the reading used here.

### Boundaries

- Spec only. No code of this feature exists. Of the shared pieces, the capability types and `Admit` (`apps/whr/internal/runtime/`)
  and the sandbox model exist (Feature 008 T001 to T003, merged in PR #303); the Plan, the conformance suite and Feature 008
  T004 to T017 are open (tasks.md), the `sandbox` command group is not registered (T016, T017 are open) and Feature 011 is
  itself a draft. This feature writes no implementation code and registers no command.
- Out of scope: a VM provider (Feature 011 sets its own `interactive_shell` value and conformance; the owner's 2026-10-31
  direction, recorded as decisions 9 and 14 in the merged PR #290, is that `whr sandbox shell <id>` is one command
  with the same behavior for container and VM backends, a backend without an enforced `pty` being refused under
  Feature 011 FR-016), a non-interactive
  `exec` (Feature 008 OD-1), attaching to a process that is already running (no reattach), a persistent or multiplexed
  session, the shell of the ADR 0009 workspace container (served by `whr shell --local`, Feature 010, unchanged), and any
  remote or control-plane access path.
- Feature 006 FR-030 is unchanged: `whr` does not install, configure, authenticate, launch or wrap an agent's native tool.
  The shell starts only a login shell and takes no command (FR-007).

### Owner decisions

- **2026-10-31 (decision 1 of PR #290, merged)**: the container provider should also get an interactive shell in
  the future; until a separate feature builds it, the container provider keeps `interactive_shell=none`. This feature is
  the separate feature. Its approval does not change what `main` says today; the change takes effect only through the
  tasks of a later plan (see "Changes to existing documents").
- **2026-10-31 (decisions 9 and 14 of PR #290, merged)**: one shared set of core commands for all backends, and one
  shell command that refuses with `refused.interactive_unavailable` where `pty` is not enforced. Feature 012 was referenced
  there as a draft; both decisions are now on `main`.
- **2026-10-31 (decisions D1 to D8 for this feature)**, as the owner gave them to the author through the orchestrating
  session; a tracker or PR link is added when the text is approved at an exact head:
  - **D1**: a shell in an `untrusted_job` instance is allowed only after explicit confirmation with a warning that
    unfiltered terminal control sequences from an instance process can act on the owner's terminal; no confirmation, no
    shell; `--yes` alone does not satisfy it. `developer` needs no such confirmation (FR-023). The exact flag and prompt form
    is an author proposal.
  - **D2**: the container runtime's detach key sequence is disabled; the session ends only when the shell ends or the client
    is interrupted or killed (FR-008, FR-016, FR-017).
  - **D3**: the login shell is a field of the Plan next to the working directory; `/bin/sh` is the default for the job
    profiles, and `developer` uses the shell of its development environment (FR-008).
  - **D4**: `ready` does not imply a running container; a stopped or missing container is reported with exit 68 naming the
    state and `whr` never starts it (FR-005).
  - **D5**: no re-entry after `finished`; a shell is allowed from `ready` only, once per instance (FR-005).
  - **D6**: the shell is local-only and needs no grant (FR-010).
  - **D7**: a later `whr sandbox` invocation reconciles `running` records: if the exec process is no longer running in the
    container, the record becomes `failed` with reason `session_lost` (FR-024). This is the owner's accepted direction and
    depends on a provider query that is not verified; until it is, the gap stays under "Not enforced".
  - **D8**: (a) a container stopped or removed from outside: the client status is passed through and the instance is
    recorded `failed`/`container_lost`; (b) a client that cannot be launched: exit 74, `failed`/`start_failed`, no rollback
    to `ready`; (c) `ready` to `running` is atomic, a new Feature 008 requirement with the mechanism left to the plan, and
    "exactly one concurrent shell wins" depends on it; (d) for a killed client the client's exit status is passed through,
    and the recorded status additionally holds the shell's status when readable and different (FR-014, FR-016).
- **2026-10-31 (decision D9, the endpoint option of `shell`, given after D1 to D8)**: `whr sandbox shell` also accepts
  `--container-endpoint <endpoint>`, consistent with Feature 009 FR-029, FR-030 and FR-032, which name the later commands of
  Feature 012 among the consumers of the one endpoint selection function. A malformed value is a usage error, exit 64
  (FR-007). This resolves the conflict between the earlier text of FR-007 (global options, `--confirm` and `--yes` only) and
  Feature 009 FR-030; Feature 009 is unchanged. The tracker or PR link is added with the other decisions when the text is
  approved at an exact head.
- **2026-11-01 (owner answers to the plan questions, issue #317, comment 6559753902 and its addendum 6560732494, which supersedes only the "impossible -> 72" half of item 1a)**: (1a) SPLIT: a declined confirmation or an unanswered prompt (end of input)
  exits 72, not 70; an impossible confirmation (standard error not a terminal and no `--confirm`) stays 70 as ADR 0013 row 70
  says, so no ADR change is needed (FR-015, FR-023); (1b) a killed or lost client stream exits 72 (FR-015, FR-016); (2) the location of the instance record store is
  the one decided for Feature 011 (its OQ-1): the Feature 009 state directory below `~/.local/state/whr/` with one lock file; the
  mechanism stays the plan's choice (research R-1); (3) task T022
  may add `interactive_shell` and `refused.interactive_unavailable` to the Feature 008 contracts only if Feature 011 has not landed
  them first; (4) SC-010 covers both Linux with Docker Engine and macOS with OrbStack; (5) a terminal restore mechanism after a
  killed client is built (FR-025), its design is not decided; (6) the reconciliation (FR-024, T014) is held until V5; (7) the author
  proposals are tested as written; (8) V1 to V5 (T002) follow plan approval as scheduled; (10) plan decisions 1 to 4 stand as written.
- Author proposals that remain (marked where they appear), tested as written (owner answer 7 of 2026-11-01): the environment
  variable `WHR_SANDBOX_ID` (FR-009); the SIGHUP rule (FR-013); the structure of the failure `reason` field and its values
  (FR-014); the exact confirmation form of D1
  (FR-023); the owner process id and start time recorded for D7 (FR-024); the widened `running` to `failed` trigger text.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Open a shell in a ready developer instance (Priority: P1)

A developer who created a `developer` instance runs `whr sandbox shell <id>` in a terminal and gets a login shell in the
container, with the Plan's user, working directory, mounts and environment, and nothing else added.

**Independent Test**: fake provider recording the `Shell` call and the exec plan; the plan has the Plan's mounts and
environment and the allowed additions only (FR-008, FR-009).

**Acceptance Scenarios**:

1. **Given** a `ready` instance and a provider reporting `interactive_shell=pty` with status `enforced`, and a terminal on
   stdin and stdout, **When** the command runs, **Then** one shell session starts, the instance becomes `running`, and
   when the shell exits with status N the command exits N and the instance is `finished` (FR-014, FR-015).
2. **Given** the same instance, **When** the terminal is resized during the session, **Then** the new size reaches the
   in-container pty (FR-012).

### User Story 2 - Refusal when the shell is not enforced (Priority: P1)

**Independent Test**: admission table row: `interactive_shell` of `none`, `pty` with `not_enforced`, `pty` with `unknown`
against the request; each is refused with the stable code and zero provider calls beyond the read-only capability report.

**Acceptance Scenarios**:

1. **Given** a provider reporting `interactive_shell=none`, **When** the command runs, **Then** it is refused with
   `refused.interactive_unavailable`, exit 68, no session and no state change (FR-004).
2. **Given** `pty` with status `unknown`, **Then** the same refusal; `unknown` never satisfies a requirement (Feature 008
   FR-015).

### User Story 3 - No terminal, no shell (Priority: P1)

**Acceptance Scenarios**:

1. **Given** standard input or output is not a terminal, **When** the command runs, **Then** it exits 64 before any
   provider call that allocates anything, and no fallback to a piped shell exists (FR-007).
2. **Given** `--json`, a trailing command or any further positional argument, **Then** exit 64 (FR-007).
3. **Given** an `untrusted_job` instance, no `--confirm <id>` and standard error not a terminal, **Then** no prompt is shown
   and the command exits 70 with a `whr:` line naming `--confirm` (FR-023).
4. **Given** `--container-endpoint` with a malformed value (empty, relative or otherwise malformed, Feature 009 FR-030),
   **Then** exit 64 before any provider call (FR-007); **Given** a well-formed value with another scheme (for example `tcp://`), **Then** exit 68 at step 5; **Given** a well-formed `unix://` value, **Then** it is the one
   endpoint used at step 5 and no lower source replaces it (FR-005, Feature 009 FR-029).

### User Story 4 - The container disappears during the session (Priority: P2)

**Acceptance Scenarios**:

1. **Given** the container is stopped or removed from outside while the shell runs, **When** the client ends, **Then** the
   outcome table of FR-016 applies: the client's status is passed through unchanged, the instance is `failed` with reason
   `container_lost`, and no `whr:` line is written.
2. **Given** the daemon is unreachable after the client ends, **Then** exit 72, `failed` with reason `transport_lost`.
3. **Given** the container runtime client is killed and the shell keeps running, **Then** exit 72, `failed` with reason
   `end_undetermined` (FR-016, FR-017). No key sequence detaches (FR-008).

### User Story 5 - Confirmation for an untrusted job (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a `ready` `untrusted_job` instance, **When** the command runs without confirmation, **Then** a warning about
   unfiltered terminal control sequences is shown on standard error, which must be a terminal for the prompt (otherwise see
   User Story 3 scenario 3), and without an explicit confirmation no shell starts (exit 72 when the prompt is declined or left unanswered, FR-023).
2. **Given** `--yes` alone, **Then** it does not count as confirmation. A `developer` instance asks for nothing.

### Edge Cases

- With a provider reporting `interactive_shell=pty` with status `enforced` (otherwise `Admit` refuses first, FR-004), an
  instance in `preparing`, `running`, `finished`, `failed`, `discarding`, `discarded` or `cleanup_incomplete`: refused
  `refused.invalid_state`, exit 68, no state change (FR-005, Feature 008 data model). A second shell on an instance whose
  first shell has not ended is the `running` case.
- A `ready` record whose container is stopped or missing: exit 68 naming the state; `whr` never starts the container (`ready`
  does not imply a running container). Any other record state is `refused.invalid_state` (FR-005 step 4).
- Unknown id: exit 65 (malformed id: 64). A container carrying only some of the ownership labels, or labels that do not match the instance
  record: `refused.foreign_object`, exit 68, zero mutations (FR-006).
- The selector names the ADR 0009 workspace container, a container id or a name: it is not an instance id, so exit 64
  (malformed) or 65 (no record); the operator uses `whr shell --local` for the workspace container. `refused.foreign_object`
  arises only when an existing instance record points to an object that lacks the labels or whose values differ (FR-006).
- No usable endpoint (Feature 009 FR-032), `docker` client absent, daemon unreachable, a non-`unix://` endpoint (from
  `--container-endpoint` or otherwise) or no permission on the socket: exit 68 with
  remediation, as Feature 010 FR-002 and FR-009 state for `shell --local`; no `sudo` is run.
- The shell executable is missing in the image: the runtime reports its own error and status (126 or 127 are the usual
  shell conventions), which `whr` cannot tell from a shell exit (Feature 010 Edge Cases, OQ-6). They pass through, and the
  exec process counts as ended, so the instance is `finished` with that status (row 3 of FR-016); this relies on the
  unverified end-of-exec query, and without it the case falls into row 4 (exit 72, `end_undetermined`). There is no rollback
  to `ready`, and `whr` does not substitute another shell. Only a client that cannot be launched at all is a failed start
  (FR-014).
- The terminal type is hostile or unusual: `TERM` is validated, otherwise a fixed default is used (FR-009).
- Two terminals attempt a shell on the same instance at once: the move `ready` to `running` is atomic (FR-014), so exactly
  one wins and the other gets `refused.invalid_state`, exit 68.
- The container runtime client is killed: FR-017. `whr` itself killed without a handler (SIGKILL, out-of-memory kill, host
  crash): FR-017 and FR-024.
- The client binary disappears between step 5 and the launch: the launch fails, so it is a failed start (exit 74,
  `start_failed`); reading it as an absent client (exit 68) is possible and is left to the plan.

## Requirements *(mandatory)*

### Functional Requirements

**Command**

- **FR-001** (command): The command is `whr sandbox shell <id>` (proposed syntax until approved, like the rest of the
  `sandbox` group, Feature 008 FR-028). It MUST have no short form. It starts a fresh login shell inside the instance
  that ends with the session, mirroring the meaning of `shell` in Feature 006 FR-007 ("a fresh login shell ... that ends
  with its attachment") and Feature 010 FR-007. It MUST NOT be registered, and `contracts/cli/commands.json` and the
  frozen exit-code contract MUST NOT be edited, by this feature's approval alone: registration is an owner-gated task
  after the ADR 0014 amendment (Feature 008 T016, T017), and the passthrough list change of FR-015 needs its own
  owner-reviewed amendment of ADR 0013.
- **FR-002** (selector): `<id>` is an instance id, `sbx-` followed by 16 lowercase base32 characters (Feature 008 FR-002), matched exactly against
  the instance records; no name, prefix or container-id lookup exists in this command. A malformed `<id>` exits 64; a
  well-formed id with no record exits 65. The command addresses the container by its full runtime id after resolution, never by name.

  *Alternatives considered* (author analysis, not an owner decision):

  | Option | Shape | Assessment |
  | --- | --- | --- |
  | A | `whr sandbox shell <id>` | Chosen. `shell` is the existing Feature 006 verb for a fresh login shell; the `sandbox` group (Feature 008 OD-4, option A) owns instance verbs; no short form is needed (ADR 0014 alias rules 3 and 5, Feature 008 command-surface.md "no short forms are needed"). |
  | B | `whr sandbox exec -it <id>` | Rejected for this feature: `exec` is deferred by Feature 008 OD-1 and its contract is an opaque argv as data (FR-029), whereas a shell takes no argv. A later `exec` stays independent. |
  | C | `whr shell --sandbox <id>` (flag on the existing verb) | Rejected: the workspace verb `shell` is the remote, grant-based command (Feature 006 FR-006) with `--local` as its one exception (Feature 010); a further selector flag would overload one entry with two trust models and two exit-code paths. |
  | D | `whr sandbox attach <id>` | Rejected: in Feature 006, `console` attaches to a session that outlives its attachments (FR-007a) and `agent attach` also attaches to an existing session; this shell does not outlive its session (FR-014), so the name would mislead. |

**Capability and admission**

- **FR-003** (capability): The container provider MUST report `interactive_shell=pty` with status `enforced`, for each
  profile it supports, only when the conformance cases S12-C1 to S12-C8 below pass for it and the real-host evidence gate of SC-010 is met. The
  reconciliation of FR-024 (case S12-C9) is not a gate for this claim (author proposal): it is claimed separately, once
  its query is verified. The end-of-exec query behind rows 3 and 4 of FR-016 is part of case S12-C7 and so of the gate. Until then it reports `interactive_shell=none` (Feature 011 FR-003 as it stands). A
  report claiming more than its tests prove is non-conformant (Feature 008 FR-018). The `scope` states that the claim covers
  a pty allocated by the container runtime for a login shell inside the instance's container, and nothing about what runs
  in it.
- **FR-004** (admission): `whr sandbox shell` is a request for the requirement `interactive_shell=pty`. `Admit` MUST decide
  it as a pure function of request and capabilities (Feature 008 FR-013). A request whose `interactive_shell` is not `pty`
  with status `enforced` MUST be refused with `refused.interactive_unavailable` (Feature 011 FR-016 and its refusal table),
  before any side effect, with no fallback to a non-interactive or piped mode. `unknown` never satisfies it. The code is
  not in `specs/008-workspace-sandbox/contracts/refusal-codes.json` on `main`; it reaches that file through Feature 011
  (which appends its eight new codes). This feature introduces no further code compared with Feature 011. If Feature 011
  has not landed first, this feature's tasks append `refused.interactive_unavailable` themselves, with the "when" text of
  Feature 011, and that is then a new code compared with `main`.
- **FR-005** (order and state): The command MUST check, in this order, and stop at the first failure: (1) usage and
  terminal conditions (FR-007), exit 64; (2) the instance record exists, exit 65; (3) `Admit` for the instance's profile and
  provider, exit 68 `refused.interactive_unavailable`; (4) the record's state is `ready` per the Feature 008 data model,
  otherwise `refused.invalid_state`, exit 68 (this reads only the record, so a `discarded`, `discarding`,
  `cleanup_incomplete` or create-failed instance whose container is gone gets this code, not a container error; a
  `finished` instance is refused too, because a shell is allowed from `ready` only and once per instance, with no re-entry);
  (5) container runtime access: the client is present, the endpoint selected by the one rule of Feature 009 FR-029 (`--container-endpoint`
  first, FR-007) is a `unix://` socket and the daemon is reachable, otherwise exit 68 with remediation (Feature 009 FR-032,
  Feature 010 FR-009); (6) ownership (FR-006): the provider
  inspects the container by its full id; if it no longer exists, exit 68 naming the state `missing` (not
  `refused.foreign_object`, which is for an existing object whose labels are absent or differ); (7) the container is running,
  otherwise exit 68 naming the state (`stopped`, `paused` or `restarting`); `whr` never starts, stops or changes the
  container; (8) the forbidden-mount re-check of FR-018, with the existing codes `refused.host_socket_mount` or
  `refused.tool_mount_writable`, exit 68; (9) for an `untrusted_job`, the confirmation of FR-023. Then the commit point of
  FR-014. Step 4 reads only the record, steps 5 to 8 make read-only provider queries only, and step 9 talks to the person.
  The Feature 008 state machine needs no new state or transition pair: `ready` to `running` at the commit point,
  `running` to `finished` or `failed` at the end. No transition back to `ready` exists: a failed start is `running` to
  `failed` (FR-014). Two trigger texts of the data model are widened (see "Changes to existing documents"): the `ready` to
  `running` trigger "exec started" also covers a shell started, and the `running` to `failed` trigger, which names only
  "cancellation, crash or deadline", also covers a client that cannot be launched (`start_failed`), loss of the container or
  daemon, a signal to `whr` after the commit point, a session whose end cannot be established, and the reconciliation of
  FR-024 (`session_lost`) (author proposal for the text; the behaviors are owner decisions D7 and D8). A `whr` that dies
  without a handler after the commit point moves nothing by itself; FR-017 and FR-024 cover that.
- **FR-006** (ownership): At step 6 of FR-005, before any change, the provider MUST inspect the target by its full id and verify that it carries
  every ownership label of the Feature 008 data model with values equal to the instance record (Feature 008 FR-005; the
  runtime-provider contract already requires `Create`, `Exec`, `Remove` and `Cancel` to re-verify). An existing object that lacks
  them, or whose values differ, is refused with `refused.foreign_object`, exit 68, zero mutations. `Shell` MUST NOT select
  an object by name, by label query alone or by image.

**Terminal behavior**

- **FR-007** (terminal required): The command MUST require standard input and standard output to be terminals, MUST take
  no trailing command and no positional argument beyond `<id>`, accepting only the options `--confirm <id>` (FR-023) and
  `--container-endpoint <endpoint>` (owner decision D9; Feature 009 FR-029, FR-030), the global options and `--yes` (which has
  no effect on FR-023), treating a malformed `--container-endpoint` value as a usage error (exit 64, Feature 009 FR-030; a
  well-formed value with another scheme is refused at step 5 of FR-005 with exit 68), and MUST reject `--json` as a usage error (exit 64), as Feature 010 FR-006
  does for `shell --local`. A pty MUST be allocated only in this case; no pty is requested when either stream is not a
  terminal, and there is no documented fallback to a piped shell, because that mode is the deferred `exec` (Feature 008
  OD-1). Standard error need not be a terminal, except that an `untrusted_job` without `--confirm <id>` then exits 70 (FR-023).
- **FR-008** (exec form): For the container provider the hand-over MUST be one invocation of the container runtime client
  with a fixed argument vector (no shell, no string evaluation) built only from validated values, addressing the
  container by full id and passing the selected `unix://` endpoint explicitly, as Feature 010 FR-002 and FR-004 state
  for `shell --local`. The invocation MUST request an interactive pty session, MUST disable the runtime's detach key sequence
  (owner decision D2; Feature 010 FR-004 instead fixes a rarely typed sequence for `shell --local`, a difference that
  stays), the Plan's user, working directory and login shell, and the environment of FR-009. The Plan carries the login
  shell as a field next to the working directory (D3): `/bin/sh` for the job profiles, and for `developer` the shell of its
  development environment, which is the `human_shell` of the workspace manifest (Feature 006 FR-047; Feature 010 FR-005
  reads it the same way). Whether the runtime can disable detaching is not verified; a provider whose runtime cannot MUST NOT
  report `pty` with status `enforced`. It MUST NOT use the runtime's privileged option,
  add capabilities, add or change mounts, devices or limits, or read an environment file. The exact vector is the
  provider's and is asserted by the conformance suite (case S12-C2); this text states the constraints.
- **FR-009** (environment): The command accepts no operator-supplied environment (FR-007). The shell environment has these
  sources and no other that `whr` sets:
  - the container's create-time environment, inherited by the exec (to be verified for the container runtime client): for
    jobs this includes the four identity variables only if `Create` put them there from the values the caller gave at create
    (`--git-author-*`, `--git-committer-*`, held by the caller and never logged, Feature 008 FR-023, FR-030); otherwise they
    are absent and commits fail;
  - for the two job profiles, `GIT_CONFIG_GLOBAL` (the read-only profile file) and `GIT_CONFIG_NOSYSTEM=1`, which `whr`
    sets itself as Feature 008 FR-023 requires for `Exec`; they are present in a job shell and absent in a `developer` shell
    (whose host configuration comes from the mount, FR-019 there);
  - `TERM` and `WHR_SANDBOX_ID=<id>` (author proposal, so a prompt can show it). `TERM` MUST be the host value when it
    matches `[A-Za-z0-9._+-]{1,64}`, otherwise `xterm-256color` (as Feature 010 FR-005).
  No other host environment variable is forwarded. The Plan's environment was already filtered at `Create` (Feature 008
  FR-023); `Shell` accepts no operator-supplied value, so it scrubs nothing and has no scrub list to report, and it removes
  no variable that the image or container configuration sets (see "Not enforced"). Because these sources are fixed and take nothing from the operator, the `developer`
  refusal `refused.git_env_override` (an operator-supplied `GIT_CONFIG_*` variable) cannot arise for a shell; that holds only
  as long as the sources stay these. Values MUST NOT be placed in the argument vector (visible to other local users in the
  process list); the provider passes them by a route that does not expose them, which is to be verified (Verification
  before the plan).
- **FR-010** (notice): When standard error is a terminal, `whr` MUST write one line to it before the hand-over, with the
  instance id and profile escaped per Feature 006 FR-022, naming that the session has no grant and uses host container
  runtime access (the byte-exact text is fixed by the plan, modeled on Feature 010 FR-010; no detach keys exist to name). The
  command is local-only and needs no grant (owner decision D6; it extends the exception that Feature 006 FR-006 makes for
  `shell --local`, see the changes list). While the client owns the terminal `whr` MUST write nothing to the terminal
  stream. Color MUST NOT be required.
- **FR-011** (terminal safety): The terminal-safety rules are those of the local shell of Feature 006 as Feature 011
  FR-016 requires: `whr` does not relay the stream, the container's process owns the terminal directly, and the OSC, DCS,
  APC, PM and SOS filtering of Feature 006 FR-022a does not apply to it (Feature 006 FR-022a states this exception for
  `shell --local`; Feature 010 FR-014). Feature 006 FR-022 applies to every line `whr` itself writes (notice, errors,
  candidate lists), which contain untrusted names. `--help` MUST state that the session stream is neither inspected nor
  filtered. Consequence, stated plainly: a process in the instance, including one in an `untrusted_job`, can write
  control sequences that change the terminal title, write the clipboard or otherwise act on the owner's terminal. This
  feature does not prevent that; for an `untrusted_job` it requires an explicit, informed confirmation first (FR-023, owner
  decision D1) and states the rest under "Not enforced".
- **FR-012** (resize): The initial terminal size MUST reach the in-container pty at start, and later size changes MUST be
  propagated for the lifetime of the session. For the container provider this is expected to be carried by the container
  runtime client (Feature 010 Edge Cases states the same expectation for `shell --local`); it is not verified here and is
  checked at the owner gate of SC-010. `whr` adds no resize handling of its own unless that check fails.
- **FR-013** (signals and job control): With a pty, keyboard interrupt, suspend and quit characters are bytes delivered to
  the in-container terminal and act on the in-container shell and its jobs; job control is the shell's. `whr` MUST NOT
  convert them into host-side signals or exit codes. For signals sent to the `whr` process itself:
  - SIGINT or SIGTERM before the commit point of FR-014: exit 72, nothing created, no state change, no leftover (nothing has
    been changed yet, so no rollback exists);
  - SIGHUP (for example the terminal closes) before the commit point: handled like SIGTERM (author proposal; Feature 006
    names only SIGINT and SIGTERM);
  - SIGINT, SIGTERM or SIGHUP after the commit point (hand-over has begun): `whr` records the instance `failed` with reason `interrupted`, exits 72 with a
    `whr:` line once the client has ended or been abandoned, and does not claim that the in-container shell ended (FR-017).
    A signal while the confirmation prompt of FR-023 is shown is before the commit point.
  How the container runtime client reacts to these signals is not verified here.

**Exit status and lifecycle**

- **FR-014** (state and record): The notice of FR-010 is written first. The commit point is the move of the instance record
  from `ready` to `running`, made as the last action before the container runtime client is launched; hand-over begins at
  the commit point. The move MUST be atomic: it applies only if the state is still `ready`, and the loser of a race gets
  `refused.invalid_state`, exit 68, and starts nothing. This is a new Feature 008 requirement (owner decision D8c): that
  specification defines no instance-record store, location, lock or atomic update, so the mechanism is left to the plan, and
  "exactly one concurrent shell wins" depends on it. A failed start is only this: the client cannot be launched (an
  operating-system error); the record then moves `running` to `failed` with reason `start_failed`, exit 74 and a `whr:` line;
  there is no transition back to `ready` (D8b). When the provider has established that the shell process ended (FR-016) the
  record moves to `finished`. The status passed through is the client's exit status. The status recorded is that client
  status and, when the provider can read the exec process's own status and it differs (a killed client reports its own signal
  status, for example 137, not the shell's), also the shell's status (D8d). A recorded status is not acceptance (Feature 008
  FR-007). A loss, a signal after the commit point, a failed start, an undetermined end or a reconciliation moves the
  instance to `failed` with a `reason` (author proposal for the field; values `transport_lost`, `container_lost`,
  `end_undetermined`, `interrupted`, `start_failed` and `session_lost`), from which export remains allowed for the job
  profiles with `partial=true` (Feature 008 FR-007). A `developer` instance keeps its results in the repository (data model),
  so it has no result bundle; the `failed` state and reason are the only record that the session did not end cleanly.
  `discard` is refused while the instance is `running` (data model); `clean` only selects finished or failed ephemeral
  instances (command-surface.md), so it does not touch a `running` one. The session content, the environment and the
  terminal bytes MUST NOT be recorded or logged; only the `whr:` lines and the status/reason fields are (as Feature 010
  FR-015).
- **FR-015** (exit codes): The command MUST use the ADR 0013 table as it stands, with no new code or category:
  - the status of the remote side, 0 to 255, unchanged, whenever the exec process has exited and the daemon is reachable,
    including when it died because its container was stopped or removed (the passthrough rule, Feature 006 FR-019 and
    Feature 010 FR-008, which name `shell` including `shell --local`, `console`, `ssh` and `agent attach` today;
    `sandbox shell` is added by the amendment listed below). One stated exception to "the remote status": when the container
    runtime client was killed, the passed-through status is the client's own signal status (D8d), not the shell's; the
    amendments of Feature 006 FR-019 and ADR 0013 say so;
  - 64 for usage, no terminal, `--json`, a trailing command or further argument;
  - 65 for an unknown instance;
  - 68 for any `refused.*` (including `refused.interactive_unavailable`, `refused.foreign_object`,
    `refused.invalid_state`), a non-running container, no usable endpoint (Feature 009 FR-032), an absent client, an unreachable
    socket or a non-`unix://` endpoint (the mapping of Feature 008 FR-035 and Feature 010 FR-009), all before the hand-over;
  - 72 where the ADR 0013 definition ("user interrupt, or transport lost during an interactive command") holds:
    SIGINT, SIGTERM or SIGHUP before or after the commit point, daemon unreachable after the client ends, and a client stream
    that ended while the remote side did not exit (a killed or lost client). The owner confirmed on 2026-11-01 that reading
    that last case as a transport loss is right; it needs no new code. A confirmation prompt (FR-023) that is declined or left
    unanswered (end of input) is also read as a cancellation, which command-surface.md lines 41 and 42 list under 72 ("interrupt or
    cancellation") and which Feature 006 (a declined wizard prompt) and Feature 009 FR-004 (end of input at its wizard question)
    give 72; the owner chose this reading on 2026-11-01 rather than the "not given" of ADR 0013 row 70;
  - 70 for a confirmation that is impossible: no prompt can be shown because standard error is not a terminal and no `--confirm`
    is given (ADR 0013 row 70: a required human decision refused in unattended or answer-file mode, or not given; command-surface.md
    lists "unattended confirmation missing" under 70; this command has no unattended or answer-file mode of its own, and a run
    without a terminal on standard input or output is 64, FR-007). The split of the confirmation cases (owner decision of
    2026-11-01):

    | Confirmation case (FR-023) | Exit |
    | --- | --- |
    | prompt shown and declined (any answer other than the instance id) | 72 |
    | prompt shown and left unanswered (end of input) | 72 |
    | no prompt possible (standard error not a terminal, no `--confirm`) | 70 |
    | `--confirm` value that does not equal the instance id | 64 |

  - 74 for an unclassified failure, including a client that cannot be launched (`start_failed`, FR-014).
  Exit codes 66 and 67 are not used (no remote authority), 71 and 73 are not used by this command. Because remote statuses overlap 64 to 79, CLI failures MUST also write a `whr:` line to standard error
  and a status coming from the shell MUST NOT (Feature 010 FR-008, ADR 0013). `--json` is rejected, so the `kind`
  field of the JSON exit event is not available for this command. The `whr:` line is advisory (ADR 0013), so for
  `sandbox shell` a status in 64 to 79 is ambiguous by design, as ADR 0013 and `contracts/cli/exit-codes.json` state for
  `ssh`, including a remote 72 against the CLI's own 72; the same gap already exists for `shell --local`.
- **FR-016** (end of the session): After every client exit, whatever its status, `whr` MUST run one read-only query within a
  5-second budget (Feature 010 FR-004, FR-008 pattern) that establishes the daemon, the container state and whether the exec
  process has ended. Establishing the last point is the provider's responsibility and uses a read-only query that is not
  verified (Verification before the plan). The outcome is one row of this table:

  | Case | Exit code | Instance | `whr:` line |
  | --- | --- | --- | --- |
  | Daemon unreachable, or the query does not answer in time | 72 | `failed`, reason `transport_lost` | yes |
  | Daemon reachable; container stopped (not running) or removed (owner decision D8a) | the client status, 0 to 255, unchanged (the remote process exited) | `failed`, reason `container_lost` | no (a remote status) |
  | Container running; exec process ended | the client status, 0 to 255, unchanged | `finished`, status recorded as in FR-014 | no |
  | Container running; exec process still running or its end cannot be established (a killed or lost client) | 72 (the client stream ended, the remote side did not exit) | `failed`, reason `end_undetermined` | yes |

  Why: when the daemon is reachable and the exec process has exited, even because its container was stopped or removed, the
  remote side did exit, so the status passes through as Feature 006 FR-019 and Feature 010 FR-008 require; the instance is
  nevertheless recorded `failed` with reason `container_lost`, because the shell did not end by itself, and the status alone
  (for example 137 or 143, or 0) cannot tell the two apart. A stop or removal caused by the shell's own commands falls in the
  same row. Because no key sequence detaches (FR-008), a deliberate detach does not exist. A client stream that ends while
  the remote side did not exit is read as a transport loss (exit 72, ADR 0013); the owner confirmed that reading on
  2026-11-01. Signals sent to `whr` are FR-013.
- **FR-017** (killed client, orphans and a killed `whr`): With detaching disabled (FR-008), the session ends when the shell
  ends or when the client is interrupted or killed. A killed client can leave the in-container shell running (Feature 010
  FR-007 accepts this for `shell --local`). This feature does not track or reap such a process. When the client ended while
  `whr` itself is still running, the FR-016 query selects the row: if the exec process is still running or its end cannot be
  established, the last row (exit 72, `failed`, `end_undetermined`, a `whr:` line), so that a job profile's export carries
  `partial=true` (Feature 008 FR-007); if the exec process has already ended, row 3, with the status rules of FR-014. A running
  orphan is bounded by `discard` (refused while `running`; allowed from `failed`, where it still refuses job results not
  exported to an explicit destination unless the operator confirms deletion, Feature 008 FR-008), whose provider `Remove` is
  bounded, releases the instance's labeled objects and preserves results (Feature 008 FR-017). A `whr` process that dies
  without running a handler at any point after the commit point of FR-014, including between the commit point and the
  launch of the client, when no exec was ever started (SIGKILL, out-of-memory kill, host crash), produces no exit code, no
  `whr:` line and no further transition: the record stays `running`, `discard` refuses it, `clean` does not select it, a new
  shell is refused at step 4 of FR-005 and export is not allowed (the data model allows it only from `finished` or `failed`),
  so job results cannot be exported either, until the reconciliation of FR-024 has run.
- **FR-024** (reconciliation of `running` records; owner direction D7): A later `whr sandbox` invocation MUST, before it
  acts on a `running` record, check whether the exec process still runs in the container; if it does not (it ended, or was
  never started), the record becomes `failed` with reason `session_lost`, from which export and `discard` follow the rules of
  FR-014. A record whose owner is still alive MUST NOT be touched: the plan therefore has the `running` record carry the owner
  process id and start time (author proposal, modeled on the reservation record of Feature 011 FR-009), so that a concurrent
  `whr` between the commit point and the launch is not marked. The query that tells whether the exec process runs is not
  verified (Verification before the plan). Until it is, no provider may claim this behavior, and the gap of FR-017 remains
  listed under "Not enforced". Case S12-C9 is not a gate for `pty`/`enforced` (FR-003).
- **FR-025** (terminal restore after a killed client; owner decision of 2026-11-01): The feature MUST design and implement a mechanism
  that restores the owner's terminal to a usable state after the container runtime client was killed (FR-017). The mechanism is not
  designed yet. A killed client cannot restore the terminal itself, so the design needs an owner decision (task T028) before the
  implementation (task T029). Until the mechanism is built nothing is claimed about the terminal state after a killed client, and
  the SC-010 run observes and records it. FR-010, FR-011 and FR-015 continue to apply to the mechanism.

**Security**

- **FR-018** (no widening): A shell session MUST NOT mount the host Docker socket (forbidden in every plan by runtime-provider.md "Mount and environment plan";
  Feature 008 FR-010 states it for `untrusted_job`) or another host control socket (the scope of refusal
  `refused.host_socket_mount` in refusal-codes.json), MUST NOT use privileged mode, add capabilities, devices or
  mounts, and MUST NOT change the instance's Plan, labels, tool mounts (read-only, Feature 008 FR-016) or limits. The
  process runs with the instance's existing container, user and mounts; the session adds a process, not a boundary
  change. At step 8 of FR-005, before hand-over, the provider MUST re-inspect the container and refuse (with the existing codes
  `refused.host_socket_mount`, `refused.tool_mount_writable`) if it carries a forbidden mount, as defense in depth.
- **FR-019** (credentials): `whr` MUST NOT receive, store or relay a credential for the session and MUST NOT put one in
  the argument vector or the environment it sets (Feature 008 FR-030, Feature 010 FR-006). The four identity variables are
  not credentials and their values MUST NOT be logged. A credential already present in the instance (for example one an
  external runner placed there) is readable by the shell user; that is outside `whr` (Feature 008 FR-030).
- **FR-020** (no agent launcher): The command takes no command and no agent flag, installs, configures and authenticates
  nothing, and does not detect agents (Feature 006 FR-030, Feature 008 FR-029). A person can run any program in the shell;
  `whr` neither recognizes nor restricts it.
- **FR-021** (profiles): `Admit` is capability and profile based (Feature 008 FR-024). The shell does not change the trust
  profile table: a `developer` shell runs where checkout, home and host global git configuration are writable by design,
  so what a person or program does there carries the owner-level risk stated in Feature 008 FR-024; a
  `cooperative_job` shell can write shared repository metadata; an `untrusted_job` shell reaches only its copy and
  scratch home but has unrestricted network (Feature 008 plan threat model). The shell adds an interactive way to use those
  accesses and no new mount, network or credential access. It does add one channel: terminal output from the instance
  reaches the owner's terminal unfiltered (FR-011), for every profile. Feature 008 FR-024 has the creation result and the documentation state the `developer` risk; the notice of FR-010
  names the profile again.
- **FR-022** (vendor and provider neutrality): The command, `Admit`, the refusal code and the lifecycle MUST NOT name or
  branch on a provider kind (Feature 008 FR-034). Other providers declare their own `interactive_shell` value, and a
  provider that declares `pty` must pass the same cases; adding one does not change this feature's contract text. Normative
  text names no VM backend; Docker appears only as the container runtime of Feature 008 (`docs/container-runtime.md`) and in
  citations. Generated text (notice, help, errors) MUST NOT name a model or a VM backend. The vendor-name gate of Feature
  011 FR-022 lists its gated files and does not include this one; adding it is not part of this feature's change list
  (the owner may add it to Feature 011's list).

- **FR-023** (confirmation for an `untrusted_job`; owner decision D1): Before the commit point, for an `untrusted_job`
  instance only, the command MUST warn that unfiltered terminal control sequences written by a process in the instance can
  act on the owner's terminal (title, clipboard and similar, FR-011) and MUST NOT start the shell without an explicit
  confirmation of that warning. `--yes` alone MUST NOT satisfy it: the prompt is still shown. `developer` and
  `cooperative_job` ask for no such confirmation (the owner decided on `untrusted_job` only). Following Feature 006 FR-014
  (standard output carries command data only; prompts and diagnostics go to standard error; repeated in FR-045 and Feature
  009 FR-005 and FR-020), the warning and the prompt are written to standard error and the answer is read from standard input (a
  terminal, FR-007). No exception to Feature 006 FR-014 is made: the shell session's own terminal stream is the interactive
  channel that Feature 006 FR-022a and Feature 010 FR-014 already treat as outside `whr`'s relay, not a prompt. If no
  `--confirm <id>` is given and standard error is not a terminal, no prompt is shown, nothing is started and the command
  exits 70 with a `whr:` line naming `--confirm`. A run without a terminal on standard input or output never reaches this
  step (exit 64 at step 1). Exit codes (owner decision of 2026-11-01, Open question 1a, a split; the table is in FR-015): a
  prompt that is declined (any answer other than the instance id) or left unanswered (end of input) is 72 (a
  cancellation: command-surface.md "72 interrupt or cancellation"; Feature 006 gives 72 for a declined wizard prompt and Feature 009
  FR-004 for end of input at its wizard question); the missing confirmation just described is 70 (needs-human; ADR 0013 "required
  human decision refused in unattended or answer-file mode, or not given"; Feature 008 command-surface.md says "70 unattended
  confirmation missing"). The precedent is not uniform: Feature 006 and Feature 008 put a missing confirmation at 70, and
  Feature 006 also a declined normal confirmation (throwaway staging) at 70; this feature deliberately sets a declined or
  unanswered prompt to 72 by the owner decision, and keeps a missing confirmation at 70. A `--confirm` value that does not equal the instance id is 64 (usage). The exact
  form is an author proposal under D1: a prompt that requires typing the instance id, and a flag `--confirm <id>` that, with a
  value equal to the instance id, skips the prompt (the per-item `--confirm <id>` pattern of Feature 009; Feature 011 FR-033
  in the merged PR #290 uses it too). The confirmation is not part of `Admit` (it is not a capability) and adds no
  refusal code. It does not remove the risk; see "Not enforced".

**Conformance (cases added to the Feature 008 suite)**

The labels S12-C1 to S12-C9 are stable. Feature 008 numbers its own cases 1 to 8 and Feature 011 FR-025 appends further cases
to the same suite, so the plan assigns the final numbers and updates the references here.


S12-C1. `Shell` on a provider with `interactive_shell` of `none`, `pty`+`not_enforced` and `pty`+`unknown` is refused with
   `refused.interactive_unavailable` and zero provider side-effect calls (FR-004).
S12-C2. The recorded exec plan for each profile has: full container id, explicit endpoint, interactive pty, the detach key
    sequence disabled, the Plan's user, working directory and login shell (`/bin/sh` for the job profiles by default); no privileged option, capability, mount, device, limit or env-file; no environment
    value in the argument vector (FR-008, FR-009, FR-018).
S12-C3. Environment: only the sources listed in FR-009; jobs carry `GIT_CONFIG_GLOBAL` (the read-only profile file) and
    `GIT_CONFIG_NOSYSTEM=1`, `developer` carries neither; identity variables present in a job shell only if `Create` put them
    in the container environment, otherwise absent; no operator-supplied value is accepted or scrubbed and no scrub list exists; hostile `TERM` falls back (FR-007, FR-009).
S12-C4. Ownership: missing label, value mismatch and an unlabeled object behind an existing instance record are refused with `refused.foreign_object` and zero
    mutations; a selector that is a container id, a name or the ADR 0009 workspace container exits 64 or 65 (FR-002, FR-006).
S12-C5. State (against a provider reporting `pty`/`enforced`; against `none`, `Admit` refuses first with `refused.interactive_unavailable`): every state other than `ready` is refused with `refused.invalid_state`, including a `discarded` record whose container no longer exists (the state check precedes the container inspection); for a `ready` record, a stopped, paused, restarting or missing container exits 68 naming the state and is not `refused.foreign_object`; two concurrent starts on one `ready` record: exactly one wins (the atomic transition of FR-014) and the other gets `refused.invalid_state`; a `finished` record gets no second shell; `ready` becomes `running` then
    `finished`; `discard` refuses a `running` instance and `clean` does not select it (FR-005, FR-014).
S12-C6. Terminal: stdin or stdout not a terminal, `--json`, trailing arguments, a malformed `--container-endpoint` exit 64 and call no pty-allocating operation
    (FR-007); exit status 0, 1, 2, 126, 127, 130 and 255 pass through unchanged; a CLI failure writes a `whr:` line and a
    shell status does not (FR-015).
S12-C7. End of session: each row of the FR-016 table with its exit code, instance state, reason and `whr:` line (daemon
    unreachable 72; container stopped or removed: client status passed through, `failed`/`container_lost`, no `whr:` line;
    exec ended: status passed through, `finished`; killed or lost client with the exec still running or undetermined: 72,
    `end_undetermined`); no key sequence detaches; SIGINT, SIGTERM and SIGHUP before the commit point leave no state change
    and exit 72, and after it exit 72 and `failed`/`interrupted`; a client that cannot be launched gives
    `failed`/`start_failed`, exit 74 and no rollback to `ready`; a missing shell executable (status 126 or 127) passes
    through and ends `finished` where the end-of-exec query exists, otherwise falls into row 4; a killed client whose exec
    process already ended is row 3 with the client's own signal status passed through and the shell's status also recorded
    when readable and different (FR-013, FR-014, FR-016, FR-017).
S12-C8. Confirmation: an `untrusted_job` without confirmation starts nothing; a prompt answered with anything other than the
    instance id, or left unanswered, gives 72; `--yes` alone still shows the prompt and starts nothing without the answer;
    a `--confirm` value other than the instance id gives 64; `--confirm <id>` with the right id, or the right answer, proceeds;
    `developer` and `cooperative_job` are never asked; a signal during the prompt exits 72 with no state change; the
    warning and prompt go to standard error with standard output left as command data only; an `untrusted_job` without
    `--confirm <id>` and with standard error not a terminal shows no prompt, starts nothing and exits 70 with a `whr:` line
    naming `--confirm`; without a terminal on standard input or output the run exits 64 at step 1 (FR-007, FR-023, FR-013).
S12-C9. Reconciliation (needs the verified query): a `running` record whose exec process is gone or was never started becomes
    `failed`/`session_lost` on a later `whr sandbox` invocation; a `running` record whose owner process is alive is untouched;
    after reconciliation `discard` and, for jobs, export work as for `failed` (FR-024).

### Key Entities

No new entity. Uses ExecutionInstance, ProviderCapabilities, SandboxRequest, AdmissionDecision, Refusal, OwnershipLabels and
Plan of the [Feature 008 data model](../008-workspace-sandbox/data-model.md). Additions to existing entities are listed in
"Changes to existing documents": a `Shell` operation on the provider interface and a failure `reason` on the instance
record.

## Not enforced

This feature enforces nothing about what happens inside the shell. In particular it does not:

- stop a program in the instance from writing terminal control sequences to the owner's terminal (FR-011), including from
  an `untrusted_job`;
- limit what a person or program does with the instance's mounts, network (no egress enforcement,
  `egress_enforcement=none` for the container provider) or credentials already present;
- provide kernel isolation beyond the shared-kernel claim of the container provider (`kernel_isolation=shared_kernel`);
- end, track or reap an in-container shell after the client or `whr` is killed (FR-017);
- restore the owner's terminal after a killed client, until the mechanism of FR-025 is designed and built;
- recover an instance left `running` when `whr` died without a handler after the commit point, until the reconciliation
  query of FR-024 is verified and built;
- make the confirmation of FR-023 anything but an informed consent: it does not filter terminal sequences, and it is not
  asked for `developer` and `cooperative_job`, whose shells have the same unfiltered channel;
- stop a person in the shell from exporting `GIT_CONFIG_*` or other git-control variables themselves, or remove variables the
  image sets (FR-009 describes only the environment the shell starts with);
- hide the shell's process, environment or file contents from other processes in the same container;
- prevent a `developer` shell from changing the host global git configuration, which later runs code on the host
  (Feature 008 FR-024);
- guarantee resize propagation or signal behavior of the container runtime client until the owner gate of SC-010 has run.

## Success Criteria *(mandatory)*

- **SC-001**: The admission table for `interactive_shell` (`none`, `pty` not enforced, `pty` unknown, `pty` enforced) yields
  the expected decision, with `refused.interactive_unavailable` and zero side-effect calls for the first three (FR-004,
  Feature 011 SC-014).
- **SC-002**: The container provider's reports contain `interactive_shell=none` until cases S12-C1 to S12-C8 pass and the
  real-host evidence of SC-010 is recorded, and `pty`/`enforced` afterward (FR-003).
- **SC-003**: The recorded exec plan matches FR-008, FR-009 and FR-018 for each of the three profiles, with a canary
  host-socket, writable-tool-mount and credential fixture absent (Feature 008 FR-027 style).
- **SC-004**: Foreign, partially labeled and mismatched objects are refused with zero mutations (FR-006).
- **SC-005**: Against a provider reporting `pty`/`enforced`, every state other than `ready`, including `discarded` with no
  container, is refused with
  `refused.invalid_state` and unchanged; of two concurrent starts exactly one proceeds (FR-014, MUST); a `finished` record gets no second shell; a `ready` record with a missing container exits 68 naming `missing`, not
  `refused.foreign_object` (FR-005, FR-014).
- **SC-006**: Non-terminal use, `--json`, trailing arguments and a malformed `--container-endpoint` value exit 64 without allocating a pty (FR-007).
- **SC-007**: Shell statuses 0 to 255 pass through unchanged and CLI failures use only 64, 65, 68, 70, 72, 74 with a `whr:` line
  (FR-015); the exit mapping function of Feature 008 T004 (`ExitFor`) gains no new code and its freeze test is unchanged
  except for the `sandbox shell` passthrough entry once the ADR 0013 amendment lands.
- **SC-008**: Signals, loss, container stop or removal, and a killed client produce exactly the exit code, state, reason and `whr:`
  line of the FR-016 table and FR-013 (case S12-C7), with no recorded session content (FR-014).
- **SC-009**: A second provider fixture declaring `pty` passes cases S12-C1 to S12-C8 with no change to production contract code
  (FR-022, Feature 008 SC-010).
- **SC-010** (owner gate, real hosts): on both real Docker hosts, Linux with Docker Engine and macOS with OrbStack (the macOS evidence
  comes from the owner's Mac), each with a terminal, evidence recorded in the PR or tracker shows, for each host:
  a shell starts and its status passes through; resize reaches the pty; job control (suspend, resume, interrupt) acts
  in the container; stopping the container from another terminal gives the `container_lost` row of the FR-016 table (status passed through, instance `failed`), and a killed client with the shell still running gives the `end_undetermined` row; no key sequence detaches; the environment passing route of
  FR-009 does not expose values in the process list. Until this evidence exists for both hosts FR-003 keeps `none`.
- **SC-011**: FR-001, FR-002, FR-010, FR-011, FR-012, FR-020 and the notice and help text parts of FR-022 are checked
  by independent review of the spec and, at implementation, by the tests named in the plan: FR-001 and FR-002 by the
  command table freeze test after T016; FR-010 by a byte-exact notice test; FR-011 by a help-text test and the owner gate
  of SC-010; FR-012 by SC-010; FR-020 by case S12-C2 (no command, no agent flag in the exec plan). FR-019 is covered by case S12-C3
  (no credential in the environment) and case S12-C2 (none in the argument vector) in addition to SC-003; FR-021 is a
  statement of scope covered by SC-003 and case S12-C2 and has no separate test.

- **SC-012**: An `untrusted_job` shell starts only after the explicit confirmation of FR-023, `--yes` alone never starts it,
  the exit codes are 72 (prompt declined or unanswered), 70 (no prompt possible because standard error is not a terminal and no `--confirm` is given) and 64 (wrong `--confirm` value) as FR-023 states, with the prompt on standard error, and `developer` is never asked (case S12-C8).
- **SC-013**: With the verified query, reconciliation marks gone or never-started `running` records `failed`/`session_lost`
  and leaves live ones alone (case S12-C9). Before the query is verified this criterion is not claimed and the gap stays
  under "Not enforced".

## Open questions for the owner

1. **Exit codes of two author-proposed readings.** RESOLVED 2026-11-01 (owner answers, issue #317, comment 6559753902 and its addendum 6560732494, which supersedes only the "impossible -> 72" half of item 1a). (a) SPLIT, a deviation from the
   author proposal for the first two cases: a declined confirmation or an unanswered prompt (end of input) exits 72, not 70
   (a cancellation: command-surface.md lines 41 and 42, the wizard prompt of Feature 006, end of input at the Feature 009 FR-004
   question); an impossible confirmation (no prompt possible: standard error not a terminal and no `--confirm`) stays 70, as ADR 0013 row 70, command-surface.md and Feature 008 FR-035 say
   (FR-015, FR-023). (b) Accepted: exit 72 for a killed or lost client stream (FR-015, FR-016). Neither needs a new code or an
   ADR 0013 change.

The open questions 1 to 7 of earlier drafts were decided by the owner on 2026-10-31 (D1 to D7), and four author proposals
became D8. What is otherwise left is for the plan, not for a decision: the mechanism of the atomic start transition (D8c),
the exact confirmation form (FR-023), and the verification items below. The design of the terminal restore (FR-025) needs an owner
decision (task T028).

## Assumptions

- The instance and Plan model of Feature 008 is unchanged apart from the additions in "Changes to existing documents", and
  `Create` produces a container that is able to host an exec session; `ready` does not imply that the container is running
  (D4), so a stopped container is reported, never started.
- The container provider is local-only and uses the same host Docker access as `shell --local` (D6); there is no
  control-plane path and no grant. This extends the relaxation reasoning of Feature 010 FR-013 and needs the Feature 006
  amendments listed below.
- Reading about the container runtime client (resize carriage, signal handling, environment passing, detach disabling) in
  other specs is secondhand; none of it was run for this feature.

### Verification before the plan (checks, not decisions)

- Whether the container runtime client carries the initial and later terminal size for the exec (FR-012).
- Whether the runtime can disable its detach key sequence (FR-008); if not, no provider may report `pty`/`enforced`.
- Which route passes environment values to the exec without placing them in the argument vector (FR-009).
- How the client behaves on SIGHUP, SIGTERM and SIGINT sent to it after the hand-over, and what status it returns for a
  killed client, a stop and a removed container (FR-013, FR-016).
- Which read-only query establishes that the exec process ended (the command-line client does not print an exec id; the
  engine API is a candidate) (FR-014, FR-016), and the same or a related query for the reconciliation of FR-024 (exec process
  gone or never started).

## Changes to existing documents (on approval; each lands through the plan and tasks, none is made by this change)

- `specs/011-vm-provider/spec.md`: FR-003 (container provider report `interactive_shell` from `none` to `pty`/`enforced`
  once this feature is implemented), and the "Owner decisions" entry 1 of that spec (the line "the container provider reports `interactive_shell=none`";
  PR #290 removed the earlier "Author proposals" entry); the Feature 011 FR-016 text stays unchanged. Feature 011 A1 (capability field
  `interactive_shell`) is a prerequisite of this feature; if A1 is not approved first, this feature has to add that field
  itself.
- `specs/008-workspace-sandbox/spec.md`: FR-028 (verb list gains `shell`, proposed), FR-029 (a note that `shell` starts no
  agent), FR-035 (mapping: no new code, passthrough).
- `specs/008-workspace-sandbox/contracts/runtime-provider.md`: capability table row `interactive_shell` for the container
  provider (after 011 A1: `none` becomes `pty`); a `Shell` operation on the illustrative interface; conformance cases S12-C1 to
  S12-C9; the note that `Create`, `Exec`, `Shell`, `Remove` and `Cancel` re-verify ownership.
- `specs/008-workspace-sandbox/contracts/command-surface.md`: verbs table row for `shell`; exit-code paragraph.
- `specs/008-workspace-sandbox/data-model.md` and `contracts/`:
  - the `ready` to `running` trigger "exec started" also covers a shell started (Shell is an operation separate from Exec),
    and the transition is an atomic conditional update of the instance record (a new Feature 008 requirement, owner decision
    D8c; the data model and Feature 008 define no record store or locking, so the mechanism is fixed by the plan);
  - the `running` to `failed` trigger text is widened beyond "cancellation, crash or deadline" to a client that cannot be
    launched (`start_failed`), loss of the container or daemon, a signal to `whr` after the commit point, a session whose end
    cannot be established, and the reconciliation `session_lost` (FR-024; the reconciliation depends on a query that is not
    verified, and the `running` record carries an owner process id and start time);
  - a failure `reason` on the instance record (values `transport_lost`, `container_lost`, `end_undetermined`, `interrupted`,
    `start_failed`, `session_lost`) and, beside the recorded status, the optional exec process status (FR-014);
  - Plan gains a working directory (the data model's Plan lists user and home but no working directory; the per-profile
    values are fixed by the plan) and a login shell field (D3: `/bin/sh` default for the job profiles; `developer` carries the
    `human_shell` of the workspace manifest);
  - `refusal-codes.json`: the "when" text of `refused.host_socket_mount` ("would be mounted", plan-time wording) is widened to
    "is or would be mounted", and that of `refused.tool_mount_writable` ("would be writable") to "is or would be writable", so
    that both cover the step-8 re-check of an existing container (FR-018); no code beyond Feature 011's
    `refused.interactive_unavailable`; if Feature 011 has not landed first, append that one code (see FR-004).
- `specs/008-workspace-sandbox/tasks.md` and `plan.md`: new tasks and evidence rows for cases S12-C1 to S12-C9, after T001 and T002.
- `docs/adr/0013-whr-exit-codes.md` and `contracts/cli/exit-codes.json` (passthrough `commands`): add `sandbox shell`; no new code or category. The passthrough text states the one exception of FR-015: for a killed client the
  passed-through status is the client's own signal status, not the shell's. The ambiguity note is extended to `sandbox shell`
  (text only; the ADR and `exit-codes.json` name only `ssh` today, and `shell --local` has the same gap). The owner confirmed
  (2026-11-01) the author reading that a client stream ending while the remote side did not exit (a killed or lost client) is a
  transport loss (72). The owner also decided (2026-11-01) 72 for a declined confirmation or an unanswered prompt (a
  cancellation, as for the Feature 006 wizard prompt and the end of input at the Feature 009 FR-004 question) and 70 for an
  impossible confirmation (FR-023, FR-015), which is what ADR 0013 row 70 and command-surface.md already say: no ADR 0013 change and no change of the 70 text of
  command-surface.md or Feature 008 FR-035 is needed for the confirmation. This change edits neither ADR 0013 nor `exit-codes.json`.
  `docs/adr/0014-whr-command-structure.md` (T017 amendment, group `sandbox`) and `contracts/cli/commands.json`
  (T016): register the verb with its flag list (`--confirm`, `--container-endpoint`, FR-007). Feature 008 tasks.md scopes T016 to the seven existing verbs, so T016 and T017 must be
  extended to cover `shell`, or a new task added, before the freeze test of SC-011 can name it. These are owner-gated.
- `specs/006-whr-cli/spec.md`: FR-019 (list gains `sandbox shell`, with the killed-client status exception), FR-006 (the
  exception for the grant requirement extends to `sandbox shell`, owner decision D6; the text today limits it to
  `shell --local`), FR-022a (the relay filtering does not apply to `sandbox shell`, as it states for `shell --local`) and
  FR-047 (human-shell policy for new shell sessions applies to the `developer` login shell). FR-030 is unchanged.
- `specs/010-host-and-workspace-panes/spec.md`: no text change. Its FR-013 (grant relaxation), FR-014 (no filtering) and
  Edge Cases / OQ-6 (126 and 127 pass through) are scoped to `shell --local`; this feature reuses the notice pattern and liveness query
  by reference, differs on detach (disabled here, a fixed sequence there) and carries the extension in the Feature 006 amendments above.
- `apps/whr/internal/runtime/` (ADR 0010 layout): `Shell` on the provider interface, fake provider support and the
  conformance cases. No code is written by this change.
