---
title: "Spike #89: workspaces, bundles and the console"
description: "Streaming a git bundle out of a running environment, what one agent can do to another's worktree, how far GIT_CONFIG_COUNT protects a console, and Fedora and Ubuntu LTS bases on Apple Container 1.5.0."
weight: 8
---

> Source: [`spike/workspaces`](https://github.com/wstein/workharbor/tree/spike/workspaces/spikes/workspaces) at `2931104`, with the scripts, the unedited `*-results.txt` and `RESULTS.md` under `spikes/workspaces/`. Tracks [#89](https://github.com/wstein/workharbor/issues/89). Recorded on 1 October 2026. **Partial:** several agents on one sign-in and the external SSD are not measured.

Measured on a 16 GiB development Mac (not the Mac mini) with Apple `container` 1.5.0 and `golang:1.27.1-trixie` (it has git) as the environment image. The figures are one run each.

## Bundle export

A workspace folder holds the agent's own clone and is bind-mounted into a running environment, with one `git worktree` per agent. The host streams `container exec <env> git -C <worktree> bundle create - main..<branch>` into a file and fetches it into a supervisor-owned bare repository with no system or global config, `core.hooksPath=/dev/null` and `core.fsmonitor=false`.

| | 3 commits | 401 commits with a 60 MB blob |
| --- | --- | --- |
| bundle size | 1.0 KB | 63 MB |
| `bundle create`, streamed to the host | 108 ms | 1.8 s |
| `git bundle verify` | 34 ms | 37 ms |
| fetch into the bare repository | 53 ms | 412 ms |

The environment keeps running. A fsmonitor, two hooks and a clean filter planted in the clone's `.git` did not run on the host; running host git inside that clone did run them. `git bundle verify` passed a bundle truncated to half its size, while a fetch into a repository without the objects refused both that and a bundle with random bytes in its middle, leaving no ref: the import has to be the fetch.

## One agent against another

Two agents in one environment, as the same user, share the clone's `.git`. One edited the other's worktree, moved the other's branch with `update-ref` and changed `core.hooksPath` for both. `git branch -D` of a branch checked out in another worktree was refused, which is a safety check and not a boundary.

## Console git

With a planted hook pair, fsmonitor, clean filter, textconv and alias, plain git triggered all of them. `GIT_CONFIG_COUNT` with `core.hooksPath=/dev/null` and `core.fsmonitor=false` stopped the hooks and fsmonitor, not the filter, textconv or alias; those stopped only when overridden by name.

## Bases and mounts

Console images with git, zsh, fish and jq build on Fedora 44 and Ubuntu 24.04.5 LTS. The runtime conformance suite passes on both (about 28 s each) and the egress test on Ubuntu. A workspaces root mounted read-only with one workspace read-write over it behaves as intended.

## Not measured

Two or three Claude Code sessions on one sign-in (it needs a login, issue #82) and a workspace on an external SSD (no disk here).
