# Feature Specification: Interactive shell for the container provider

**Feature identifier**: `012-container-interactive-shell`
**Feature Branch**: `docs/spec012-container-interactive-shell`
**Created**: 2026-10-31
**Input**: Owner decision 1 of 2026-10-31, recorded in the unmerged PR #290 (`docs(spec): record owner decisions for
Feature 011`): the container provider should get an interactive shell in the future; until a separate feature builds it,
the container provider keeps `interactive_shell=none`. This feature is that separate feature.
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md).
**Related**: [Feature 008](../008-workspace-sandbox/spec.md) (RuntimeProvider contract, FR-005, FR-012 to FR-015, FR-024,
FR-028 to FR-030, FR-034, FR-035, [refusal codes](../008-workspace-sandbox/contracts/refusal-codes.json),
[runtime provider contract](../008-workspace-sandbox/contracts/runtime-provider.md),
[command surface](../008-workspace-sandbox/contracts/command-surface.md)),
[Feature 011](../011-vm-provider/spec.md) (capability field `interactive_shell`, FR-003, FR-004, FR-016, refusal code
`refused.interactive_unavailable`), [Feature 010](../010-host-and-workspace-panes/spec.md) (`shell --local`, FR-001 to
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
- **Hand-over**: the point after which `whr` no longer writes to the terminal and the container runtime client owns the
  terminal directly, as for `whr shell --local` (Feature 010 FR-010, FR-014).

### Boundaries

- Spec only. No runtime code exists: `apps/whr/internal/runtime/`, `Admit`, the capability types and the conformance suite
  are not built (Feature 008 T001 and T002 are open, tasks.md), the `sandbox` command group is not registered (T016, T017
  are open) and Feature 011 is itself a draft. This feature writes no implementation code and registers no command.
- Out of scope: a VM provider (Feature 011 sets its own `interactive_shell` value and conformance), a non-interactive
  `exec` (Feature 008 OD-1), attaching to a process that is already running (no reattach), a persistent or multiplexed
  session, the shell of the ADR 0009 workspace container (served by `whr shell --local`, Feature 010, unchanged), and any
  remote or control-plane access path.
- Feature 006 FR-030 is unchanged: `whr` does not install, configure, authenticate, launch or wrap an agent's native tool.
  The shell starts only a login shell and takes no command (FR-007).

### Owner decisions

- **2026-10-31 (decision 1 of PR #290, not yet on `main`)**: Docker should also get an interactive shell in the future;
  until a separate feature builds it, the container provider keeps `interactive_shell=none`. This feature is the separate
  feature. Its approval does not change what `main` says today; the change takes effect only through the tasks of a later
  plan (see "Changes to existing documents").
- No other owner decision is recorded for this feature. Choices below that go beyond the cited texts are author
  proposals and are marked as such.

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
2. **Given** `--json`, a trailing command or any further argument, **Then** exit 64 (FR-007).

### User Story 4 - The container disappears during the session (Priority: P2)

**Acceptance Scenarios**:

1. **Given** the container is stopped or removed from outside while the shell runs, **When** the client ends, **Then** `whr`
   decides between a shell status and a transport loss by the liveness rule of FR-016, records `failed` for a loss, and
   exits 72 for a loss.

### Edge Cases

- Instance in `preparing`, `running`, `finished`, `failed`, `discarding`, `discarded` or `cleanup_incomplete`: refused
  `refused.invalid_state`, exit 68, no state change (FR-005, Feature 008 data model). A second shell on an instance whose
  first shell has not ended is the `running` case.
- Instance record exists but the container is stopped or missing: exit 68 naming the state; `whr` starts nothing.
- Unknown id: exit 65. A container carrying only some of the ownership labels, or labels that do not match the instance
  record: `refused.foreign_object`, exit 68, zero mutations (FR-006).
- The selector names the ADR 0009 workspace container or any object without the sandbox ownership labels:
  `refused.foreign_object`; the operator uses `whr shell --local` for the workspace container.
- `docker` client absent, daemon unreachable, a non-`unix://` endpoint or no permission on the socket: exit 68 with
  remediation, as Feature 010 FR-002 and FR-009 state for `shell --local`; no `sudo` is run.
- The shell executable is missing in the image: the runtime reports its own error and status (126 or 127 are the usual
  shell conventions), which pass through; `whr` does not substitute another shell (as Feature 010 Edge Cases).
