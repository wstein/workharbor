# Feature Specification: Host setup wizard and doctor repair

**Feature identifier**: `009-host-setup-wizard`
**Feature Branch**: `docs/spec009-host-setup-wizard`
**Created**: 2026-10-20
**Status**: Source implementation is authorized under owner H37 (2026-10-20), using bounded manual per-work-item scope/evidence and independent exact-head review. H38 confirms DP-1 to DP-5 and confirmed no-op protocol bookkeeping. This is not full-feature acceptance, task completion, AP approval, live-operation or build authority. The H35 loader/proposal slice has its own earlier scoped CLEAR; later changes need their own review. Owner requirements and decisions of 2026-10-20 are recorded as D-1 to D-9; OD-1 to OD-5 are confirmed
(2026-10-20, OD-2 amended).
**Related**: [Feature 006](../006-whr-cli/spec.md) (FR-009, FR-026, FR-043 to FR-046, FR-060, FR-061, FR-063, T015, T027),
[Feature 008](../008-workspace-sandbox/spec.md) (FR-019 to FR-022, FR-026, OD-5),
[ADR 0013](../../docs/adr/0013-whr-exit-codes.md), [ADR 0014](../../docs/adr/0014-whr-command-structure.md),
[container runtime page](../../docs/container-runtime.md), archived requirement R-40
(`docs/archive/workharbor-old/docs/content/docs/requirements.md`, line 41).

## Context and scope

The owner works on a Mac. Feature 006 gives `whr setup` only as Linux/systemd host enrollment (FR-009, T015) and `whr doctor` only as a
read-only diagnosis (FR-060); on macOS nothing prepares the host. The owner wants the Mac host and the sandbox to give the same user
experience. This feature defines `whr setup` on macOS as a guided wizard, `whr doctor --fix` as its repair counterpart on the same engine,
one small curated host catalogue owned by WorkHarbor, a dotfile preview/backup engine, the guided move of the global git configuration,
and the Feature 006 amendment proposals this needs.

**Threat model (owner decision D-8)**: `whr setup` and `whr doctor --fix` run on the owner's Mac, with no agent access, from a catalogue
shipped by the project, and the owner confirms each step. The design defends against mistakes and surprises, not against attackers.

### Terms

- **Mac host**: the owner's macOS machine; the domain of `whr setup` and `whr doctor --fix`.
- **Docker VM**: the Linux VM of the container runtime (OrbStack, or the Lima VM that Colima manages). `whr` never changes it.
- **Selected endpoint**: the one `unix://` socket address that `whr` uses to reach the container runtime, chosen by the rule of FR-029
  (D-12). Where this and the other features say "discovered" socket or endpoint, they mean the selected endpoint.
- **Item**: one catalogue entry: a tool, a container runtime or a dotfile. The reserved profile item `git-config` stands for the git move.
- **Create class**: adds something absent (install a tool, create a file). **Modify class**: changes something present (upgrade, replace a
  file, move the git configuration, run a privileged command).
- **Host profile**: the record of the owner's selection, including opted-out items ([data model](data-model.md)).
- **Real run**: a run of `setup` or `doctor --fix` that is not `--dry-run`, from plan confirmation on.

### Boundaries

- `whr` never changes the Docker VM (no daemon or VM settings, kernel, VM-level mounts), never starts, configures or authenticates the
  container runtime application, and never mounts the Docker socket into an environment; it only connects to verify it (D-3).
- Not in this increment: changing the login shell, editing shell startup files other than catalogue dotfiles, installing anything on
  Linux or Windows, package managers other than Homebrew, terminal profiles or fonts, a mirror of the BundleDock toolbox on the host, a restore command (backups
  are restored by hand), and real-host acceptance runs (owner gates).
- Linux `whr setup` keeps the Feature 006 enrollment (FR-001). Feature 006 FR-030 and ADR 0009 are unchanged.

### Owner decisions

- **D-1 (wizard)**: after `whr` is on the Mac (download or Homebrew), `whr setup` is a wizard with simple guided instructions, for example
  "Set up the recommended developer experience on this host? [yes / customize / no]". It checks tools and settings already present first and
  installs and configures orbstack (container runtime), git, jq, starship, fish and similar tools (the initial entries are in FR-010), so the host and the sandbox share one user
  experience. Opt-out per item; nothing silently overwritten.
- **D-2 (doctor)**: `whr doctor` stays the diagnose command; `whr doctor [--fix]` repairs deviations with the same engine,
  non-interactively, under the same preview, backup and confirmation rules. Without `--fix` it stays read-only (amendment AP-4).
- **D-3 (Docker VM and socket)**: `whr` never changes the Docker VM and never mounts the Docker socket into an environment; it only reads and
  verifies it.
- **D-4 (git configuration)**: the wizard checks the global git configuration and offers the guided move to `~/.config/git` (XDG) with
  backup and explicit confirmation; host-only keys are reported per Feature 008. The developer sandbox then uses the host global git
  configuration read/write (Feature 008 FR-019).
- **D-5 (R-40)**: R-40 ("setup never installs a container runtime") is reversed in principle; the exact supersession is OD-5.
- **D-6 (host mutation)**: installing software and editing dotfiles are host mutations. Real runs are owner gates; all tests use a fake
  package manager, fake sudo, a fake network port and a temporary HOME.
- **D-7 (admin tool, network, protocol)**: (A) `whr setup` and `whr doctor --fix` MAY call the system `sudo` per catalogue-marked privileged
  step, with the exact command shown and confirmed, but never run elevated themselves, never handle a password, and never run sudo without a
  terminal (exit 70). (B) `setup` and `doctor` MAY use the network in every mode, with destinations and purpose listed up front; no
  `curl | sh`. (C) Each real run writes a protocol (readable file plus JSON), owner-only, append-only, with no secrets. `--dry-run` writes
  nothing and runs no sudo; plain `doctor` writes nothing and runs no sudo.
- **D-8 (scope, 2026-10-20)**: the threat model above; this feature is cut to the essentials.
- **D-9 (OD-1 to OD-5, confirmed 2026-10-20)**: OD-1 (Homebrew only plus the OrbStack cask), OD-3 (dotfile preview, backup, no silent overwrite), OD-4 (Linux enrollment kept) and OD-5 (R-40 superseded) as written in the plan. OD-2 is amended: the host catalogue is a small curated list owned by WorkHarbor (13 initial entries, FR-010; entries are added deliberately), each with a minimum version only. It is not a mirror of the BundleDock toolbox: no host install of toolbox tools, no version equality between host and toolbox, no toolbox sync. The same user experience means the same few DX tools and configs (starship, fish, git), not parity.
- **D-10 (Colima, owner decision of 2026-10-31)**: Colima is added as a second container-runtime catalog entry, an alternative next to
  `orbstack`. Lima is not its own entry: it comes only as a dependency of Colima. Lima as a VM driver for Linux guests is out of scope here
  (Tart stays the VM driver for macOS guests, Feature 011). Colima is open source and installed from Homebrew core; it is not preselected
  and needs no third-party tap. FR-026 and FR-027 carry the details; the points marked author proposal there, and the open questions after
  FR-027, are not owner decisions.
- **D-11 (container storage location, owner decision of 2026-10-31)**: `disk.container_storage` does not query the runtime API (the no-API rule of Feature 006 FR-060 is unchanged; only its budget and file-read lists get entries). The wizard
  records the storage location; doctor reads it from the runtime's readable configuration file first and falls back to the record
  (Feature 011 FR-041, decision 21; Colima details in FR-028).
- **D-12 (container runtime endpoint, OWNER DECISION of 2026-10-31, from a voice conversation)**: how `whr` chooses the container runtime
  endpoint. (a) Default, when the user specifies nothing: the standard Docker environment variable `DOCKER_HOST`, then the standard socket
  `/var/run/docker.sock`; whatever is active there is used. No guessing among other runtimes. (b) The user can override per invocation on the
  command line, or fix the endpoint as a configuration parameter (a config value); an explicit specification beats the default. (c) If the
  default endpoint is missing (no `DOCKER_HOST`, no standard socket), `whr setup` and `whr doctor` investigate which options exist on the
  host (the documented candidate list, including the Colima and OrbStack sockets) and ask interactively which one to use; the choice is
  written to the config value in the host profile, the same place as the recorded container storage location (D-11; the record name
  `container_endpoint` is an author proposal in line with `container_storage`). (d) The configured value stays authoritative even if the
  standard socket later appears again: no silent backend switching. D-12 supersedes OQ-1 (resolved): a non-default Colima profile is
  reached by selecting its socket with the command-line option or the config value; there is no probe of `~/.colima/<profile>`. The
  measured example behind (d) is OG-1 result M-6. FR-029 to FR-032 carry the details.
