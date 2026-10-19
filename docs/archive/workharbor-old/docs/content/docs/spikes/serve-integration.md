---
title: "Integration run: a real whr serve"
description: "A real supervisor on Apple Container 1.5.0 up to the agent's first request, without a login: what worked and the seven things it found."
weight: 9
---

> Source: [`spike/serve-integration`](https://github.com/wstein/workharbor/tree/spike/serve-integration/spikes/serve-integration) at `e5188a5`, with `run.sh`, its helpers, the unedited output and `RESULTS.md` under `spikes/serve-integration/`. Groundwork for [#28](https://github.com/wstein/workharbor/issues/28), tracks [#27](https://github.com/wstein/workharbor/issues/27). Recorded on 1 October 2026 on a 16 GiB development Mac, not the Mac mini.

`whr serve` was run for real against Apple Container 1.5.0, with the pinned Claude Code from the tool store, a stand-in for the GitHub API and a local repository as the workspace's source. Everything up to the agent's first request ran without a login.

## What worked

- A workspace and an agent were created through the real stack: the agent clone seeded from the local repository, the environment with the tool store read-only at `/tools`, the agent home on a volume, an internal network and the egress sidecar running the installed `whr-proxy`, and the agent's worktree made inside the environment. Direct network access from the guest failed; the same request through the sidecar reached the allowlisted host.
- `whr run <issue-url> --agent docs-ws/docs` loaded the issue through the GitHub App client and started Claude Code in its worktree. Claude answered `Not logged in · Please run /login`, which became the login question of D23: the run paused, the task moved to `awaiting_guidance` and `whr inbox` showed "The login expired" with `resume` and `cancel`.
- `whr serve` stopped on SIGTERM with a session attached.

## What it found

| Finding | State |
| --- | --- |
| The default image `fedora:latest` has no git, so the worktree cannot be made | issue #98, a decision |
| A failed workspace creation left the home volume behind | fixed |
| A cancelled exec never finished while the caller kept its stdin pipe open, so `whr serve` hung on SIGTERM | fixed, with a test |
| The agent session was stopped when the API request that started it returned | fixed, with a test |
| A new volume is root's, so the unprivileged agent could not write its home | fixed, with a test |
| The agent had no `HTTPS_PROXY` and no `HOME`; a resumed run had no environment or worktree | fixed |
| No way to create a workspace or an agent from the CLI or the API; `whr ls` shows an agent ID | issue #99 |

## Not covered

The sign-in and everything after it (the real request through the proxy, resuming, a real turn, the bundle export and the push) wait for a logged-in agent home (issue #82); the real-GitHub check of #27 waits for the App of #73.
