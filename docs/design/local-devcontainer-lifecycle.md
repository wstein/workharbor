# Proposed local devcontainer, staging, and temporary container lifecycle in `whr`

Date: 2026-10-19
Status: Proposed design and Spec 006 amendment draft (candidate FR-066, FR-067, FR-068, SC-024, and task T038).
This document records the specification and task breakdown for owner review; it does not claim implemented CLI behavior or authorize live container mutations without explicit invocation.

Historical design context: the authoritative local lifecycle contract is now
[local-stage-promote](../../specs/006-whr-cli/contracts/local-stage-promote.md),
with the owner-approved
[throwaway amendment](../../specs/006-whr-cli/contracts/throwaway-staging-amendment-20261025.md)
taking precedence. Managed candidates use fresh `whr-stage-home-<id>` homes.
Normal confirmed remove or successful promote disposes of the candidate
container, home, clone/results and records last, without `--volumes --force`.
Generic removal keeps its existing flags and the whole `anchorage-home-*` guard.
This record does not establish acceptance or reopen the selected CLI surface.

## 1. Purpose, container roles, and governance boundary

In [Feature 006 (`whr` product CLI)](../../specs/006-whr-cli/spec.md) and [ADR 0014](../adr/0014-whr-command-structure.md), `workspace.ls` (`whr workspace ls` / `whr ls`) and `workspace.rm` (`whr workspace rm` / `whr rm`) are registered in [`contracts/cli/commands.json`](../../contracts/cli/commands.json) with `increment: "later"`, `needs: "control_plane"`, and `blocked_on: "owner_api"`.

When working locally with devcontainers, an operator uses three distinct container roles so the active working environment is never disrupted while testing or upgrading:

| Role | Default container name | Container hostname | Identity label | Prompt indicator | Purpose |
| --- | --- | --- | --- | --- | --- |
| **Primary** | `whr-anchorage` | `anchorage` | `devcontainer.local_folder` | `captain@anchorage on <host>` | Active daily working devcontainer |
| **Staging** | `whr-anchorage-staging` | `anchorage-staging` | `anchorage-staging=1` | `captain@anchorage-staging on <host> (staging)` | Side-by-side candidate built and verified while primary stays running; promoted once verified |
| **Temp** | `whr-anchorage-1` | `anchorage-1` | `anchorage-temp=1` | `captain@anchorage-1 on <host> (temp)` | Disposable throwaway sandbox for one-off experiments |

To manage these containers without a remote control plane or sign-in, `whr` needs direct host-local operations:

1. **View (`ls`)**: List local devcontainer, staging, and temporary workspace containers, their roles (`primary`, `staging`, `temp`), states, workspace folders, labels, and mounted home volumes.
2. **Stop and remove container (`rm`)**: Stop and remove an identified local workspace, staging, or temporary container while preserving its named home volume by default.
3. **Remove container and isolated home volume (`rm --volumes`)**: Stop and remove both the container and its associated writable home volume (`anchorage-home-...`), while strictly protecting shared read-only BundleDock volumes (`bundledock-*` mounted under `/opt/bdock/*`).
4. **Side-by-side stage and promote (`stage` / `promote`)**: Build and verify a `staging` container alongside the running primary devcontainer, then promote the verified image to replace the primary container and clean up the staging container and its staging home volume.

## 2. Command surface options (owner decision point)

### Option B1: `--local` mode on `workspace.ls` and `workspace.rm` plus `workspace.stage` / `workspace.promote` (Recommended)

Extend `workspace.ls` and `workspace.rm` in [`contracts/cli/commands.json`](../../contracts/cli/commands.json) to support local Docker execution when `--local` is supplied, and support role-targeted staging and promotion:

| Operation | Canonical command | Short alias | Flags |
| --- | --- | --- | --- |
| **1. View** | `whr workspace ls --local` | `whr ls --local` | `--local`, `--role <primary\|staging\|temp>`, `--label <key[=value]>`, `--json` |
| **2. Stop & remove container** | `whr workspace rm --local [<id\|name>]` | `whr rm --local [<id\|name>]` | `--local`, `--role <staging\|temp>`, `--label <key[=value]>`, `--dry-run`, `--yes`, `--json` |
| **3. Remove container + home volume** | `whr workspace rm --local --volumes [<id\|name>]` | `whr rm --local --volumes [<id\|name>]` | `--local`, `--volumes`, `--role <staging\|temp>`, `--label <key[=value]>`, `--dry-run`, `--yes`, `--json` |
| **4a. Stage candidate alongside primary** | `whr workspace stage --local` | — | `--local`, `--smoke`, `--dry-run`, `--json` |
| **4b. Promote verified staging to primary** | `whr workspace promote --local` | — | `--local`, `--dry-run`, `--yes`, `--json` |

