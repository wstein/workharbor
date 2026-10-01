# Serve integration run (issues #27, #28)

`run.sh` drives a real `whr serve` on Apple Container 1.5.0 up to the agent's first request, without a login, on a 16 GiB development Mac (not the Mac mini). It builds `whr`, `whr-shim` and `whr-proxy` from the commit, builds the tool store with the pinned Claude Code 2.1.285, writes a configuration and secrets (0600), starts a stand-in for the GitHub API (`fakegithub`), creates a workspace and an agent through the real stack (`setup`, because there is no CLI for it yet: #99), starts `whr serve`, runs `whr run` on an issue the stand-in serves, then looks inside the environment. `output/` is the unedited output of the last run; `evidence/` holds the first run (the default image has no git) and the console output of the final run. Trailing whitespace was stripped from every file.

## What works (run 4)

- **Workspace, environment and agent.** The agent clone is seeded from a local repository, the environment comes up with the tool store read-only at `/tools` (profile and links resolve), the agent home on a named volume, the workspace at `/ws`, an internal network and the egress sidecar running the installed `whr-proxy` (it answers `404` from `api.anthropic.com` through the proxy and `000` direct, so the allowlist and the isolation both hold). The worktree is made inside the environment, `HOME` and the agent home are writable by uid 1000.
- **The start.** `whr run <issue-url> --agent docs-ws/docs` loads the issue through the GitHub App client (the stand-in saw the installation lookup, the token mint and the issue read, never a credential), starts the agent in `/ws/wt/docs` and attaches. Claude Code starts, reports its session, answers `Not logged in · Please run /login`, and that becomes the auth question of D23: run `paused`, Decision "The login expired" (`resume`, `cancel`), task `awaiting_guidance`, the message stored as a transcript event. `whr inbox` shows the question and `whr logs` the whole sequence.
- **Shutdown.** `whr serve` stops on SIGTERM with a session attached.

## What it found (each fixed or an issue)

| # | Finding | State |
| --- | --- | --- |
| 1 | The default image `fedora:latest` has no git: the worktree cannot be made. | Issue #98 (a decision) |
| 2 | A failed workspace creation left the home volume behind. | Fixed (`fix(service): remove the home volume when creating a workspace fails`) |
| 3 | A cancelled exec never finished while the caller kept its stdin pipe open (an agent's stream-json input), so `whr serve` hung on SIGTERM with a session attached. | Fixed (`fix(runtime): end a cancelled exec when the caller's stdin stays open`), with a test that fails without the fix |
| 4 | The agent session was started with the API request's context and stopped when the request returned: the run went running, interrupted and failed within a second. | Fixed (`fix(service): keep a session alive past its request and pass the proxy`), with a test that fails without the fix |
| 5 | A new volume is root's, so the unprivileged agent could not write its home. | Fixed (`fix(runtime): hand a new volume to the environment's user`), with a test |
| 6 | The agent had no `HTTPS_PROXY` and no `HOME`; a resumed run had no environment or worktree. | Fixed (same commit as 4): `StartSpec.Env` |
| 7 | No way to create a workspace or an agent from the CLI or the API; `whr ls` shows an agent ID. | Issue #99 |
| 8 | The tool store is read-only by design, so the script's own `rm -rf` needs `chmod -R u+w` first. | Script |

## Not covered

The sign-in and everything after it (the real Anthropic request through the proxy, `resume`, a real turn, the bundle export and `Prepare`) wait for a logged-in agent home (issue #82). The real-GitHub check of #27 waits for the App of #73. The memory and speed of the environment on the Mac mini were not measured.
