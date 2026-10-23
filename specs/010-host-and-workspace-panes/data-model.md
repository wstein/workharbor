# Data model: Host and workspace panes

All values are ephemeral. No session contents, selectors, host environment or credential values are persisted by the feature.
This design adds no database or durable console state.

| Value | Fields and relationships | Validation / lifetime |
| --- | --- | --- |
| Local endpoint | Unix socket endpoint, discovery source, remaining deadline | FR-060 order; reject non-Unix schemes before Docker; pin same endpoint across calls. Discovery/preflight shares 5 s. |
| Local workspace record | FR-066 `id`, `name`, `role`, `image`, `state`, `status`, `workspace_folder`, `labels`, `home_volume`, `protected_volumes` | Full 64-hex ID; recognized label required. Both running and stopped records participate in selection. Only schema-approved public fields leave discovery. |
| Workspace selection | One local record plus original selector in memory | Exact full name first, otherwise unique ID prefix of at least 12 hex chars; no manifest/devcontainer alias or name prefix. Missing 65, ambiguous 64, then non-running 68. |
| Shell preferences | `human_shell`, resolved remote user, in-container workspace path, directory source (`workspace_mount` or `container_working_dir`), fallback reason/warning, TERM | FR-005 precedence/validation; absent matching mount uses the validated configured working directory and requires a warning naming it before exec, gated on the FR-005/FR-010 spec reconciliation in the plan. Reuse `workspacecfg` safe manifest reader, `MaxManifestBytes` and `CheckManifest`/validated `HumanShell`: host read ≤64 KiB without following links; absent shell defaults fish, no executable availability exec. |
| Exec plan | Pinned endpoint and full ID, fixed inspect/exec argv, minimal host child env, three explicit container env additions | Names/users/path/TERM validated before interactive exec; no command string or provider credential. No mutation of container/network/volume. |
| Exec result | Docker child status; optional liveness outcome | Status 0 passes immediately; nonzero performs one 5 s inspect. Reachable daemon preserves status; unavailable/timeout returns 72 with `whr:` diagnostic. No session transcript retained. |
| Pane identity | Container name, FNV-1a 32-bit index modulo 5, glyph, optional tint, pane ID and tab ID | Plugin only; name validates against 006 FR-058. Same name bytes yield stable glyph/tint. Host marker is reserved. Labels remain cosmetic. |
| Owner-gate record | Exact tested source revisions, runtime/terminal versions, controlled fixture identity, step results, owner acceptance reference | Separate reviewed evidence, never generated from shell contents or host env. Initially all live observations unverified. |

## Discovery to shell transitions

`usage/terminal admission → endpoint admission → labelled discovery → unique selection → state check → bounded inspect and manifest
resolution → validated argv → one interactive exec → status classification → exit`.
Directory selection retains its source and fallback reason so P2 cannot lose the required warning while building exec argv; a matching mount needs no fallback warning. Warning placement follows the explicit FR-005/FR-010 amendment prerequisite in [plan.md](plan.md#p2-working-directory-fallback-and-warning-prerequisite), with a dedicated P2 test.
All preflight work spends the common discovery deadline. No preflight refusal runs interactive exec. Nonzero exec adds at most one
post-exit liveness inspect with a separate deadline. Paused/restarting are rejected even where a Docker boolean reports running.

A shell belongs to one exec, not to a named persistent console. Herdr on the host can retain the pane across SSH loss; detached or
abnormally disconnected execs may outlive the client and are not reaped by `whr`. Closing a pane is not a data/container-delete action.

## Default application

For a new console session: explicit manifest `console_mode` wins; an omitted field resolves to `plain` for old and new manifests.
An existing console retains its original backend/shell/environment; default changes do not transition a live session. This design
specifies the future runtime consumer without claiming that the currently absent owner API is implemented by P3.

## Fixed identity constants

| Index | Workspace glyph | Optional tint |
| --- | --- | --- |
| 0 | 🔷 | `#14204c` |
| 1 | 🟠 | `#483024` |
| 2 | 🟣 | `#180428` |
| 3 | 🟡 | `#383000` |
| 4 | ⬜ | `#184c44` |

Host: `🟥 HOST`, optional tint `#3a0d0d`. Workspace pane title AND tab label: `<glyph> ws:<name>`.
Collisions in index are expected; full name text distinguishes workspaces. SC-010 verifies stable hash and FR-021a contrast/difference
thresholds; SC-009 verifies actual notebook rendering. Neither colour nor title is an authorization boundary.
