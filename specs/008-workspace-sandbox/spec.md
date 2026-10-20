# Feature Specification: Workspace and sandbox execution model

**Feature identifier**: `008-workspace-sandbox`
**Feature Branch**: `docs/spec008-workspace-sandbox`
**Created**: 2026-10-20
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md). Research input: `IDEAS_NOTES.md` sections 4 to 11 of the Anchorage umbrella repository (branch
`docs/ideas-codex-delivery-plan`, unmerged; not in this repository; research, not requirements; the points this feature relies on are
stated in this specification).
**Related**: [Feature 006](../006-whr-cli/spec.md) (FR-030 of Feature 006, FR-066 to FR-069, T038, T039),
[ADR 0009](../../docs/adr/0009-in-container-ai-agents.md), [ADR 0013](../../docs/adr/0013-whr-exit-codes.md),
[ADR 0014](../../docs/adr/0014-whr-command-structure.md), [Feature 007](../007-bundledock-shell-policy/spec.md).

## Context and scope

Feature 006 specifies `whr` for durable workspaces. It has no model for disposable or less-trusted execution next to a
workspace, and no statement of what a container backend does and does not protect. This feature defines:

1. a durable **Workspace** versus explicit **Execution Instances** (sandboxes) with three **trust profiles**;
2. a **RuntimeProvider** capability contract with explicit refusal of unavailable protections;
3. a **Git configuration profile** per environment (owner decision, 2026-10-20);
4. the threat model and its canary acceptance tests;
5. a **proposed** command surface; and
6. a first implementable increment that is library-level and offline.

### Terms

- **Mac host**: the owner's macOS machine, which owns `~/.gitconfig` and `~/.config/git`. Which `whr` command acts there is open (OD-5).
- **Docker VM**: the Linux VM of the container provider (for example OrbStack). `whr` never changes it and only reads and
  verifies from inside.
- **Sandbox backend**: the per-job isolation chosen through a RuntimeProvider: a container sharing the Docker VM kernel
  (this increment) or, later, a microVM with its own guest kernel. Backends differ only in the capabilities they declare.

### Boundaries

- FR-030 of Feature 006 is unchanged: `whr` does not install, configure, authenticate, launch or wrap an agent's native
  tool and does not receive or relay provider credentials. Running an agent is an external-runner concern
  ([command surface](contracts/command-surface.md)). A change to Feature 006 FR-030 is a separate proposal; none is made here.
- ADR 0009 stays: agents hold no host-agent credentials, control sockets or other workspaces' data.
- Not in this increment (each needs its own specification): real Docker execution, host mutation, any microVM provider,
  credential mediation, egress enforcement, host tool gateway, snapshots and rollback, IDE attachment verification.
- Command registration: Stage 1 registers no command; task T016 (Stage 2, owner-gated) registers the `sandbox` group after the
  ADR 0014 amendment (T017).
- No claim is made of universal containment, exfiltration prevention, performance or portability. Every protection is
  stated with its scope and residual risk.

### Owner decisions

- **2026-10-20 (git configuration profile)**: the developer sandbox uses the host global git configuration directly,
  read/write and always in sync; the agent profile is declared by WorkHarbor policy and never derived from the host.
  Details in FR-019 to FR-025.
- **2026-10-20 (Mac host, owner statement)**: "whr setup arbeitet auf dem Mac host, der Linux VM host wird von whr nie
  veraendert." The Docker VM (the Linux VM of the container provider, for example OrbStack) is never changed by `whr`: no
  daemon or VM settings, kernel or VM-level mounts; inside the Docker VM and the sandbox backend `whr` only reads and verifies
  (preflight). The global git configuration check, and the optional move to `~/.config/git`, belong on the Mac host: the check
  always runs and is a read-only report; the move is only a guided step with a backup and explicit confirmation, never silent.
  Which command carries them is open (OD-5; recommended: a read-only `doctor` section plus a separate guided move): Feature 006 FR-009/FR-026 and T015 define `whr setup` as Linux/systemd host
  enrollment of the host that receives the host agent, and T015 mutates that host; the owner statement concerns the Docker VM,
  not T015's host. This feature does not change T015 or FR-009.
