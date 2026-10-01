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
| 6. Auth, quota, approval signals | Pass: approvals (round-trip, modes), missing-login signal, token streaming. A mid-session expiry was not reproduced | Quota seen as text only |

Antigravity (`agy` 1.1.12) is covered in its own section below: headless works, there is no streaming input.

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

**Token-level streaming.** `--include-partial-messages` adds `stream_event` records: `message_start`, `content_block_start`, `content_block_delta` (`text_delta` and `thinking_delta`, plus a `signature_delta`), `content_block_stop`, `message_delta`, `message_stop`. The full `assistant` message still follows. The harness coalesces deltas (about every 150 ms, always before any other event) into one `delta` event, and the page shows them in a live bubble that the final `text` event replaces. A 12-number answer arrived as 3 deltas over about 240 ms. The cost is more events in the log and on the wire; a real service may want to keep deltas out of the durable log and replay only the final text.

**Auth and an expired login.** `init.apiKeySource` is `none` for a subscription login, and it is also `none` when nobody is logged in, so it cannot tell the two apart. Tested with an empty throwaway `CLAUDE_CONFIG_DIR` (the real login was never touched): the stream carries an `assistant` event with `error: "authentication_failed"`, then a `result` with `is_error: true`, `result: "Not logged in · Please run /login"` and, oddly, `subtype: "success"`, and the process exits 1. So `auth_expired` must be detected from the assistant event's `error` field or `is_error`, never from `subtype` or the init event. The harness maps it to an `auth_expired` event and an `auth expired` status, and the page shows a "log in again" card. A login that expires mid-session (rather than missing from the start) was not reproduced.

**Approvals (round-trip works, no restart).** `--permission-prompts host --permission-prompt-tool mcp__workharbor__approve --mcp-config <file> --strict-mcp-config` makes the agent call a small MCP server (the harness binary in `-mcp-permission` mode, stdio) whenever a tool needs permission. The helper forwards `{tool_name, input, tool_use_id}` to the supervisor, which publishes an `approval` event and holds the call open until a human answers on the page. The helper returns `{"behavior":"allow","updatedInput":...}` or `{"behavior":"deny","message":...}`.

- **Approve:** `Write x.txt` waited in `awaiting approval`; after Approve the file existed with the right content, in the same live session.
- **Deny:** after Deny with a message, no file was created and the agent reported "blocked by a permission hook" with the message. The agent can be told why.
- **Timeout and failure.** The supervisor denies after 10 minutes with no answer, and the helper fails closed (deny) if the supervisor is unreachable. Pending approvals do not survive a supervisor restart (the agent process dies too).
- **Without the prompt tool**, `--permission-prompts none` denies automatically and says so; the denial text claims all further approvals are denied for the rest of that session, so the round-trip is the better design.
- **Plan mode** ends in `ExitPlanMode`, which arrives as an approval request: the plan itself becomes the thing the human approves.
- **Token handling.** The approve route needs a random per-run token that reaches the helper through its environment. The token sits in `mcp.json` (mode 0600) in the data dir; a real service would keep it in the credential service.

**Permission modes** (`--permission-mode`, chosen per session on the page; changing it restarts the process with `--resume`). The tool allowlist stays fixed: only `Read` and a few harmless `Bash` prefixes.

| Mode | Observed for `Write` |
| --- | --- |
| `manual` | Approval requested |
| `acceptEdits` | Written with no prompt |
| `dontAsk` | Denied silently, no approval event |
| `auto` | Approval requested, same as `manual` in every case below |
| `plan` | Agent writes a plan, then `ExitPlanMode` raises an approval |
| `bypassPermissions` | Not offered: it switches every prompt off and the spike runs on the host |

**`auto` versus `manual`, six actions** (every approval was denied, so only what the CLI allowed by itself ran; all of it harmless in the scratch repo):

| Action | `auto` | `manual` |
| --- | --- | --- |
| `Bash: pwd` | Ran, no prompt | Ran, no prompt |
| `Bash: git status` | Ran, no prompt | Ran, no prompt |
| `Bash: touch t1.txt` | Asked | Asked |
| `Write` a file | Asked | Asked |
| `Bash: curl -sI https://example.com` | Asked | Asked |
| `Bash: rm a.txt` | Asked | Asked |

In headless mode `auto` behaved exactly like `manual`: read-only commands are allowed by the CLI itself in both, and anything that writes, reaches the network or deletes was asked in both. Whatever `auto` is meant to do (a classifier, or something tied to an account tier) is not visible here, so do not rely on it to reduce prompts.

