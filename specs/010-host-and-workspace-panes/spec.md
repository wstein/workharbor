# Feature Specification: Host and workspace panes in one host session

**Feature identifier**: `010-host-and-workspace-panes`
**Feature Branch**: `docs/spec-010-host-and-workspace-panes`
**Created**: 2026-10-23
**Status**: P1–P4 implementation packages are merged; see the
[implementation and acceptance evidence](../../docs/quality/010-host-and-workspace-panes/implementation.md).
The settled decisions below are unchanged. Whole-feature acceptance remains open:
SC-009 owner gates G-A/T018 and G-B/T030 are UNVERIFIED, T031 is incomplete, and
SC-007's remote session-creation consumer is absent. Package merges do not establish
real-host acceptance or approve Proposed ADR 0015.
**Related**: [Feature 006](../006-whr-cli/spec.md) (FR-006, FR-007, FR-007a, FR-019, FR-022, FR-022a, FR-029, FR-030, FR-047, FR-048,
FR-052, FR-058, FR-066, FR-067, FR-063), [ADR 0012](../../docs/adr/0012-persistent-workspace-console.md),
[ADR 0013](../../docs/adr/0013-whr-exit-codes.md), [ADR 0014](../../docs/adr/0014-whr-command-structure.md),
[ADR 0015](../../docs/adr/0015-host-session-and-local-shell.md), [Feature 009](../009-host-setup-wizard/spec.md) (host catalogue lists herdr).
**Input**: Owner request of 2026-10-23: work from a notebook over SSH and VPN on the WorkHarbor host, attach to one persistent Herdr
session that runs on the host only, and open any number of panes of two clearly distinguishable kinds, privileged host shells and
shells inside a workspace container, with a standard, short command instead of a hand-typed `docker exec`.

## Context and scope

The owner reaches the WorkHarbor host over SSH and VPN and runs one herdr session there. Inside it, a pane is either a host shell (the
owner's own account on the host, unrestricted) or a shell in a workspace container. Today the second kind is a hand-typed
`docker exec` against a container that must first be found. This feature standardises that step the way SourceSteward bundles long
Git and GitHub command lines: one short command that finds the container by the labels `whr ls --local` already uses, picks the
right user, directory and `TERM`, and hands the terminal over.

Feature 006 does not allow this yet. Its `shell` goes through the control plane with a single-use grant (FR-006), the control plane and
owner API do not exist (`workspace.shell` and `workspace.console` are `Increment: later`, `Needs: control_plane`, `BlockedOn:
owner_api` in `apps/whr/internal/commands/entries.go`), and FR-048 makes herdr the default backend of the in-container console, which
would nest herdr in the host herdr. This feature adds a host-local shell mode, reconciles FR-048 and decides how the persistent console
relates to local mode.

### Terms

- **Host session**: the single herdr session on the WorkHarbor host to which the owner attaches over SSH.
- **Host pane**: a pane running a shell of the owner's host account, opened by herdr itself. `whr` has no part in it.
- **Workspace pane**: a pane whose process is `whr shell --local <ws>`.
- **Local mode**: a `whr` command run with `--local` on the Docker host, talking only to the local Docker daemon (FR-066). No control
  plane, login or grant is involved.
- **Workspace container**: a running container that `whr ls --local` returns (FR-066 labels), identified by its container name or an
  unambiguous prefix of its 64-hex ID.

### Verified facts and unverified claims

Checked on 2026-10-23 against the repository at `f4c4da80` and the installed `herdr` 0.9.3 (`herdr plugin --help` and sub-help, no
herdr session was started) and https://herdr.dev/docs/plugins.

Verified:

- No `whr shell` or `whr console` exists; both are registered only as unavailable (`entries.go`). Only `apps/host_agent/internal/workspaceshell/policy.go`
  exists for the shell policy. `ls --local`, `rm --local`, `stage --local`, `promote --local` and `drift --local` are specified in FR-066 to FR-069, but
  no implementation was found in this checkout: `workspace.ls` is registered as `later`, `Needs: control_plane`, and no file under
  `apps` or `contracts` contains `localWorkspaceListResult` or `ls --local`. The 2026-10-23 spike claimed `ls --local` was
  implemented; that claim is not confirmed here. Local `shell` therefore depends on FR-066 discovery being implemented first (see
  Dependencies).
- herdr 0.9.3 has `herdr plugin {install, uninstall, link, unlink, enable, disable, list, config-dir, action, log, pane}`;
  `herdr plugin pane open` takes `--plugin`, `--entrypoint`, `--placement`, `--workspace`, `--target-pane`, `--direction`, `--cwd`,
  repeatable `--env KEY=VALUE`, `--focus` and `--no-focus`. The placements it lists are `overlay`, `split`, `tab` and `zoomed`
  (the documentation additionally lists `popup` for manifests).
- The plugin manifest is `herdr-plugin.toml` with required `id`, `name`, `version`, `min_herdr_version`, `[[panes]]` (`id`, `title`,
  `placement`, `command` as an argv array) and `[[actions]]` (`id`, `title`, `contexts`, `command`). Commands receive
  `HERDR_PLUGIN_CONTEXT_JSON`, `HERDR_PANE_ID`, `HERDR_WORKSPACE_ID` and `HERDR_PLUGIN_ROOT` among others.
- `herdr pane rename <pane> <label>` and `herdr pane report-metadata --source <id> --title <text> --display-agent <text>` exist; the latter
  is documented as display-only.
- `herdr tab` offers only `list`, `create`, `get`, `focus`, `close` and `rename` (no colour); `herdr workspace` adds only `report-metadata`;
  the plugin manifest documentation lists no style fields; the rendering of `report-metadata` tokens is undocumented.
- Owner-verified on the owner's own herdr, 2026-10-23 (not by the author of this specification): (a) `printf '\033]11;#3a0d0d\033\\'` in a
  herdr pane sets only that pane's background to dark red, not the tab and not the outer terminal; (b) on host "mini3",
  `herdr tab rename "$HERDR_TAB_ID" "🟥 HOST 3"` returned the label "🟥 HOST 3" (tab w9:tC) and it renders as intended in the tab label.