- The terminal type is hostile or unusual: `TERM` is validated, otherwise a fixed default is used (FR-009).
- Two terminals attempt a shell on the same instance: the second is refused as `running` (FR-005).
- The user detaches with the fixed detach keys or the client is killed: FR-017.

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
- **FR-002** (selector): `<id>` is the instance id, resolved by the same rule as the other `sandbox` verbs. The command
  addresses the container by its full runtime id after resolution, never by name.

  *Alternatives considered* (author analysis, not an owner decision):

  | Option | Shape | Assessment |
  | --- | --- | --- |
  | A | `whr sandbox shell <id>` | Chosen. `shell` is the existing Feature 006 verb for a fresh login shell; the `sandbox` group (Feature 008 OD-4, option A) owns instance verbs; no short form is needed (ADR 0014 alias rules 3 and 5, Feature 008 command-surface.md "no short forms are needed"). |
  | B | `whr sandbox exec -it <id>` | Rejected for this feature: `exec` is deferred by Feature 008 OD-1 and its contract is an opaque argv as data (FR-029), whereas a shell takes no argv. A later `exec` stays independent. |
  | C | `whr shell --sandbox <id>` (flag on the existing verb) | Rejected: the workspace verb `shell` is the remote, grant-based command (Feature 006 FR-006) with `--local` as its one exception (Feature 010); a further selector flag would overload one entry with two trust models and two exit-code paths. |
  | D | `whr sandbox attach <id>` | Rejected: in Feature 006, `console` and `agent attach` attach to a process that outlives its attachments (FR-007a); this shell does not outlive its session (FR-014), so the name would mislead. |

**Capability and admission**

- **FR-003** (capability): The container provider MUST report `interactive_shell=pty` with status `enforced`, for each
  profile it supports, only when the conformance cases of this feature (cases 9 to 15 below) pass for it and the real-host
  evidence gate of SC-010 is met. Until then it reports `interactive_shell=none` (Feature 011 FR-003 as it stands). A
  report claiming more than its tests prove is non-conformant (Feature 008 FR-018). The `scope` states that the claim covers
  a pty allocated by the container runtime for a login shell inside the instance's container, and nothing about what runs
  in it.
- **FR-004** (admission): `whr sandbox shell` is a request for the requirement `interactive_shell=pty`. `Admit` MUST decide
  it as a pure function of request and capabilities (Feature 008 FR-013). A request whose `interactive_shell` is not `pty`
  with status `enforced` MUST be refused with `refused.interactive_unavailable` (Feature 011 FR-016, already in its
  refusal table), before any side effect, with no fallback to a non-interactive or piped mode. `unknown` never satisfies
  it. No new refusal code is introduced by this feature.
- **FR-005** (order and state): The command MUST check, in this order, and stop at the first failure: (1) usage and
  terminal conditions (FR-007), exit 64; (2) the instance exists, exit 65; (3) `Admit` for the instance's profile and
  provider, exit 68 `refused.interactive_unavailable`; (4) ownership (FR-006), exit 68; (5) state `ready` per the Feature
  008 data model, otherwise `refused.invalid_state`, exit 68; (6) the container is running, otherwise exit 68 naming the
  state. Steps 3 to 6 make read-only provider queries only. The shell is allowed from `ready` only, so the Feature 008
  state machine needs no new transition: `ready` to `running` on hand-over, `running` to `finished` or `failed` at the end
  (FR-014).
- **FR-006** (ownership): Before any change the provider MUST inspect the target by its full id and verify that it carries
  every ownership label of the Feature 008 data model with values equal to the instance record (Feature 008 FR-005; the
  runtime-provider contract already requires `Create`, `Exec`, `Remove` and `Cancel` to re-verify). An object that lacks
  them, or whose values differ, is refused with `refused.foreign_object`, exit 68, zero mutations. `Shell` MUST NOT select
  an object by name, by label query alone or by image.

**Terminal behavior**

- **FR-007** (terminal required): The command MUST require standard input and standard output to be terminals, MUST take
  no trailing command and no further arguments, and MUST reject `--json` as a usage error (exit 64), as Feature 010 FR-006
  does for `shell --local`. A pty MUST be allocated only in this case; no pty is requested when either stream is not a
  terminal, and there is no documented fallback to a piped shell, because that mode is the deferred `exec` (Feature 008
  OD-1). Standard error need not be a terminal.
