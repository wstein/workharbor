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

`inbox` lists what waits for you: a tool approval, a question with options, an egress host the repository requests, and "Ready to push?". `approve` and `reject` answer an approval; `answer` picks one option of a question (resume, retry, cancel, rework, start). A "Ready to push?" review needs `--sha`, the exact commit you were shown, so the approval covers that commit and no other. On the phone the web app answers the same decisions; a review and an egress host there need your passkey.

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
whr usage [--by task|run|repo|day|month|all] [--repo owner/name] [--since 7d] [--task #42]
```

`purge` is refused while the run is running (pause it first) and, without `--yes`, says what goes and asks. `usage` totals what the agents reported; on a subscription the usage windows are the number that matters, and no cost is invented.

## In an emergency

`whr kill-all` stops every run, cancels every unfinished task and revokes the forge tokens the supervisor holds. It cannot revoke the agent's own credentials, which stay with you. Without `--yes` it asks you to type `kill-all`.