- Feature 009's host catalogue lists herdr (minimum 0.9.5 in `catalogue.json`, `assets/README.md`) as a macOS host tool. Whether a
  Linux host gets herdr from the BundleDock toolbox (`/opt/bundledock/toolbox/bin/herdr` exists in the development sandbox) is not
  established for the WorkHarbor host.

Unverified (not run, not measured):

- Pane-title rename with a glyph prefix, whether a pane title and tab label survive `clear` and a program that sets its own title, how
  emoji width and alignment behave in narrow tab labels, and the rendering over SSH from the owner's notebook terminal. These stay in
  the owner gate SC-009. Colour is never the only indicator and is never a requirement of `whr`.
- Whether a plugin pane `command` can be given per-open arguments other than through `--env`; the plan assumes env only.
- The behaviour of herdr and of `docker exec` when the SSH connection drops (panes are expected to survive because the herdr server
  and the pty live on the host; not tested).
- The spike's assessment of the third-party `gambtho/herdr-devcontainer` plugin (poor fit because it starts containers itself,
  which competes with the WorkHarbor lifecycle). It was not repeated here and no requirement relies on it.
- Whether the final WorkHarbor workspace container label name exists yet. FR-066 says only "a WorkHarbor workspace label".

### Interim shell function (not a requirement)

Until `whr shell --local` exists, the owner can use a function of the form
`hws() { docker exec -it -u <user> -w <dir> "$(docker ps -q --filter "label=<workspace label>" --filter "name=$1")" fish -l; }`
in the host shell, and open the pane in herdr with a title chosen by hand. This is an interim convenience of the owner's own host
configuration; it is no WorkHarbor deliverable and nothing in this feature depends on it.

Without the plugin, the shell prompt (if it shows `WHR_LOCAL_WORKSPACE`) is the only always-visible signal of a workspace pane; the
start notice scrolls away. The persistent pane title is the reliable indicator; without the plugin it is set by hand with
`herdr pane rename` (OQ-9).

### Boundaries

- Interactive, human-initiated use only. There is no queue, no scheduler, no unattended mode and no command argument.
- `whr` does not launch, wrap, authenticate or configure any agent tool and does not receive or relay provider credentials
  (Feature 006 FR-030). herdr is a multiplexer here, not an agent launcher. An agent that the owner starts by hand in a pane is outside
  `whr`.
- Docker is the only supported provider (tier 1). No Windows host or client. No cloud or hosted workspaces.

### Out of scope

Cloud boxes and hosted sessions; agent queues, dispatch or automation of any kind; VNC, desktop or browser streaming; a colour,
badge or theme requirement on `whr` (the optional plugin contract of FR-021 and FR-021a is the only place colour appears); a host-shell mode in `whr`; starting, stopping, creating or removing containers from `shell --local`;
starting a multiplexer in a container; a `console --local`; a plugin implementation or its packaging (only its contract is specified
as optional here); Windows; changes to the Docker VM or socket permissions; any change to remote `shell`, `console`, `ssh` or `agent`
beyond the Feature 006 reconciliation listed below.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Open a shell in a workspace from the host (Priority: P1)

On the host, the owner types `whr shell --local <ws>` and gets an interactive login shell inside that workspace container, in its
workspace directory, with a working terminal type. When the shell ends, `whr` exits with the shell's status.

**Why this priority**: it replaces the hand-typed `docker exec` and is the smallest piece that delivers the owner's use case.
**Independent Test**: with a fake Docker daemon and a fake `docker` executable on a test `PATH`, drive the command through the PTY test
harness and assert the exact argument vectors, the exit codes and the refusals. A real-host run is a separate owner gate.

**Acceptance Scenarios**:

1. **Given** one running labelled workspace container `ws-a` with remote user `captain` and workspace folder `/workspaces/a`,
   **When** `whr shell --local ws-a` runs on a terminal, **Then** `whr` runs one read-only `docker inspect` and exactly one
   `docker exec`, with the byte-exact vectors of FR-004, and no shell string is evaluated.
2. **Given** that shell, **When** the owner runs `exit 7`, **Then** `whr` exits 7 and writes no `whr:` line; **When** the shell is
   killed by a signal, **Then** `whr` exits with the status Docker reports (128 plus the signal number).
3. **Given** `ws-a` is stopped, **When** the command runs, **Then** it exits 68 (conflict) naming the state, does not start the
   container and runs no `docker exec`.
4. **Given** no matching container, an unlabelled foreign container, or a name that matches a container lacking a recognised label,
   **When** the command runs, **Then** it exits 65 (not found) and runs no `docker exec`.
5. **Given** a selector that matches two containers, **When** the command runs, **Then** it exits 64, lists the candidates (name and
   12-character ID prefix, escaped per FR-022) and runs no `docker exec`.
6. **Given** standard input or output is not a terminal, or `--json` is given, or a trailing command is given, **When** the command
   runs, **Then** it exits 64 before contacting Docker; no automation path exists.
7. **Given** a Docker socket that is unreachable or a daemon that does not answer within the 5-second budget of FR-066, **When**
   the command runs, **Then** it exits 68 with the doctor remediation link, and never falls back to the control plane.
8. **Given** a discovered endpoint that is not `unix://` (for example `DOCKER_HOST=tcp://...` or `ssh://...`), **When** the command runs, **Then** it exits 68 (ADR 0013 conflict, as FR-063 uses for an unsupported
   platform), runs no `docker inspect` and no `docker exec`, and names the endpoint scheme only.