- **FR-008** (exec form): For the container provider the hand-over MUST be one invocation of the container runtime client
  with a fixed argument vector (no shell, no string evaluation) built only from validated values, addressing the
  container by full id and passing the discovered `unix://` endpoint explicitly, as Feature 010 FR-002 and FR-004 state
  for `shell --local`. The invocation MUST request an interactive pty session, the fixed detach keys of Feature 010 FR-004,
  the Plan's user and working directory, and the environment of FR-009. It MUST NOT use the runtime's privileged option,
  add capabilities, add or change mounts, devices or limits, or read an environment file. The exact vector is the
  provider's and is asserted by the conformance suite (case 10); this text states the constraints.
- **FR-009** (environment): The shell environment MUST be the Plan's environment filtered by profile exactly as `Exec`
  filters it (Feature 008 FR-023 and runtime-provider.md: only the four identity variables for jobs, other git-control
  variables scrubbed with the scrub listed in the result, `GIT_CONFIG_*` refused for `developer` with
  `refused.git_env_override`), plus `TERM` and `WHR_SANDBOX_ID=<id>` (author proposal, so a prompt can show it). `TERM`
  MUST be the host value when it matches `[A-Za-z0-9._+-]{1,64}`, otherwise `xterm-256color` (as Feature 010 FR-005). No
  other host environment variable is forwarded. Values MUST NOT be placed in the argument vector (visible to other local
  users in the process list); the provider passes them by a route that does not expose them, which is to be verified
  (Verification before the plan).
- **FR-010** (notice): When standard error is a terminal, `whr` MUST write one line to it before the hand-over, with the
  instance id and profile escaped per Feature 006 FR-022, naming that the session has no grant, uses host container
  runtime access and which detach keys apply (the byte-exact text is fixed by the plan, modeled on Feature 010 FR-010).
  After the hand-over `whr` MUST write nothing to the terminal stream. Color MUST NOT be required.
- **FR-011** (terminal safety): The terminal-safety rules are those of the local shell of Feature 006 as Feature 011
  FR-016 requires: `whr` does not relay the stream, the container's process owns the terminal directly, and the OSC, DCS,
  APC, PM and SOS filtering of Feature 006 FR-022a does not apply to it (Feature 006 FR-022a states this exception for
  `shell --local`; Feature 010 FR-014). Feature 006 FR-022 applies to every line `whr` itself writes (notice, errors,
  candidate lists), which contain untrusted names. `--help` MUST state that the session stream is neither inspected nor
  filtered. Consequence, stated plainly: a process in the instance, including one in an `untrusted_job`, can write
  control sequences that change the terminal title, write the clipboard or otherwise act on the owner's terminal. This
  feature does not prevent that (see "Not enforced" and Open question 1).
- **FR-012** (resize): The initial terminal size MUST reach the in-container pty at start, and later size changes MUST be
  propagated for the lifetime of the session. For the container provider this is expected to be carried by the container
  runtime client (Feature 010 Edge Cases states the same expectation for `shell --local`); it is not verified here and is
  checked at the owner gate of SC-010. `whr` adds no resize handling of its own unless that check fails.
- **FR-013** (signals and job control): With a pty, keyboard interrupt, suspend and quit characters are bytes delivered to
  the in-container terminal and act on the in-container shell and its jobs; job control is the shell's. `whr` MUST NOT
  convert them into host-side signals or exit codes. For signals sent to the `whr` process itself:
  - SIGINT or SIGTERM before the hand-over: exit 72, nothing created, no state change, no leftover (rollback of any
    transition made so far);
  - SIGHUP (for example the terminal closes) before the hand-over: handled like SIGTERM (author proposal; Feature 006
    names only SIGINT and SIGTERM);
  - SIGINT, SIGTERM or SIGHUP after the hand-over: the session is a transport loss for `whr`'s purposes; it records the
    instance `failed` with a reason, exits 72, and does not claim that the in-container shell ended (FR-017).
  How the container runtime client reacts to these signals is not verified here.

**Exit status and lifecycle**

- **FR-014** (state and record): At hand-over the instance record moves `ready` to `running`; when the provider has
  established that the shell process ended (FR-016) it moves to `finished` with the shell's status recorded, as Feature 008
  data model states for `exec`. A recorded status is not acceptance (Feature 008 FR-007). A loss, a signal after hand-over,
  or an undeterminable end moves the instance to `failed` with a reason, from which export remains allowed with
  `partial=true` (Feature 008 FR-007). `discard` and `clean` are refused while the instance is `running` (data model).
  The session content, the environment and the terminal bytes MUST NOT be recorded or logged; only the `whr:` lines and the
  status/reason fields are (as Feature 010 FR-015).
