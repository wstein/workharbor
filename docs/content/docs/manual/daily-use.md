---
title: Daily use
description: Start a task, follow it, answer what the agent asks, pause, resume and clean up.
weight: 3
toc: true
---

A draft of the everyday commands, checked against `whr --help` of a build from `main`. **Every command here is provisional and has not been run against a release yet** ({{< status unverified >}}); `wh/docs` will check each one against `v0.1.0` (issue #65). Start the supervisor first: [Run the supervisor](run-the-supervisor.md).

## Start a task

```text
whr run <issue-url> --agent <workspace>/<role> [--prompt <text>]
```

Starts a task from an issue on a named agent. You start every run; forge events only fill the inbox. The issue's text is untrusted data to the agent and to you: read it as such. A run of an issue by an author you do not trust is held until you say start.

`whr ls` lists unfinished tasks (`--all` includes finished ones).

## Follow it

```text
whr logs <task> [-f] [--since <event-id>]
whr say <task> [message|-] [-f <file>]
```

`logs` prints a task's events; `-f` keeps following and reconnects. `say` sends a message to the running agent and tells you how it was delivered: now, at its next step, or as a resumed turn. A message is `-` for standard input or read from a file with `-f`.

## Answer what it asks

```text
whr inbox
whr approve <decision> [--reason <text>] [--sha <commit>]
whr reject  <decision> [--reason <text>]
whr answer  <decision> <option> [--reason <text>]
```

`inbox` lists what waits for you: a tool approval, an "Accept this task?" for a card in the agent's queue, a question with options, an egress host the repository requests, and "Ready to push?". `approve` and `reject` answer an approval; `answer` picks one option of a question (resume, retry, cancel, rework, start). A "Ready to push?" review needs `--sha`, the exact commit you were shown, so the approval covers that commit and no other. On the phone the web app answers the same decisions; a review and an egress host there need your passkey.

## Pause, resume, cancel

```text
whr pause <task>     # a hard interrupt: the agent process is stopped, the environment keeps running
whr resume <task>    # starts again from the agent's session, with a briefing
whr cancel <task>
```

Pausing supersedes the questions and approvals the run raised. No agent can pause cooperatively.

## Look at the work, and tidy up

```text
whr open <workspace>[/<role>]   # the supervisor's own copy of the agent's branch, for your editor; prints its path
whr purge <task> [--yes]        # deletes the stored transcript; audit entries, usage and Decisions stay
whr usage [--by task|run|repo|agent|model|day|month|all] [--repo owner/name] [--since 7d] [--until <time>] [--task #42]
```

`purge` is refused while the run is running (pause it first) and, without `--yes`, says what goes and asks. `usage` totals what the agents reported; on a subscription the usage windows are the number that matters, and no cost is invented.

## In an emergency

`whr kill-all` stops every run, cancels every unfinished task and revokes the forge tokens the supervisor holds. It cannot revoke the agent's own credentials, which stay with you. Without `--yes` it asks you to type `kill-all`.

## Reading the project board from the agent lanes

The sessions that build workharbor share one GitHub token, and a board query is the expensive call. `scripts/board-snapshot.sh` (needs `bash`, `jq` and a logged-in `gh`; it holds no token and writes none) is the one way they read the board:

```text
scripts/board-snapshot.sh                  # the snapshot JSON: {"fetched_at": <unix>, "items": [...]}
scripts/board-snapshot.sh queue wh/platform   # the lane's Todo cards, P1 first, then by issue number
scripts/board-snapshot.sh card 132         # one card: status, session, priority, title
scripts/board-snapshot.sh --refresh        # force a query
```

The snapshot is one file, `${XDG_CACHE_HOME:-$HOME/.cache}/workharbor/board.json` (`WHR_BOARD_SNAPSHOT` overrides the absolute path), in a `0700` directory with mode `0600`, outside the repository and never committed. While it is younger than 5 minutes (`WHR_BOARD_MAX_AGE`, in seconds) the script prints it and makes no GitHub call. Past that, one caller takes a lock and makes one query; lanes asking at the same moment wait and share the result. No timer and no daemon run: nothing is queried while nobody asks. If the query fails (rate limit), the script keeps the old file, prints it, says `stale` on stderr and exits 0; with no file at all it exits 1.

Writes go through the same script, so the cache stays right without a query:

```bash
scripts/board-snapshot.sh move 132 "In review"      # Status
scripts/board-snapshot.sh move 132 140 141 "In review"  # several issues at once
scripts/board-snapshot.sh session 132 wh/review     # Session
scripts/board-snapshot.sh priority 132 P2           # Priority
scripts/board-snapshot.sh add 140                   # put an issue on the board
```

Each writes by item ID, never through `gh project item-edit --url`, whose project-wide lookup trips a secondary rate limit (#165): one query finds the issue's project item, one `updateProjectV2ItemFieldValue` mutation sets the field (`add` looks up the issue's node ID, then runs `addProjectV2ItemById`), so a write is at most two small GraphQL calls and there is no retry. The field and option IDs are cached in `board-fields.json` next to the snapshot and fetched once when missing. Before any call to GitHub the script reads `gh api rate_limit` (free) and warns on stderr when the GraphQL budget is under 20 %, naming the reset time. Only if GitHub accepts the write is that one card patched in the cache, under the same lock, so two lanes writing at once both end up in it; `fetched_at` does not change, because a write does not make old data fresh. A failed write (rate limit, bad value) leaves the cache untouched, says so on stderr and exits 1. With no cache, or a stale one, the write still happens and no cache is created or patched. The number must be digits, and the status, lane and priority must be one of the values the script accepts (`Todo`, `In progress`, `Blocked`, `In review`; the lanes of the project board paragraph in `AGENTS.md`; `P1`, `P2`, `P3`), checked before any call. `move` does not set `Ready to push` or `Done` and refuses them before any call: `Ready to push` is set only by `wh/review` (after its review comment; `wh/dispatch` may set it on its behalf for the reviewed sha), and `Done` by closing the issue or by the human; `Ready to push` through `scripts/board-snapshot.sh ready <number>...`, which only `wh/review` runs (or `wh/dispatch` on its behalf, for the reviewed sha). Comments still go straight to GitHub.

The limit: a card moved by hand in the browser or by another tool is not seen until the snapshot is 5 minutes old or a read passes `--refresh`.