9. **Given** a running shell, **When** the owner presses ctrl-p, **Then** the shell receives it (Docker's default detach sequence
   ctrl-p ctrl-q does not apply); only the fixed sequence of FR-004 detaches the client, the start notice names that sequence, and
   `--help` states that a detached shell keeps running in the container. The key behaviour on a real Docker host is an owner gate
   (SC-009) and unverified until run.
10. **Given** a container whose labels yield a user or directory that fails the rules of FR-005, **When** the command runs, **Then**
   it exits 68, names which value was refused (escaped per FR-022) and runs no `docker exec`.

11. **Given** a running shell, **When** the Docker daemon disappears so that the client exits with 255 or 1 and the liveness inspect cannot
   connect, **Then** `whr` exits 72 with a `whr:` line; **When** the daemon is reachable and the shell itself exits 255, **Then** `whr`
   exits 255 without a `whr:` line.
12. **Given** a container whose configured shell executable does not exist, **When** the command runs, **Then** `docker exec` reports its
   own error and `whr` passes its status (126 or 127) through after the hand-over; `whr` substitutes no other shell and writes no
   `whr:` line (OQ-6).
13. **Given** a pane that was open when the SSH connection dropped, **When** the owner reconnects and reattaches to the herdr session,
   **Then** the same shell is still there and `whr` has no part in the reattachment; closing the pane instead does not promise that
   the container process ends (FR-007).

---

### User Story 2 - Tell host panes and workspace panes apart (Priority: P1)

In the one host session the owner can tell which panes are host shells and which are inside which workspace.

**Why this priority**: the use case names it: two clearly distinguishable kinds.
**Scope**: three signals exist. The stderr notice line and the `WHR_LOCAL_WORKSPACE` variable are delivered by `whr` and can be
scrolled away or ignored by the prompt. The persistent pane title (`ws:<name>` or `HOST`, set with `herdr pane rename` or
`report-metadata`) survives screen clears and is the documented reliable indicator; the optional plugin sets it automatically (Feature
010 FR-018). Without the plugin, identity relies on setting the title by hand or on prompt setup in the owner's shell; prompt markers are a
BundleDock shell-policy matter and not part of this feature (OQ-9). The plugin's emoji marker in the title and tab label is the default identity (FR-021); background colour is an optional extra (FR-021a). `whr` emits neither.
**Independent Test**: PTY test of the local-mode notice and environment; a manual walkthrough with the optional plugin for the title.

**Acceptance Scenarios**:

1. **Given** a successful start, **When** the shell is attached, **Then** the container process environment contains
   `WHR_LOCAL_WORKSPACE=<container name>` and `WHR_LOCAL_MODE=1`, and nothing else from the host environment except `TERM`.
2. **Given** a terminal, **When** the command starts, **Then** it first writes one line to standard error, `whr: local shell in
   <container name> (no grant; host Docker access; detach keys ctrl-] ctrl-^)`, with the name escaped per FR-022, and then writes
   nothing of its own to the terminal stream (the fallback warning of scenario 5 comes before the hand-over, never after it).
3. **Given** the optional plugin of User Story 3, **When** a workspace pane is opened, **Then** its title is `<glyph> ws:<name>` and a plain host
   pane is titled `🟥 HOST` (FR-021); the titles do not depend on background colour.
4. **Given** no plugin, **When** the owner renames panes by hand per the documentation (without the plugin identity relies on that
   manual title or on prompt setup), **Then** the variable and the notice still identify the workspace.
5. **Given** no mount destination matches the host `workspace_folder` (FR-005), **When** the command starts, **Then** `whr` writes one
   additional warning line to standard error after the notice and before the hand-over, also when standard error is redirected, naming the fallback reason
   and the container's configured working directory used as `<dir>` (both escaped per FR-022); with a matching mount no warning is written.

---

### User Story 3 - Open workspace panes from a picker (Priority: P3, optional)

A small herdr plugin offers an overlay that lists the owner's running local workspaces and opens the chosen one in a new pane.

**Why this priority**: convenience only; User Story 1 is complete without it.
**Independent Test**: a manifest and script review plus a PTY test of the script against a fake `whr`; running herdr is not required
by this specification.

**Acceptance Scenarios**:

1. **Given** the plugin is installed and workspaces are running, **When** the owner invokes its picker action, **Then** an overlay pane
   lists the names of the workspaces whose `state` is `running` in `whr ls --local --json`, and nothing else (FR-018, OQ-12).
2. **Given** a choice, **When** the owner confirms, **Then** the plugin opens one pane (`herdr plugin pane open ... --env
   WHR_PANE_WORKSPACE=<name>`) whose process is `whr shell --local <name>` and sets the title `ws:<name>`.
2a. **Given** a result whose `state` is not `running` (stopped, paused, exited or any other value), **When** the picker builds its
   list, **Then** that workspace is not offered; the picker reads no field other than `name` and `state` and shows the name only
   (OQ-12).
3. **Given** a name that does not match the workspace name rule of Feature 006 FR-058 (`^[a-z0-9]$|^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`, lowercase only), **When** it appears in the list, **Then** the plugin does not
   offer it.
4. **Given** the plugin is absent, disabled or herdr is not installed, **When** the owner uses `whr shell --local`, **Then** nothing
   changes.

5. **Given** the plugin opens a workspace pane, **When** the pane starts, **Then** its tab label carries the same marker text
   (`herdr tab rename`), and clearing the screen leaves title and label unchanged (SC-009).
6. **Given** the optional colour feature (FR-021a) is enabled, **When** the pane command starts, **Then** it emits one OSC 11 sequence
   with the pane's tint before it `exec`s `whr shell --local <name>`, and `whr` itself emits no escape sequence.

---

### User Story 4 - Keep the persistent console consistent (Priority: P2)