- **FR-015** (exit codes): The command MUST use the ADR 0013 table without a new code or category:
  - the shell's own status 0 to 255, unchanged, once the shell has ended (the passthrough rule, Feature 006 FR-019, which
    names `shell` including `shell --local`, `console`, `ssh` and `agent attach` today; `sandbox shell` is added by the
    amendment listed below);
  - 64 for usage, no terminal, `--json`, a trailing command or further argument;
  - 65 for an unknown instance;
  - 68 for any `refused.*` (including `refused.interactive_unavailable`, `refused.foreign_object`,
    `refused.invalid_state`, `refused.git_env_override`), a non-running container, an absent client, an unreachable
    socket or a non-`unix://` endpoint (the mapping of Feature 008 FR-035 and Feature 010 FR-009);
  - 72 for SIGINT, SIGTERM or SIGHUP before or after the hand-over and for transport loss after it;
  - 74 for an unclassified failure.
  Exit codes 66 and 67 are not used (no remote authority), 70 is not used (no confirmation exists), 71 and 73 are not used
  by this command. Because remote statuses overlap 64 to 79, CLI failures MUST also write a `whr:` line to standard error
  and a status coming from the shell MUST NOT (Feature 010 FR-008, ADR 0013). `--json` is rejected, so the `kind`
  field of the JSON exit event is not available for this command.
- **FR-016** (container stop or removal mid-session): The container can stop or be removed from outside (for example a
  runtime stop, an out-of-memory kill, a daemon restart). The client then ends with a non-zero status that cannot be told
  from a shell status. After any non-zero client status `whr` MUST run one read-only liveness query within a 5-second
  budget (Feature 010 FR-004, FR-008 pattern): if the container or daemon cannot be reached, or the query does not
  answer in time, exit 72 and record `failed` with reason `transport_lost`; otherwise pass the client status through and
  record `finished`, provided the provider can establish that the exec process ended (next sentence). Establishing that the
  shell process ended is the provider's responsibility and MUST use a read-only query; if it cannot be established, the
  instance is recorded `failed` with reason `end_undetermined`, not `finished`. A status such as 137 or 143 can be a real shell status
  after a stop signal; passing it through is the rule, and the container state in the `whr:` line for a loss is the
  distinguishing signal.
- **FR-017** (detach and orphans): The fixed detach keys of Feature 010 FR-004 can leave the in-container shell running
  after the client ends, and a killed `whr` or client can too (Feature 010 FR-007 accepts this for `shell --local`). This
  feature does not track or reap such a process. It records the instance `failed` with reason `session_detached_or_lost`
  rather than `finished`, so that export carries `partial=true`, and a running orphan is bounded by `discard`, whose
  provider `Remove` is bounded, releases the instance's labelled objects and preserves results (Feature 008 FR-017). Whether
  the shell session should disable detaching is Open question 2.

**Security**

