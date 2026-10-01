---
title: Architecture
description: "The runtime and agent adapters, reconciler, events, plugins, tool store, usage and the host resources."
weight: 3
toc: true
---

## 5. Architecture

Logical components live in one Go binary on the Mac; boundaries are package interfaces, not microservices.

| Component | Responsibility |
| --- | --- |
| Control plane | Tasks, runs, workspaces, decisions, policies, integration config, event log, reconciler |
| Web UI | v0: task list, live transcript and inbox; writes are answering Decisions, sending messages, starting tasks, pause/resume/cancel and transcript purge (§9.3). Server-rendered (D8) |
| CLI (`whr`) | Same operations through the shared API |
| Host worker | Executes a **fixed set** of authorized lifecycle operations beside the runtime |
| Agent adapter | Start, observe, instruct, pause, resume a coding agent |
| Runtime adapter | Provision and manage environments |
| Forge adapter | Issues, PRs, reviews, metadata, webhooks; enforces the policy table |
| CI adapter | Interface only in release 1 |
| Credential service | Encrypted store (macOS Keychain holds the master key); issues per-run credentials |

Host worker and credential service are package boundaries on one host. Keep the contract remote-capable so a remote worker can be added later, without building inter-process auth now.

### 5.1 Runtime adapter

Covers provision, start/stop/delete, inspect, resource limits, logs, exec, storage and endpoint discovery. The Apple Container behaviour below was measured in the [Apple Container](../spikes/apple-container.md) and [host reachability](../spikes/host-reachability.md) spikes. Capabilities are explicit and never assumed:

| Capability | Purpose |
| --- | --- |
| Isolation boundary | Shared-kernel container, guest kernel or full VM |
| CPU architecture | Image, toolchain and IDE compatibility |
| Persistent storage | What survives stop, rebuild, delete |
| Networking | Reachability and supported isolation controls |
| Suspend/checkpoint | Reported support only |
| SSH/browser access | Intervention endpoints |