The owner who uses `whr console <ws>` (once the owner API exists) gets a plain persistent console by default, and never a
multiplexer nested in their host pane.

**Why this priority**: it removes the conflict between FR-048 and the host-session model.
**Independent Test**: manifest default vectors and a wizard transcript, after the follow-up work items.

**Acceptance Scenarios**:

1. **Given** a manifest without `console_mode`, **When** a console session is created, **Then** it runs in `plain` mode.
2. **Given** `console_mode: herdr`, **When** the owner selects it, **Then** it is honoured unchanged under FR-048's other rules, `whr`
   emits no nesting advice, and the host herdr is unaffected.
3. **Given** `whr console <ws> --local`, **When** it runs, **Then** it exits 64 and names the host session and `whr shell --local`.
4. **Given** `whr shell --local`, **When** it runs, **Then** it never starts, attaches to or configures a console, and the in-container
   console defaults play no role.

## Edge Cases

- Several workspaces whose names share a prefix: an exact container name wins; otherwise a unique ID prefix of at least 12 hex
  characters; otherwise a unique name prefix is not accepted (FR-003).
- A container that is running but whose shell executable is missing: `docker exec` fails with its own error and status (126 or
  127), which pass through (FR-008); `whr` does not substitute another shell and cannot tell this from a shell exit (OQ-6).
- A hostile container name or label (control characters, bidirectional overrides): escaped in every `whr` message (FR-022); the
  selector is never passed through a shell.
- A container that is paused or restarting: treated as not running, exit 68.
- The host session or SSH link drops while a workspace pane is open: the herdr server, not `whr`, keeps the pane (unverified, SC-009);
  the shell survives and is reattached with herdr. If `whr` itself is killed, the exec process may keep running in the container
  (FR-007). The Docker daemon vanishing mid-session is a transport loss, exit 72 (FR-008). Exit 72 before the hand-over is also used
  for SIGINT or SIGTERM during discovery.
- A program inside the pane renames the pane, sets a title or changes the background: the marker and tint are overwritten; this is
  accepted (cosmetic, not a security boundary).
- The terminal lacks emoji support or the font draws them wide: the label may misalign or show boxes; the text `HOST` and `ws:<name>`
  remain readable.
- The terminal is resized: `docker exec -t` carries the new size; `whr` adds no handling.
- Two panes open the same workspace: each is a separate fresh login shell (FR-007 semantics); there is no shared state.
- A workspace that is rebuilt or stopped while a pane is open: the shell ends with the container; its status passes through.
- `docker` executable absent from `PATH`: exit 68 with remediation naming the container runtime page.
- A user without access to the Docker socket: exit 68 naming the socket and the doctor check; no `sudo` is run.
- The control plane exists and is reachable: `--local` still never contacts it.

## Requirements *(mandatory)*

### Functional Requirements

**Local shell mode**

- **FR-001**: `whr workspace shell --local <ws>` (short form `whr shell --local <ws>`) MUST open an interactive login shell inside the
  workspace container selected by FR-003. It MUST NOT contact the control plane, require login or a grant, or read or write
  credentials, and it MUST NOT be selected implicitly: without `--local`, `shell` keeps the Feature 006 behaviour (exit 64 until the
  owner API exists). `--local` MUST NOT be accepted by `console`, `ssh` or `agent attach`, except that `console --local` is refused as
  FR-011 states.
- **FR-002**: Local mode MUST run only on the Docker host and MUST use the local daemon found by the socket discovery rules of
  `whr doctor` (FR-060), within the 5-second budget and fixed-argument rule of FR-066. The discovered endpoint MUST be a `unix://`
  socket; any other scheme (`tcp://`, `ssh://`, `npipe://`) is refused with exit 68 before any
  `docker` call (ADR 0013: conflict or wrong state; FR-063 uses 68 for an unsupported platform). The endpoint MUST be passed
  explicitly as `--host=<endpoint>` to every `docker` call, so discovery, inspect and exec use the same daemon. FR-060 discovery reads only `DOCKER_HOST` and the documented socket list, never a
  docker context, and the explicit `--host` makes contexts irrelevant. No remote network endpoint is contacted.
- **FR-003**: The selector `<ws>` MUST be resolved in this order. (1) Among all containers returned by FR-066 discovery, whatever their state,
  match by full container name or by a unique prefix of at least 12 hex characters of the ID; zero matches, or a name that matches only
  a container without a recognised label, exit 65; several matches exit 64 and list the candidates. (2) Only then check the state of the
  single match: a match that is not `running` (stopped, paused, restarting) exits 68 naming the state. `shell --local` MUST NOT
  start, stop, create, remove or modify any container, network or volume.
