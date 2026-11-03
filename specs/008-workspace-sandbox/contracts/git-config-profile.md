# Git configuration profile (owner decision, 2026-10-20)

Status: proposed; implements FR-019 to FR-026. Go package `apps/whr/internal/gitprofile`.

| Environment | Global configuration | Delivery | Writable by the environment | Identity |
| --- | --- | --- | --- | --- |
| `developer` | the host global configuration itself, no copy | containing directory (XDG `~/.config/git`) mounted read/write at the container's XDG path (below) | yes: it writes to the host file | host configuration (`user.*`) |
| `cooperative_job`, `untrusted_job` | [agent-git-profile.v1.json](agent-git-profile.v1.json), never from the host | one generated file mounted read-only; `GIT_CONFIG_GLOBAL` points to it; `GIT_CONFIG_NOSYSTEM=1` | no | operator-supplied `GIT_AUTHOR_*`, `GIT_COMMITTER_*` |

The repository-local `.git/config` travels with the repository: bind-mounted for `developer`, shared worktree metadata for
`cooperative_job`, generated for `untrusted_job` (FR-010).

## Why a directory

Git rewrites configuration by writing `config.lock` and renaming it over `config`. A single-file bind mount cannot be
renamed over (`Device or resource busy`). The plan therefore mounts the directory. A writable single-file git
configuration mount is refused (`refused.git_config_single_file_mount`); the read-only agent file needs no rename.

## Host check (read-only; `GitProfileReport`)

1. Location: `xdg`, `legacy_home_file`, `split`, `absent`, `symlink` (see [catalogue](git-host-only-keys.json)).
2. Host-only keys: one finding per occurrence: key, origin file, remedy code. Values are never emitted. Listing uses fixed
   argv `git config --file <path> --list --show-origin -z --no-includes` with a fresh environment, `GIT_CONFIG_NOSYSTEM=1`,
   a 5 s deadline and a 256 KiB output bound; it executes no hook or filter.
3. Includes: `include.path` and `includeIf` keys are reported, and an include target that is unreadable is a finding.

## Paths

Host source: the Mac host's `$XDG_CONFIG_HOME/git` if set, else `~/.config/git`. Container target: `$XDG_CONFIG_HOME/git` if
the container sets it, else `$HOME/.config/git` of the container user; the container `HOME` is provider-defined (for example
`/home/captain` or `/home/vscode`), so the `Plan` records the resolved target and the preflight checks origins against it.
`GIT_CONFIG_GLOBAL` is not set for `developer`.

## Writable configuration risk

The `developer` mount lets anything in the sandbox write the Mac host's global git configuration. Code placed there
(`credential.helper = !cmd`, `core.fsmonitor`, `core.sshCommand`, `core.hooksPath`, `alias.*`) runs on the Mac host with the
owner's account when the owner next runs git. Hence the mount exists only for the `developer` profile (FR-024); the workspace
container of ADR 0009 and every job profile never get it. `whr` cannot detect an agent run in a `developer` sandbox; the
creation result states the risk. A global `credential.helper=` in the agent profile resets only helpers inherited from
earlier global and system config; it does not reset helpers in a repository-local config, which is why `cooperative_job`
(shared local config) is acknowledged (FR-011) and `untrusted_job` gets a generated local config.

## Where the check and the move run (owner statement 2026-10-20; OD-5 resolved by Feature 009)

The owner stated that `whr setup` works on the Mac host and that `whr` never changes the Docker VM. The check runs on the Mac
host every time and only reports. The move of `~/.gitconfig` to `~/.config/git/config` is guided: show the plan, write a
verified backup first (in Feature 009's `<state>/backups/<run-id>/`, FR-013, FR-014), require explicit confirmation, refuse in
unattended mode (needs-human, 70), verify afterwards that `git config --global --list` is unchanged in content. Inside the
Docker VM and the sandbox backend `whr` only reads and verifies (preflight). Feature 006 defines `setup` as Linux/systemd
enrollment of the host receiving the host agent (FR-009, T015), so the check is the doctor check `git.global_config` and the move is the `git-config` step of the macOS `setup` wizard and `doctor --fix` (Feature 009, DP-4).

## Preflight (before `ready`, `developer` only)

Run inside the environment with fixed argv: (1) `git config --global --list --show-origin` exits 0 and every origin lies
under the mounted directory; (2) `git config --global whr.preflight.<nonce> <nonce>` then `--unset` of the same key. Both
must succeed. Failure names the cause (`read_failed`, `write_failed`, `rename_busy`, `origin_outside_mount`) and blocks
readiness (`refused.git_config_not_ready`). A stale `whr.preflight.*` key from a crash is reported and removed by the next
run. The probe writes to the owner's real configuration; it is namespaced and removed.

## Canary tests

Per environment, with a canary key in the host fixture and another in the agent profile: developer shows the first and not
the second; jobs show the second and not the first; no `user.*` in a job; commit in a job without identity variables fails;
with them succeeds with exactly those values. Real-Docker confirmation of `Device or resource busy` behavior is an owner gate.