- **D-13 (Docker contexts, endpoint record, drift, single machine; OWNER DECISION of 2026-10-31)**: (e) Docker contexts are read only
  for detection and drift comparison, never for selection. Selection and connection always use the resolved endpoint (socket), never the
  context. Setup and doctor read the context metadata files (a plain file read of the context endpoints; no `docker context` command, no
  daemon call; never TLS or key material, only the endpoint from the metadata) as an additional source of truth for which targets exist
  on the host. Feature 010 FR-002 (no context indirection for selection) stays; its wording is amended so that the read-only detection
  use is explicit. (f) The host profile stores both the resolved endpoint (authoritative) and the context name it came from
  (informational provenance only, may be absent). If the context is later deleted or changed, `whr` keeps working through the stored
  endpoint. (g) `doctor` reports drift as warnings, never as a failure and never by silently switching: the stored endpoint is not
  reachable; the stored context name no longer exists or now points to a different endpoint; the active context's endpoint differs from
  the stored endpoint. Re-recording the stored values is a modify-class repair with preview and per-item `--confirm`. (h) Remote targets
  (`ssh://`, `tcp://`, anything that is not a local Unix socket) are not supported: single machine only for now, cluster support would be
  a future feature. Setup and doctor list such contexts transparently as "not supported" with the reason, never offer them for
  selection and never use them; the ownership and access checks of Feature 008 apply to local sockets only. Explicit non-goal: remote
  sessions. FR-029 to FR-032 carry the details.
