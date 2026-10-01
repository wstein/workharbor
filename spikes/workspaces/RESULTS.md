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

## 2. Worktrees: what one agent can do to another (`worktrees-console.sh`, part 3)

Two agents (`docs`, `runtime`) have one worktree each in one clone, in one environment, as the same user. Agent `docs` could: edit a file in `runtime`'s worktree; move the `agent/runtime` branch to its own commit with `update-ref` (the other agent's commit became unreachable from the branch); and change `core.hooksPath`, which applies to every worktree because the configuration is shared. `git branch -D agent/runtime` was refused because that branch is checked out in another worktree, but that is a safety check of git, not a boundary (`update-ref -d` or `worktree remove --force` are not). As D42 says: one trust domain.

## 3. Console git with `GIT_CONFIG_COUNT` (part 5 of the same script)

The clone's `.git` had a `post-checkout` and a `pre-commit` hook, `core.fsmonitor`, a clean filter, a `diff` textconv, an alias and `core.pager`, with markers written to a file the script reads. Run commands: `checkout`, `status`, `add`, `commit`, `diff`, `log`, the alias and `reset --hard`.

| git run with | markers that appeared |
| --- | --- |
| plain `git` | alias, filter, fsmonitor, post-checkout and pre-commit hooks, textconv |
| `GIT_CONFIG_COUNT=2` with `core.hooksPath=/dev/null` and `core.fsmonitor=false` | alias, filter, textconv |
| the same plus overrides `filter.x.clean=cat`, `diff.y.textconv=` (empty), `core.pager=cat`, `alias.st=status` | none |

So hooks and fsmonitor are stopped by the two settings, **filter drivers, textconv and aliases are not**. The third row only works because the script knows the planted driver names; a driver with a name the console does not know cannot be switched off by a fixed list, so a console that must be safe against a hostile `.git` needs another measure (read `git config --list --local` first and refuse to run, or run git outside the workspace's `.git`). `core.pager` did not fire in any run here (no terminal), so it is neither shown stopped nor shown running. Limits: one git version (the one in the image), one run.

## Not done yet

Several agents on one sign-in (needs a login, see #82), the external SSD (needs the disk), the console image with zsh, fish and jq, and Ubuntu LTS.