- **FR-004**: Local mode invokes the Docker command line client twice before the hand-over and at most once after it, each with a fixed argument vector (no shell, no string
  evaluation) built only from validated values. The read-only inspect, counted separately from the exec, is:
  `docker --host=<endpoint> container inspect --format {{json .}} <id>` (the format is one argument; `<id>` is the full 64-hex ID).
  After the hand-over, only if the client status is non-zero, one liveness inspect `docker --host=<endpoint> container inspect
  --format {{.State.Running}} <id>` within a 5-second budget (expiry exits 72, FR-008). The exec is exactly one invocation of:
  `docker --host=<endpoint> exec --interactive --tty --detach-keys=ctrl-],ctrl-^ --user=<user> --workdir=<dir> --env=TERM=<term>
  --env=WHR_LOCAL_WORKSPACE=<name> --env=WHR_LOCAL_MODE=1 <id> <shell> -l`. The container is addressed by its full ID, never by name.
  `--detach-keys` replaces Docker's default detach sequence (ctrl-p ctrl-q), which would otherwise swallow ctrl-p in readline, fish
  and vim. `ctrl-],ctrl-^` avoids ctrl-@ (ctrl-space, NUL), which readline, tmux and Emacs use. The docs.docker.com pages for `docker attach`
  and `docker exec` give the form `ctrl-<char>` but their list of characters (`a-z`, `@`, `[`, `\`, `_`, `^`) does not show `]`;
  `ctrl-]` (byte 29) is accepted by `ToBytes` in moby/term `ascii.go`, read on 2026-10-23 through a page summary, not by running
  it. That is the only verification: the
  `docker` CLI is not installed in the specification sandbox, no run was made, and the documentation does not say whether an empty
  sequence disables detaching, which is why a rarely typed two-key sequence is fixed instead. A detached client leaves the shell
  running in the container; the start notice names the sequence (FR-010) to keep that visible.
- **FR-005**: Values for the exec vector. `<name>` MUST be the container name and match `[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}`.
  `<user>` MUST be, in order, the `remoteUser` of the `devcontainer.metadata` label, the container's configured user, and
  otherwise `captain`; it MUST match `[a-z_][a-z0-9_-]{0,31}` or be a numeric uid with an optional `:gid`. `<dir>` MUST be the
  in-container path of the workspace folder (the mount destination matching the host `workspace_folder` of FR-066); when none is
  determinable it is the container's configured working directory and a warning names the fallback. `<dir>` MUST be an absolute path
  of at most 4096 bytes without NUL or newline. `<term>` MUST be the host `TERM` when it matches `[A-Za-z0-9._+-]{1,64}`, otherwise
  `xterm-256color`. `<shell>` MUST be the `human_shell` of the manifest `workharbor.json`, read from the host `workspace_folder`
  of FR-066 without following links (at most 64 KiB), and `fish` when absent (Feature 006 FR-047); there is no `--shell` flag. A
  value that fails validation exits 68 naming the value (escaped per FR-022); none is repaired or substituted. There is no
  pre-launch check of the shell executable (see Edge Cases and OQ-6).
- **FR-006**: `shell --local` MUST take no trailing command and no further arguments, MUST require standard input and output to be
  terminals, and MUST reject `--json` for local mode (unsupported, 64; the rest of the `workspace.shell` entry keeps its `events`
  JSON mode). It MUST forward no host environment variable to the container other than `TERM`, and add only
  `WHR_LOCAL_WORKSPACE=<name>` and `WHR_LOCAL_MODE=1`, and MUST NOT pass a credential, token or provider setting in the argument vector. It MUST NOT install, configure,
  authenticate, launch or wrap an agent tool, nor start herdr or any multiplexer in the container (Feature 006 FR-030).
- **FR-007**: The shell MUST be fresh (Feature 006 FR-007) and ends when its exec session ends normally, that is when the shell exits.
  Accepted lifetime, stated plainly: closing the pane or an abnormal loss of the host-side connection does not guarantee that the
  process in the container ends. The Docker daemon starts the exec with a background context and does not kill it when the client
  stream closes (read in moby `daemon/server/router/container/exec.go`, 2026-10-23, via a page summary; not run). `whr` does not
  track, reap or clean up such a process; managed cleanup is out of scope and a possible follow-up. The intended notebook case needs
  none: the herdr server on the host keeps its panes alive across a lost SSH connection (unverified, SC-009), so the same shell is
  reattached through herdr, not through `whr`. The only intended client-side exception is the fixed detach sequence of FR-004, which the
  start notice and `--help` name; no other key sequence detaches the client.
- **FR-008**: `whr` MUST exit with the status of the container process, 0 to 255, unchanged (Feature 006 FR-019). Failures before the
  hand-over use the ADR 0013 codes in FR-009 and always write a `whr:` line. The `whr:` line is not written for a remote status.
  Transport loss: the local daemon can vanish while the shell runs. The `docker` client then exits non-zero (by the `docker/cli`
  `getExecExitStatus`, read 2026-10-23 via a page summary: a lost connection while reading the exec status yields status -1, which
  an operating system shows as 255, and a stream error is returned and exits 1; neither was run). Because that cannot be told from a
  shell status, after any non-zero client status `whr` runs one read-only liveness inspect (FR-004). If it cannot connect to the daemon or does not answer within 5 seconds,
  `whr` exits 72 (interrupted, ADR 0013: transport lost during an interactive command; this is the local counterpart of Feature 006
  FR-019's transport-loss rule) and writes a `whr:` line; otherwise it passes the client status through. A status of 255 with a
  reachable daemon is passed through as a shell status.
- **FR-009**: Exit codes before the hand-over (72 also after it, for the transport loss of FR-008): 0 never; 64 for bad usage, multiple matches, non-terminal input or output, `--json`, a
  trailing command or a refused flag; 65 for no matching labelled container; 68 for a stopped or non-running container, an unreachable
  socket, a non-`unix://` endpoint, an absent client, a value that fails FR-005 validation, or a failed inspect; 72 for SIGINT or SIGTERM before the hand-over and for a lost daemon after it; 74 for an
  unclassified failure. No new code is introduced.

**Distinguishing panes**

- **FR-010**: Local mode MUST write the notice (one line) to standard error before the hand-over, `whr: local shell in <name> (no grant; host
  Docker access; detach keys ctrl-] ctrl-^)`, with the name escaped per FR-022, only when standard error is a terminal; the notice text is
  byte-exact. Only when the configured-working-directory fallback of FR-005 is used, `whr` MUST write one additional warning line to standard
  error after the notice and before the hand-over, also when standard error is redirected: `whr: warning: no workspace folder mount in <name>;
  starting in <dir>`, with name and directory (the fallback reason and directory) escaped per FR-022 (OQ-11). After the hand-over `whr` MUST write
  nothing to the terminal stream. The container environment carries `WHR_LOCAL_WORKSPACE=<name>` and `WHR_LOCAL_MODE=1` (FR-004) so
  a prompt can show them. The notice also names the detach sequence of FR-004. Colour MUST NOT be required anywhere.