- **2026-10-20 (backend neutrality)**: microVMs are treated like containers later. One sandbox lifecycle, one command
  surface and one profile, ownership, retention and cleanup model serve every RuntimeProvider; a microVM is another provider
  that declares other capabilities (its own guest kernel), not a separate concept or command family. A protection the
  selected provider does not declare is refused; there is no fallback and no provider-specific behavior hidden from the
  contract. Providers beyond the container provider are separate provider specifications.
- **2026-10-20 (PR target)**: pull requests for this feature target `main`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Developer sandbox with the host git configuration (Priority: P1)

A developer creates a `developer` sandbox for a workspace. Git inside it behaves as on the host: the host global
configuration is the same file set, read/write, with no copy.

**Independent Test**: fake provider; fixture host config directory with canary keys; the plan mounts the directory (not a
file), preflight proves read and write, and a key set inside is visible in the host fixture.

**Acceptance Scenarios**:

1. **Given** a host config at `~/.config/git/config`, **When** a developer sandbox is prepared, **Then** the mount plan
   contains the containing directory read/write and no single-file git config mount.
2. **Given** `credential.helper = osxkeychain`, **When** the check runs, **Then** it reports that key, its origin file and
   a remedy, never its value, and the environment is still reported ready.
3. **Given** a read-only or lock-failing mount (`EBUSY`), **When** preflight runs, **Then** the instance is not ready and
   the failure is named.
4. **Given** only `~/.gitconfig` exists on the host, **Then** the check reports `legacy_home_file` with the guided-move
   remedy and performs no change.

### User Story 2 - Untrusted agent job with a private copy (Priority: P1)

An operator or external runner creates an `untrusted_job` sandbox. It gets a private copy and a scratch home, never the
primary refs, config, hooks, host global git configuration or credentials, and its work leaves only as a patch.

**Independent Test**: fake provider records the mount plan and environment; canary fixtures for every asset of FR-027
are absent from the plan; export produces a patch and manifest, and nothing is pushed or applied.

**Acceptance Scenarios**:

1. **Given** a dirty workspace, **When** the copy policy runs, **Then** only the selected paths are included and ignored,
   untracked and secret-pattern paths are excluded unless named explicitly.
2. **Given** an agent profile, **Then** the instance has the declared deterministic configuration, no host global
   configuration, and commits fail unless the operator supplied `GIT_AUTHOR_*` and `GIT_COMMITTER_*`.
3. **Given** the instance finished, **When** results are exported, **Then** a patch and manifest exist, the primary
   checkout is unchanged and no remote operation occurred.

### User Story 3 - Unavailable protection is refused (Priority: P1)

A request names a protection the provider cannot enforce. It is refused with a stable code; nothing is created.

**Independent Test**: admission table of profile, requirement and provider capabilities against expected refusal codes.

**Acceptance Scenarios**:

1. **Given** `kernel_isolation=microvm` and a container provider, **Then** the request is refused with
   `refused.kernel_isolation_unavailable` and no container instance is created.
2. **Given** `egress=enforced_allowlist` and a provider reporting `none`, **Then** it is refused.
3. **Given** a capability value `unknown`, **Then** it never satisfies a requirement.

### User Story 4 - Cooperative job in a linked worktree (Priority: P2)

A trusted cooperative job uses a linked worktree as an explicitly trusted convenience, with the shared Git control data
stated as a residual risk. In this increment the operator supplies an existing linked worktree; `whr` never runs
`git worktree add`, `remove` or `prune` (FR-011).

**Independent Test**: creation without the explicit acknowledgement is refused; with it the label and result record carry
`shared_git_metadata=writable`; the common git directory is mapped at its host absolute path.

### User Story 5 - Cancel, keep, discard, clean (Priority: P2)

**Independent Test**: fake provider with injected failures; leftovers are listed, retained results survive, no volume
outside the instance's own labelled set is touched.

### Edge Cases

- Foreign or unlabelled container or volume: refused (68), zero mutations.
- Host config absent, split between `~/.gitconfig` and XDG, or a symlink: reported, never created or altered by a sandbox.
- `includeIf` rules with host paths: reported as skipped in the container; unreadable includes are findings, not silence.
- Preflight crash between set and unset of the probe key: the next preflight reports and removes the stale key.
- Result export of binary files, submodules or an empty diff: manifest states each case; an empty diff is a result.
- Provider reports a capability it cannot verify: `unknown`; refusal applies.
- The instance outlives its creator: labels, not process state, identify it.

