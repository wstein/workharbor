# Feature 009 data model

Status: proposed; implements the entities of [spec.md](spec.md). Catalogue: [host-tool-catalogue.schema.json](contracts/host-tool-catalogue.schema.json).

| Entity | Fields |
| --- | --- |
| CatalogueItem | `id`, `kind` (`runtime`, `tool`), `title`, `platforms`, `selected_by_default`, `requires`, `network` (`hosts`, `purpose`, `lookup_urls`), optional `privileged` commands. Tools carry `tool` (`command` and `version_args` (or exclusively `version_metadata: orbstack_app` for the OrbStack runtime cask), `version_regex`, `min_version`, `source`: Homebrew type, name, tap, `may_elevate`); an entry that needs a dotfile change carries `config` (`marker`: `dotfile` or `shell_hook`, `target`, `content_file`, `sha256`, `mode`). |
| DetectionResult | `item`, `state` ([item states](#item-states)), found path (redacted) and version, bounded evidence. |
| Plan | `plan_hash`, `platform`, `catalogue_version`, ordered `steps`, `mode` (`setup`, `fix`). |
| Step | `step_id` (`<item>.<kind>`), `item`, `kind` ([step kinds](#step-kinds)), `class` (`create`, `modify`, `read_only`), `depends_on`, `preview`, rollback data. |
| StepResult | `step_id`, `status` (`applied`, `unchanged`, `declined`, `opted_out`, `blocked`, `failed`, `skipped`, `rolled_back`), `reason`, `exit_status`, bounded output tail. |
| HostProfile | `catalogue_version`, `updated`, `items`: id to `selected` or `opted_out`; includes the reserved id `git-config`. No paths or values. |
| BackupRecord | `<state>/backups/<run-id>/` (0700) with `manifest.json` (0600): per file original path (`~`-relative), backup name, SHA-256, size, mode, reason, time. Byte copy verified by hash; never deleted by `whr`; restore is a manual copy. |
| Protocol | `<state>/protocols/<run-id>.jsonl` and `.txt` per real run ([protocol.schema.json](contracts/protocol.schema.json)); `<state>/run.lock` for the run lock. `<state>` is `~/.local/state/whr` (DP-1). |

## Item states

| State | Meaning | Default action |
| --- | --- | --- |
| `absent` | not found on the sanitized absolute PATH, or `git` is the Command Line Tools shim (file checks, never executed) | `install` (create) |
| `present_ok` | version at or above `min_version` | none |
| `present_outdated` | below `min_version` | `upgrade` (modify), only if Homebrew-managed and confirmed |
| `present_other_source` | not under `<prefix>/Cellar/<name>/` (formula) or without `<prefix>/Caskroom/<name>` (cask) | reported, never replaced |
| `present_unverifiable` | version not parsed, probe failed or timed out | reported, never ok |
| `unknown` | detection could not run | none |

Config states (the `config` of an entry; its step belongs to that entry and is skipped when the entry is opted out or not installed): `absent` (create), `identical` (none), `differs` (modify: skip or replace), `unsafe_target` (symlink or special file: refused).

## Step kinds

Closed set; any other kind is an internal error (74).

| Kind | Class | Effect |
| --- | --- | --- |
| `detect` | read_only | fixed-argv probe, file stat |
| `brew_install` / `brew_upgrade` | create / modify | `brew install` / `brew upgrade` (`--cask` for casks) |
| `dotfile_create` / `dotfile_replace` | create / modify | write a new file / back up then replace |
| `git_config_move` | modify | back up, stage, verify, rename, remove legacy |
| `socket_verify` | read_only | connect to a discovered socket and close it |
| `privileged_exec` | modify | `/usr/bin/sudo -- <catalogue argv>`; real runs only |
| `profile_write` | create or modify | write the host profile |

There is no kind for the Docker VM, the runtime application, a login shell or a tap.

## Run states

`detected` -> `previewed` -> `confirmed` (plan hash rechecked; lock and protocol start) -> `applying` -> `done` | `partial` | `failed` | `interrupted`.
`no` and cancel leave `previewed` with no write. A rerun starts at `detected`.

The closed `orbstack_app` probe means the application metadata key `CFBundleShortVersionString` in `/Applications/OrbStack.app/Contents/Info.plist`. The read-only owner measurement used `/usr/bin/plutil -extract CFBundleShortVersionString raw -o - /Applications/OrbStack.app/Contents/Info.plist`; this is evidence for the route, not a generic catalogue argv allowance. T002 owns detection and path/source checks. Compare the application version (observed `2.2.4`) against the approved minimum `2.2.3`; Homebrew registration `2.2.3,20963` describes package provenance separately and is never the application-version parser input. Catalogue loading executes no probe.
