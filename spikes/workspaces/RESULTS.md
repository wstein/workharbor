# Spike #89: workspaces with several agents (in progress)

Run on a Mac with Apple Container 1.5.0, `golang:1.27.1-trixie` as the environment image (it has git). Each script is run as `./<name>.sh`; its unedited output is in `<name>-results.txt` (progress lines and git's "Empty last update token" warnings from the planted fsmonitor in the guest are filtered out).

## 1. Bundle export (`bundle.sh`)

A workspace folder (the agent's own clone, seeded on the host from a forge repository) is bind-mounted at `/ws` into a running environment. `git worktree add` inside the environment makes one worktree and branch per agent. The host streams `container exec <env> git -C <worktree> bundle create - main..<branch>` into a file, then imports it into a supervisor-owned bare repository with `GIT_CONFIG_NOSYSTEM`, no global config, `core.hooksPath=/dev/null` and `core.fsmonitor=false`.

| | small range | large range |
| --- | --- | --- |
| contents | 3 commits, 2 files | 401 commits (400 x 20 files and one 60 MB random blob) |
| bundle size | 1.0 KB | 63 MB |
| `bundle create` in the guest, streamed to the host | 108 ms | 1.8 s |
| `git bundle verify` | 34 ms | 37 ms |
| fetch into the bare repository | 53 ms | 412 ms |

- **The environment keeps running** the whole time; nothing is stopped for the export.
- **Nothing planted ran on the host.** The clone's `.git` had `core.fsmonitor`, a `post-checkout` and a `reference-transaction` hook and a clean filter set; their markers live in a directory that exists only on the host. Verify and fetch left no marker. As a control, running host git inside the workspace clone (`status`, `checkout`, `add`) did create the fsmonitor and both hook markers, so the planted config works and the host must never run git in a workspace.
- **`git bundle verify` alone is not enough.** A bundle truncated to half its size still passed `verify` (exit 0), because it checks the prerequisites and not the pack. Fetching it into a fresh repository failed (exit 1) and so did a bundle with 4 KB of random bytes written into its middle; neither left a ref. The import must therefore be a fetch into a repository that does not already hold the objects; in the first version of the script the refused fetches succeeded because the earlier import had already brought the objects in, which hides a bad pack.
- Limits: one machine, local file transfer, `transfer.fsckObjects` was not set (worth setting), the size cap and timeout belong to the importing code, and the filter driver check covers `clean` only.

## Not done yet

Several agents on one sign-in, worktree interference, the external SSD, the console with `GIT_CONFIG_COUNT` and Ubuntu LTS.