- **D-14 (Docker client entry and never-selected entries, OWNER DECISIONS of 2026-10-31; they resolve OQ-2 and OQ-3)**: (i) The Docker client is a
  separate catalog entry `docker` (the Homebrew core formula `docker`, the client command line tool), not preselected, independent of the Colima
  entry and without any new dependency or `requires` mechanism. If the user selected Colima and no Docker client is found, `setup` and `doctor`
  say so explicitly and name the `docker` entry; this is message text and nothing is installed automatically. (j) The host profile stores
  nothing for a catalog entry that the user never selected; absence means "not selected", and there is no `not_selected` state. `doctor`
  reports a check that depends on an unselected entry as `skipped` with a reason, never as a failure, and `setup` offers every
  not-preselected entry, unchecked, on each run. This settles the shared question of Feature 011 FR-040 as well (the VM tool and the two entries of
  the owner's own source). FR-033 and FR-034 carry the details.

Author proposals for D-12 and D-13 (not owner decisions; the owner may change them):

- **EP-1 (precedence)**: command-line option, then config value, then `DOCKER_HOST`, then the standard socket. The owner fixed that an
  explicit specification beats the default; the order within that and the position of the config value above `DOCKER_HOST` are
  proposals. A stale config value then wins over an exported `DOCKER_HOST`, which is why drift is reported (D-13 (g), FR-032).
- **EP-2 (names)**: option `--container-endpoint <endpoint>`; config key `container_endpoint` with the fields `socket` and `context`;
  reserved profile item id `container-endpoint` for confirmation and opt-out, like `git-config` (not a catalogue entry).
- **EP-3 (ordinary commands)**: commands other than `setup` and `doctor --fix` never prompt and never investigate; with no usable
  endpoint they exit 68 (the existing conflict or wrong-state code, the precedent of Feature 010 FR-009) with a `whr:` line naming the
  option, the config value and `whr setup`. No new exit code, refusal code or step kind is introduced anywhere in D-12 and D-13.
- **EP-4 (interaction)**: the question is asked only on a terminal, on standard error, once, with numbered options and a default `skip`
  (nothing recorded); the options are the sockets of the candidate list that accept a connection plus the local-socket contexts, with
  the context name shown. Exit 70 naming `--container-endpoint` when options were found, no terminal is present and nothing answers
  the question; no question and no 70 when nothing was found.
- **EP-5 (reporting)**: drift warnings are evidence entries and `whr: warning` lines of `container.socket`, not a new check status
  (the statuses stay `pass`, `fail`, `skipped`, `unknown`; compare Feature 011 decision 19(a)); the check keeps the status of the
  endpoint itself.
- **EP-6 (candidate list)**: the investigation list is the documented list of `doctor-checks.json` without its two default entries;
  the second Colima socket is not added (OQ-6).
- **EP-7 (Docker client entry details, D-14 (i))**: id `docker`, kind `tool`, title naming the Docker client only, platforms `darwin-arm64` and `darwin-amd64` (Linux installs nothing, FR-001),
  `selected_by_default` false, `network` host `formulae.brew.sh`, `tool.source` the Homebrew formula `docker` from `homebrew/core`, `may_elevate` false, `command` `docker`; `version_args`, `version_regex`
  and `min_version` are measured at implementation (OG-1) and not fixed here. A `docker` command found outside Homebrew (for example the client that the OrbStack cask provides) is `present_other_source`, which
  counts as "a Docker client is found" and is never replaced (FR-008). The message is a note in the evidence of `host.tool.colima` and a line in the `setup` plan and report; no check status changes.
- **EP-8 (skip reason)**: the `skipped` reason text for a check that depends on an unselected entry is `unselected` (a skip reason, not an item state, refusal code or step kind).

OD-1 to OD-5 are in the [plan](plan.md#owner-decisions), confirmed 2026-10-20. Owner H38 (2026-10-20) also confirms the existing DP-1 to DP-5 defaults and the no-op bookkeeping choice in FR-021; AP decisions, measured production minima and AP-6 reason placement remain separate. H37 permits source-only dependents to use exact independently CLEAR local prerequisite parents with per-work-item evidence before owner merge, without treating open task/real-host acceptance as complete.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - First setup of a Mac host (Priority: P1)

An owner with `whr` installed runs `whr setup`; the wizard lists what is present and missing, asks one question, and on `yes` installs the
missing items and writes the missing dotfiles.

**Independent Test**: fake package manager, temporary HOME: an empty host ends with the recommended items installed through the fake, dotfiles
created, the profile and protocol written, and nothing outside the plan touched.

**Acceptance Scenarios**:

1. **Given** Homebrew present and nothing installed, **When** the owner answers `yes`, **Then** the plan listed before the question is applied
   (create class only) and the result lists each step.
2. **Given** `no` or end of input, **Then** nothing is written (`no` exits 0; cancellation exits 72).
3. **Given** the runtime was installed, **Then** `whr` only reports that it must be opened once and verifies the socket by connecting.

### User Story 2 - Customize, opt out, re-run (Priority: P1)

The owner answers `customize` and decides item by item; a skip is remembered; present tools are never replaced silently; a rerun changes nothing.

**Acceptance Scenarios**:

1. **Given** `customize`, **When** the owner skips `fish`, **Then** the plan has no fish install or dotfile and the profile records the opt-out.
2. **Given** an opted-out item, **Then** a rerun preselects it off, `doctor` reports it `skipped: opted_out` and `doctor --fix` never repairs it.
3. **Given** `jq` below the minimum or a tool found outside Homebrew, **Then** an upgrade is a modify-class step needing its own confirmation, or
   the tool is only reported and never replaced.
4. **Given** a completed run, **When** the identical run repeats, **Then** it makes no install, managed configuration/profile change or backup; after confirmation it still creates the required protocol and uses run-lock bookkeeping (FR-019, FR-021).

### User Story 3 - Move the global git configuration (Priority: P1)

The owner has `~/.gitconfig`; the wizard reports host-only keys and offers the move to `~/.config/git/config` after a backup and explicit confirmation.

**Acceptance Scenarios**:

1. **Given** only `~/.gitconfig`, **When** the owner confirms, **Then** a backup exists, the XDG file holds the same effective configuration, the
   legacy file is removed only after verification, and no value is printed.
2. **Given** a symlink, a relative `include.path`, `GIT_CONFIG_GLOBAL` or a foreign `XDG_CONFIG_HOME`, a git lock file, or another `whr` run,
   **Then** the move is refused with its code and nothing changes.

### User Story 4 - Diagnose and repair with doctor (Priority: P1)

**Acceptance Scenarios**:

1. **Given** deviations, **When** `whr doctor` runs, **Then** nothing on the host changes and no sudo runs.
2. **Given** `whr doctor --fix` without `--yes`, **Then** it prints the preview and exits 70; with `--yes` create-class repairs run, modify-class
   repairs only with `--confirm <id>`, opted-out items never.
3. **Given** no host profile, **Then** `--fix` refuses (`host_setup.profile_absent`) and points to `whr setup`.

### User Story 5 - Privileged steps, failure and unattended runs (Priority: P2)

**Acceptance Scenarios**:

1. **Given** a catalogue step marked privileged, **Then** its exact `sudo` command is shown and confirmed on a terminal; `sudo` itself asks for
   the password; without a terminal, with `--json`, `--non-interactive` or without confirmation it is not run (exit 70).
2. **Given** the package manager fails on step 3 of 5, **Then** independent steps still run, dependents are `blocked`, the exit status is 73 and
   completed and remaining steps are listed; a rerun re-detects and does only what remains.
   Exception: if the failure retains a child process handle (cleanup failed), the run stops, lists the remaining steps, skips profile persistence
   and returns the handle for reconciliation (`package_manager_failed`, exit 68, or 73 if something changed).
3. **Given** no terminal or `--json`, **Then** no prompt is shown and missing confirmations yield exit 70 naming the flags.

### User Story 6 - Linux is unchanged (Priority: P2)

On Linux `whr setup` keeps Feature 006 enrollment; the wizard and `doctor --fix` do nothing there (`doctor --fix` refuses, 68).

### Edge Cases

- **Tool at another version**: below the minimum is `present_outdated` (upgrade offered, never forced); at or above it is `present_ok`; an
  unparseable version is `present_unverifiable`, never ok; no downgrade.
- **Homebrew missing**: Homebrew-backed steps are `blocked`; `whr` prints the official instructions and never installs Homebrew.
- **Offline**: detection, preview and dry run work; install steps fail through Homebrew and are retried by a rerun; an unreachable lookup is
  reported `not verified`.
- **Root or sudo start** (darwin): refused. **Another run active or a git lock file present**: refused.
- **File changed between preview and write, or a symlink or special-file target**: refused; nothing written.
- **Fresh Mac**: `/usr/bin/git` is the Command Line Tools shim; it is detected without executing it and `git` counts as absent.
- **Interrupt**: the current child gets SIGINT, no further step starts, completed steps stay, an in-flight file write is rolled back.

## Requirements *(mandatory)*

### Functional Requirements

**Command surface and platform**

- **FR-001**: `whr setup` and `whr doctor` stay one command each (`host.setup`, `host.doctor`). `setup` selects its fixed step list by detected
  platform, not by flag: darwin runs the wizard; Linux runs the Feature 006 enrollment (FR-009, T015) unchanged; other platforms are refused
  before any change (`host_setup.platform_unsupported`, 68). `doctor --fix` is darwin only.
- **FR-002**: `whr` MUST NOT change the Docker VM or mount the Docker socket (D-3); the only interactions are connecting to a selected or investigated
  socket and closing it (FR-029, FR-031; Feature 006 FR-060) and the plain file reads of FR-031 and FR-032. Plan steps have a closed set of kinds ([data model](data-model.md#step-kinds)); none touches the VM.
- **FR-003**: `setup` MAY install a container runtime only when the catalogue lists it and the owner has not opted out (R-40 superseded,
  OD-5). Plain `doctor` installs nothing. `whr` never starts, configures or signs in to the runtime application.

**Wizard and plan**

- **FR-004**: Detection MUST precede the plan and every question. The wizard shows the plan, then asks one question with the answers `yes`,
  `customize`, `no` ([wizard flow](contracts/wizard-flow.md)). `yes` applies the displayed create-class plan; modify-class steps are asked one
  by one (default no); `no` exits 0 with no write; end of input or interrupt exits 72.
- **FR-005**: `customize` asks per item and allows opt-out of every item, including the runtime and git. Every skip of a preselected item is recorded as an opt-out in
  the host profile (a never-selected, not-preselected entry leaves no record, FR-034); an opted-out item is never installed, upgraded, written or repaired by `setup` or `doctor --fix`; a rerun preselects it
  off. `git-config` is a reserved profile item. A `no` answer writes no profile. Prompts go to standard error.
- **FR-006**: Detection MUST be read-only and bounded: fixed argv, no shell, 5 s per probe, commands resolved on a fixed sanitized absolute
  PATH (`/opt/homebrew/bin`, `/opt/homebrew/sbin`, `/usr/local/bin`, `/usr/local/sbin`, `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin`, never the
  user's PATH). On macOS the Command Line Tools `git` shim is detected by file checks without executing it (Feature 006 FR-060) and `git`
  then counts as absent. Detection never runs `brew`; an item is Homebrew-managed when its resolved path is under `<prefix>/Cellar/<name>/`
  or `<prefix>/Caskroom/<name>` exists (layout assumption, OG-1). States: [item states](data-model.md#item-states).
- **FR-007**: Every mutation is a typed step of a plan with id, item, kind, class, preview and rollback data; the plan has a stable hash that
  is rechecked after confirmation; nothing runs that the plan did not list. A failed step blocks only its dependents.
- **FR-008**: Nothing is silently overwritten or replaced. Create-class steps need `yes` (or `--yes`); every modify-class step needs its own
  confirmation (prompt or `--confirm <id>`) with a diff for files. An existing tool from another source is never replaced or removed.

**Installation, elevation, network**

- **FR-009**: Installation goes through a package-manager port; the only adapter is Homebrew (formula and the OrbStack cask), located at
  `/opt/homebrew/bin/brew` or `/usr/local/bin/brew`, started with fixed argv, no shell, an allow-listed environment and a per-step deadline
  ([contract](contracts/wizard-flow.md#brew-invocation)). Only taps `homebrew/core` and `homebrew/cask`; no `--HEAD`, `--force`,
  `--ignore-dependencies`, `--no-quarantine`; no downgrade; `brew` is never run through `sudo`. If `brew` is absent the steps are `blocked`
  (`package_manager_missing`) and `whr` never installs Homebrew. After install or upgrade the version is re-detected; below the minimum fails
  the step (`version_below_minimum`), reported, not rolled back.
- **FR-010**: Every installable item is listed up front, before the question, with type, name, tap, minimum version, declared
  network destinations and purpose, and any privileged command. Each entry has a `selected_by_default` flag and, where it needs a
  dotfile change, a `config` marker (`dotfile` or `shell_hook`). The initial catalogue has exactly 13 entries. Preselected core: `orbstack` (cask), `git`, `gh`, `jq`, `fish`, `starship`; preselected recommended: `ripgrep` (rg), `fd`, `fzf`, `zoxide`, `bat`, `jj`, `herdr` (all 13 entries preselected, each individually opt-out under FR-005). Entries needing a dotfile or shell hook: `starship`, `fzf`, `zoxide`, `fish`. Nothing else from the toolbox goes on the host (no scanners, linters, language tools, hugo, typst, delta, lazygit, direnv, mise, uv, tmux). More entries are added deliberately by the owner (OD-2, OG-7); FR-026 adds Colima and FR-033 adds the Docker client. The wizard shows the diff of every dotfile config change (git move uses the structural preview of FR-014); it never touches git configuration (except the guided move of FR-014) or the login shell. It MUST NOT contain an entry that installs, configures or
  authenticates an AI agent tool (Feature 006 FR-030).
- **FR-011**: `whr` refuses to start as root or with `SUDO_USER` set (darwin; `host_setup.running_as_root`) and never runs elevated itself.
  In real runs of `setup` and `doctor --fix` a step marked `privileged` in the catalogue runs as `/usr/bin/sudo -- <catalogue argv>`, fixed
  argv, no shell, allow-listed environment, followed by `/usr/bin/sudo -k`. The exact command is shown in the preview and confirmed (prompt in
  `setup`; `--yes` with `--confirm <id>` in `doctor --fix`). The system `sudo` prompts on the terminal; `whr` never reads, stores, logs or
  passes a password. Without a terminal, with `--json`, `--non-interactive` or without confirmation the step is `blocked`
  (`sudo_requires_terminal`) and the run exits 70 (73 when other steps were applied). `--dry-run` shows the command and runs nothing. The
  initial catalogue has no privileged step; each privileged command is reviewed with its catalogue entry.
- **FR-012**: `setup` and `doctor` MAY use the network in every mode. Downloads of software are done by Homebrew; `whr` itself makes only
  read-only HTTPS lookups of catalogue-declared URLs (fixed User-Agent, no host data) and reports a failed lookup as `not verified`. No
  `curl | sh`. A version below the catalogue minimum is reported, not forced.

**Dotfiles and git configuration**

- **FR-013**: A dotfile target is a home-relative path from the catalogue with no `..` and no absolute path. Before any write `whr` shows a
  bounded unified diff (full content for a new file); before replacing existing content it makes a verified backup (`<state>/backups/<run-id>/`,
  mode 0600 in 0700; never deleted by `whr`; restore is a manual copy, stated in the result). A symlink or special-file target is refused. The
  write is atomic (temporary file, rename), the target is re-hashed immediately before it and a change since the preview refuses it. Content
  comes only from the catalogue template and matches its SHA-256; `whr` never merges into a user's file (replace or skip). Missing parent
  directories under the home directory are created without following links. `--dry-run` shows the same preview and writes nothing.
- **FR-014**: The wizard classifies the global git configuration with the Feature 008 classes (xdg, legacy_home_file, split, absent, symlink)
  and reports host-only keys by key, origin file and remedy, never by value. The move to `~/.config/git/config` is offered only for
  `legacy_home_file` and `split`, needs explicit confirmation and a backup, and shows only a structural move preview naming key names, redacted origin/destination paths and actions, never a content/value diff. It proceeds by staging the new content in a temporary file next
  to the target (for `split`, XDG content followed by legacy content), verifying with `git config --file <staged> --includes --list` that the
  effective key/value sequence equals the current one (compared in memory), then rename, removal of the legacy file and a final check. It is
  refused, with no change, on a symlink, a relative `include.path` or `includeIf` path, `GIT_CONFIG_GLOBAL` set, a different
  `XDG_CONFIG_HOME`, an existing `~/.gitconfig.lock` or `~/.config/git/config.lock`, or another active `whr` run. There is no crash-resume
  machinery: a rerun re-detects, and a failure restores from the backup. `setup` and `doctor --fix` never set, change or remove a git key and
  never print git values; `absent` is reported only.

**Doctor and repair**

- **FR-015**: Plain `whr doctor` stays read-only for the host (no install, no write, no sudo, no protocol; the network is allowed) and gains
  reporting checks `host.profile`, `host.tool.<id>`, `host.dotfile.<id>`, `git.global_config` (amendment AP-6). Without a host profile the
  item checks are `skipped: no_host_profile`; an opted-out item is `skipped: opted_out`.
- **FR-016**: `whr doctor --fix` builds its plan from doctor findings restricted to selected items of the host profile, through the same
  engine, plan model, preview, backup and confirmation rules as the wizard (equal state, equal plan hash). It never prompts. Without `--yes`
  it prints the preview and exits 70; with `--yes` it applies create-class steps; modify-class and privileged steps need `--confirm <id>`
  (`git-config` for the git move). It does not repair non-host findings and does not touch the Docker VM.
- **FR-017**: Unattended runs (`--non-interactive`, no terminal on standard input or error, or `--json`) never prompt and never run sudo;
  missing confirmation yields exit 70 naming the flags. CI is never inferred from environment variables.
- **FR-018**: `--dry-run` (setup and fix) writes nothing at all (no protocol, lock, backup, dotfile or install), runs no sudo and no
  mutating process; it performs only detection and read-only lookups and prints the preview, which may show the exact `sudo` command.

**Run, protocol, output**

- **FR-019**: A real run takes an exclusive run lock (`flock` in the state directory) from plan confirmation on; a second run is refused
  (`host_setup.run_locked`, 68). It writes one protocol in `~/.local/state/whr/protocols/` (DP-1, confirmed H38), including every confirmed no-op real run: a JSON Lines file `<run-id>.jsonl` and a readable
  `<run-id>.txt`, mode 0600 in a 0700 directory, append-only, created exclusively without following links. Records: time (UTC), run id,
  platform, catalogue version, the selection (answer, opt-outs), per step kind, item, exact argv, whether sudo and network were used, exit
  status and result, backup paths with SHA-256, and for dotfiles the diff size. It never contains a password, token, git configuration value
  or environment contents. `whr` never deletes protocols and warns on standard error when the directory holds more than 500. A `no` answer,
  `--dry-run` and plain `doctor` write no protocol and take no lock. A run without a writable protocol does not mutate
  (`host_setup.protocol_unwritable`). The JSON journal is the single record of a run; the text file renders it. Each record kind requires its own payload under the protocol schema; format validation asserts UTC date-time, while ordering, redaction, file modes and durability remain behavioral tests.
- **FR-020**: With `--json` the result is one envelope of ADR 0013/0014 and Feature 006 FR-015 (`host.setup` or `host.doctor`), prompts and
  previews on standard error, evidence redacted as in Feature 006 FR-060. Exit codes use only the ADR 0013 table ([flow](contracts/wizard-flow.md#exit-codes)):
  0 success or `no`; 64 usage; 68 refusal, unsupported platform, root start, run lock, blocked or failed with nothing applied, failed doctor
  check; 70 needs-human (unattended without confirmation, privileged step without terminal); 71 deadline with nothing applied; 72 interrupt or
  cancellation; 73 partial failure (a mutation applied and selected work failed, blocked, timed out or was interrupted; completed and remaining steps listed; precedence over 72, 71, 68). Ordinary opt-outs and declines are successful unless they actually block a selected dependent; 74
  internal, including an invalid catalogue.
- **FR-021**: A rerun with the same selection changes no managed tool installation, configuration, host profile or backup. Confirmed real no-op runs still create a fresh protocol and use run-lock bookkeeping (H38); those required protocol/lock effects are excluded from managed-state no-op comparisons. A `no` answer, plain doctor and dry-run remain entirely write-free. Interruption or failure leaves
  state that a rerun re-detects; it does only what remains.

**Catalogue, tests, governance**

- **FR-022**: The catalogue is one JSON file validated by [host-tool-catalogue.schema.json](contracts/host-tool-catalogue.schema.json).
  The list is small, curated and owned by WorkHarbor; each entry has a minimum version only (OD-2). `whr` embeds one snapshot per release and
  never fetches a catalogue at run time. The catalogue is not derived from, synchronized with or compared to the BundleDock toolbox.
- **FR-023**: Tests run offline with a fake package manager, fake sudo, a fake network port, a temporary HOME, fake clocks and sockets: no real
  `brew`, `sudo`, network, Docker or real HOME. A fake exec port records every call and fails on any call outside the allowed argv table. Real
  runs are owner gates (OG-1, OG-2, OG-9).
- **FR-024**: Implementation PRs MUST NOT edit Feature 006 text, Feature 008 text, `contracts/cli/*`, ADR 0013, ADR 0014 or
  `docs/container-runtime.md`; those are the amendment proposals (AP-1 to AP-9) and owner-gated tasks. Each PR records an empty
  `git diff --stat` of those paths, except the named gated tasks.
- **FR-025**: `whr` never changes the login shell, edits shell startup files other than catalogue dotfiles, adds Homebrew taps or sends
  telemetry.

**Container runtime alternatives (D-10)**

- **FR-026** (Colima entry): The catalog MUST gain one entry for Colima, in addition to the 13 initial entries of FR-010 (a deliberate
  owner addition under OD-2; it is not part of the initial 13, so `orbstack` stays the only preselected runtime). Its fields follow the
  existing entries: `id` `colima`, `kind` `runtime`, title naming Colima only (author proposal: the Lima dependency is not named in the title, to keep that vendor name out of command and preview text; Feature 011 FR-022 would allow it in catalog data, so this is a choice, not a requirement; the PR text mentions the dependency), `platforms` `darwin-arm64` and `darwin-amd64`, `selected_by_default` false, `network` (host `formulae.brew.sh`, the
  read-only lookup URL of the formula, purpose as the other formula entries), and `tool` with `source` = Homebrew formula `colima` from
  `homebrew/core`, `may_elevate` false, `command` `colima`, a `version_args` and `version_regex` that read the version of the `colima` command,
  and a `min_version`. No new schema property is needed: `kind` `runtime` is not tied to the OrbStack-only `version_metadata` probe, and the
  closed `source.tap` list stays `homebrew/core` and `homebrew/cask` (so the closed third-party list of Feature 011 FR-038 is unaffected).
  The exact `version_args`, `version_regex` and `min_version` are measured on the owner's Mac (OG-1), like the other minima, and are not
  fixed by this text. Behavior is the generic one: detection, `present_*` states, install and upgrade through FR-009, per-item opt-out (FR-005),
  `host.tool.colima` in plain `doctor` (FR-015) and `doctor --fix` for a selected item (FR-016). Colima is installed only when the owner selects it
  (`customize`, or the selection flag that names the item); `yes` alone installs the preselected items only. Lima is not a catalog entry,
  is not detected, is not an item in the host profile and has no step of its own: it arrives only as a Homebrew dependency (FR-009 already
  forbids `--ignore-dependencies`; the formula declares Lima as a hard dependency, M-2). Linux is unchanged (FR-001: nothing is installed on Linux).
- **FR-027** (what `whr` does and does not do with Colima): D-3, FR-002 and FR-003 apply to Colima unchanged: `whr` never runs
  `colima start`, `stop`, `delete`, `ssh` or `template`, never writes a Colima profile file (`~/.colima`), never creates or switches a Docker context (that would
  edit the Docker client configuration), never starts or configures the Lima VM and never mounts the Colima socket into an environment;
  it only connects to the discovered socket (Feature 006 FR-060; Feature 011 FR-039 and Feature 012 add their own read-only calls). The report after a
  real run (wizard flow, step 7) names the next step for the installed runtime in the same shape as for OrbStack: start the runtime once
  yourself (for Colima `colima start`); `whr` does not start or configure it; then run `whr doctor` (author proposal for the exact
  wording). One endpoint rule applies to every consumer of the container runtime, with Colima as the case that exposes it, because
  its socket is under the user's home and not at `/var/run/docker.sock`: the endpoint is the one that the selection rule of FR-029 (D-12) yields.
  The default Colima profile is reached by selecting `~/.colima/default/docker.sock` (FR-030, FR-031); it is no longer tried by default. No consumer hard-codes a socket path of its own: `container.socket`,
  `container.image` and `container.leftovers` (Feature 011 FR-039), the runtime-access step of `whr shell` (Feature 012 step 5 and Feature 010
  FR-002) all use that one endpoint and the `unix://` rule, and each `docker` call names it as `--host=<endpoint>` (Feature 010 FR-002, Feature 012). The
  image build of Feature 011 FR-034 and FR-037 does the same (author proposal P-2: Feature 011 says today only "the container runtime's normal
  build command", which would use the Docker client's active context, possibly a different daemon than the one the checks read). Docker contexts are never
  consulted to choose the endpoint (Feature 010 FR-002, `docs/container-runtime.md`; OQ-1 is resolved by D-12); they are read only for detection and drift (D-13, FR-031, FR-032).

- **FR-028** (Colima storage location, owner decision D-11; Feature 011 FR-041 gives the generic rule): The Colima profile is found from the
  selected endpoint, not from an environment variable: an endpoint `<dir>/<profile>/docker.sock` whose directory holds `colima.yaml` is the
  profile `<profile>` (measured for the default profile: `~/.colima/default/docker.sock` and `~/.colima/default/colima.yaml` sit in one directory, M-4; other profile layouts are not measured). An endpoint without a profile directory, such as
  `~/.colima/docker.sock` (M-3), matches no profile, so the first source is absent and the recorded value, else `unknown`, is used (OQ-6). Doctor reads that
  file as a plain bounded file read (no `colima` process, no runtime API) and uses it as the first source. From the file `whr` reads only
  the virtual disk size settings (the keys `disk` and `rootDisk`; GiB according to the comments of the measured file, M-4): these are the maximum sizes of the VM disk, not free space and
  not used space, and they are information only (Feature 011 FR-041). The measured file states no host path for the VM disk and Docker's data lives inside the VM (M-5); the host location that is
  measured is therefore the volume that holds the Colima home directory (default `~/.colima` according to the file comments; the profile directory lies in it), and the number reported is
  the free space of that host volume. Not measured, and therefore still hedged: a `COLIMA_HOME` override or an XDG location, non-default profile layouts, whether the file exists before the first start, and where the disk image file lies below the Colima home directory; if a real file ever states the location,
  the stated location replaces the directory rule. The wizard determines the same location when `colima.yaml` exists and records it
  (FR-041). The file exists after the first start and is re-written at later starts (M-4), so nothing is recorded right after the install, the report names the later `whr setup` run, and doctor reads the file afresh each time rather than trusting an earlier read.
  A runtime without a readable configuration file (for OrbStack none is specified here) gives no first source: the recorded value, else `unknown`.

**Container runtime endpoint selection (D-12, D-13)**

- **FR-029** (selection rule; D-12 (a), (b), (d); the order is EP-1): Every consumer of the container runtime (`container.socket`, `container.image`,
  `container.leftovers`, the `disk.container_storage` lookup, `whr ls --local`, `whr shell --local`, the runtime-access step of Feature 012, the image build of
  Feature 011 and the `socket_verify` step) MUST get its endpoint from one selection function that yields one endpoint or none, in this order, and the first source that is
  specified is the only one used: (1) the command-line option `--container-endpoint` of FR-030; (2) the config value `container_endpoint` of the host
  profile; (3) `DOCKER_HOST`; (4) the standard socket `/var/run/docker.sock`. Sources (3) and (4) are the default and follow what the Docker client itself would use
  first: whatever runtime is active there is used, with no guessing among other runtimes. A specified source that is unreachable, malformed or not a `unix://`
  socket is reported as such and is never replaced by a lower source or by a candidate of the documented list (no silent backend switching). The configured value stays
  authoritative when the standard socket appears or disappears later; only the user changes it (FR-031), never `whr` by itself. Selection starts no process, reads no Docker
  context, probes no directory such as `~/.colima/<profile>` and draws on the shared 5 s budget like the socket discovery it replaces. The documented
  candidate list of `doctor-checks.json` is no longer a default search; only FR-031 uses it. `DOCKER_HOST` keeps the Feature 006 FR-060 rules (a non-`unix` scheme is
  `unknown` with reason `remote_not_checked` in doctor and exit 68 in the local commands of Feature 010). `--host=<endpoint>` is passed to every `docker` call as before.
- **FR-030** (override and config value; D-12 (b), D-13 (f); names are EP-2): `--container-endpoint <endpoint>` is accepted by the commands that use the runtime: `setup` and `doctor`
  (darwin; on Linux `setup` it is a usage error like the other wizard-only flags), `ls --local`, `shell --local` and the later commands of Features 011 and 012. The value is `unix://`
  followed by an absolute path; an empty, relative or otherwise malformed value is a usage error (64, Feature 006 FR-063); another scheme follows the `DOCKER_HOST` rule of FR-029 (D-13 (h)).
  The option is per invocation and is never recorded by itself outside the rule of FR-031. The config value is the host-profile record `container_endpoint` with the fields `socket` (the
  authoritative endpoint; a path below the home directory is stored home-relative, otherwise absolute, like the other profile paths) and `context` (the name of the Docker context it came from; informational
  provenance only; absent when the endpoint was not taken from a context). Only `socket` is ever used to connect. If the context is deleted or changed later, the stored `socket` keeps working (FR-032). The data-model
  line "No paths or values" is widened for this record, as for the storage and image records of Feature 011.
- **FR-031** (investigation and question; D-12 (c), D-13 (e), (h); mechanics are EP-4): On macOS, `whr setup` and the interactive `whr doctor` path of OQ-4, when no option is given, no config value is
  recorded, `DOCKER_HOST` is unset or empty and the standard socket is missing (what "missing" means is OQ-5), investigate which options exist and ask which one to use. The investigation is read-only and
  bounded: it dials each fixed path of the candidate list (the entries of `doctor-checks.json` `socket_candidates` after `DOCKER_HOST` and `/var/run/docker.sock`: `$XDG_RUNTIME_DIR/docker.sock`,
  `~/.orbstack/run/docker.sock`, `~/.docker/run/docker.sock`, `~/.colima/default/docker.sock`, `~/.rd/docker.sock`) with the dial cap of doctor, sends nothing, lists no directory (no `~/.colima` scan), starts no
  process, and additionally reads the Docker context metadata (plain bounded file reads: the Docker client configuration file for the active context name and the context metadata files for names and
  endpoints; the TLS and key files of a context are never opened; the `docker context` command is never run; the file layout is unmeasured, OG-1). Context endpoints that are local `unix://` sockets are options, listed
  with the context name; a context with any other target (`ssh://`, `tcp://`, ...) is listed as "not supported: remote target" with that reason, is never offered and never used (D-13 (h)). An option found
  twice (a candidate and a context endpoint) is listed once with the context name. The question goes to standard error, offers the options that accept a connection by number and `skip`
  (default), and a `skip` is recorded as an opt-out of the reserved item `container-endpoint` (FR-005; a rerun does not ask again). An answer is recorded as a `profile_write` step (existing kind; create class when
  no value is recorded, modify class when it replaces one) with the `socket` and, when it came from a context, the `context` name, shown in the preview, confirmed by `yes` or `--yes` (create) or by its own confirmation
  (modify; `--confirm container-endpoint`), and written to the protocol; the storage location of FR-028 is determined from the new endpoint in the same run. `--container-endpoint` answers the question (Feature 006 FR-045: every
  choice has a flag and the same inputs give equal configuration in wizard and unattended modes): with no recorded value its value is used for the run and planned as the same `profile_write`. Prompts go to standard error
  (Feature 006 FR-014); with `--json`, `--non-interactive` or no terminal the question is not asked and, if options were found and nothing answers it, the run exits 70 naming `--container-endpoint` (FR-017, Feature 006 FR-045); end
  of input or interrupt at the question exits 72; if nothing was found there is no question and no 70, and the report says so and points to the container-runtime page. Nothing is installed, started or configured by the investigation.
- **FR-032** (ordinary commands and doctor reporting; D-12, D-13 (g); EP-3, EP-5): Commands other than `setup` and `doctor --fix` never prompt and never investigate. When the selection yields
  no endpoint or an unusable one they run no `docker` call and exit 68 (EP-3; a malformed option value is 64) with a `whr:` line that names the source concerned and the remedies (the option, the config value, `whr setup`).
  Plain `doctor` stays read-only and asks nothing: `container.socket` names the source used (`option`, `config`, `DOCKER_HOST`, `standard`) in its evidence; when no endpoint is selected it fails as today and its evidence lists
  what the investigation of FR-031 found. Doctor also reports drift, as warnings that never change the check status and never switch the endpoint (D-13 (g), EP-5): (i) the stored endpoint is not reachable (the check status itself is
  then `fail` as for any unreachable endpoint; the warning adds the recorded provenance); (ii) the stored context name no longer exists or now points to a different endpoint; (iii) the active context's endpoint, or the `DOCKER_HOST` endpoint,
  differs from the used endpoint (the motivating case is OG-1 result M-6 turned around: a stored endpoint kept while the standard socket or the active context points elsewhere). The reads for (ii) and (iii) are the plain file reads of FR-031,
  draw on the shared 5 s budget and never choose the endpoint. Re-recording the stored values to the current ones is a modify-class `profile_write` repair of the reserved item `container-endpoint` in `doctor --fix`, with preview and the per-item
  `--confirm container-endpoint` (FR-016); `--yes` alone never does it.

**Docker client entry and unselected entries (D-14)**

- **FR-033** (Docker client entry; D-14 (i); details are EP-7): The catalog MUST gain the entry `docker` in addition to the 13 initial entries and Colima (FR-026), following the pattern of the Colima entry and
  not preselected. The catalog then holds 15 entries on this text alone; with the 16 entries of Feature 011 (13 initial, the VM tool and two more) and the two of this feature the total is 18. No `requires`
  relation is added, the loader rule "every `requires` id exists" is unchanged, and the Colima entry has none. Selecting Colima does not select, plan or install `docker`. When the host profile has Colima as selected
  (or Colima is installed and detected) and no Docker client is found (the item states `absent`; a client from another source, `present_other_source`, counts as found), the wizard plan and report, and plain `doctor`
  (evidence of `host.tool.colima`), say so explicitly and name the entry `docker` and how to select it; nothing is installed automatically, and `doctor --fix` repairs `docker` only if the user selected it. Without a
  client, Feature 012 step 5 and the Feature 011 image build refuse with the exit code and remediation they already specify.
- **FR-034** (unselected entries; D-14 (j); amends FR-005 and FR-015): The host profile stores nothing for a catalog entry that the user never selected; absence means "not selected" and no item state or profile value
  `not_selected` exists. Declining a not-preselected entry in `customize` records nothing (the opt-out of FR-005 applies to preselected entries that the user deselects). `setup` offers every not-preselected entry,
  unchecked, on each run. Plain `doctor` reports `host.tool.<id>` and any check that depends on an unselected entry as `skipped` with the reason `unselected` (EP-8), never as `fail`, and `doctor --fix` never
  installs it. Selecting the entry records `selected`; an entry the user later deselects records an opt-out as before. Feature 011 FR-040 states the same rule for its entries.

**OG-1 results (measured on the owner's Mac, reported by the owner on 2026-10-31)**

Environment: macOS on arm64. User name and home path are not recorded here; `~` stands for the home directory. Everything not listed as measured stays hedged where it is used.

- **M-1 (versions)**: Colima 0.10.3: `colima version` prints `colima version 0.10.3`, then `git commit: <hash>`. Docker server 29.5.2 inside the VM, `vmType` `vz`, profile `default`. The output
  shape fits a `version_args` of `version` and a pattern on the first line (FR-026); the exact catalog values and `min_version` are still set at implementation. No minimum Colima version is measured.
- **M-2 (Lima)**: `brew deps colima` prints `lima`: Lima is a hard Homebrew dependency of the Colima formula (Lima 2.2.1 installed). A plain `brew install colima` therefore installs it.
- **M-3 (sockets)**: with the default profile running, both `~/.colima/docker.sock` and `~/.colima/default/docker.sock` exist as sockets, plus `~/.colima/default/containerd.sock`; `~/.colima` also holds `_lima`, `_store`,
  `_templates` and the profile directory `default`. `docker -H unix://$HOME/.colima/docker.sock info --format '{{.Name}} {{.ServerVersion}}'` printed `colima 29.5.2`, so that socket is Colima's. The documented candidate
  `~/.colima/default/docker.sock` is correct. Not measured: whether the two are the same socket or independent, and whether both persist across Colima versions (OQ-6).
- **M-4 (configuration file)**: `~/.colima/default/colima.yaml` exists after the first start and is re-written at later starts (its modification time changed after a start). Keys include `disk` (GiB, default 100: the virtual disk size for container
  data), `rootDisk` (GiB, default 20), `cpu`, `memory`, `vmType`, `runtime` (`docker`) and `autoActivate`. The file comments mention `$COLIMA_HOME` (default `~/.colima`). Not measured: whether the file exists before the first start,
  the effect of a `COLIMA_HOME` override, and the layout of non-default profiles.
- **M-5 (no host path)**: the file states no host path for the VM disk; Docker's data lives inside the VM (Docker Root Dir `/var/lib/docker` in the guest). The measurable host location is the volume that holds the Colima home
  directory; `disk` is a size setting, never free space. Not measured: where the disk image file lies below the Colima home directory.
- **M-6 (OrbStack side by side)**: `/var/run/docker.sock` was a symbolic link to OrbStack's socket below `~/.orbstack/run/`; `DOCKER_HOST` was empty; `docker context ls` showed `default` (based on `DOCKER_HOST`, `unix:///var/run/docker.sock`)
  and `orbstack` (active); no `colima` context existed although `autoActivate` is true in the yaml (the reason is not established). With both runtimes installed and running, the standard socket and the active context pointed to OrbStack and Colima was
  empty. The default of D-12 (a) selects OrbStack there, as the plain `docker` client does; this is the motivating example for D-12 (d).
- **Still unmeasured**: a `COLIMA_HOME` override or an XDG location, socket paths of non-default profiles, the minimum Colima version, the behavior of a stopped Colima, the Docker context file layout used by FR-031, and the
  behavior of the `docker` client formula (version output, coexistence with the OrbStack client).

Open questions for the owner (none is decided by this text; each states the author proposal):

- **OQ-1 (Docker context and Colima profiles) - RESOLVED by D-12 and D-13 (2026-10-31)**: The question was whether detection, runtime access and the
  image build should use the active Docker context, and how non-default Colima profiles and several running runtimes are handled. Outcome: the default is `DOCKER_HOST`, then the standard socket,
  whatever is active there (the OrbStack-before-Colima order is gone); a non-default profile is reached by selecting its socket with the option or the config value, with no probe of `~/.colima/<profile>`;
  contexts are read only for detection and drift, never for selection (D-13 (e)). The location of the Colima home directory and the socket names are partly measured (M-3, M-4); a `COLIMA_HOME` override and
  an XDG location stay unmeasured.
- **OQ-2 (Docker client) - RESOLVED by D-14 (i) (2026-10-31)**: Colima provides the daemon, not the `docker` client. Outcome: a separate not-preselected catalog entry `docker`, no `requires` mechanism, and a message when Colima is selected and no client is found (FR-033).
- **OQ-3 (alternative, not exclusive) - RESOLVED by D-14 (j) (2026-10-31)**: Nothing makes OrbStack and Colima exclusive; both can be selected, installed and detected, and D-12 says which one is used. A never-selected entry leaves no trace in the host profile (FR-034), for
  Colima and for the entries of Feature 011 FR-040 alike. A user who wants only Colima opts out of the preselected `orbstack` in `customize` (FR-005).

- **OQ-4 (who asks in `doctor`)**: D-12 (c) has `whr doctor` ask which endpoint to use and write the answer. Plain `doctor` is read-only and writes nothing (D-2, FR-015, FR-018), and `doctor --fix` never prompts
  and needs an existing host profile (FR-016, FR-017, `host_setup.profile_absent`). Options: (a) `doctor --fix` on a terminal asks this one question as a narrow exception to "never prompts" and, like the wizard,
  writes the answer after a preview, while plain `doctor` only lists what the investigation found and names `whr setup`; (b) plain `doctor` itself asks and writes on a terminal (amends the read-only rule of D-2 and
  FR-015); (c) only `whr setup` asks and `doctor` reports. Author proposal: (a).
- **OQ-5 (what "missing" means)**: D-12 (c) speaks of a missing default endpoint (no `DOCKER_HOST`, no standard socket). Open: a standard socket that exists but refuses a connection (runtime stopped or a stale file), and a
  `DOCKER_HOST` that is set but dead. Author proposal: both are specified-but-unreachable, so `container.socket` fails as today, nothing is investigated and nothing is asked; only an absent standard socket path
  (after following links, so a dangling link counts as absent) with `DOCKER_HOST` unset or empty starts the investigation.
- **OQ-6 (second Colima socket)**: with the default profile running, `~/.colima/docker.sock` exists next to `~/.colima/default/docker.sock` (M-3). Whether it is an alias of the profile socket, and whether to list it as an
  investigation option, is not measured or decided. Author proposal: do not list it (the profile-qualified path is the documented one and lets FR-028 find the profile directory); a user can still select it with the option.
- **OQ-7 (Linux and the unreachable stored endpoint)**: (1) The host profile is written by the macOS wizard only, so on Linux the endpoint is chosen by `--container-endpoint` or `DOCKER_HOST`; a rootless engine at
  `$XDG_RUNTIME_DIR/docker.sock` is no longer found by default (D-12 (a)). Open: whether Linux gets a way to record the value. Author proposal: no, the option and `DOCKER_HOST` suffice for now. (2) D-13 (g) says drift is reported
  as a warning that never fails, while an unreachable stored endpoint also makes `container.socket` itself `fail` (as every unreachable endpoint does today and as Feature 006 FR-063 exits 68). Author proposal: keep
  the `fail` for the unreachable endpoint and add the warning only for the provenance and context drift.

### Key Entities

CatalogueItem, DetectionResult, Plan, Step, HostProfile, BackupRecord, Protocol ([data model](data-model.md)).

## Success Criteria *(mandatory)*

- **SC-001**: For every item state the plan has exactly the expected steps; detection and `--dry-run` make zero mutating calls; the CLT `git`
  shim fixture is never executed (FR-004, FR-006, FR-007, FR-018).
- **SC-002**: `yes`, `customize` and `no` give the expected plans and writes; every item can be deselected; opt-outs persist and are honored by
  `setup` and `doctor --fix` (FR-004, FR-005).
- **SC-003**: For every existing dotfile-target fixture with different content, nothing is written without the per-item confirmation, bytes are
  identical after a refusal, and a verified backup precedes every replacement (FR-008, FR-013).
- **SC-004**: The fake exec recorder shows only allowed fixed argv and environment, no shell, no `brew` under sudo, no forbidden flag or
  other tap, no downgrade, and refusal to start as root (FR-009, FR-011).
- **SC-005**: With a fake sudo, a privileged step runs only after its exact command was shown and confirmed, only with a terminal, never
  receives or logs a password canary, is followed by `sudo -k`, is blocked with exit 70 otherwise (in `setup` and `doctor --fix`), and never
  runs in `--dry-run` or plain `doctor` (FR-011, FR-017, FR-018).
- **SC-006**: No step, argument or path touches the Docker VM or mounts the socket; the socket is only connected to (FR-002).
- **SC-007**: Git move fixtures give the specified outcome per location class and per refusal; the effective key/value sequence is equal
  before and after; a failure restores the original bytes; preview, confirmation, errors and logs reveal only structural keys/redacted paths, never the git-value canary; lock file or another run refuses (FR-014).
- **SC-008**: An identical rerun leaves installations, managed configuration/profile and backups unchanged, while confirmed protocol/lock bookkeeping still occurs; injected failure, timeout and interrupt give exits 73, 71, 72 with completed and remaining steps
  listed and a correct rerun (FR-020, FR-021).
- **SC-009**: Unattended runs never prompt; `--yes` does not approve modify-class steps; exit 70 names the flags; exit codes equal FR-020 and
  the JSON envelope validates and contains no home path, user name, host name, environment value or git value (FR-017, FR-020).
- **SC-010**: Plain `doctor` and `--dry-run` leave an empty filesystem difference and run no sudo (the fake network port may be used);
  `--fix` and the wizard yield the same plan hash for equal state (FR-015, FR-016, FR-018).
- **SC-011**: A real run's protocol has the FR-019 records, modes 0600/0700, is append-only, holds no canary password, token, git value or
  environment content, is never deleted, and a second concurrent run is refused (FR-019).
- **SC-012**: The catalogue schema loads with the Go loader; the shipped catalogue has the initial items and a minimum version per tool (FR-022).
- **SC-013**: Linux `setup` reaches the T015 step list, other platforms are refused before any change, and `doctor --fix` on Linux is refused
  (FR-001).
- **SC-014**: All tests pass offline with `go test` and `python3 scripts/check_repo.py`; the per-PR diff of frozen paths is empty except the
  named gated tasks (FR-023, FR-024).
- **SC-015**: The shipped catalog has the Colima entry of FR-026 and the Docker client entry of FR-033 (15 entries; 18 with Feature 011), each: `selected_by_default` false,
  Homebrew formula from `homebrew/core`, platforms darwin only, no `config`, no privileged step; the schema and the loader accept it with no
  change to the `source.tap` enum; `yes` without selecting it plans no Colima step; selecting it plans one `brew_install` (create class)
  and re-detection afterwards; fake `brew` calls show no `--ignore-dependencies`, no tap and no other formula name than the entry's.
- **SC-016**: No plan step, argument or path starts, stops, configures or deletes a Colima profile or Lima VM, edits `~/.colima` or the Docker
  client configuration, or mounts a socket (FR-027, SC-006); the fake exec recorder fails on any such call. With a fake socket only at
  `~/.colima/default/docker.sock` and that endpoint selected by the option or the config value, `container.socket` passes and the consumers of FR-027 name that same endpoint as `--host=<endpoint>`, with no
  fallback to `/var/run/docker.sock`.
- **SC-017**: With a fake endpoint `~/.colima/<profile>/docker.sock` and a fake `colima.yaml` in that directory, the Colima source of FR-028
  reads the file only (no process, no API call), reports the `disk` and `rootDisk` values as information, and measures the fake free bytes of
  the volume that holds the Colima data directory; for a non-Colima endpoint it does not read any Colima file; the wizard records the location only
  when the file exists (Feature 011 SC-036).

- **SC-018**: With fakes for the option, the host profile, `DOCKER_HOST` and the standard socket, the selection function yields the option, else the config value, else `DOCKER_HOST`, else the standard socket (EP-1); a specified but
  unreachable or non-`unix://` source is reported and never replaced by a lower source or a candidate; a configured value stays in force when the standard socket appears or disappears; no process starts, no directory is listed and no Docker
  context is read by the selection (FR-029, D-12).
- **SC-019**: With no option, no config value, `DOCKER_HOST` unset and no standard socket, the investigation dials only the fixed candidate paths and reads context metadata files (no `docker` command, no daemon call, no TLS or key file, no
  `~/.colima` listing), lists options with context names and lists non-`unix` contexts as "not supported" with the reason, never offering them; the question and the preview go to standard error; an answer is recorded as one `profile_write` with `socket` and
  optional `context`; `skip` records an opt-out; no terminal, `--json` or `--non-interactive` with options found and no answer exits 70 naming `--container-endpoint`; end of input exits 72; nothing found asks nothing (FR-031).
- **SC-020**: Ordinary commands with no usable endpoint never prompt, run no `docker` call and exit 68 with the `whr:` line; a malformed option value exits 64; no new exit code, refusal code or step kind exists (FR-032, EP-3).
- **SC-021**: Doctor reports each drift of D-13 (g) as a warning in the evidence and in a `whr: warning` line, never changes a status for it, never switches the endpoint, reads context files only within the shared 5 s budget and never reads them to choose; re-recording is a
  modify-class `profile_write` that needs `--confirm container-endpoint` (FR-032).
- **SC-022**: The shipped catalog has the `docker` entry (not preselected, formula `docker` from `homebrew/core`, darwin only, no `config`, no privileged step, no `requires`); with Colima selected and no Docker client found, the plan, report and `host.tool.colima` evidence name it and
  install nothing; a client from another source counts as found (FR-033).
- **SC-023**: A catalog entry that was never selected has no profile record; `customize` declining it records nothing; `setup` offers it unchecked on each run; `host.tool.<id>` and dependent checks are `skipped` with the reason `unselected`, never `fail` (FR-034).

## Assumptions

- Feature 006 T025 (foundation), T027 (local doctor), T031 (terminal harness) and T032 (wizard framework) and Feature 008 T005 (git location
  check) are available when the tasks that need them start; if not, work stops and asks.
- Homebrew and OrbStack facts (cask names, prefixes, Cellar and Caskroom layout) are measured on the owner's Mac in T002 and T003 (OG-1).
- DP-1 to DP-5 are confirmed by H38; the plan decision matrix records every consumer. AP-6 reason/result placement remains unselected and blocks only affected JSON-result/schema integration and registration; it does not block independent pure functions or source-only fixtures. AP applications and real Mac measurements keep their separate gates.

## Colima addition: changes to existing documents and conformance cases (D-10, D-11; spec-only, nothing below is implemented)

Catalog count: Colima (FR-026) and the Docker client (FR-033) are two entries more than the catalog holds when they are implemented: 15 on the Feature 009 text alone (13 initial entries),
18 once the Feature 011 FR-040 entries are in (Feature 011 counts 16 itself: the 13 initial entries, the VM tool and two more). The reserved items `git-config` and `container-endpoint` keep being
no catalog entries: the sentence "it is not a fourteenth catalogue entry" in `wizard-flow.md` becomes "sixteenth" (or "nineteenth" after Feature 011).

Changes on approval, each through the plan and tasks, with the owner gates of FR-024 where they name frozen paths:

- `specs/009-host-setup-wizard/` : `spec.md` (this text; FR-010 keeps "initial catalogue has exactly 13 entries", the additions are named), `plan.md`
  (decision matrix row for D-10 and D-11, requirement-to-evidence rows for FR-026 to FR-028), `tasks.md` (a catalog task, a wizard-flow and
  recording task and the OG-1 measurement task; T001 is already checked, so this reopens the catalog work as a new task, not an edit of T001),
  `contracts/wizard-flow.md` (report line for starting the runtime; the "fourteenth" sentence, now "sixteenth"), `data-model.md` (HostProfile "No paths or
  values" widened for the storage record; Terms "Docker VM"), `checklists/requirements.md`, `analysis.md`, `quickstart.md` and `HANDOFF.md` (entry counts, "13 entries"); the places that now say 13 or "exactly 13" are
  listed in the Feature 011 amendment (D-9, `plan.md` OD-2, `tasks.md` T001, `quickstart.md`, `HANDOFF.md`, `checklists/requirements.md`) and are reopened by one new implementation task.
- `contracts/host-tool-catalogue.schema.json`: no property or enum change for the Colima entry; `x-initial-items` and
  `x-initial-defaults.selected_by_default_false` gain the entry (or a new "added items" list, if the owner wants "initial" to keep its meaning).
- `apps/whr/internal/hostsetup/catalogue/assets/catalogue.json` (two new entries, `colima` and `docker`; `catalogue_version` bump), `apps/whr/internal/hostsetup/catalogue/production_test.go` (the want-count of 13 items becomes 15, 18 with Feature 011) and its loader tests,
  `TestInitialCatalogueEntries` (exact production ids, defaults and minima: reopened), the detection fixtures for a Homebrew-managed
  `colima` and `docker` formula paths, and the quality record `docs/quality/009-host-setup-wizard/issue176-catalogue-tests.md`.
- `contracts/cli/doctor-checks.json` and `TestDoctorChecksMatchContract`: the `disk.container_storage` text (lookup order, no API) and the
  5 s budget rule (file read, filesystem call); the socket candidate list changes meaning (default search becomes the investigation list), see the section on D-12 and D-13 below. `specs/006-whr-cli/spec.md` FR-060: the same two
  statements, and its `disk.container_storage` sentence. Both are owner-gated (FR-024).
- `docs/container-runtime.md`: name Colima as a supported alternative next to OrbStack (owner-gated, FR-024); `README.md`/`docs` mentions of "OrbStack" as the
  only runtime, if any, are checked when this lands.
- `specs/008-workspace-sandbox/spec.md`: wording only, "Docker VM ... (for example OrbStack)" gains Colima; no requirement changes
  (Feature 008 has no socket path or runtime list).
- `specs/011-vm-provider/spec.md`: this change already adds FR-041, SC-036, decision 21 and the FR-039, decision 13 and changes-list edits for the
  storage lookup. Its count sentences (FR-040 "16 entries", SC-035, decision 12, the "seventeenth" sentence and the bullets "now 13 of 16" and "loader tests become 16" of its Feature 009 amendment)
  count only the entries of Feature 011 and now say so and give the combined total in a parenthesis (18 entries, "nineteenth", "13 of 18", loader tests 18, with Colima and the Docker client); the sentences are final text when the implementation tasks land. FR-034 and FR-037 gain the explicit `--host=<endpoint>` for the build if P-2 is accepted.
- `specs/012-container-interactive-shell/spec.md`, `specs/010-host-and-workspace-panes/spec.md`: see the section on D-12 and D-13 below.
- `apps/whr/internal/doctor/` (`socket.go`, tests): reopened by D-12 and by the storage lookup, see below.

Conformance cases (fakes only; FR-023):

| Case | Setup | Expected |
| --- | --- | --- |
| CC-1 catalog entry | shipped catalog | Colima present, `kind` runtime, Homebrew core formula, darwin only, not preselected, no config, no privileged step; schema and loader accept it unchanged; `orbstack` still the only preselected runtime |
| CC-2 yes plan | `yes` with Colima absent | no Colima step; with Colima selected, one `brew_install`; no `--ignore-dependencies`, tap or other formula name |
| CC-3 detection | formula path under Cellar; version at, above, below minimum; shim-like or unparseable output | `present_ok`, `present_outdated`, `present_unverifiable`; another source is never replaced |
| CC-4 boundary | any run | no `colima start/stop/delete/ssh/template`, no write under `~/.colima`, no Docker context change, no socket mount (SC-016) |
| CC-5 endpoint | socket only at `~/.colima/default/docker.sock`, selected by option or config value | `container.socket` passes; checks, `whr shell` and the image build use that endpoint as `--host`; no fallback to `/var/run/docker.sock`; without the selection the default fails (CC-15) |
| CC-6 two runtimes | OrbStack and Colima sockets both accept, `DOCKER_HOST` unset | the standard socket decides (D-12 (a)); no documented order; the evidence names the source (replaced by CC-12) |
| CC-7 profile | endpoint `~/.colima/work/docker.sock` selected by option or config value | used as is; never probed or found by default (replaced by CC-19) |
| CC-8 storage config first | `colima.yaml` and a differing recorded value | file wins; evidence names the source; `disk` shown as information only; free space from the host volume; no API call |
| CC-9 storage fallback | file absent, recorded value present, then neither | recorded value used; then `unknown` with a reason; never `fail` for a missing location |
| CC-10 recording | wizard run with and without `colima.yaml` | recorded with preview only when the file exists; `doctor` and `doctor --fix` record nothing; a second `setup` run records after the first start |

## Endpoint selection (D-12, D-13) and catalog additions (D-14): changes to existing documents and conformance cases (spec-only, nothing below is implemented)

Changes to existing documents, on approval, each through the plan and tasks, with the owner gates of FR-024 (and OG-4 to OG-6) where they name frozen paths. This change itself edits only the `spec.md` files of Features 009, 010 and 011.

- `specs/006-whr-cli/spec.md` (owner-gated): FR-060 `container.socket` (today "when `DOCKER_HOST` is set it is the only candidate ... otherwise it discovers ... from the documented candidates") gains the selection rule of FR-029, the evidence
  `source` values, the investigation of FR-031 for `setup` and the drift warnings of FR-032; its 5 s budget sentence ("each socket dial, `git --version` and `daemon.json` read draws on it") also names the context file reads; acceptance scenarios that mention
  `DOCKER_HOST` (the `unknown` case for a non-`unix` scheme stays) and the sentence about `ls --local` sharing the discovery (FR-066) are checked. Command surface: `--container-endpoint` for `setup` (darwin only), `doctor`, `ls --local` and `shell --local`;
  the principle of FR-045 (every choice has a flag, prompts on standard error, no prompt without a terminal) and FR-014 are applied, not changed. No exit code changes (ADR 0013 is untouched).
- `contracts/cli/commands.json`, `apps/whr/internal/commands/entries.go` and their freeze tests (`TestCommandTableMatchesContract`, `TestLocalShellContractAmendment`, `TestLocalLsContractAmendment`): the flag lists of `host.setup`, `host.doctor`, `workspace.ls` and `workspace.shell`.
- `contracts/cli/doctor-checks.json` and `TestDoctorChecksMatchContract`: `socket_candidates` is split into the default (`DOCKER_HOST`, then `/var/run/docker.sock`) and the investigation list (the remaining entries, not tried by default); `container.socket` texts (pass, fail, unknown), the evidence `source`
  values and the drift warnings; the 5 s budget rule names the context file reads. `apps/whr/internal/doctor/socket.go` (`candidateTemplates`, `CandidateOrder`, `ResolveLocalSocket`), `apps/whr/internal/localdocker/endpoint.go` and their tests: one selection function and the investigation.
- `specs/009-host-setup-wizard/`: `contracts/wizard-flow.md` (the question after detection, the flag `--container-endpoint`, the accepted ids gain the reserved `container-endpoint`, exit 70 and 72 lines, the "not a ... catalogue entry" sentence), `contracts/refusal-codes.json`
  (the `host_setup.unknown_item` text names the reserved id; no new code), `data-model.md` (HostProfile gains `container_endpoint` with `socket` and `context`; "No paths or values" widened; the profile stores nothing for never-selected entries), `plan.md` (decision matrix rows for D-12 to D-14,
  evidence rows for FR-029 to FR-034, the OG-1 results), `tasks.md` (new tasks: selection function, investigation and recording, drift reporting, `docker` entry; T001 stays checked), `checklists/requirements.md`, `analysis.md`.
- `contracts/host-tool-catalogue.schema.json` (no property change; `x-initial-items`/`x-initial-defaults` gain the two entries or an "added items" list), `apps/whr/internal/hostsetup/catalogue/assets/catalogue.json`, `production_test.go` (want-count 15, then 18), loader tests, `TestInitialCatalogueEntries`.
- `specs/010-host-and-workspace-panes/`: `spec.md` FR-002, scenario 8 and Dependencies are amended by this change (selected endpoint; read-only context detection explicit); `research.md` ("retains `DOCKER_HOST`, candidate order"), `contracts/local-cli.md` ("Socket admission reads only `DOCKER_HOST` and
  documented doctor candidates") and `docs/quality/010-host-and-workspace-panes/p1.md` follow in the implementation task.
- `docs/container-runtime.md` (owner-gated): "Local workspace discovery" ("an explicit `DOCKER_HOST` wins; otherwise the documented doctor candidates are tried in order ... Docker contexts are not consulted") becomes the selection rule with the option and the config value, and states that contexts are read for
  detection and drift only; `docs/design/local-devcontainer-lifecycle.md` line 62 ("same socket and `DOCKER_HOST` rules") is checked.
- `specs/011-vm-provider/spec.md`: this change edits the wording of FR-039 and FR-041, adds decision 23, states the unselected-entry rule in FR-040 and gives the combined entry counts in a parenthesis (see the Colima section above).
- `specs/012-container-interactive-shell/spec.md`: its wording "discovered `unix://` endpoint" (step 5 and the exec plan) means the selected endpoint (Terms); a wording edit follows with the implementation. `specs/008-workspace-sandbox/spec.md`: none (ownership and access checks apply to local sockets only, D-13 (h)).

Conformance cases (fakes only; FR-023):

| Case | Setup | Expected |
| --- | --- | --- |
| CC-11 default `DOCKER_HOST` | `DOCKER_HOST` names a reachable socket, the standard socket also exists | `DOCKER_HOST` used; evidence source `DOCKER_HOST`; no candidate dialed |
| CC-12 standard socket | `DOCKER_HOST` unset; the standard socket links to runtime A; runtime B socket also present | A used (M-6); B never probed; no documented order |
| CC-13 precedence | all four sources set to different sockets, then removed top-down | option, config value, `DOCKER_HOST`, standard socket in turn (EP-1); an unreachable specified source gives `fail`/68 with no fallback |
| CC-14 authoritative config | config value set; the standard socket appears, later disappears | config value used throughout; drift warning while it differs; nothing switches or is rewritten |
| CC-15 investigation | no source usable; sockets at three candidate paths; context files for two local and one `ssh://` context | read-only dials in order, context metadata read, options listed once with context names, the remote context shown as not supported with the reason and not offered; question on stderr; answer 2 gives one previewed `profile_write` with `socket` and `context`; no process, no `~/.colima` listing, no TLS file opened |
| CC-16 unattended | as CC-15 without a terminal, with `--json`, with `--non-interactive`; with end of input; with `skip`; with nothing found | 70 naming `--container-endpoint`; 72; opt-out recorded and no re-ask; no question and no 70 |
| CC-17 ordinary commands | `ls --local`, `shell --local` with no endpoint; malformed option; `tcp://` option | no prompt, no `docker` call; 68; 64; 68 |
| CC-18 drift warnings | stored endpoint unreachable; stored context deleted; stored context now elsewhere; active context or `DOCKER_HOST` differs | one warning each in evidence and on stderr; status unchanged by the warning; no switch; `doctor --fix --confirm container-endpoint` re-records; `--yes` alone does not |
| CC-19 profile by selection | option naming `~/.colima/work/docker.sock` with a fake `colima.yaml` beside it | endpoint used; FR-028 derives profile `work`; no directory listing |
| CC-20 second Colima socket | `~/.colima/docker.sock` selected | works as an endpoint; FR-028 finds no profile, so recorded value else `unknown` (OQ-6) |
| CC-21 Docker client message | Colima selected, no `docker` found; then a client from another source | message names the `docker` entry and installs nothing; then no message |
| CC-22 unselected entries | entries never selected; `customize` declines one; plain `doctor` | profile has no record for them; they are offered unchecked next run; `skipped` with reason `unselected`, never `fail` |