**Console relation**

- **FR-011**: `whr console <ws> --local` MUST be refused with exit 64 and a message that names `whr shell --local <ws>` and the host
  multiplexer session. `whr console <ws>` stays the remote, grant-based persistent console of ADR 0012 and FR-007a, unchanged except
  for the default backend of FR-012.
- **FR-012**: The default console mode is `plain`. herdr in the container is optional, selectable by `console_mode: herdr`, never
  required for any `whr` command, and `whr` MUST NOT give nesting guidance, detect or avoid nested multiplexers, or start herdr
  because a host multiplexer exists. Feature 006 FR-048 and the related text are amended by the reconciliation below.

**Trust boundary and output**

- **FR-013**: Local mode relaxes Feature 006 FR-006 as follows, and only for `shell --local`: no single-use, expiring grant is required
  and no control-plane authorization is evaluated, because the caller can already run `docker exec` on this host (a member of the
  docker group or the owner of a rootless, Docker Desktop or OrbStack daemon), which is the same capability. The reasoning is limited to
  the single-owner, own-host model: `whr` adds no privilege, runs no `sudo`, grants no access to a container, the control plane
  or the browser, exposes the socket to nothing (constitution IV), and the relaxation does not change a remote command. The
  recognised-label rule of FR-066 is a usability guard that keeps `whr` from offering unrelated containers; it is not a security
  boundary: labels can be set by anyone who can create a container, and the holder of the Docker socket is trusted.
- **FR-014**: Output filtering. Feature 006 FR-022a does not apply to the terminal stream of `shell --local`, because `whr` does not relay
  it: the child process owns the owner's terminal directly, exactly as a host shell does. `whr` does not inspect, buffer or filter that
  stream, and this is stated in `--help`. FR-022 still applies to every line that `whr` itself writes (the notice, candidate lists,
  errors), which contain untrusted container names. This matches Feature 006 FR-008a, where a stream `whr` does not relay is out of
  FR-022a's reach.
- **FR-015**: `whr` MUST NOT record, log or persist the content of the session, the environment or the container selector beyond the
  `whr:` lines on standard error. No audit record is written, because `shell --local` mutates nothing.

**Host session**

- **FR-016**: WorkHarbor MUST NOT depend on a multiplexer. Every `whr` command works without herdr. `whr` MUST NOT start, stop,
  configure, authenticate or script the host herdr or any pane, and `whr shell --local` MUST NOT start herdr anywhere. An explicit
  `console_mode: herdr` stays supported under Feature 006 FR-048.
- **FR-017**: Host panes are ordinary host shells opened by the multiplexer. `whr` MUST NOT offer a host-shell mode, a `--host` option or any
  way to run a command on the host through a workspace.

**Optional plugin (specification of the contract only)**

- **FR-018**: An optional herdr plugin MAY be provided later through a separate reviewed change; it is not part of the product
  dependencies, must not be required by any check of this repository and must not modify `whr`. Its contract: a pane entry for the
  picker (placement `overlay`) that runs `whr ls --local --json` and reads only the `name` and `state` of each result (`state` solely to
  decide eligibility: only a record whose `state` is exactly `running` is offered; the picker shows the name only, adds no CLI filter and
  `whr shell --local` still refuses a stale or non-running selection (OQ-12)); an entry that opens a
  `tab` or `split` pane running exactly `whr shell --local <name>`; the name reaches the process only through `--env` or the pane
  command argv and is validated before use against the workspace name rule of Feature 006 FR-058 (`^[a-z0-9]$|^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`); uppercase names and others are intentionally not offered; the pane title and tab label are set per FR-021;
  it launches no agent tool and holds no credential.
- **FR-019**: The persistent pane title, with the glyph marker of FR-021, is the documented reliable indicator of the pane kind (`HOST` or `ws:<name>`). Host pane titles (`HOST`) are set by the owner's herdr configuration or a plugin action, never by `whr`.

### Reconciliation with Feature 006 (amendments applied in the same change)

This feature amends Feature 006 as follows. The same sentences are applied to
[`specs/006-whr-cli/spec.md`](../006-whr-cli/spec.md) in the commit that follows this one; nothing else in 006 changes.

| Item | Amendment |
| --- | --- |
| FR-006 | adds: except `shell --local` of Feature 010 FR-013 |
| FR-007 | adds the local alternative of FR-001 |
| FR-007a | adds: the console has no local mode (Feature 010 FR-011) |
| FR-019 | adds `shell --local` with exit-code passthrough |
| FR-022a | adds: not applied to `shell --local` (Feature 010 FR-014) |
| FR-048 | default console mode plain; herdr optional, never required; no nesting guidance; host herdr is the multiplexer |
| FR-058, US0 scenario 1 | default `console_mode` is `plain` (schema, vectors and code follow, FR-020) |
| Context paragraph "Local workspace initialization" | wording of the owner's request, now "fish default and a console mode choice" |
| Manifest settings text (FR-058) | `human_shell` is also read by `shell --local` (FR-005) |
| US0 scenario 7 | worded for the console mode in general |
| FR-066 | notes that `shell --local` reuses the discovery |
| SC-015, Assumptions | worded for the plain default |