## Requirements *(mandatory)*

### Functional Requirements

**Model**

- **FR-001**: A Workspace is the durable identity of a repository, its `workharbor.json` and its devcontainer
  configuration, with its human environment. A sandbox operation (create, exec, keep, export, discard,
  clean) MUST NOT delete or replace the workspace checkout, the primary home volume or tool volumes in any profile. Writes of an
  instance to the checkout, refs, hooks or home are limited to what its profile mounts writable (FR-003, FR-010, FR-011,
  data-model profile table): `developer` mounts checkout and home read/write by design, `cooperative_job` shares repository
  metadata, `untrusted_job` reaches none of them.
- **FR-002**: An Execution Instance (sandbox) MUST exist only by explicit creation. Its id is `sbx-` followed by 16
  lowercase base32 characters from a CSPRNG (injectable in tests), unique, never reused. An optional name is unique among
  non-discarded instances of one workspace.
- **FR-003**: Each instance MUST have exactly one trust profile fixed at creation: `developer` (trusted human or
  developer), `cooperative_job` (linked worktree, explicitly trusted convenience) or `untrusted_job` (private clone or
  export plus scratch home). The profile follows requested capabilities and the policy of
  [data-model](data-model.md); "human" and "agent" are typical uses, not the policy.
- **FR-004**: Lifecycle states are `preparing`, `ready`, `running`, `finished`, `failed`, `discarding`, `discarded` and
  `cleanup_incomplete`, with the transitions of the data model. A request invalid in the current state MUST fail with
  conflict and change nothing.
- **FR-005**: Every container, volume and directory of an instance MUST carry the ownership labels of the data model.
  Every mutating operation MUST verify them first; an unlabelled or foreign object is refused with zero mutations,
  consistent with FR-067 of Feature 006.
- **FR-006**: Retention is `ephemeral` or `kept`. `keep` retains the instance and its results for review; it MUST NOT
  imply merge, push, publication or acceptance. `clean` MUST NOT remove a `kept` instance.
- **FR-007**: A finished `cooperative_job` or `untrusted_job` MUST export its result as a patch (binary-safe, relative to a
  recorded base commit) plus a manifest (instance id, profile, base commit, base snapshot hash recorded at create, result tree hash, export
  time, exit status, content hashes, copy policy, shared-metadata flag) in the instance's result directory. Export is also allowed for a `failed` (cancelled, crashed) instance and
  the manifest then states `partial=true`. Export MUST NOT push, merge, apply to the primary checkout or run hooks. A
  successful process exit MUST NOT be recorded as acceptance. Because the instance's `.git` and attributes are controlled by
  the instance, export MUST NOT run git against an instance-writable repository on the host with that repository's
  configuration trusted: either the patch is produced inside the instance and copied out as data, or the host copies the
  instance tree out without `.git` (symlinks kept as symlinks, never followed) and computes
  `git diff --no-index --binary --no-ext-diff --no-textconv` against the base tree exported from the primary at the base
  commit, with `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_OPTIONAL_LOCKS=0`, no pager and a fresh
  environment, run from an empty working directory outside any repository with `GIT_CEILING_DIRECTORIES` set to its
  parent, so no enclosing repository's configuration or attributes are found. The base snapshot hash (a sha256 over sorted
  path, mode and content hash of the base tree) is recorded at create; the result tree hash is the same listing over the
  exported instance tree. The result directory is written without following symlinks (no-follow open of every component, exclusive
  create, identity check).
- **FR-008**: `discard` MUST refuse (conflict) while results exist that were not exported to an explicit destination,
  unless the operator confirms results deletion; unattended use without confirmation is needs-human.
- **FR-009**: A private copy MUST follow an explicit copy policy: tracked files at the base commit; selected dirty
  tracked changes only when named; untracked and ignored paths only when named; paths matching secret patterns
  (`contracts/runtime-provider.md`) refused unless named and confirmed; submodules excluded unless requested, then copied
  at their pinned commit without shared metadata, and a dirty submodule is refused.
