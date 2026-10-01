# Results

Measured on 1 October 2026 on the Mac mini, with Claude Code 2.1.285 (model alias `haiku`, subscription login) and Codex CLI 0.159.2 (ChatGPT login). Tracks [issue #1](https://github.com/wstein/workharbor/issues/1). Agents ran on the host in a scratch repository, with no isolation.

## Summary

| Question | Claude Code | Codex CLI |
| --- | --- | --- |
| 1. Headless, streamed structured output | Pass | Partial: `codex exec --json` emits events; no real run (usage limit) |
| 2. Normalize into one event type | Pass | Not tested |
| 3. Mid-run message injection | Pass | Not tested; `exec` takes its prompt once (unverified) |
| 4. Live page over SSE, send and cancel | Pass | Not tested |
| 5. Reconnect replay, restart and resume | Pass | Not tested (`exec resume` exists) |
| 6. Auth, quota, approval signals | Partial | Quota seen as text only |

## Claude Code

Invocation: `claude -p --input-format stream-json --output-format stream-json --verbose --permission-prompts none --model haiku --allowedTools ...`. One process stays alive across turns.

**Events.** Newline-delimited JSON with `type` in `system` (`init`, `hook_*`, `thinking_tokens`, `permission_denied`), `assistant` and `user` (content blocks `text`, `thinking`, `tool_use`, `tool_result`), `rate_limit_event` and `result`. Normalized to: `session`, `user`, `text`, `thinking`, `tool_call`, `tool_result`, `usage`, `permission`, `result`, `status`, `error`.

**Mid-run injection.** With stdin kept open, a message written 5 s into a 12 s `sleep` tool call was incorporated in the same run: the final result was `done BANANA`, with a single `result` event. It is delivered at the next model step after the running tool finishes, not by interrupting the tool. The UI should say "will be seen at the next step".

**Detach and reattach.** `EventSource` reconnects with `Last-Event-ID`; the server replays exactly the missed events from an append-only log. A page reload shows the full transcript.

**Supervisor restart.** Killing the supervisor and the agent, then restarting the supervisor, replays the transcript from the JSONL file. `POST /start?resume=1` starts `claude --resume <session-id>`; the agent remembered earlier context (`BANANA`). The session ID is the same before and after the resume. The persisted status still read `idle` with no process alive, so the real reconciler must mark such a run `interrupted` (design §5.3).

**Quirks.**
- The `init` event, and so the session ID, only appears after the first user message.
- User messages are not echoed in the output; the supervisor must log its own.
- `--bare` skips OAuth and keychain reads, so it must not be used with subscription logins (as the help text describes it; not tested).
- A hook in the user's settings (`hook_started` and `hook_response`) shows up in the stream.
- The CLI refused a standalone `sleep 30` with a `tool_use_error`, a reminder that the agent's own tool rules apply on top of ours.

**Usage and quota.** `rate_limit_event.rate_limit_info` has `status`, `rateLimitType`, `resetsAt`, `overageStatus`, and `unifiedWindows.five_hour` and `seven_day`, each with `utilization` (0 to 1) and `resetsAt`. That feeds the usage meter and `quota_exhausted`. What `status` reads when the window is exhausted was not observed.

**Auth.** `init.apiKeySource` tells the auth mode. How an expired login shows up was not tested.

**Approvals.** A tool that is not allowed gives `system/permission_denied` and an error `tool_result`, then the agent explains it cannot proceed. `--permission-prompts host` without `--permission-prompt-tool` still denied. A real approval round-trip needs `--permission-prompt-tool` (an MCP tool) or the SDK control protocol; untested. Fallback that works with the flags seen here: show the denial as a Decision, and on approval resume the session with the tool allowed.

**Cancel.** SIGINT stops the process immediately, even mid-tool-call. The session stays resumable. There is no cooperative "stop after this turn".

## Codex CLI

`codex exec --json --ephemeral --sandbox read-only --skip-git-repo-check -C <dir> "<prompt>"` emits `thread.started` (with `thread_id`), `turn.started`, then in this run `error` and `turn.failed`. The ChatGPT login had reached its usage limit (until 3 October 2026, 19:12), so the run failed before doing any work.

- The usage limit is reported only as human text; the reset time sits inside the message, with no structured field. The adapter would have to parse it or poll another source.
- The prompt is passed once as an argument or on stdin. There is no streaming input flag in `codex exec --help`, so mid-run injection is probably unsupported in `exec` mode. **Unverified.**
- `codex exec resume <id>` and `fork` exist, so resume between turns is likely.
- Everything else is untested. Re-run after the limit resets.

## Consequences for the design

- §5.2: mid-run message injection and a structured event stream are achievable with Claude Code, so keeping them required for release 1 is realistic. For Codex they may need a degraded mode (message delivered as a resumed turn).
- §5.2: add `permission_denied` as the source of approval Decisions, with the resume-with-tool-allowed fallback.
- §5.2 and §7: the usage window and reset time are available from Claude Code; for Codex they are text.
- §5.3: persist the transcript and agent session ID. Resuming from the agent session works.
- §9.3: delivery semantics for a sent message are "at the next step", not instantaneous. Cancel is a hard interrupt.

## Not tested

Partial token streaming (`--include-partial-messages`), `--permission-prompt-tool`, expired-login behaviour, multi-session concurrency, running inside Apple Container, and Antigravity.
