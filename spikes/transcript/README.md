# Transcript spike

Throwaway code for [issue #1](https://github.com/wstein/workharbor/issues/1). Not shipping code, not part of `make check` (it is a nested Go module). See [RESULTS.md](RESULTS.md) for what it found.

It drives Claude Code headless, normalizes the output into typed events, streams them to a phone-sized page over SSE, and injects messages mid-run.

## Run

**It runs the agent on the host without isolation.** Point it only at a scratch repository, never a real one, and never mount `$HOME` or `~/.ssh`. It uses your own agent login and a little of your usage window.

```bash
mkdir /tmp/scratch && cd /tmp/scratch && git init && echo hello > a.txt
cd spikes/transcript
go run . -dir /tmp/scratch
# open http://127.0.0.1:8787
```

Flags: `-dir` (required), `-addr` (keep it on loopback), `-data` (persisted transcript), `-model`, `-tools` (tools allowed without asking), `-approvals` (default on: permission prompts go to the page).

With approvals on, anything outside `-tools` raises an Approve or Deny card on the page and the agent waits. The page also picks the permission mode per session: `manual`, `acceptEdits`, `auto`, `plan`, `dontAsk`. `bypassPermissions` is not offered, because the agent runs on the host.

## API

| Route | Purpose |
| --- | --- |
| `GET /events` | SSE stream; honours `Last-Event-ID` and `?since=` |
| `GET /state` | status, session ID, event count, whether the process is alive |
| `POST /start` | start a session with `{"text": "..."}`; `?resume=1` resumes the persisted session, `?mode=` sets the permission mode |
| `POST /approve` | answer a pending approval: `{"id": "a-1", "allow": true, "message": "..."}` |
| `POST /say` | inject a message into the running session |
| `POST /cancel` | interrupt the agent |

All agent output is untrusted: the page renders it with `textContent` only.