- **FR-010**: An `untrusted_job` MUST NOT receive shared writable Git metadata (no linked worktree, no alternates, no
  shared object store), the host Docker socket, the primary `.git/config` or hooks, or the host global git configuration.
  The copy's `.git/config` is generated and MUST NOT carry remotes with credentials, `credential.*`, `url.*.insteadOf`
  or include paths from the primary.
- **FR-011**: A `cooperative_job` MUST require an explicit acknowledgement that the worktree shares repository metadata,
  refs, hooks and the primary `.git/config` (including any credentials or `url.*.insteadOf` rules in it); the instance record,
  labels and manifest MUST carry `shared_git_metadata=writable`. In this increment the linked worktree is supplied by the
  operator; `whr` MUST NOT run `git worktree add`, `remove` or `prune` (they write primary `.git/worktrees`). The worktree's
  `.git` file holds an absolute host path, so the worktree and its common git directory MUST be mounted at their host
  absolute paths, and `discard` removes only the container and scratch home, never the worktree.

**Backend capability table**

- **FR-012**: A RuntimeProvider MUST report capabilities for: kernel isolation; filesystem and Git access; git
  configuration profile per environment; home state; tool mounts; egress enforcement; credential mediation; host tools;
  IDE access; cancellation and cleanup, each with a scope and a residual-risk statement
  ([contract](contracts/runtime-provider.md)).
- **FR-013**: Admission MUST be a pure function of request and capabilities. A request whose required protection is not
  `enforced` MUST be refused with a stable code from [refusal-codes.json](contracts/refusal-codes.json) before any
  provider side effect. There is no downgrade, no fallback and no "best effort" for a stated requirement.
- **FR-014**: A request for `kernel_isolation=microvm` MUST NEVER be satisfied by a shared-kernel container provider.
  Provider selection MUST NOT substitute another provider kind implicitly.
- **FR-015**: Capability values are `enforced`, `not_enforced` or `unknown`; `unknown` never satisfies a requirement.
- **FR-016**: Tool bundles are mounted read-only at `/opt/bdock/<bundle>`; no instance profile may mount them writable or
  remove, replace or write `bundledock-*` volumes.
- **FR-017**: Cancellation and cleanup MUST be bounded by a deadline, release resources of the instance only, report
  leftovers by id and label, preserve retained results and never delete volumes outside the instance's labelled set.
- **FR-018**: A conformance suite MUST run against the fake provider and any provider; a provider whose reports exceed
  what its tests prove is non-conformant.

**Git configuration profile**

- **FR-019**: A `developer` sandbox MUST use the host global git configuration directly, read/write, with no copy and no
  snapshot, so host preferences apply and the files are always in sync. The repository-local `.git/config` travels with
  the repository mount.
- **FR-020**: The host configuration MUST be made available by mounting its containing directory (the XDG location
  `~/.config/git`), never as a single-file bind mount, because git updates configuration by lock-file rename, which
  fails with `Device or resource busy` over a single-file bind mount. A single-file git config mount MUST be refused
  unless read-only (agent profile).
- **FR-021**: A read-only check MUST report each host-only key, naming the exact key, the origin file and a remedy, and
  MUST NOT print values. The catalogue is [git-host-only-keys.json](contracts/git-host-only-keys.json). Findings never
  disappear silently; they do not by themselves block readiness.
- **FR-022**: Before a developer environment is reported ready, preflight MUST prove that the mounted configuration is
  readable and writable and that `git config --global` works (a list and a set-and-unset of a nonce-named `whr.preflight`
  key). A failure blocks readiness and names the cause. A stale probe key is reported and removed.
- **FR-023**: The agent profile (`cooperative_job`, `untrusted_job`) MUST be declared by WorkHarbor policy
  ([agent-git-profile.v1.json](contracts/agent-git-profile.v1.json): minimal, deterministic, versioned), never derived
  from the host. The host global configuration MUST NOT be mounted. The profile is delivered read-only; no `user.*` is
  written. Commit identity comes from the operator via `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and
  `GIT_COMMITTER_EMAIL`; absent identity makes commits fail rather than guessing. For job profiles every other git-control
  variable in the exec environment (`GIT_CONFIG_*`, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY`,
  `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_EXEC_PATH`, `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_ASKPASS`, `GIT_EXTERNAL_DIFF`,
  `GIT_PAGER`) MUST have its operator-supplied value scrubbed (and the scrub listed in the create or exec result); `whr` then
  sets `GIT_CONFIG_GLOBAL` (the read-only profile file) and `GIT_CONFIG_NOSYSTEM=1` itself; for the `developer` profile an operator-supplied
  `GIT_CONFIG_*` variable MUST be refused (`refused.git_env_override`), because it would defeat FR-019.