- **Preserves existing remote contract**: When `--local` is omitted, `workspace.ls` and `workspace.rm` remain control-plane commands (`needs: "control_plane"`, `blocked_on: "owner_api"`) and continue to refuse with exit code `64` (`TestLaterCommandsRefuseWithoutBackend`).
- **No duplicate command groups**: Reuses the canonical `workspace.ls` and `workspace.rm` identifiers in `--json` output (`"command": "workspace.ls"` / `"command": "workspace.rm"` with `"scope": "local"`).

### Option B2: Dedicated local subgroup (`whr workspace temp` or `whr devcontainer`)

Add dedicated local-only entries (`workspace.temp.ls`, `workspace.temp.rm`, `workspace.staging.*`) to [`contracts/cli/commands.json`](../../contracts/cli/commands.json). This keeps `workspace.ls` and `workspace.rm` exclusively remote, at the cost of a new command group or three-level path in the command table.

## 3. Draft specification amendment (`specs/006-whr-cli/spec.md`)

### Functional requirements

- **FR-066 — Local container discovery (`whr workspace ls --local`)**:
  - When invoked with `--local`, `workspace.ls` inspects the local Docker engine (using the same socket and `DOCKER_HOST` rules as [`whr doctor`](../container-runtime.md)) with a 5-second total deadline, fixed argument vectors (no shell evaluation), and no network access beyond the local daemon socket.
  - By default, discovery matches containers carrying at least one recognized workspace or devcontainer label: `anchorage-staging`, `anchorage-temp`, `devcontainer.local_folder`, `devcontainer.config_file`, or a WorkHarbor workspace label. Passing `--role <primary|staging|temp>` or `--label <key[=value]>` restricts matching accordingly. Unlabelled foreign containers are never returned.
  - Each matched container entry reports:
    - `id` (64-hex container ID; human output displays 12-char prefix),
    - `name` (container name without leading slash, e.g. `whr-anchorage`, `whr-anchorage-staging`, `whr-anchorage-1`),
    - `role` (`"primary"`, `"staging"`, or `"temp"`),
    - `image` (configured image reference),
    - `state` and `status`,
    - `workspace_folder` (resolved from `devcontainer.local_folder` or `/workspaces/*` bind mount, if present),
    - `labels` (the matched workspace/staging/temp labels),
    - `home_volume` (name of the writable named volume mounted at `/home/captain` or legacy `/home/vscode`, or `null`),
    - `protected_volumes` (names of read-only `/opt/bdock/*` or `bundledock-*` volumes attached to the container).
  - Human output escapes all container names, image strings, paths, and label values through the FR-022 terminal-escape filter. `--json` emits one schema-valid envelope (`localWorkspaceListResult` in [`contracts/cli/output.schema.json`](../../contracts/cli/output.schema.json)).

- **FR-067 — Local container removal and volume cleanup (`whr workspace rm --local`)**:
  - **Target selection**: Accepts an explicit container `<id|name>` positional argument, `--role <staging|temp>`, or `--label <key[=value]>` (for example `--label anchorage-temp=1` or `--label anchorage-staging=1`). Specifying none, or specifying conflicting selectors, exits `64` (`err_usage`).
  - **Ownership gate before mutation**: Before stopping or removing any container, `whr` inspects the target container and verifies that it carries a recognized workspace/devcontainer label (`anchorage-staging`, `anchorage-temp`, `devcontainer.local_folder`, `devcontainer.config_file`, or the explicit `--label` filter). A container that exists but lacks any recognized workspace/devcontainer label is refused with exit code `68` (`err_invalid_state` / foreign container refusal) and zero state changes.
  - **Operation 2 — Container removal (default, without `--volumes`)**:
    - Stops and removes each verified target container (`docker rm -f -- <full-id>`) and leaves its `/home/captain` (or legacy `/home/vscode`) named volume intact.
    - Reports removed container IDs and preserved `home_volume` names in human and `--json` output.
  - **Operation 3 — Container and temporary home volume removal (`--volumes`)**:
    - Inspects `.Mounts` of each verified container prior to container removal.
    - Selects a volume for deletion **only** when all four conditions hold:
      1. `Type == "volume"`,
      2. `Destination` is `"/home/captain"` (or legacy `"/home/vscode"`),
      3. `RW == true`, and
      4. `Name` matches the workspace home volume pattern (`anchorage-home-*` or `workharbor-home-*`) and does **not** start with `bundledock-`.
    - **Hard safety invariant**: Volumes mounted under `/opt/bdock/*`, read-only mounts (`RW == false`), bind mounts, and `bundledock-*` tool volumes are **never** removed under any flag combination.
    - Removes the container first (`docker rm -f -- <full-id>`), then removes the selected home volume (`docker volume rm -- <volume-name>`). If the container is removed but volume deletion fails, `whr` records the partial outcome and exits `73` (`err_partial_failure`) naming the remaining volume.
  - **Dry-run and confirmation**:
    - `--dry-run` performs full discovery, ownership verification, and mount classification, outputs the planned container and volume removals (`applied: false`), and executes no `docker rm` or `docker volume rm` command.
    - Without `--dry-run`, running in non-interactive mode, with `--json`, or without a terminal requires `--yes`; omitting `--yes` exits `70` (`err_confirmation_required`) naming `--yes`, per [ADR 0013](../adr/0013-whr-exit-codes.md).

