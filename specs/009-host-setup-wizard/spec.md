# Feature Specification: Host setup wizard and doctor repair

**Feature identifier**: `009-host-setup-wizard`
**Feature Branch**: `docs/spec009-host-setup-wizard`
**Created**: 2026-10-20
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md). Owner requirements and decisions of 2026-10-20 are recorded as D-1 to D-9; OD-1 to OD-5 are confirmed
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
- **Docker VM**: the Linux VM of the container runtime (OrbStack). `whr` never changes it.
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

OD-1 to OD-5 are in the [plan](plan.md#owner-decisions), confirmed 2026-10-20.

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
4. **Given** a completed run, **When** the identical run repeats, **Then** it makes no mutating call, no backup and no file change.

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
- **FR-002**: `whr` MUST NOT change the Docker VM or mount the Docker socket (D-3); the only interaction is connecting to a discovered socket
  and closing it (Feature 006 FR-060). Plan steps have a closed set of kinds ([data model](data-model.md#step-kinds)); none touches the VM.
- **FR-003**: `setup` MAY install a container runtime only when the catalogue lists it and the owner has not opted out (R-40 superseded,
  OD-5). Plain `doctor` installs nothing. `whr` never starts, configures or signs in to the runtime application.

**Wizard and plan**

- **FR-004**: Detection MUST precede the plan and every question. The wizard shows the plan, then asks one question with the answers `yes`,
  `customize`, `no` ([wizard flow](contracts/wizard-flow.md)). `yes` applies the displayed create-class plan; modify-class steps are asked one
  by one (default no); `no` exits 0 with no write; end of input or interrupt exits 72.
- **FR-005**: `customize` asks per item and allows opt-out of every item, including the runtime and git. Every skip is recorded as an opt-out in
  the host profile; an opted-out item is never installed, upgraded, written or repaired by `setup` or `doctor --fix`; a rerun preselects it
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
  dotfile change, a `config` marker (`dotfile` or `shell_hook`). The initial catalogue has exactly 13 entries. Preselected core: `orbstack` (cask), `git`, `gh`, `jq`, `fish`, `starship`; preselected recommended: `ripgrep` (rg), `fd`, `fzf`, `zoxide`, `bat`, `jj`, `herdr` (all 13 entries preselected, each individually opt-out under FR-005). Entries needing a dotfile or shell hook: `starship`, `fzf`, `zoxide`, `fish`. Nothing else from the toolbox goes on the host (no scanners, linters, language tools, hugo, typst, delta, lazygit, direnv, mise, uv, tmux). More entries are added deliberately by the owner (OD-2, OG-7). The wizard shows the diff of every config change; it never touches git configuration (except the guided move of FR-014) or the login shell. It MUST NOT contain an entry that installs, configures or
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
  `legacy_home_file` and `split`, needs explicit confirmation and a backup, and proceeds by staging the new content in a temporary file next
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
  (`host_setup.run_locked`, 68). It writes one protocol in `<state>/protocols/` (DP-1): a JSON Lines file `<run-id>.jsonl` and a readable
  `<run-id>.txt`, mode 0600 in a 0700 directory, append-only, created exclusively without following links. Records: time (UTC), run id,
  platform, catalogue version, the selection (answer, opt-outs), per step kind, item, exact argv, whether sudo and network were used, exit
  status and result, backup paths with SHA-256, and for dotfiles the diff size. It never contains a password, token, git configuration value
  or environment contents. `whr` never deletes protocols and warns on standard error when the directory holds more than 500. A `no` answer,
  `--dry-run` and plain `doctor` write no protocol and take no lock. A run without a writable protocol does not mutate
  (`host_setup.protocol_unwritable`). The JSON journal is the single record of a run; the text file renders it.
- **FR-020**: With `--json` the result is one envelope of ADR 0013/0014 and Feature 006 FR-015 (`host.setup` or `host.doctor`), prompts and
  previews on standard error, evidence redacted as in Feature 006 FR-060. Exit codes use only the ADR 0013 table ([flow](contracts/wizard-flow.md#exit-codes)):
  0 success or `no`; 64 usage; 68 refusal, unsupported platform, root start, run lock, blocked or failed with nothing applied, failed doctor
  check; 70 needs-human (unattended without confirmation, privileged step without terminal); 71 deadline with nothing applied; 72 interrupt or
  cancellation; 73 partial failure (something applied, something not; completed and remaining steps listed; precedence over 72, 71, 68); 74
  internal, including an invalid catalogue.
- **FR-021**: A rerun with the same selection is a no-op (no mutating call, no backup, byte-identical files). Interruption or failure leaves
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

### Key Entities

CatalogueItem, DetectionResult, Plan, Step, HostProfile, BackupRecord, Protocol ([data model](data-model.md)).

## Success Criteria *(mandatory)*

- **SC-001**: For every item state the plan has exactly the expected steps; detection and `--dry-run` make zero mutating calls; the CLT `git`
  shim fixture is never executed (FR-004, FR-006, FR-007, FR-018).
- **SC-002**: `yes`, `customize` and `no` give the expected plans and writes; every item can be deselected; opt-outs persist and are honored by
  `setup` and `doctor --fix` (FR-004, FR-005).
- **SC-003**: For every existing-target fixture with different content, nothing is written without the per-item confirmation, bytes are
  identical after a refusal, and a verified backup precedes every replacement (FR-008, FR-013).
- **SC-004**: The fake exec recorder shows only allowed fixed argv and environment, no shell, no `brew` under sudo, no forbidden flag or
  other tap, no downgrade, and refusal to start as root (FR-009, FR-011).
- **SC-005**: With a fake sudo, a privileged step runs only after its exact command was shown and confirmed, only with a terminal, never
  receives or logs a password canary, is followed by `sudo -k`, is blocked with exit 70 otherwise (in `setup` and `doctor --fix`), and never
  runs in `--dry-run` or plain `doctor` (FR-011, FR-017, FR-018).
- **SC-006**: No step, argument or path touches the Docker VM or mounts the socket; the socket is only connected to (FR-002).
- **SC-007**: Git move fixtures give the specified outcome per location class and per refusal; the effective key/value sequence is equal
  before and after; a failure restores the original bytes; no value is printed; lock file or another run refuses (FR-014).
- **SC-008**: An identical rerun is a no-op; injected failure, timeout and interrupt give exits 73, 71, 72 with completed and remaining steps
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

## Assumptions

- Feature 006 T025 (foundation), T027 (local doctor), T031 (terminal harness) and T032 (wizard framework) and Feature 008 T005 (git location
  check) are available when the tasks that need them start; if not, work stops and asks.
- Homebrew and OrbStack facts (cask names, prefixes, Cellar and Caskroom layout) are measured on the owner's Mac in T002 and T003 (OG-1).
- The owner confirms DP-1 to DP-5 before the tasks they affect start; the tasks name them.