- **FR-024**: Admission is capability and profile based; `whr` does not detect agents (FR-029). The profile requested at
  creation decides the git environment: the host global git configuration mount (read/write) MUST be planned only for the
  `developer` profile and MUST be refused for any other profile (`refused.host_git_config_for_agent`). Risk, stated in
  full: a write to that configuration (`credential.helper` with `!cmd`, `core.fsmonitor`, `core.sshCommand`, `core.hooksPath`,
  `alias.*`) executes code on the Mac host with the owner's account the next time the owner runs git there. Therefore
  whatever runs in a `developer` sandbox is trusted at the owner's level, and the operator or external runner MUST NOT choose
  `developer` for an agent; `whr` states this in the creation result and documentation and cannot enforce the intent.
  The workspace container of ADR 0009 (where wave-one agents run) is a different object: this feature gives it no
  host-configuration mount, and a plan for it containing one is refused. Agents are to use `cooperative_job` or
  `untrusted_job`.
- **FR-025**: Canary-key tests MUST run per environment: a canary in the host global configuration is visible in
  `developer` and absent in `cooperative_job` and `untrusted_job`; a canary in the agent profile is the reverse.
- **FR-026**: Checking, and optionally moving, the host global git configuration to `~/.config/git` happens on the Mac host
  (owner statement, 2026-10-20): the check always runs and is a read-only report; the move is guided, makes a backup first,
  needs explicit confirmation and is never silent. `whr` never alters the Docker VM. The carrying command is open (OD-5, recommended option B: a read-only `doctor` section and a separate guided move, proposed name `whr host git-config move`, `--dry-run` as default);
  this increment ships only the check library (T005) and the amendment proposal (T010), not a command.

**Threat model**