- **FR-068 — Side-by-side staging and promotion workflow (`whr workspace stage --local` / `whr workspace promote --local`)**:
  - **Staging (`stage --local`)**: Launches a candidate devcontainer (`--name whr-anchorage-staging`, `--hostname anchorage-staging`, `--id-label anchorage-staging=1`, `ANCHORAGE_ROLE=staging`, fresh isolated `whr-stage-home-<id>` volume) alongside the active primary `whr-anchorage` container without stopping or modifying the primary container. When `--smoke` is passed, runs container smoke verification against the staged container before returning.
  - **Promotion (`promote --local`)**: Selects the original primary by exact name/full ID and verifies the receipt-bound candidate using the declared Docker healthcheck or explicit workspace smoke. It imports the contracted result refs before clone disposal, recreates the primary with its original home, and disposes of the candidate container, fresh `whr-stage-home-<id>` volume, clone/results and records last. This uses normal confirmation; promote has no `--volumes` or `--force` flags. A verified rollback restores the original before candidate disposal; incomplete disposal reports residuals and the normal removal command.

### Success criteria

- **SC-024**: Local workspace listing, staging, promotion, and removal (`FR-066`, `FR-067`, `FR-068`) pass offline unit and fake-Docker integration tests covering:
  1. `whr ls --local`, `whr ls --local --role staging`, and `whr ls --local --label anchorage-temp=1` (human and `--json` schema conformance, role classification, FR-022 escaping of hostile container names/labels);
  2. `whr rm --local <id> --yes` removing the container while preserving `anchorage-home-*`;
  3. `whr rm --local --label anchorage-temp=1 --volumes --yes` removing both the container and `anchorage-home-*` while leaving all `/opt/bdock/*` (`bundledock-*`) volumes untouched;
  4. `whr workspace stage --local` and `whr workspace promote --local --yes` keeping the primary container intact during staging and cleaning up the staging container and staging home volume on promotion;
  5. refusal of foreign unlabelled containers with exit `68` and zero mutations;
  6. `--dry-run` parity with zero mutations;
  7. missing `--yes` in non-interactive / `--json` mode exiting `70`;
  8. injected volume-removal failure after container removal exiting `73` with accurate residual reporting; and
  9. unchanged refusal (exit `64`) when `whr ls` or `whr rm` is called without `--local`.

## 4. Draft task entry (`specs/006-whr-cli/tasks.md`)

- [ ] **T038 [US1, US4]** Local devcontainer and temporary container lifecycle (`FR-066`, `FR-067`, `SC-024`) in `apps/whr/internal/localdocker/`, `apps/whr/internal/commands/`, [`contracts/cli/commands.json`](../../contracts/cli/commands.json), and [`contracts/cli/output.schema.json`](../../contracts/cli/output.schema.json):
  - Add `--local` and `--label` flags to `workspace.ls`, and `--local`, `--label`, `--volumes`, `--dry-run`, and `--yes` flags to `workspace.rm` in `contracts/cli/commands.json` and `apps/whr/internal/commands/entries.go`.
  - Add `localWorkspaceListResult` and `localWorkspaceRemoveResult` definitions to `contracts/cli/output.schema.json`.
  - Implement bounded local Docker inspection and removal in `apps/whr/internal/localdocker/` (5 s deadline, fixed argv, ownership label check, `/home/captain` writable volume filter, hard exclusion of `/opt/bdock/*` and `bundledock-*` volumes).
  - **Failing tests first**:
    - `TestLocalLsListsLabelledContainersAndVolumes`
    - `TestLocalLsLabelFilter`
    - `TestLocalLsEscapesHostileMetadata` (FR-022 corpus)
    - `TestLocalRmContainerOnlyPreservesHomeVolume` (Operation 2)
    - `TestLocalRmWithVolumesRemovesHomeVolumeOnly` (Operation 3)
    - `TestLocalRmNeverRemovesBundleDockOrReadOnlyVolumes`
    - `TestLocalRmRefusesForeignUnlabelledContainer` (exit 68)
    - `TestLocalRmDryRunExecutesNoMutations`
    - `TestLocalRmNonInteractiveRequiresYes` (exit 70)
    - `TestLocalRmPartialVolumeFailureExits73`
    - `TestLocalCommandsJSONMatchesSchema`
    - `TestWorkspaceLsAndRmWithoutLocalStillRefuse` (preserves `T025` backend refusal when `--local` is absent)
  - **Acceptance**: All `SC-024` cases pass offline with fake Docker fixtures; `python3 scripts/check_repo.py` and `go test ./...` in `apps/whr` pass; independent exact-head review is `CLEAR`.
  - **Prerequisites**: `T025`.