- **FR-020**: The following artifacts MUST be reconciled by separate owner-gated work items, not by this specification: the manifest
  schema default and vectors (`contracts/workspace/workharbor.schema.json`, `vectors.json`), `contracts/cli/commands.json` and its Go mirror
  `apps/whr/internal/commands/entries.go` (adding the `--local` flag of `workspace.shell` with `--json` refused for local mode only, with the freeze test of FR-051 and FR-052),
  `apps/whr/internal/scaffold/proposal.go` and `apps/whr/internal/wizard/wizard.go` (default `plain`), `contracts/cli/output.schema.json`
  (the `console_mode` default text) and `docs/product.md`/`docs/architecture.md` if they describe the console. Until then those
  artifacts and this text disagree, and the disagreement is recorded here.
  Implementation evidence for FR-020: the separate work items landed in owner-merged PRs
  [#220](https://github.com/wstein/workharbor/pull/220), [#221](https://github.com/wstein/workharbor/pull/221) and
  [#222](https://github.com/wstein/workharbor/pull/222). The recorded configuration/command disagreement is resolved;
  remote console runtime acceptance remains pending its consumer. This note changes no requirement or settled decision.
- **FR-021**: Default identity marker of the plugin. The plugin MUST prefix a fixed glyph on both the pane title (`herdr pane rename` or
  `report-metadata --title`) and the tab label (`herdr tab rename`): host `🟥 HOST`; workspace `<glyph> ws:<name>`. The glyph is chosen from the
  fixed set `🔷` (blue diamond), `🟠` (orange circle), `🟣` (purple circle), `🟡` (yellow circle), `⬜` (white square) by index = FNV-1a 32-bit hash
  of the workspace name bytes modulo 5, in that order; the set does not contain the host glyph. The identity cue for every viewer, including those with colour-vision deficiency, is the
  text in the title and label (`HOST`, `ws:<name>`); the glyph is a secondary cue and makes no accessibility claim. Glyphs survive
  `clear`, need no OSC support and add a cue that is not a background. Limits: emoji rendering depends on terminal and font, width and
  alignment in tab labels are unverified, and the marker is cosmetic, not a security boundary: container output can set its own pane's title or background through OSC 2 and OSC 11,
  which FR-014 does not filter, and a program in the pane can rename its own pane. The tab rename and display were owner-verified (see facts); the rest is the owner gate SC-009.
- **FR-021a**: Optional pane background (P3, on top of FR-021). The plugin's pane command MAY emit one OSC 11 background sequence for its
  own pane (`printf '\033]11;<hex>\033\\'`) before it `exec`s `whr shell --local <name>` or the host shell. `whr` emits nothing; Feature 006
  FR-022a is untouched. Host tint `#3a0d0d` (owner-verified, see facts). Workspace tint from the fixed palette, same index as FR-021:
  `#14204c`, `#483024`, `#180428`, `#383000`, `#184c44` (in the order of the glyph set). Each tint MUST reach a contrast ratio of at least 4.5 against
  the reference foreground `#c0c0c0`, and every pair of tints (host included) MUST differ by CIEDE2000 of at least 8 under normal vision
  and at least 6 under each of simulated protanopia, deuteranopia and tritanopia (Machado 2009, severity 1, applied in linear sRGB; an
  approximation, not a clinical statement). Measured by the author with a throwaway script outside the repository, 2026-10-23: lowest
  contrast 5.4 (`#184c44`); lowest pairwise CIEDE2000 10.5 normal, 8.2 protanopia, 7.3 deuteranopia, 7.0 tritanopia. A draft palette of
  `#0d2238 #3a2a0d #2a0d3a #34370d #262626` failed (for example 2.4 under deuteranopia) and was replaced. The palette holds five tints; a
  workspace beyond five shares a tint with another by the hash, and its title text stays unique. These constraints are re-checked by a
  unit-testable script in the plugin follow-up (SC-010); the measurements above are not an acceptance result.   Colour is never the only indicator. A full-screen program may overpaint
  the background or change it; no reset is required because the pane closes with its shell. Herdr 0.9.3 has no tab or workspace colour;
  the only tab-level cue is the label of FR-021. If another tool also sets pane colours (see `docs/references.md`, herdr-colors), the
  later writer wins; coexistence is not designed here.

### Key Entities

- **Local workspace match**: the result of FR-003: container ID, name, role, state, workspace folder, remote user and shell, all from
  FR-066 discovery or the fixed preflight.
- **Exec plan**: the fixed argument vector of FR-004, built only from the match and validated values; it holds no secret.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With a fake `docker` and fake daemon, a table of scenarios (running, stopped, paused, unlabelled, ambiguous, absent,
  unreachable socket, non-`unix://` endpoint) yields the exit codes of FR-009 and no `docker exec` in every refusal. A missing shell is not
  a refusal: its 126 or 127 passes through (SC-003).
- **SC-002**: The argument vectors (inspect, exec and the post-exec liveness inspect with its 5-second budget) for a fixed container, user, directory, `TERM` and shell equal the vectors of FR-004 byte for byte;
  property tests over hostile names, `TERM` values and directories show no argument injection and no shell evaluation.
- **SC-003**: Exit statuses 0, 1, 7, 126, 127 and 137 of the fake child pass through unchanged and without a `whr:` line.
- **SC-004**: With no terminal, `--json`, a trailing command, or `console --local`, `whr` exits 64 and starts nothing.
- **SC-005**: The environment of the fake child contains `TERM`, `WHR_LOCAL_WORKSPACE` and `WHR_LOCAL_MODE` and no other host variable; a
  planted `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` and `GITHUB_TOKEN` never reach the child, the argument vector or output.
- **SC-006**: A hostile container name appears in every `whr` message with control, bidirectional and C1 characters replaced
  (FR-022), and the terminal stream of the child is byte-identical to what the child wrote (FR-014).
- **SC-007**: The manifest default and wizard default are `plain`, `console_mode: herdr` still validates, and `whr shell --local` starts no herdr (an explicit `console_mode: herdr` stays supported under Feature 006 FR-048), after the follow-up work items of FR-020. Verification of a real host pane (SSH, herdr, Docker) is a separate owner
  gate and is labelled unverified until run.
- **SC-008**: The documentation names the interim shell function as interim and non-normative and never claims that `whr` itself has a colour feature (the optional plugin may).
- **SC-009**: Owner gate on a real Linux or macOS Docker host: each of ctrl-p, ctrl-q, ctrl-space, ctrl-\ and ctrl-] alone reaches the shell and is not swallowed, `ctrl-] ctrl-^` detaches the client and the shell
  stays running, the exit status passes through; and the owner disconnects the SSH session, reconnects, reattaches to the herdr session and verifies
  that the shell state (a running command, the working directory) and the pane title are intact; and, with the plugin: only the opened pane takes the background (not the other panes, the tab or the outer terminal), the pane title and tab label carry the glyph and survive `clear`, and the emoji render acceptably in the owner's notebook terminal over SSH, including narrow tab labels; and all are recorded with Docker and herdr versions. Unverified until run.
- **SC-010**: Offline, a script in the plugin follow-up proves that the hash maps a fixed table of workspace names to the same glyph and
  tint on every run, that no workspace glyph equals the host glyph, that every tint meets the contrast and CIEDE2000 constraints of
  FR-021a, and that the pane command writes exactly one OSC 11 sequence before `exec`. Not claimed verified by this specification.
- **SC-011**: With a fake `docker` whose inspect shows no mount destination for the host `workspace_folder`, `whr` writes the one fallback
  warning line `whr: warning: no workspace folder mount in <name>; starting in <dir>` (escaped) to standard error after the notice and before the child starts and uses the configured working directory as `<dir>`;
  with a matching mount no warning is written; with standard error redirected to a file the warning is still written and the notice is not;
  nothing is written after the hand-over; a fallback directory that fails FR-005 validation (absolute, at most 4096 bytes, no NUL or newline)
  exits 68 naming the value (escaped), starts no exec and writes no warning line.

## Dependencies

- FR-066 discovery (label rules, 5-second budget, socket discovery of FR-060) must exist before `shell --local`; the discovery code
  is shared, not copied. Feature 006's wave order and the owner's task approval govern when this is built; this text grants no
  implementation or build authority.

## Assumptions

- The owner is the only user of the host, and a member of the group that may use the Docker socket.
- The Docker command line client is installed on the host; local mode uses it instead of the Engine API (OQ-1, foreman decision).
- The workspace container carries FR-066 labels, and the in-container path of the workspace folder is derivable from them.
- Herdr runs on the host with its own server socket under the owner's account; `whr` never touches it.
- The control plane and owner API may or may not exist; local mode is independent of both.

## Decisions

Owner decisions of 2026-10-23 on the former open questions, ids kept; no rule changes except OQ-11 and OQ-12. OQ-1 and OQ-4 are foreman decisions; OQ-3 is withdrawn.

- **OQ-1** (foreman decision): the `docker` command line client with the fixed argument vectors of FR-004; no Engine API client.
- **OQ-2** (owner-decided 2026-10-23: no): `<ws>` matches only the container name or ID, not the workspace name from `workharbor.json` or the devcontainer name, until the final WorkHarbor workspace label exists; the spike did not verify it.
- **OQ-4** (foreman decision): keep `WHR_LOCAL_WORKSPACE` and `WHR_LOCAL_MODE` in the container environment; they are the only variables `whr` adds.
- **OQ-5** (owner-decided 2026-10-23: `integrations/herdr/`): the optional plugin lives in `integrations/herdr/` in this repository, as a separate spec task, never part of `make check`.
- **OQ-6** (owner-decided 2026-10-23: yes): a missing shell executable shows as Docker's own error (126 or 127) instead of a `whr` refusal with remediation. This relaxes Feature 006 FR-047 ("refuse with remediation"; never substitute a shell, which still holds), because the only pre-launch check would be a second `docker exec`, which FR-004 forbids.
- **OQ-7** (owner-decided 2026-10-23: yes): the manifest default `plain` also applies to existing manifests that omit the field, because
  the field's default is read at session creation; existing consoles keep their old state (FR-047, FR-007a).
- **OQ-8** (owner-decided 2026-10-23: no): the same local mode does not cover `agent attach`; FR-029 requires the session contract and
  grants.
- **OQ-9** (owner-decided 2026-10-23: yes): the persistent pane title plus tab label, each with the emoji marker of FR-021, is the indicator of the pane kind, with prompt markers
  left to the BundleDock shell policy. The plugin sets title and label with the marker, the owner sets them by hand
  without the plugin, and `WHR_LOCAL_WORKSPACE` stays available to a prompt.
- **OQ-10** (owner-decided 2026-10-23: yes): the glyph set of FR-021 and the tint palette of FR-021a are accepted with their measured limits (lowest pairwise CIEDE2000 10.5 normal, 8.2
  protanopia, 7.3 deuteranopia, 7.0 tritanopia; five tints; marker default, background optional P3). The palette is changed only if the SC-010 script fails.
- **OQ-11** (owner-decided 2026-10-23: fallback with warning): when no workspace-folder mount destination is determinable, `<dir>` is the container's
  configured working directory and `whr` writes one warning line naming the fallback (FR-005, FR-010), instead of a silent fallback or exit 68. This amends
  the earlier "exactly one line" rule of FR-010: the notice stays one byte-exact, terminal-only line, and the warning is written also when standard error is
  redirected, before the hand-over only.
- **OQ-12** (owner-decided 2026-10-23: option A): the picker of US3 reads `name` and the existing public `state` of `whr ls --local --json`,
  `state` solely for eligibility, and offers only workspaces whose `state` is `running`. This widens FR-018's "only `name`" by one field and
  adds no CLI filter; `shell --local` still refuses a stale or non-running selection (exit 68). Considered and not chosen: (B) a
  `--state running` flag on `ls --local` (changes the FR-066 interface and reopens P1); (C) list all and let `shell --local` refuse
  (offers stopped workspaces, opens a failing pane).