- **FR-027**: The threat model ([plan.md](plan.md#threat-model)) is binding on tests, with canary fixtures per profile and
  per asset, each asserted exactly as the plan's threat table states, so that every canary test is satisfiable:
  `untrusted_job`: everything absent except the copy and scratch home; `cooperative_job`: repository refs, hooks and the
  primary `.git/config` (including any credentials or `url.*.insteadOf` rules in it) are visible and writable, acknowledged
  through `--acknowledge-shared-git` (FR-011), while the keychain and other host credentials, sibling workspaces, primary
  home, Docker socket and host global git configuration are absent; `developer`: checkout, home and host global git
  configuration are writable by design, the Docker socket and sibling workspaces are absent. Tool roots are read-only in all.

**Command surface and boundary**

- **FR-028**: `whr sandbox create|ls|show|keep|discard|clean|export` is proposed syntax until approved (owner decision OD-4,
  2026-10-20: a new `sandbox` group, option A). Stage 1 MUST NOT register any command, edit `contracts/cli/commands.json` or
  alter the frozen command, exit-code or output contracts. Registration (task T016, Stage 2) needs the ADR 0014 amendment, the
  CLI amendment process, freeze-test changes and the additive optional `error.refusal` output field (OD-2). `exec` stays
  proposal text: its exit-status passthrough needs a separate ADR 0013 amendment (OD-1).
- **FR-029**: There is no `whr sandbox run` that launches an agent. The proposed `exec` runs an operator-supplied argv as opaque data;
  `whr` MUST NOT recognize, install, configure, authenticate or add flags for any agent tool (Feature 006 FR-030 unchanged).
- **FR-030**: `whr` MUST NOT receive, store or relay provider credentials for an instance. The four identity variables of
  FR-023 are not credentials; their values MUST NOT be logged. A credential an external runner places in an instance is
  outside `whr` and exposed to that instance.
- **FR-035**: Errors map to ADR 0013 codes without new codes: usage 64, not found 65, capability refusal, foreign
  object and wrong state 68, needs-human 70, timeout 71, interrupt 72, incomplete cleanup 73, internal 74. The stable
  refusal code travels in a separate field; the mapping is one pure function with a freeze test.

**Increment**

- **FR-031**: The first increment is Stage 1 (contracts, capability model, admission, refusal, model, Git profile checks,
  fake provider) and Stage 2 (container provider with private copy, scratch home, labels, cancellation and cleanup,
  verified with fake Docker only) plus, owner-gated, registration of the `sandbox` group (T016). Each stage is delivered as pull requests of at most about 1,500 changed lines.
- **FR-032**: Tests MUST run offline with no Docker, no network, no credentials and no host mutation. Real Docker,
  image builds and host mutation are owner gates.
- **FR-033**: A requirements-to-evidence table ([plan.md](plan.md#requirements-to-evidence)) MUST name owner repository,
  prerequisite, acceptance test and evidence location for each requirement group, and be kept current.

**Backend neutrality**

- **FR-034**: Lifecycle, profiles, labels, retention, results, refusal codes and commands MUST NOT name or branch on a
  provider kind. Differences between providers MUST appear only as declared capability values and in the provider's own
  `Plan`. Adding a provider MUST NOT require a change to the contract, the data model, the admission function or the
  conformance suite; it adds a provider and its own tests. A provider-specific behavior that is not a declared capability
  is a defect.

### Key Entities

Workspace, ExecutionInstance, TrustProfile, ProviderCapabilities, SandboxRequest, AdmissionDecision, Refusal,
OwnershipLabels, ResultBundle, CopyPolicy, GitProfileReport, HostOnlyFinding ([data-model](data-model.md)).

## Success Criteria *(mandatory)*

- **SC-001**: The admission table (profile by requirement by provider capability) yields exactly the expected decision and
  refusal code for every row, with zero provider calls on refusal (FR-013, FR-015).
- **SC-002**: A microVM request against every shared-kernel provider fixture is refused and creates nothing (FR-014).
- **SC-003**: Developer plan: directory mount, no single-file mount, preflight read/write proved, canary visible; EBUSY and
  read-only fixtures block readiness; host-only keys are reported without values (FR-019 to FR-022, FR-025).
- **SC-004**: Agent plan: no host global config anywhere, profile read-only and equal to the versioned file, identity only
  from the four variables, canaries absent (FR-023, FR-025).
- **SC-005**: For each profile, every canary asset fixture is absent, read-only or writable exactly as the plan's per-profile
  threat table states (FR-027, FR-010, FR-011, FR-016).
- **SC-006**: The state machine rejects every illegal transition; foreign objects are refused with zero mutations;
  `keep` then `clean` leaves the instance (FR-004 to FR-006, FR-008).
- **SC-007**: Cancellation, failed start and interrupted cleanup keep results, list leftovers and touch only labelled
  objects (FR-017).
- **SC-008**: The error mapping equals the table of FR-035; Stage 1 and T011 to T015 pull requests do not modify
  `contracts/cli/`, ADR 0013 or ADR 0014, checked by a per-PR diff against the merge base. The only exceptions are T016
  (`commands.json`, `output.schema.json`) and T017 (`docs/adr/0014`), each owner-gated (FR-028).
- **SC-009**: All tests pass offline with `go test` and `python3 scripts/check_repo.py` (FR-032).
- **SC-010**: The conformance suite passes unchanged for the fake provider and the container provider, and a second fake
  provider declaring `microvm` is added in a test only, with no change to production contract code (FR-034).
- **SC-011**: Export from a finished and from a failed instance yields a patch and manifest; hostile `.git/config` and
  attributes canaries in the copy (fsmonitor, textconv, external diff, filter, hooks) execute nothing; the result directory
  refuses symlink and swap tricks; nothing is pushed or applied to the primary checkout (FR-007).
- **SC-012**: Operator-supplied git-control variables are scrubbed for job profiles and refused for `developer` (FR-023).

## Assumptions

- Feature 006 T025 (foundation: exit table, output envelope) is available; Stage 2 reuses the Docker port and ownership
  gate of T038 rather than duplicating them.
- Provider kinds beyond standard Docker (Docker Sandboxes, Podman, VM backends) are candidates for later specifications;
  nothing here verifies them.
- Upstream facts in the research input were inspected on 2026-10-20 and are not acceptance evidence.