- **FR-018** (no widening): A shell session MUST NOT mount the host Docker socket or any host control socket (Feature
  008 FR-010 and the Plan section of runtime-provider.md, refusal `refused.host_socket_mount`), MUST NOT use privileged mode, add capabilities, devices or
  mounts, and MUST NOT change the instance's Plan, labels, tool mounts (read-only, Feature 008 FR-016) or limits. The
  process runs with the instance's existing container, user and mounts; the session adds a process, not a boundary
  change. Before hand-over the provider MUST re-inspect the container and refuse (with the existing codes
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
  accesses and no new access. The creation and `show` output of Feature 008 already states the profile; the notice of FR-010
  names it again.
- **FR-022** (vendor and provider neutrality): The command, `Admit`, the refusal code and the lifecycle MUST NOT name or
  branch on a provider kind (Feature 008 FR-034). Other providers declare their own `interactive_shell` value, and a
  provider that declares `pty` must pass the same cases; adding one does not change this feature's contract text. Normative
  text names no vendor product; the container runtime is the one of Feature 008 and `docs/container-runtime.md`. Generated
  text (notice, help, errors) MUST name no model or vendor beyond the container runtime's generic name, and the vendor-name
  gate of Feature 011 FR-022, once it exists, applies to this file.

**Conformance (cases added to the Feature 008 suite, numbered after its eight)**

9. `Shell` on a provider with `interactive_shell` of `none`, `pty`+`not_enforced` and `pty`+`unknown` is refused with
   `refused.interactive_unavailable` and zero provider side-effect calls (FR-004).
10. The recorded exec plan for each profile has: full container id, explicit endpoint, interactive pty, detach keys, the
    Plan's user and working directory; no privileged option, capability, mount, device, limit or env-file; no environment
    value in the argument vector (FR-008, FR-009, FR-018).
11. Environment: only the allowed set; identity variables for jobs; scrubbed git-control variables for jobs; `GIT_CONFIG_*`
    refused for `developer`; hostile `TERM` falls back (FR-009).
12. Ownership: missing label, value mismatch and an unlabelled object are refused with `refused.foreign_object` and zero
    mutations; the ADR 0009 workspace container is not selectable (FR-006).
13. State: every state other than `ready` is refused with `refused.invalid_state`; `ready` becomes `running` then
    `finished`; `discard` and `clean` refuse a `running` instance (FR-005, FR-014).
14. Terminal: stdin or stdout not a terminal, `--json`, trailing arguments exit 64 and call no pty-allocating operation
    (FR-007); exit status 0, 1, 2, 126, 127, 130 and 255 pass through unchanged; a CLI failure writes a `whr:` line and a
    shell status does not (FR-015).
15. Loss: client exit with an unreachable container or daemon gives exit 72 and `failed`; a reachable one passes the status
    through; an undeterminable end gives `failed`; SIGINT, SIGTERM, SIGHUP before hand-over leave no state change and exit 72;
    after hand-over exit 72 and `failed` (FR-013, FR-016, FR-017).

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
- end, track or reap an in-container shell after a detach or after `whr` is killed (FR-017);
- hide the shell's process, environment or file contents from other processes in the same container;
- prevent a `developer` shell from changing the host global git configuration, which later runs code on the host
  (Feature 008 FR-024);
- guarantee resize propagation or signal behavior of the container runtime client until the owner gate of SC-010 has run.

## Success Criteria *(mandatory)*

- **SC-001**: The admission table for `interactive_shell` (`none`, `pty` not enforced, `pty` unknown, `pty` enforced) yields
  the expected decision, with `refused.interactive_unavailable` and zero side-effect calls for the first three (FR-004,
  Feature 011 SC-014).
- **SC-002**: The container provider's reports contain `interactive_shell=none` until cases 9 to 15 pass, and
  `pty`/`enforced` afterward (FR-003).
- **SC-003**: The recorded exec plan matches FR-008, FR-009 and FR-018 for each of the three profiles, with a canary
  host-socket, writable-tool-mount and credential fixture absent (Feature 008 FR-027 style).
- **SC-004**: Foreign, partially labelled and mismatched objects are refused with zero mutations (FR-006).
- **SC-005**: Every state other than `ready` is refused with `refused.invalid_state` and unchanged (FR-005, FR-014).
- **SC-006**: Non-terminal use, `--json`, trailing arguments exit 64 without allocating a pty (FR-007).
- **SC-007**: Shell statuses 0 to 255 pass through unchanged and CLI failures use only 64, 65, 68, 72, 74 with a `whr:` line
  (FR-015); the exit mapping function of Feature 008 T004 (`ExitFor`) gains no new code and its freeze test is unchanged
  except for the `sandbox shell` passthrough entry once the ADR 0013 amendment lands.
- **SC-008**: Signals and loss produce the recorded states and exit 72 as FR-013, FR-016 and FR-017 state, with no
  recorded session content.
- **SC-009**: A second provider fixture declaring `pty` passes cases 9 to 15 with no change to production contract code
  (FR-022, Feature 008 SC-010).
- **SC-010** (owner gate, real host): on a real Docker host with a terminal, evidence recorded in the PR or tracker shows:
  a shell starts and its status passes through; resize reaches the pty; job control (suspend, resume, interrupt) acts
  in the container; stopping the container from another terminal gives the FR-016 outcome; the environment passing route of
  FR-009 does not expose values in the process list. Until this evidence exists FR-003 keeps `none`.

## Open questions for the owner

1. **Untrusted jobs and terminal sequences.** Admission is by capability and does not exclude `untrusted_job`
   (Feature 008 FR-024), and the stream is not filtered (FR-011). Should an `untrusted_job` shell be excluded, require an
   explicit acknowledgement, or be served by a filtering relay (a different design with its own pty handling, cf. Feature 006
   FR-022a)? Until decided this text admits it and states the risk.
2. **Detach keys.** Should the sandbox shell disable detaching so that closing the client reliably ends the session, if the
   container runtime allows it (not verified; Feature 010 FR-004 reports the documentation is silent)?
3. **Shell for job profiles.** Feature 010 FR-005 takes the shell from the workspace manifest. A job's scratch home has
   no equivalent in Feature 008. Which login shell does the Plan carry for `cooperative_job` and `untrusted_job`?
4. **`ready` and a running container.** Feature 008 does not say whether a `ready` instance's container is already running
   (FR-005 step 6 refuses a stopped one and starts nothing). Should `ready` imply a running container?
5. **Re-entry.** The state machine allows one shell (or exec) per instance, because `finished` only allows export and
   discard. Is that acceptable, or should a later change allow `finished` to `running` again?

## Assumptions

- The instance and Plan model of Feature 008 is unchanged and `Create` produces a container that is able to host an exec
  session; this is not stated in Feature 008 (Open question 4).
- The container provider is local-only and uses the same host Docker access as `shell --local`; there is no control-plane
  path and no grant, and the relaxation reasoning of Feature 010 FR-013 applies by analogy. The owner approval of this text
  covers that extension, not Feature 010's text.
- Reading about the container runtime client (resize carriage, signal handling, detach semantics, environment passing) in
  other specs is secondhand; none of it was run for this feature.

### Verification before the plan (checks, not decisions)

- Whether the container runtime client carries the initial and later terminal size for a detached-keys exec (FR-012).
- Which route passes environment values to the exec without placing them in the argument vector (FR-009).
- How the client behaves on SIGHUP, SIGTERM and SIGINT sent to it after the hand-over, and what status it returns for a
  detach, a stop and a removed container (FR-013, FR-016).
- Which read-only query establishes that the exec process ended (the command-line client does not print an exec id; the
  engine API is a candidate) (FR-014, FR-016).

## Changes to existing documents (on approval; each lands through the plan and tasks, none is made by this change)

- `specs/011-vm-provider/spec.md`: FR-003 (container provider report `interactive_shell` from `none` to `pty`/`enforced`
  once this feature is implemented), the "Author proposals" or, if PR #290 has merged, "Owner decisions" entry 1, the
  assumption that repeats it, and the Feature 011 FR-016 text stays unchanged. Feature 011 A1 (capability field
  `interactive_shell`) is a prerequisite of this feature; if A1 is not approved first, this feature has to add that field
  itself.
- `specs/008-workspace-sandbox/spec.md`: FR-028 (verb list gains `shell`, proposed), FR-029 (a note that `shell` starts no
  agent), FR-035 (mapping: no new code, passthrough).
- `specs/008-workspace-sandbox/contracts/runtime-provider.md`: capability table row `interactive_shell` for the container
  provider (after 011 A1: `none` becomes `pty`); a `Shell` operation on the illustrative interface; conformance cases 9 to
  15; the note that `Create`, `Exec`, `Shell`, `Remove` and `Cancel` re-verify ownership.
- `specs/008-workspace-sandbox/contracts/command-surface.md`: verbs table row for `shell`; exit-code paragraph.
- `specs/008-workspace-sandbox/data-model.md` and `contracts/`: a failure `reason` on the instance record; Plan gains the
  login shell (Open question 3). `refusal-codes.json` needs no new code.
- `specs/008-workspace-sandbox/tasks.md` and `plan.md`: new tasks and evidence rows for cases 9 to 15, after T001 and T002.
- `docs/adr/0013-whr-exit-codes.md` and `contracts/cli/exit-codes.json` (passthrough `commands`): add `sandbox shell`.
  `docs/adr/0014-whr-command-structure.md` (T017 amendment, group `sandbox`) and `contracts/cli/commands.json`
  (T016): register the verb. These are owner-gated.
- `specs/006-whr-cli/spec.md`: FR-019 list gains `sandbox shell` (a one-line amendment; FR-030 is unchanged).
- `specs/010-host-and-workspace-panes/spec.md`: no change; its detach keys, notice pattern and liveness query are reused
  by reference.
- `apps/whr/internal/runtime/` (ADR 0010 layout): `Shell` on the provider interface, fake provider support and the
  conformance cases. No code is written by this change.