**The Go contract** (issue #20, `internal/runtime`):

- **`Spec`** carries what the hardened environment needs: image, owner and labels, CPUs, memory and a disk quota, the network (default or `--internal`, by name), the user (never root), a read-only root, `cap-drop ALL`, `--init`, tmpfs mounts and the mounts. `Validate` rejects a spec that is not hardened: a root or empty user, no `cap-drop ALL`, no `--init`, no owner, a relative or duplicate target. Mounts are `bind` (a host path, checked by `CheckMount`, §7.4) or `volume` (a name); a bind mount of a forbidden path never reaches the runtime.
- **Owner label.** Every environment carries the supervisor's owner label. `List(owner)` returns only environments with that label, and an adapter refuses to start, stop or delete one it does not own. Removal is by exact ID, never a pattern (`rm --all` deletes every container on the machine).
- **`Inspect`** returns a typed `Info`: the ID, owner, labels, state (`provisioning`, `running`, `stopped`, `deleted` as in §4.1) and the current address, which is empty unless the environment is running and is never stored. An unknown environment is `ErrNotFound`.
- **`Exec`** streams: separate stdout and stderr readers, `Wait` for the exit code, and cancellation through the context. Exec in an environment that is not running is `ErrNotRunning`. `ExecRequest.Stdin` is an optional reader the adapter copies into the process and closes at its end; with a pipe the caller keeps writing while the command runs, which is how the Claude Code adapter sends `stream-json` input and mid-run messages (§5.2). Without a reader the command's stdin is closed. `container exec` carries stdin through to the guest: the runtime conformance suite's stdin checks pass against the Apple Container adapter (`go test -tags applecontainer ./internal/runtime/apple`, container 1.5.0, issue #26).
- **Cancel kills the process in the guest.** Cancelling the context ends the stream, and the process inside the environment must be gone, not only the client (spike #2: SIGINT was not forwarded, and the agent kept running). The suite proves it with a second `exec` that looks for the process, so a backend that only returns from `Wait` fails.
- **Lifecycle is idempotent.** Start of a running and stop of a stopped environment succeed, so a reconciler can retry (§5.3).
- **Hardening is required, not possible** (issue #26). `Spec.Validate` is not enough on its own: a spec with the default network, a writable root, another task's volume and a bind of `~/.ssh` validated. `runtime.Prepare` is the one checked step. It runs `Validate`, requires `Network.Internal` and `ReadOnlyRoot`, checks every bind mount with `CheckMount` (and `CheckMountsWithin` the workspace roots), checks that every named volume belongs to this environment (`Owns`), and returns a `PreparedSpec` with unexported fields. `Provision` takes only a `PreparedSpec`, so nothing unchecked can reach the runtime. `CheckMount` returns the resolved path and `Prepare` puts that path in the prepared spec, so the adapter mounts exactly what was checked: a source swapped for a symlink after the check is not followed.
- **The contract owns the surroundings.** Provisioning creates the per-environment `--internal` network, the agent-home volume and the egress sidecar (`Spec.Egress`: the proxy's image, its allowlist and the host binary of the proxy), and deleting the environment removes all three, by exact name. `Resources` reports them, so the conformance suite can check that they exist and that they are gone. A writable volume is exclusive (§4.4): starting a second environment whose volume another running environment holds read-write is refused with `ErrVolumeBusy`.
- **Cache objects are read-only.** For a `--shared` topic, `Spec.Alternates` lists the cache's `objects` directory (issue #45), and `Prepare` turns each into a read-only bind mount at the same host path, because the clone's alternates file names that path. They are checked like any bind mount and must lie under the cache root.
- **Fakes and conformance.** `runtimetest` has an in-memory fake that can simulate a service restart (every environment becomes `stopped`, as measured below) and the conformance suite every backend must pass; the real Apple Container adapter (#26) runs the same suite on the Mac. `internal/runtime/apple` runs it behind the `applecontainer` build tag (`go test -tags applecontainer ./internal/runtime/apple`, `container` 1.5.0 with a local `fedora` image, about 30 s). The same tag runs the egress conformance: a direct connection fails, an allowlisted host answers through the sidecar (`CONNECT` 200), a raw IP and an unlisted host get 403, and the guest resolves no names. The adapter passes mounts with `-v` (its value is not split at commas, so a path cannot add an option; a `:` in a path is refused in `Prepare`), exec environment through a `0600` `--env-file` and never on the command line, reserves the `workharbor.` label prefix for its own labels, never `--ssh`, `--rm` or a published port, finds a volume's name in the mount type (its `source` is the image file path), and acts on exact names and the owner label only. An environment's `Addr` and `Proxy` are read from `container list` on every call and never stored.
- **Environment from the repository (D38, issue #77).** `internal/devcontainer` reads `.devcontainer/devcontainer.json` (or `.devcontainer.json`) with `git show` of the default branch, never from a working tree, as JSON with comments and trailing commas. It honours `image` or `build.dockerfile` and `build.context`, `containerEnv`, `postCreateCommand` as a string or an array, and `customizations.workharbor.egress` as a list of hosts the human must confirm (a request, never a grant). It refuses the whole file, naming every key, when it has `initializeCommand`, `mounts`, `workspaceMount`, `runArgs`, `privileged`, `capAdd`, `securityOpt`, or a `remoteUser` or `containerUser` that is root; every other key is ignored with a note, `features` and `forwardPorts` until the full support (#76). Building from a Dockerfile, the egress Decisions and the mapping to a `runtime.Spec` come with #76. This repository's own file builds `.devcontainer/Dockerfile` (the Go version of `go.mod`, a non-root `agent` user with a passwd entry, which the tests' `ssh-keygen` needs) and asks for `proxy.golang.org` and `sum.golang.org`.

Do not pretend backends share Docker semantics. One **runtime conformance suite** (the §12 checklist, automated) must pass for every backend; it turns capability flags into verified claims.

**Measured on Apple Container 1.5.0** (macOS 26.6.2, spike #2, issue #2):

| Capability | Observed |
| --- | --- |
| Isolation boundary | A lightweight VM per container: its own Linux kernel and one host runtime process each |
| CPU architecture | arm64 guests; a Rosetta flag exists and was not tested |
| Persistent storage | Named volumes (ext4 image files, exclusive while writable) and bind mounts survive delete; the root filesystem does not (§4.4) |
| Networking | The default NAT network reaches the LAN, the internet, other containers and host services bound to all interfaces. `--internal` networks block the internet, the LAN and other containers, but **not the host**: an internal guest reaches host services bound to the Mac's LAN address or to all interfaces (issue #69, branch `spike/host-reachability`). Only loopback-only listeners are out of reach (§7.2) |
| Resource limits | `--cpus` sets the vCPU count; `--memory` is enforced by a cgroup inside the VM (a larger allocation is killed with exit 137 and the container survives) |
| Restart policy | None. After a crash or a service restart every container is `stopped` (§5.3) |
| Suspend/checkpoint | None observed |
| SSH/browser access | Not tested; `exec` works |

Adapter rules that follow from it:

- Always pass `--init`: a stop took 145 ms with it and 5.3 s without, because PID 1 ignored SIGTERM.
- Never store a container's IP; it changes across recreate and restart. Read it with `inspect`.
- Remove only containers by exact ID. `container rm --all` deletes every container on the machine, including ones the supervisor did not create.
- Never pass `--ssh`, which forwards the host ssh-agent into the container.
- A container starts in about 1.1 s and `exec` is ready in about 100 ms, so recycling environments (§4.3) is cheap.
- **Cancel via in-guest launcher (`whr-shim`, D25)**: do not cancel by signalling the host `container exec` client; Apple Container fails with `"failed to send signal: missing signal in xpc message"` and leaves the guest process running. Instead, start commands under `/tools/whr-shim run -pidfile ...` (in their own process group) and cancel with `container exec <id> /tools/whr-shim kill -pidfile ... -grace 500ms`. Measured in spike #10: cooperative processes terminate via SIGINT in 10–15 ms (125 ms total exec roundtrip), stubborn trees ignoring SIGINT are killed via SIGKILL after grace in ~514 ms (600 ms roundtrip), exit code is preserved (130 on SIGINT), and zero orphan processes remain.

### 5.2 Agent adapter

Specified as explicitly as the runtime contract, and versioned: the contract carries a `contract_version`, and an adapter declares which version it implements. Release 1 ships Claude Code and Codex CLI as built-in adapters against it. Codex CLI lacks mid-run injection and host-routed approvals in what [spike #1](../spikes/agent-contract.md) could test (approvals and cancel: [spike #7](../spikes/agent-approval.md); planted configuration: [spike #68](../spikes/claude-config.md)), so in release 1 it runs in the degraded mode below, labelled in the UI. *Full mode* needs every capability marked so; an agent without them runs degraded (D12 requires full mode only of Claude Code, the first agent). Capability flags:

- headless / unattended operation
- **mid-run message injection (required for full mode)**: send a user message into a running session and report how it was delivered (injected now, or at the next turn). Without it an agent cannot be a remote-controlled assistant (§1); an agent that lacks it may only run in a degraded mode that the UI labels
- **structured event stream (required for every adapter)**: messages, tool calls, diffs and test results as typed events, which feed the live transcript (§9.3)
- cooperative pause (e.g. stop after current turn); reported false by every measured agent (D11)
- session persistence and resume
- PR/issue tooling
- "awaiting guidance" signal (how the agent raises a blocking Decision)
- **approval prompts routed to the host (required for full mode)**: the agent blocks on a permission request and the supervisor answers it with a human's allow or deny and a reason (§4.2). An agent without it can only run with a fixed allowlist and every other action denied
- **auth modes**, reported explicitly and never assumed:
    - `api-key`: the key stays in the host-side proxy and is issued per run (§7.3). Until that proxy exists, the key comes from a `0600` file on the supervisor's side (the configuration's `agent_api_key_env_file`) and reaches the agent through the runtime's env file, never a command line.
    - `subscription`: a consumer-plan login (for example Claude or ChatGPT sign-in) kept in a dedicated per-environment auth directory. The CLI refreshes the token itself, so it cannot sit behind the proxy.
    - **`whr` never handles a subscription credential (D40).** The human signs in inside the environment through the vendor's own flow, from a terminal attached to it, and the CLI writes the login to the agent-home volume, where it survives stop, start and rebuild (D16). `whr` does not read, copy, store, relay or log it, and does not record that terminal session. A subscription token found in the supervisor's configuration is refused. Which flows work behind the egress sidecar is spike #82.
- **auth and quota blocking states**: the adapter reports `auth_expired` and `quota_exhausted` (with the reset time when known). Each opens a blocking Decision and pauses the run instead of failing or retrying. Re-login is a UI action through a browser or device-code flow.

**The Go contract** (issue #20, `internal/agent`):

- **`ContractVersion`** is a constant; `Capabilities` carries the version the adapter implements, the flags above, the auth modes and whether it reports quota. `Mode()` computes the mode from them: *full* needs headless, structured events, mid-run injection and host-routed approvals; an agent with events and headless only is *degraded*; anything less is unsupported.
- **`Start` and `Resume`** take a `StartSpec` (environment, working directory, prompt, auth mode, permission mode, tool allowlist, approver and approval timeout) and return a `Session`. `Events` is a typed stream: message, tool call, tool result, diff, test result, usage, approval, `auth_expired`, `quota_exhausted` (with the reset time when known), result and error.
- **The session ID arrives with the `session` event.** `Session.ID()` may be empty until then (spike #1: Claude Code reports it after the first message). `Instruct` and `Stop` are called after the `session` event, and the suite waits for it; a started session that never reports an ID fails the suite.
- **Permission mode and allowlist.** `manual` routes every permission prompt to the host and needs an adapter with host approvals. `dontAsk` never asks: a tool on `AllowedTools` runs and every other is denied, and the approver is not consulted. A degraded agent (§5.2) runs in `dontAsk`; the allowlist is also what limits an agent that cannot be asked. An empty mode means `manual`. An adapter that cannot honour the mode refuses to start with `ErrUnsupported`. `bypassPermissions` and `auto` are not in the contract (§6).
- **The approval event is the record.** An `approval` event carries the request ID, the tool, the allow or deny and the reason, and the audit entry (§5.4) is written from it. The suite asserts on the event and not on text the agent prints.
- **Stop cancels a pending approval.** An approval that waits for a human is cancelled with the session (D23), so the approver's context ends, the approval event records a denial and the result is `stopped`.
- **Usage events** carry a typed payload (§5.7).
- **`Instruct` returns the delivery**: `injected` (now), `next_turn` (after the running tool, as measured for Claude Code) or `resumed_turn` (degraded: the message becomes a resumed turn). An agent without injection never claims the first two.
- **Approvals are a host callback, and fail closed.** The adapter blocks the agent on its permission request and asks the `Approver`. An error, a cancelled context or no answer within the approval timeout is a denial, and the agent sees the denial.
- **Cooperative pause stays a capability flag** (D11): a session implements `Pauser` only if the flag is true, and the conformance suite checks both ways.
- **Auth and quota end a run without failing it.** The session emits `auth_expired` or `quota_exhausted` and finishes with that status and no error, so the supervisor opens a blocking Decision (§4.2) instead of retrying; the session stays resumable.
- **Stop is a hard interrupt** and leaves the session resumable. **`agenttest`** has a scripted fake agent and the conformance suite; the Claude Code adapter (#25) and the Codex adapter (#35) run it too.

**The Claude Code adapter** (issue #25, `internal/agent/claude`). It runs `claude -p --input-format stream-json --output-format stream-json --verbose` through the runtime's `Exec` with `Stdin` (§5.1), as spike #1 did, and turns each output line into an agent event:

- **Session.** One process is one run of turns: the prompt is the first user message, a mid-run `Instruct` is another line on stdin (`next_turn`, as measured), and the adapter closes stdin after the `result` event so the process ends. A further turn is `Resume` with `--resume <id>`. The `session` event comes from `system/init`, which only appears after the first message.
- **Stop** cancels the `Exec` context. The runtime contract (§5.1) requires that this ends the process in the guest, so the adapter needs no signal of its own; the session stays resumable by its ID.
- **Events.** `assistant` text is a message, `tool_use` a tool call, `tool_result` a tool result, `thinking` is dropped, and `result` ends the run and produces one `usage` event with the model, the reported cost, the token counts from `result.usage` (input, output, cache read, cache write) and the usage windows from the last `rate_limit_event`. A tool result with `tool_result_meta[].non_execution_kind` (recorded: `permission-rule` for a denial) is a tool that never ran, and is recorded as denied.
- **`auth_expired`** comes from the `error` code `authentication_failed` on an `assistant` event, never from `subtype` or `apiKeySource` (§5.2, spike #1). The run ends `auth_expired` even though the `result` says `is_error` with `subtype: "success"`.
- **Permission modes.** `dontAsk` runs with `--allowedTools` and the CLI denies the rest silently; the adapter records each decision as an approval event (an allowed tool from its allowlist, a denial from the `permission_denied` system event). A tool that is not on the allowlist and returns an `is_error` result with no denial event is recorded as **denied**, never allowed, because that is also how a denial can come back; each approval ID is unique, including denials that match no tool use. `manual` needs the stdio approval channel (D26), whose reproducible evidence is pending (issue #7), and is refused with `ErrUnsupported` until then, so the adapter reports `HostApprovals` false and runs in the degraded mode (§5.2). It still reports mid-run injection.
- **Only the supervisor writes what the CLI reads.** The agent can write its own home (`CLAUDE_CONFIG_DIR`) and its repository, and Claude Code reads settings, hooks, MCP servers and skills from both. Measured with Claude Code 2.1.285 (branch `spike/claude-config`): with no flags, a hook planted in the repository's `.claude/settings.json` and `settings.local.json`, one in the agent home's `settings.json`, a project `.mcp.json` server and a project skill all took effect; with `--setting-sources user` the agent home's hook still fired, so pinning to `user` was not enough. With `--setting-sources ""` (no source), `--strict-mcp-config` and a supervisor-written `--settings` (a file or an inline JSON string), none of the planted hook, MCP server or skill took effect, and the supervisor's own hook did. The adapter passes those flags on every start and every resume, plus `--disable-slash-commands` (skills) as defense in depth. That planted permission rules are ignored too is **inferred**: the same loader reads them, but it was not shown without a model call.
- **What the CLI still allows on its own** (§6): the built-in read-only allows (spike #1: `pwd` and `git status` ran in `manual` and `dontAsk`), the built-in plugins, and whatever `--allowedTools` and the supervisor's `--settings` grant. Anything else is denied in `dontAsk`.
- **Bounded input.** stderr is kept to a capped number of lines and bytes. A result that has arrived is kept when `Stop` comes late, and `Resume` reports `ErrNoSession` only for an error result that says the session is missing; any other early failure is an ordinary error.
- **Unverified, taken from the spike's field names only:** `quota_exhausted` is raised when a usage window reaches utilization 1 and `rate_limit_info.status` is anything but `allowed` (what `rate_limit_event.status` reads when exhausted was not observed); a `Resume` of an unknown session is recognized as an error `result` before any `init` and no login failure (the CLI's real signal was not captured). **Verified on real streams** (spike #7, `spike/agent-approval` commit `22ddc7f`, `internal/agent/claude/testdata/recorded`): the `init` and `result` shapes, the token counts, the rate-limit windows with epoch-second reset times, the denial marker, and `session_id` on every event. Still constructed from the spike's description, because no recording exists: the expired login and the exhausted quota.

**Measured in spike #1** (issue #1; branch `spike/transcript`, `RESULTS.md`), with Claude Code 2.1.285, Codex CLI 0.159.2 and Antigravity `agy` 1.1.12 on one machine:

| Capability | Claude Code | Codex CLI | Antigravity |
| --- | --- | --- | --- |
| Headless, typed events | Yes (`stream-json` in and out) | `exec --json`; only start and error events seen | Yes (`--output-format stream-json`) |
| Mid-run message | Yes, picked up at the next model step | Not found in `exec` (unverified) | No: one prompt per run |
| Resume | Yes (`--resume`), same session ID | `exec resume` exists, untested | `--conversation <id>`, untested |
| Approvals to the host | Yes, on the host through an MCP prompt tool; in a container, the stdio control protocol (D26, evidence pending in #7) | Untested | None found in print mode; the run ends in `ERROR` on a denial |
| Usage window | Structured: five-hour and seven-day windows with reset time | Text only, reset time inside the message | Not observed |
| Cancel | Hard interrupt only, session stays resumable | Untested | Untested |

Findings that shape the contract:

- A user message sent mid-run is delivered at the next model step, after the running tool finishes, not by interrupting it. The UI says so.
- The session ID only appears after the first user message, and user messages are not echoed in the output, so the supervisor logs its own.
- There is no cooperative pause; cancel is a hard interrupt.
- Agents without streaming input (Codex `exec`, `agy` print mode) run in the degraded mode: a message becomes a resumed turn, labelled in the UI.
- Token-level streaming is available from Claude Code (`--include-partial-messages` adds `text_delta` chunks, and the full message still follows). Coalesce deltas (about 150 ms) and let the final message replace them; deltas are live-only and never stored (§5.4).
- A missing or expired login is signalled by an `assistant` event with `error: "authentication_failed"`, then a `result` with `is_error: true` and `subtype: "success"`. Detect `auth_expired` from the error code, never from `subtype`, and never from `apiKeySource`, which reads `none` both for a subscription login and for no login at all. A login that expires mid-session was not reproduced.

### 5.3 Reconciler

Desired state lives in the database. A loop compares it with actual runtime state, marks orphaned runs `interrupted`, and resumes from the agent session rather than the VM. This is the answer to Apple Container's missing restart-policy recovery.

Measured in spike #2 (issue #2):

- **No restart policy.** After the host-side runtime process of a container was killed, it stayed `stopped`, with its volume state intact, until started again.
- **A service restart ends everything.** `container system stop` took 0.4 s and ended every container VM at once; after `container system start` (0.4 s) every container was `stopped`, including those that had been running. Nothing came back by itself. Root filesystems, volumes, bind-mounted data, networks (also custom `--internal` ones) and images survived; every process inside the containers was gone.
- **After a Mac reboot** nothing starts the services either: there is no LaunchAgent or LaunchDaemon plist for them on disk. The supervisor's own launchd job must run `container system start --disable-kernel-install` and then reconcile. The flag skips the interactive kernel-install prompt, so the kernel must already be installed once at setup (`container system kernel set --recommended`); without it no container starts. A reboot itself was not triggered.
- **Recovery loop.** List containers, start those that should be running, wait for `exec` to answer (about 100 ms after start), then resume the agent from its session. Container IPs change on every start, so they are read again each time and never stored.

**The Go service** (issue #23, `internal/service`) is the one layer the JSON API and the web UI call (D8). Its reconciler is DB-first (D6) and takes the clock and the runtime and agent adapters as parameters, so tests run it on the fakes with an injected clock. One pass, per active task:

1. List the environments the runtime reports for this owner (`List(owner)`, never every container).
2. For each environment of the task, `ObserveEnv` with what the runtime says; an environment the runtime no longer knows is observed as gone. Live runs in an environment seen stopped or gone become `interrupted` and their open questions and approvals are superseded.
3. **A run without a session is lost.** A `starting` or `running` run that has no attached agent session is `interrupted`, even when its container is still up: after a supervisor restart the container survives and the agent process that the supervisor owned does not. A run whose launch is in progress is not lost; the service marks it before it calls the agent.
4. For each `interrupted` run, unless it waits for a reset it chose ("resume at reset", §4.2): check that `Resume` can succeed (no open login or quota question) **before** starting anything, so a run that waits for the human costs no container; start its environment if it is stopped, wait until `exec` answers (polling with the injected clock, bounded), observe the environment `running`, move the run to `starting` with `Resume` and relaunch the agent with `Resume(sessionID)`. The session ID is recorded on the run when the agent reports it (the `session` event), because the session, not the environment, is what survives. On success the run is `running`. A session the agent no longer knows (`ErrNoSession`), or a run that never reported one, ends `failed` at once, which opens a blocking retry-or-cancel Decision (§4.1). Any other error returns the run to `interrupted` and counts an attempt; when the attempts (3 by default) are used up the run ends `failed` too.
5. **Every resume starts with the briefing** (D27): the first message to the agent says that the process ended, that a tool call that was running may have had effects that are unknown or partial, which Decisions were superseded (their tool and input), and that the agent must check the workspace before repeating anything.
6. Resume the runs whose `resume_at_reset` Decision is due.
7. **A shutdown interrupts, it does not stop.** `Shutdown` ends the sessions, and the runs they served become `interrupted`, so the next start resumes them; only a stop the human asks for (cancel) or an agent that finished ends a run for good.

The sessions map is keyed by run, and a finished session removes its entry only if the entry is still that session, so a relaunch during a pass is not lost.

Only the container's state and the agent session ID are stored; the address `Info.Addr` is read for use and never written. Nothing in the reconciler sets a state by itself: it calls the aggregate.

### 5.4 Events, idempotency and retention

Per-task append-only event log doubles as audit trail, UI feed and CLI stream. Every mutating command accepts an idempotency key.

**Retention.** A chat grows with every message, tool call, tool result and diff, so the log has two tiers:

- **Audit entries** are never purged: state changes, Decisions and their answers, usage records (§5.7), approvals (including the tool and a capped input), commits and PR links, credential issue and revoke, policy denials, permission-mode changes, and the record of every purge.
- **Transcript content** is bulk and has retention: assistant text, tool inputs and results, diffs, thinking and attachments. Streamed token deltas are never kept durably, only the final message (§5.2).
- **Limits.** A size cap and an age limit per task, with the cap and limit set by policy, and a manual purge from the web UI and `whr purge` (§9.3). Deleting a task purges its transcript.
- **A purge records itself.** It deletes transcript content and keeps one audit entry: who, when, and what was removed (event count and bytes). Audit entries refer to transcript content by hash, so a purge leaves a verifiable gap and never silently rewrites history (§7.7).
- **The agent's own session is separate.** A purge does not touch the session the agent resumes from; shrinking the agent's context (compaction or a new session) is a different action with its own consequence, the agent forgetting, and is not offered as a purge.
- **Redaction at ingest.** Secrets are redacted before anything is written: events, Decision inputs and audit entries alike (§7.3). Audit entries are never purged, so redacting later would be too late. What remains is treated as untrusted data when shown.
- **Live-only events.** Token deltas and heartbeats go to connected clients through an in-memory fan-out and are never written. `--since` (§9.2) replays durable events only; a client that reconnects mid-message gets the final message when it is written.

**The store** (issue #21, `internal/store`):

- **SQLite in WAL mode** through the pure-Go driver `modernc.org/sqlite`, so the supervisor stays one static binary (D3) that cross-compiles to Linux. Foreign keys are on. Migrations are embedded SQL applied in order and recorded in `schema_migrations`; a database newer than the binary is refused.
- **Tables:** tasks (saved together with their runs, environments and review candidates), decisions, events and idempotency keys.
- **Versions and compare-and-swap.** A task aggregate and a Decision each carry an integer version. A save writes `WHERE version = expected` and bumps it; no row updated means another writer got there first, reported as a conflict (exit code 5). An answer and an expiry of one Decision cannot both win, and neither can two commands on one task.
- **One transaction.** The domain methods record the events a change produced (`TakeEvents`), and the store writes the new state and those events together, so there is never a state without its audit entry or an audit entry without its state.
- **Events are append-only** with a monotonic, never reused sequence number, which is what `--since` (§9.2) replays. Each has a tier. Triggers refuse any update and any delete of an audit row; the only way rows leave is `Purge`, which deletes transcript rows and appends one audit entry (who, when, how many events and bytes) in the same transaction.
- **Idempotency.** A mutating command runs under its key: the response is stored in the same transaction as the changes, so a replay of the same key and request returns the stored response without doing anything again, and the same key with a different request is refused as a conflict.
- **Redaction is on by default** (issue #22, `internal/redact`). The store redacts before it writes: event payloads (audit and transcript), the text of Decisions (subject, input, reason, answer, actor), task and candidate text, stored idempotency responses and the actor of a purge. What is held in memory is not changed, only what is persisted. Idempotency keys are opaque identifiers chosen by the client and are stored as given, so a client must not put a secret in one.
- **What the redactor knows.** Exact secrets registered for a run (the scoped tokens the credential service issued, §7.3) are replaced in every form they may take in text: raw, JSON-escaped, URL-escaped and base64. Well-known token formats are replaced whether or not they were registered: GitHub, GitLab, Anthropic, OpenAI, AWS, Slack and Google keys, JWTs, private key blocks, bearer tokens, passwords in URLs and values of secret-looking names (`token`, `password`, `api_key`, `authorization`). A numeric value, such as a usage count named `input_tokens`, is left alone, because usage records must stay correct (§5.7). The same redactor wraps log output and `slog` records, so a token is kept out of logs by the same rules.
- **A floor, not a proof.** A secret in a format the redactor has never seen, and not registered, passes. A canary test therefore pushes a token of every kind through every write path, closes the database and scans the files, including the write-ahead log, for it; a control run shows the scan finds a token that was not redacted.

### 5.5 Adapter plugins

New agents (and later runtime or forge backends) are added as **out-of-process plugins**, not in-process code. A plugin is a separate executable that speaks the versioned adapter contract (§5.2) over stdio or a local socket (JSON-RPC style). Go's in-process `plugin` package is not used: it is fragile and would put third-party code inside the supervisor.

- **Release 1:** the contract is the design; Claude Code and Codex CLI are built-in adapters against it. No loader.
- **Medium term:** a plugin loader, once two built-in adapters have proved the contract. Whether an existing agent-client protocol (for example Zed's ACP) already covers part of the contract is {{< status unverified >}}; check it in the §12 scorecard and reuse it if it fits.
- **Conformance.** A plugin declares its capabilities and must pass the same conformance suite as a built-in adapter, so a capability flag is a verified claim (§5.1).
- **Wire types.** Everything that crosses the seam has a stable JSON name (snake case): capabilities, the start spec, events, results, approval requests and answers. Times are RFC 3339 and a zero time is left out. An error crosses as a string code (`unsupported`, `unsupported_auth`, `no_approver`, `no_session`, `not_running`, `bad_spec`), and `agent.ErrorFor` maps a code back to the sentinel, so `errors.Is` works on the supervisor's side of the seam.
- **The approver is a reverse call.** The approver cannot be a Go callback across a process. The plugin sends an `approve` request (an approval request) to the supervisor on the same connection and waits for the answer. The supervisor applies the approval timeout and the fail-closed rule (§5.2), so a lost connection, a plugin that dies or a late answer is a denial, and the plugin never decides.
- **Trust.** See §7.8: plugins are installed explicitly and run isolated.

### 5.6 Tool store

Environments run **stock images**. Fedora and Ubuntu LTS are the first-class bases, pinned by digest and covered by the conformance suite, with Fedora the default; other bases work best-effort (D43). The agent CLIs (Claude Code, Codex CLI, later others) and the supervisor's own helpers live once in a versioned, immutable **tool store** on the host and are mounted read-only into each environment, in the manner of a Nix store. This replaces installing an agent in every container, which took about 11 s and 230 MB each in spike #2.

```
store/<hash8>-<name>-<version>-<platform>/bin/<name>     content-addressed, never modified
profiles/<profile>/bin/<name> -> ../../../store/.../bin/<name>
```

- **Verified on download.** Claude Code is fetched from the vendor's release URL and checked against its SHA-256 manifest; Codex CLI is the static musl build from its GitHub release. The store hash is recorded in the run's audit entry, so every run says exactly which tool version ran it.
- **One build per libc.** The glibc build of Claude Code ran in fedora, debian and ubuntu and failed in alpine; its musl build ran only in alpine; Codex's static build ran in all four. The adapter picks the profile from the image's libc (the vendor installer does the same, by looking for the musl loader). A tool that needs shared libraries beyond libc would need its closure in the store, as Nix does; none of the tested tools did.
- **Immutable from inside.** The store is mounted read-only; `touch`, `rm`, appending to a tool, `chmod` and replacing a symlink all failed, and re-hashing every entry afterwards showed no change. Writable state (the agent home, with its auth directory and session) stays on the per-environment volume (§4.4).
- **Versions are profiles.** Environment A ran Claude Code 2.1.286 and environment B 2.1.285 at the same time, each resolving to its own store entry. An upgrade is a new entry and a profile change, and a rollback is a profile change.
- **Mounting.** A read-only bind mount and a read-only volume both worked, with the same startup cost (about 100 to 130 ms for `claude --version`), and four containers shared one store at once. A volume can be attached read-only by several containers but is exclusive while writable (§4.4), so the shared store is read-only everywhere.
- **Network.** The agent starts without network, so the egress allowlist (§7.2) no longer has to permit the download host.

**Building the store** (issue #74, `internal/toolstore`, `whr tools build -store <dir> -shim <whr-shim>`). The pins live in the repository (`internal/toolstore/pins.json`: name, version, platform, release URL and SHA-256), so a version change is a reviewed commit and never a download-time decision. A download must match the pin **and** the SHA-256 the vendor's own `manifest.json` lists for the platform (the manifest of Claude Code 2.1.285 does, and its linux-arm64 entry equals the hash spike #7 measured); a mismatch in either is refused before anything is stored. The binary is placed content-addressed at `store/<hash8>-<name>-<version>-<platform>/bin/<name>` through a staging directory and a rename, and the entry, its `bin` and the tool are made read-only (`Verify` re-hashes every entry and reports a changed or writable one). `whr-shim` is added from the launcher built from the same commit, and a profile `profiles/<name>-<version>/` links to the entries with relative links, replaced in one move, so an upgrade or a rollback is a profile change. The vendor's manifest signature (`manifestSignatureEnforcement`) is not verified here: {{< status unverified >}}.

**The configuration file** (issue #74, `internal/config`) is JSON, decoded strictly (an unknown or misspelt key is an error), so the standard library is enough; TOML would add a dependency for comments only. It holds the repositories (`owner/name` and `clone_depth`), the three roots (cache, workspaces, tool store), the GitHub App ID and its key file, the agent-login env file, the API token file and the listen address. Secrets are paths to files, never values: each must be a regular file, not a link, owned by the user, with mode `0600` and some content, and outside the workspace root, where an agent could read it. The listen address must be a loopback IP (D29: a guest reaches every other address of the host); the roots must exist and must not overlap, since the workspace root is where an agent writes. `whr serve` validates all of it at start and reports every problem with its key. The full onboarding stays in #29.

Open: how new versions are discovered (a developer bumps a pin in a commit), and how the store is garbage collected.

### 5.7 Usage and cost

A remote for agents that run detached for an hour has to say what they used. The supervisor records usage per run and reports it; it never meters the model traffic itself.

- **Source: the agent's own reports.** Each turn becomes a `usage` event with a typed payload: model, token counts, the cost with its source, and the usage windows (name, utilization from 0 to 1, reset time). The token counts are optional: a missing block means the agent did not report them, which is not the same as zero, so a token budget never reads an unknown count as 0 tokens. An adapter that reports usage sets `ReportsUsage`, and the suite checks that the events are well formed. Claude Code's `result` event carries `total_cost_usd` and `usage` with input, output, cache-read and cache-creation tokens (recorded in spike #7). Antigravity sends `usage` in `step_update` and `result`; Codex CLI is {{< status unverified >}}.
- **Reported or estimated.** Every cost carries its source. `reported` is the agent's figure; `estimated` is computed by workharbor from a pinned, dated price table and labelled as an estimate everywhere it is shown.
- **Auth mode decides what the number means** (§5.2). In `api-key` mode cost is real spend. In `subscription` mode it is notional: the plan is paid flat, and the usage-window utilization (five-hour and seven-day, §5.2) is what limits the developer, so the UI leads with that.
- **One usage window per account (D40).** Concurrent runs on one subscription share its usage window, so the UI shows the window as one account-wide figure across all running tasks, not per task, and a quota stop pauses every run on that account with one Decision each (D23).
- **Kept as audit entries.** Usage rows are small and survive a transcript purge (§5.4), so totals stay correct after history is deleted.
- **Reports.** Totals per run, task, repository and day or month: `whr usage [--task <task>] [--since <time>] [--json]`, a usage line in `whr show`, and per-task usage plus a usage-window meter in the web UI (§9.3).
- **Budgets read the same counters.** The per-run and per-task token and cost budgets of §7.4 compare against these totals: a soft threshold notifies (§9.4), and a hard limit ends the task as `failed` (D13).

## 8. Resources

Starting estimates, to be replaced by measurement. Assumes API-backed agents, not local inference.

| Workload | Guest memory | Target |
| --- | --- | --- |
| Terminal agent, Git, small scripts | 0.75–1 GiB | up to 6–8 light (stretch) |
| Agent, language server, moderate builds | 1.5–2 GiB | ~4 |
| JetBrains indexing or heavy builds | 3–4 GiB | 1–2 plus light workers |

Review corrections to the original budget:

- 8–10 GiB guests + 4–6 GiB macOS leaves near-zero slack on 16 GB. **Plan for 4 concurrent instances on 16 GB, about 6 on 24 GB and about 10 on 32 GB (D32).**

**Capacity by memory** (estimates; issue #39 measures them). macOS, the supervisor and the VPN take 4–6 GiB; a moderate environment (agent and build, including VM overhead) about 2–2.5 GiB; heavy builds or JetBrains 3–4 GiB.

| Memory | Left for environments | Moderate environments | Heavy |
| --- | --- | --- | --- |
| 16 GB (budget) | about 10–12 GiB | about 4 | 1–2 |
| 24 GB | about 18–20 GiB | about 6 | 3–4 |
| 32 GB (recommended) | about 26–28 GiB | about 10 | 5–6 |

**Storage for about 4 concurrent environments** (estimates; issue #54 measures them): macOS, apps and Homebrew 35–50 GB; Apple Container images 2–10 GB; per environment a root filesystem of 1–3 GB and an agent-home volume with build caches of 3–15 GB; repository caches and topic clones by repository size; stopped environments awaiting review stay on disk; keep 10–20% free. That is roughly 120–250 GB in use, so 512 GB is comfortable and 256 GB is tight. A few stopped spike containers already took 8.2 GB on the development Mac.
- Per-VM overhead sits outside the guest limit; a Linux kernel plus a Node-based agent is typically 300–500 MB, so the 0.75 GiB floor is tight.
- Freed guest pages are not returned to macOS: recycling is policy, not an occasional fix.
- Admission control uses host memory pressure (`memory_pressure`, `vm_stat`) plus static limits; heavy jobs are serialised. A simple admission counter ships in release 1; full scheduling is deferred.
- Benchmark on the real Mac mini before designing the scheduler.
- Explicitly set CPU/memory; `container machine` defaults to half host RAM and shares the host home.