These map onto the design's autonomy table (§6): `acceptEdits` and the allowlist are `auto`, `manual` is `ask`, `dontAsk` is `forbid`. The table is per action; the CLI's modes are coarser, so the supervisor still has to enforce the real policy outside the agent.

**Compound shell commands.** `Bash(ls:*)` style allow rules do not match a combined command such as `go version && ls -la && cat a.txt`; the CLI asks about the whole command. An allowlist for agents needs a plan for compound commands.

**Large tool inputs.** A `Write` carries the whole file in the tool call. The harness caps tool-call and approval inputs at 2,000 characters in events and the log (`{"truncated":true,"bytes":N,"preview":...}`); the full input stays with the agent.

**Cancel.** SIGINT stops the process immediately, even mid-tool-call. The session stays resumable. There is no cooperative "stop after this turn".

## Codex CLI

`codex exec --json --ephemeral --sandbox read-only --skip-git-repo-check -C <dir> "<prompt>"` emits `thread.started` (with `thread_id`), `turn.started`, then in this run `error` and `turn.failed`. The ChatGPT login had reached its usage limit (until 3 October 2026, 19:12), so the run failed before doing any work.

- The usage limit is reported only as human text; the reset time sits inside the message, with no structured field. The adapter would have to parse it or poll another source.
- The prompt is passed once as an argument or on stdin. There is no streaming input flag in `codex exec --help`, so mid-run injection is probably unsupported in `exec` mode. **Unverified.**
- `codex exec resume <id>` and `fork` exist, so resume between turns is likely.
- Everything else is untested. Re-run after the limit resets.

## Antigravity (`agy` 1.1.12)

Installed here. `agy -p "<prompt>" --output-format stream-json` ran headless with no login prompt, in about 19 s. A third-party note says headless mode goes straight to the Gemini API without an account session, and that Antigravity 2.0 may have restricted headless use for authenticated memberships; both are **unverified** here, so check the current docs.

- **Events.** Newline-delimited JSON: `init` (`conversation_id`, `cwd`, `permission_mode`, `tools`), repeated `step_update` (`step_type` of `user_input`, `tool`, `agent_response`, `checkpoint`, with `state` `ACTIVE`/`ERROR`, `tool_info`, `usage`), then `result` (`status`, `response`, `error`, `num_turns`, `usage`). A clear, typed model that normalizes easily.
- **Flags of interest.** `--output-format text|json|stream-json`, `--conversation <id>` and `--continue` (resume), `--mode accept-edits|plan`, `--sandbox`, `--print-timeout` (default 5 minutes), `--dangerously-skip-permissions`, `--json-schema`.
- **No streaming input.** `--print` takes one prompt and exits, so there is no mid-run injection; a follow-up is a resumed turn via `--conversation`. Like Codex `exec`, a degraded mode.
- **Permissions in print mode.** The agent tried `list_dir` on the home directory (outside the workspace) and was denied automatically; the whole run then ended with `result.status: ERROR` instead of carrying on, unlike Claude Code. The tool list includes `ask_permission` and `ask_question`, but nothing in print mode gave a way to answer them. A remote-approval design for it is untested.
- **Verdict.** Drivable headless with structured events and resume, so the gate in design §12 is met for read and run use. It lacks mid-run injection and a visible approval channel. Treat it as a second-tier adapter alongside Codex.

## Consequences for the design

- §5.2: mid-run message injection and a structured event stream are achievable with Claude Code, so keeping them required for release 1 is realistic. For Codex they may need a degraded mode (message delivered as a resumed turn).
- §5.2 and §4.2: approvals are a real, live Decision: the agent blocks on an MCP call, the Decision stays open until a human answers, the supervisor answers allow or deny with a reason. Add a timeout (fail closed) and treat the plan in plan mode as an approval. The adapter capability is "approval prompts routed to the host", which Claude Code has and Codex and Antigravity (print mode) do not, as far as tested.
- §6: the CLI's permission modes are coarser than the per-action autonomy table. Offer them as presets, never offer `bypassPermissions` outside an isolated environment, and enforce the real table outside the agent.
- Agent adapters without streaming input (Codex `exec`, `agy -p`) need a degraded mode: a message is delivered as a resumed turn, and the UI labels it.
- §5.2 and §7: the usage window and reset time are available from Claude Code; for Codex they are text.
- §5.3: persist the transcript and agent session ID. Resuming from the agent session works.
- §9.3: delivery semantics for a sent message are "at the next step", not instantaneous. Cancel is a hard interrupt.

## Not tested

A login that expires in the middle of a session, what `rate_limit_event.status` reads when the usage window is exhausted, multi-session concurrency, running inside Apple Container, approvals for Codex and Antigravity, and a real Codex run (usage limit).
