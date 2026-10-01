---
title: Design
description: Architecture, security model and delivery plan for workharbor.
weight: 1
toc: true
---

**CLI:** `whr` · **Status:** Revised design, 30 September 2026 · No implementation or compatibility testing yet.

Revision of the original discussion summary (`agent-work-supervisor-summary.md`) after a four-role team review (architecture, security, product/CLI, feasibility/ops). Review ratings are value/effort out of 10. Claims about Apple Container, Socktainer, Coder and Portainer come from the original sources and are **unverified** until the spikes in §12 are done.

## 1. Goal

A self-hosted service in which AI coding agents carry out project work independently while one developer acts as human-in-the-loop (HitL): intervening, answering questions, reviewing and approving.

- Agents work repository issues, modify code, run tests, update issues and open or update PRs.
- Usage is a live coding assistant, not an automation pipeline: the developer chats with an agent about the work, then detaches while it works for 15 to 60 minutes or longer, and returns when it needs them. A personal tool with one developer and a handful of concurrent sessions.
- The developer attaches via chat, SSH or an editor temporarily. Disconnecting never interrupts the agent.
- Web UI and `whr` CLI are two front ends over one service layer: the CLI calls the JSON API, the web UI is server-rendered HTML (see D8).
- First host: Apple-silicon Mac mini (16 GB, ~1 TB) on Apple Container. Other runtimes later via adapters.

The central concept is an **agent task supervisor with managed workspaces**, not an editor-centred dev environment.

**Positioning.** workharbor is a self-hosted, agent-agnostic remote for coding agents. From a phone or a browser the developer starts a task, chats with the agent, watches progress, answers its questions and pauses or cancels it, with Claude Code, Codex CLI and later other agents behind one interface (§5.2). It runs on the developer's own machine with their existing agent logins, so there is no hosted service in the middle. Hosted remote offerings from the agent vendors cover one vendor each; where they fall short for a given agent (for example a missing mobile or remote client) is **unverified** and is checked in the §12 spike. The boundary stays: agents never merge, tag, release or deploy (§6).

## 2. Requirements

| Area | Requirement |
| --- | --- |
| Deployment | On-premises, one developer |
| Hardware | Apple-silicon Mac mini, 16 GB RAM, ~1 TB |
| Runtime | Apple Container (per-container VM isolation); Docker/Podman/other microVMs later |
| Capacity | **4 concurrent instances realistic, 8 a stretch goal** (see §8) |
| Overhead | Light operational and resource cost |
| Access | VPN, temporary SSH, VS Code / JetBrains via SSH; code-server optional and later |
| Forges | Gitea, Forgejo, Codeberg, GitLab, GitHub (release 1: one) |
| CI | Drone medium/long term, behind an adapter |
| Auth | OAuth for the five forges (release 1: static token) |

## 3. Key decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | Reuse runtimes, agents and IDE connections; build a thin supervision layer | Original 9/10 direction, kept |
| D2 | **Native Apple Container path** via the `container` CLI behind the runtime adapter. Portainer/Socktainer demoted to an optional compatibility shim (rating 7 → ~4–5) | Three layers, partial compatibility, known exec and restart-recovery gaps; an adapter is needed anyway |
| D3 | **Go, single static binary** (server, host worker and `whr` as subcommands), **SQLite in WAL mode**, embedded web UI (see D8), OpenAPI as the source for the JSON API and the `whr` client types, SSE for live events | One host, one user; easy launchd packaging, later Linux cross-compile |
| D4 | Agent runner chosen **first**, by scorecard (§12), preferring one with a structured headless protocol | Constrains the whole model |
| D5 | Approval boundaries are **data** (a policy table), enforced at the forge adapter, never by prompts | Prompt rules are not a security boundary |
| D6 | Supervisor is a **DB-first reconciler**, not a process tree | Only realistic answer to reboot/restart gaps |
| D7 | Harbor metaphor is for branding and UI section names only; CLI and API use plain nouns | Guessable, searchable commands |
| D8 | **Web UI: server-rendered Go with `templ` templates, htmx and SSE**, embedded in the binary. No Node toolchain and no CSS framework in release 1. The JSON API and the HTML handlers call the **same service layer**, so nothing is implemented twice | Release 1 is a small remote-control UI: live transcript, messages, start and pause/cancel, and answering Decisions (§9.3). One language, one binary, fewer dependencies and a smaller attack surface on the same origin. Revisit (Svelte) if the UI needs rich client-side state such as inline diff review or a takeover panel |
| D9 | **Documentation site: Hugo with the Hextra theme** (Go module, pinned version), deployed to GitHub Pages, dark by default with a light toggle | Go toolchain only, no Ruby or Node. Fast builds, built-in search and dark mode. Replaces the earlier Jekyll setup |

## 4. Domain model

| Object | Responsibility | Lifetime |
| --- | --- | --- |
| Task | Issue, instructions, decisions, progress, results, PR link | Until completed or cancelled |
| Workspace | Checkout/worktree, branch, files, tool config, caches | May span several runs |
| Run | One execution of an agent in an environment | Start, pause, resume, terminate |
| Environment (`env`) | Container or VM backing a run/workspace | Stopped or recreated independently of workspace data |
| **Decision** | A question, approval or review request raised to the human | Until answered |
| **ReviewCandidate** | Task → branch → commit SHA → PR → CI results, keyed by SHA | One per pushed revision |
| Event | Append-only record of instructions, observations, decisions, actions | Permanent (audit trail and UI feed) |

### 4.1 State machines

Task, run and environment each get their own small FSM with explicit legal transitions; these are specified before coding.

- **Task:** `queued → running → awaiting_guidance → ready_for_review → completed`, plus `cancelled`.
- **Run and environment:** tracked separately. Pause is a **run** state, not an environment state.
- **Coupling rules (examples):**
  - `ready_for_review` requires a stopped run and a pinned commit SHA.
  - Pausing a run never stops its environment.
  - A passing pipeline on an earlier SHA never marks the current revision ready.

### 4.2 Decision object

Fields: ID, task, kind (`question | approval | review`), blocking flag, options, created/answered timestamps, answering actor. The inbox, `whr inbox`, notifications and the audit trail hang off it. "Awaiting guidance" is the state a task enters while a blocking Decision is open.

**Approvals are live and blocking.** Spike #1 showed the pattern with Claude Code: the agent's permission prompt is routed to the supervisor, which opens an `approval` Decision carrying the tool name and a capped copy of its input. The agent stays blocked until a human answers allow or deny, with an optional reason that is passed back to the agent. Rules:

- **Fail closed.** Deny on timeout (the spike used 10 minutes) and when the supervisor is unreachable.
- **No silent survival.** A pending approval does not survive a supervisor restart, because the agent process does not. The reconciler marks the run `interrupted` and the ask is raised again on resume.
- **Plan approval.** In plan mode the agent's `ExitPlanMode` arrives as an approval whose subject is the plan.
- **Capped input.** Tool inputs in a Decision are capped (the spike used 2,000 characters); the full input stays with the agent. All of it is untrusted data.

### 4.3 Lifecycle rules

- IDE/SSH disconnect does not pause the agent or stop the environment.
- **Detached sessions.** The agent process runs inside the environment under the supervisor with no client attached. Chat is attach and detach over a persistent session, never the owner of the process. Session state (transcript and agent session ID) lives in the workspace, so it survives a supervisor restart and a reattach from another client. The reconciler (§5.3) resumes from that session after a restart.
- A long run can outlive its credentials. Expired auth or an exhausted usage window pauses the run and opens a blocking Decision (§5.2) instead of failing it.
- Pausing lets a human inspect or edit without concurrent agent changes. Human takeover holds an explicit, visible workspace lock (agent vs human); on resume the agent is re-synced with the human's changes.
- Stopping an environment preserves files, logs and agent session state.
- Agent-level checkpoint/resume is distinct from VM suspend and must work on backends without suspend.
- Environments awaiting review may be stopped.
- **Worker recycling** (recreate the environment, keep the workspace) is a first-class lifecycle action because freed guest memory is not returned to macOS.
- Only validated runner × backend pairs may claim resumability.

### 4.4 Persistence semantics (to decide before coding)

- Volume vs bind-mounted checkout.
- Which paths survive stop, rebuild and delete: repo, caches, agent session directory.
- Git worktree per task vs full clone.
- Retention and garbage collection of completed tasks and workspaces on the 1 TB disk.
- Forbid the `$HOME` mount by default; enforce in adapter tests.

## 5. Architecture

Logical components live in one Go binary on the Mac; boundaries are package interfaces, not microservices.

| Component | Responsibility |
| --- | --- |
| Control plane | Tasks, runs, workspaces, decisions, policies, integration config, event log, reconciler |
| Web UI | v0: read-only task list, event log and inbox; answering Decisions is the only write. Server-rendered (D8) |
| CLI (`whr`) | Same operations through the shared API |
| Host worker | Executes a **fixed set** of authorized lifecycle operations beside the runtime |
| Agent adapter | Start, observe, instruct, pause, resume a coding agent |
| Runtime adapter | Provision and manage environments |
| Forge adapter | Issues, PRs, reviews, metadata, webhooks; enforces the policy table |
| CI adapter | Interface only in release 1 |
| Credential service | Encrypted store (macOS Keychain holds the master key); issues per-run credentials |

Host worker and credential service are package boundaries on one host. Keep the contract remote-capable so a remote worker can be added later, without building inter-process auth now.

### 5.1 Runtime adapter

Covers provision, start/stop/delete, inspect, resource limits, logs, exec, storage and endpoint discovery. Capabilities are explicit and never assumed:

| Capability | Purpose |
| --- | --- |
| Isolation boundary | Shared-kernel container, guest kernel or full VM |
| CPU architecture | Image, toolchain and IDE compatibility |
| Persistent storage | What survives stop, rebuild, delete |
| Networking | Reachability and supported isolation controls |
| Suspend/checkpoint | Reported support only |
| SSH/browser access | Intervention endpoints |

Do not pretend backends share Docker semantics. One **runtime conformance suite** (the §12 checklist, automated) must pass for every backend; it turns capability flags into verified claims.

### 5.2 Agent adapter

Specified as explicitly as the runtime contract, and versioned: the contract carries a `contract_version`, and an adapter declares which version it implements. Release 1 ships Claude Code and Codex CLI as built-in adapters against it. Capability flags:

- headless / unattended operation
- **mid-run message injection (required for release 1)**: send a user message into a running session and report how it was delivered (injected now, or at the next turn). Without it an agent cannot be a remote-controlled assistant (§1); an agent that lacks it may only run in a degraded mode that the UI labels
- **structured event stream (required for release 1)**: messages, tool calls, diffs and test results as typed events, which feed the live transcript (§9.3)
- cooperative pause (e.g. stop after current turn)
- session persistence and resume
- PR/issue tooling
- "awaiting guidance" signal (how the agent raises a blocking Decision)
- **approval prompts routed to the host (required for release 1)**: the agent blocks on a permission request and the supervisor answers it with a human's allow or deny and a reason (§4.2). An agent without it can only run with a fixed allowlist and every other action denied
- **auth modes**, reported explicitly and never assumed:
    - `api-key`: the key stays in the host-side proxy and is issued per run (§7.3).
    - `subscription`: a consumer-plan login (for example Claude or ChatGPT sign-in) kept in a dedicated per-environment auth directory. The CLI refreshes the token itself, so it cannot sit behind the proxy.
- **auth and quota blocking states**: the adapter reports `auth_expired` and `quota_exhausted` (with the reset time when known). Each opens a blocking Decision and pauses the run instead of failing or retrying. Re-login is a UI action through a browser or device-code flow.

**Measured in spike #1** (issue #1; branch `spike/transcript`, `RESULTS.md`), with Claude Code 2.1.285, Codex CLI 0.159.2 and Antigravity `agy` 1.1.12 on one machine:

| Capability | Claude Code | Codex CLI | Antigravity |
| --- | --- | --- | --- |
| Headless, typed events | Yes (`stream-json` in and out) | `exec --json`; only start and error events seen | Yes (`--output-format stream-json`) |
| Mid-run message | Yes, picked up at the next model step | Not found in `exec` (unverified) | No: one prompt per run |
| Resume | Yes (`--resume`), same session ID | `exec resume` exists, untested | `--conversation <id>`, untested |
| Approvals to the host | Yes, through an MCP prompt tool | Untested | None found in print mode; the run ends in `ERROR` on a denial |
| Usage window | Structured: five-hour and seven-day windows with reset time | Text only, reset time inside the message | Not observed |
| Cancel | Hard interrupt only, session stays resumable | Untested | Untested |

Findings that shape the contract:

- A user message sent mid-run is delivered at the next model step, after the running tool finishes, not by interrupting it. The UI says so.
- The session ID only appears after the first user message, and user messages are not echoed in the output, so the supervisor logs its own.
- There is no cooperative pause; cancel is a hard interrupt.
- Agents without streaming input (Codex `exec`, `agy` print mode) run in the degraded mode: a message becomes a resumed turn, labelled in the UI.
- Token-level streaming is available from Claude Code (`--include-partial-messages` adds `text_delta` chunks, and the full message still follows). Coalesce deltas (about 150 ms) and let the final message replace them; consider keeping deltas out of the durable event log.
- A missing or expired login is signalled by an `assistant` event with `error: "authentication_failed"`, then a `result` with `is_error: true` and `subtype: "success"`. Detect `auth_expired` from the error code, never from `subtype`, and never from `apiKeySource`, which reads `none` both for a subscription login and for no login at all. A login that expires mid-session was not reproduced.

### 5.3 Reconciler

Desired state lives in the database. A loop compares it with actual runtime state, marks orphaned runs `interrupted`, and resumes from the agent session rather than the VM. This is the answer to Apple Container's missing restart-policy recovery.

### 5.4 Events and idempotency

Per-task append-only event log doubles as audit trail, UI feed and CLI stream. Every mutating command accepts an idempotency key.

### 5.5 Adapter plugins

New agents (and later runtime or forge backends) are added as **out-of-process plugins**, not in-process code. A plugin is a separate executable that speaks the versioned adapter contract (§5.2) over stdio or a local socket (JSON-RPC style). Go's in-process `plugin` package is not used: it is fragile and would put third-party code inside the supervisor.

- **Release 1:** the contract is the design; Claude Code and Codex CLI are built-in adapters against it. No loader.
- **Medium term:** a plugin loader, once two built-in adapters have proved the contract. Whether an existing agent-client protocol (for example Zed's ACP) already covers part of the contract is **unverified**; check it in the §12 scorecard and reuse it if it fits.
- **Conformance.** A plugin declares its capabilities and must pass the same conformance suite as a built-in adapter, so a capability flag is a verified claim (§5.1).
- **Trust.** See §7.8: plugins are installed explicitly and run isolated.

## 6. Policy and autonomy

Autonomy is a per-repo/per-task policy table: **action → `auto | ask | forbid`**.

| Action | Default |
| --- | --- |
| Push to `agent/*` branches | auto |
| Open/update PR, comment on issue | auto |
| Merge, tag, release, deploy | **forbid** for the agent; human-gated |
| Sensitive actions triggered by untrusted input | ask |

Enforcement is outside the agent: forge branch protection, required human review, and a bot identity that cannot bypass them. Approval is per commit SHA (ties to ReviewCandidate). Every approval is a Decision record.

**Agent permission modes.** The agent CLIs have their own coarse modes. In the spike with Claude Code (a fixed allowlist of `Read` and a few harmless shell prefixes) they behaved as follows for a file write:

| Mode | Behaviour |
| --- | --- |
| `manual` | Asks (an approval Decision, §4.2) |
| `acceptEdits` | Writes without asking |
| `dontAsk` | Denies silently |
| `plan` | Plans read-only, then asks the human to approve the plan |
| `auto` | Asked in the one test; what it approves by itself is untested |

Rules for using them:

- Offer them as per-session presets over the action table, never as the policy itself. The table is per action and enforced outside the agent; the modes are coarser and live inside it.
- `bypassPermissions`, which switches every prompt off, is never offered by default and never outside an isolated environment.
- A mode is fixed when the agent process starts. Changing it on a running session restarts the process with `--resume` and keeps the session and transcript.
- Prefix allow rules such as `Bash(ls:*)` do not match a compound command like `a && b`; the CLI asks about the whole command. An allowlist needs a rule for compound commands (match each part, or ask).

## 7. Security

Threat model and autonomy policy are written before the build.

1. **Untrusted input.** Issue text, PR comments and CI logs are untrusted. Use trust tiers by author (owner vs external); hold or flag runs on issues from unknown authors. A run that combines private data, untrusted input and outbound network requires approval.
2. **Default-deny egress** through a logging allowlist proxy: forge, package registries, LLM API only. Block LAN, host, other workspaces and cloud-metadata addresses. Add to the compatibility checklist; Apple Container network isolation controls are unverified.
3. **Credentials.** Run-scoped, short-lived, single-repo, non-extractable. GitHub App installation tokens (~1 h); per-repo bot tokens or deploy keys for Gitea/Forgejo/GitLab. Inject through a git credential helper or host-side proxy so raw tokens never reach env vars, disk or logs. In `api-key` mode the LLM API key stays in the proxy. In `subscription` mode the consumer-plan login lives inside the environment (§5.2), is long-lived and not scoped to a repo, and leaks if the agent is compromised. **Accepted risk** for a single-developer, watched personal tool; limit it with a dedicated auth directory per environment (never `$HOME`), the egress allowlist, and revocation at the vendor when an environment is deleted. Revoke run-scoped credentials at run end. Redact retained transcripts. Agent and CI credentials are separate.
4. **Isolation policy, testable.** Reject mounts of `$HOME`, `~/.ssh` and runtime sockets. Non-root agents, read-only rootfs where feasible, hard CPU/memory/disk quotas, per-run timeout and token/cost budget. Escape tests (guest cannot reach host or Socktainer socket) in the conformance suite. The VM boundary does not protect what is deliberately exposed.
5. **Supervisor identity.** Login allowlist of forge users, PKCE and `state`, short-lived sessions, scoped revocable CLI tokens, CSRF protection, API bound to loopback/VPN, forge tokens encrypted at rest, webhook signature verification. Link accounts by provider instance + stable user ID, never by email.
6. **SSH/IDE access.** Short-lived per-session SSH certificates or keys, no password auth, jump host only over VPN, code-server never public and always authenticated, treat Open VSX extensions as supply-chain risk.
7. **Audit and kill switch.** Tamper-evident append-only log stored outside the workspace, linked to commit SHA. `whr kill-all` stops all runs and revokes tokens. Alert on anomalous egress or token spikes. Optional: signed bot commits.
8. **Plugins.** A plugin handles sessions, credentials and workspace access, so it is a supply-chain risk. Default deny: plugins are installed only by explicit developer action, from a pinned version or hash, and run out of process with the same isolation as any agent environment. They never receive host credentials, `$HOME`, `~/.ssh` or runtime sockets; they get only the per-run credentials a built-in adapter would. Their capabilities are checked by the conformance suite, and every plugin action appears in the audit log.

Separate identities: login identity, connected forge accounts, agent (bot) identity, supervisor sessions.

## 8. Resources

Starting estimates, to be replaced by measurement. Assumes API-backed agents, not local inference.

| Workload | Guest memory | Target |
| --- | --- | --- |
| Terminal agent, Git, small scripts | 0.75–1 GiB | up to 6–8 light (stretch) |
| Agent, language server, moderate builds | 1.5–2 GiB | ~4 |
| JetBrains indexing or heavy builds | 3–4 GiB | 1–2 plus light workers |

Review corrections to the original budget:

- 8–10 GiB guests + 4–6 GiB macOS leaves near-zero slack on 16 GB. **Plan for 4 concurrent instances; 8 is a stretch.**
- Per-VM overhead sits outside the guest limit; a Linux kernel plus a Node-based agent is typically 300–500 MB, so the 0.75 GiB floor is tight.
- Freed guest pages are not returned to macOS: recycling is policy, not an occasional fix.
- Admission control uses host memory pressure (`memory_pressure`, `vm_stat`) plus static limits; heavy jobs are serialised. A simple admission counter ships in release 1; full scheduling is deferred.
- Benchmark on the real Mac mini before designing the scheduler.
- Explicitly set CPU/memory; `container machine` defaults to half host RAM and shares the host home.

## 9. Interfaces

### 9.1 CLI grammar

Noun-verb with short aliases. Nouns: `task`, `ws`, `run`, `env`, `inbox`. `whr task create` is the canonical long form; common verbs are hoisted.

```bash
whr login --server <url>
whr run <issue-url> [--backend apple]     # create + start; the core demo
whr ls [--json]
whr show <task>                            # review card: diff stat, tests, CI for current SHA, agent notes, open decisions
whr logs <task> -f
whr watch                                  # live event stream
whr say <task> "msg"                       # or -f guidance.md, or - for stdin
whr pause|resume|cancel <task>
whr inbox [--watch]
whr approve|reject <decision>
whr diff <task>
whr ssh <ws> [--takeover]                  # --takeover pauses the agent and takes the lock
whr ssh --config                           # emit ~/.ssh/config snippet (ProxyJump) for VS Code/JetBrains
whr open <ws> --editor vscode
whr wait <task> --for decision|done
whr kill-all
whr doctor                                 # server, auth, runtime capabilities, SSH config
```

Avoid `review` as a verb (ambiguous). Task refs accept a short ID, a prefix or `repo#42`. Names are provisional.

### 9.2 Scripting contract

- **Exit codes:** 0 ok · 1 error · 2 usage · 3 not found · 4 auth · 5 conflict/wrong state · 6 needs human input (`whr wait`) · 7 timeout · 10 task failed.
- `--json` returns a stable envelope with `schema_version`; `--jsonl` for streams.
- Streams via SSE, resumable with `--since <event-id>`.
- `Idempotency-Key` on mutations; `--dry-run` for destructive actions.
- Stdout is data, stderr is human text; honour `NO_COLOR` and TTY detection.
- Shell completion generated by the CLI framework, with dynamic task/workspace ID completion.

### 9.3 Web UI

v0 shows task/issue, repo, branch, PR, recent actions, test results, pending Decisions and resource use, with a single summary card per task. Sections "Harbor" (overview) and "Inbox".

Because the app is a remote for coding agents (§1), v0 also carries the core remote-control loop:

- **Live transcript.** A structured, streamed view of the agent session (messages, tool calls, diffs, test results) over SSE. Not a raw terminal mirror, which reads badly on a phone.
- **Send a message** to the running agent (mid-run instruction injection, §5.2). Delivery is reported honestly: injected now, or delivered at the next turn.
- **Start a task** from an issue or a repo, choosing the agent.
- **Pause, resume and cancel** a run.
- **Answer Decisions**, as before.

Writes in v0 are therefore: answer Decisions, send messages, start tasks, pause/resume/cancel. Editor launch and takeover (`whr ssh --takeover`) come after v0.

**Stack (D8).** `templ` templates rendered by the Go server, htmx for partial updates and form posts, and the htmx SSE extension for live event and inbox updates. htmx is vendored and version-pinned; styling is plain CSS with design tokens shared with the documentation site (navy and teal, light and dark). Pages are semantic HTML first, so they work without JavaScript for reading. Handlers stay thin: they call the same service layer as the JSON API. Diffs are server-rendered (or use a small library such as diff2html); an interactive terminal (xterm.js) is out of scope for v0.

### 9.4 Notifications

Push when a blocking Decision stops a task: the value of a supervisor is not having to watch it. This includes `auth_expired` and `quota_exhausted` (§5.2), which are the most likely reasons a detached run stalls.

**Default channel: ntfy** (phone and desktop apps; self-hosted or ntfy.sh). Generic webhook and macOS notification are secondary channels; Pushover, Telegram and Web Push are later options. ntfy's iOS delivery through a self-hosted server is **unverified**.

- **Events:** a new blocking Decision (question, approval, review), `auth_expired`, `quota_exhausted`, and a run that ended or failed. Deduplicate and rate-limit per task so a stalled run does not notify repeatedly.
- **Generic payload.** Task ID, event kind and a link only. Never issue text, code, logs, transcripts or tokens: the message leaves the host, may pass a public relay, and issue text is untrusted input.
- **Link, not action.** The notification opens the task in the web UI behind the supervisor login. No approve or answer buttons in the push. Approvals stay per commit SHA (§6).
- **Reachability.** The API is bound to loopback or VPN (§7.5), so the link uses the VPN hostname and the phone needs that VPN to open it.
- **Topic protection.** A long random topic, or an access token on a self-hosted server. Keep the topic and token in the credential service, never in the repo or logs.
- **Best effort.** The inbox stays the source of truth; a push can arrive late or be lost.

### 9.5 Onboarding

First run is a guided sequence of six steps. The steps are the contract; the surface differs by phase. Release 1 delivers them through `whr login`, `whr doctor` and a config file, because the v0 web UI is scoped to remote control of running tasks (§9.3). A web wizard over the same service layer is a medium-term item (§13). Each step can be skipped and re-run later.

1. **Sign in.** Server URL (reached over the VPN, never public) and the single static access token, stored encrypted. OAuth sign-in comes later (§10).
2. **Connect the forge.** One forge in release 1, GitHub through a bot token; Gitea, Forgejo and GitLab later. Verify the limits the forge enforces, not prompts (§6): the bot can push `agent/*` branches and open PRs, branch protection requires a human review, the bot cannot bypass it, and merge, tag, release and deploy stay forbidden.
3. **Choose the agent login.** `subscription` (device-code sign-in; nothing typed into the web page) or `api-key` (kept in the host proxy), per §5.2. The subscription option states the accepted risk of §7.3.
4. **Check the host.** The checks of `whr doctor`: server and token, container runtime, forbidden mounts rejected, default-deny egress, agent session surviving a reboot, capacity (plan for 4 concurrent environments, §8). A check that has not been verified is reported as not verified, never as passed (the Apple Container isolation claims are unverified until the §12 spike).
5. **Set up phone notifications.** ntfy provider (self-hosted or ntfy.sh), a generated random topic stored in the credential service, and a test push that carries the generic payload of §9.4. Remind that the link needs the VPN.
6. **Ready.** Summary of what was configured and what is not yet verified, then the first command: `whr run <issue-url>`.

## 10. Forge, CI and identity integrations

Keep Git transport separate from forge API operations. Release 1 ships **one forge** (Gitea or GitHub) with a PAT or bot token and no manual-handoff half-state.

| Provider | Login | Automation |
| --- | --- | --- |
| Gitea | OAuth2, configurable instance URL | Scoped bot credentials |
| Forgejo / Codeberg | OAuth2; Codeberg as Forgejo preset | Forgejo adapter |
| GitLab | OAuth2, hosted or self-managed | GitLab adapter |
| GitHub | OAuth2 | GitHub App installation credentials |

Result chain: **Task → branch → commit SHA → PR → CI results** (ReviewCandidate). CI adapter (Drone, later Woodpecker) covers state, links, logs/artifacts, retry and cancel; it is an interface only in release 1. CLI login is browser-based with supervisor-issued credentials; a supervisor-owned device flow supports headless terminals. No separate identity service for a personal deployment.

## 11. Existing platforms

| Option | Use | Gap |
| --- | --- | --- |
| Coder | Workspace portal, IDE access | No task supervision; Apple backend unverified; external provisioners Premium |
| DevPod | Portable environments | Client-only; no central service |
| Portainer | Infra UI, templates, REST API | No agent progress/review semantics; add-ons need Business Edition + Kubernetes |
| Socktainer | Docker API over Apple Container | Partial compatibility; exec and restart recovery limits |
| Eclipse Che | Kubernetes dev workspaces | Heavy |
| code-server | Browser editor | No orchestration; Open VSX |
| **Agent-task supervisors** (OpenHands, Vibe Kanban, Sculptor, Coder Tasks) | May cover 60–80% of release 1 | **Not yet assessed; status and Apple Container support unverified** |

The original rating table missed agent-task supervisors. It now has an explicit "adopt/extend" column and is **re-rated after the spikes**.

| Strategy | Original fit | Revised note |
| --- | --- | --- |
| Existing runner + thin supervisor + native Apple Container | 9 | Preferred; runner unselected |
| Same supervisor via Portainer/Socktainer | 7 | → ~4–5; optional shim only |
| Coder workspace layer + task supervisor | 7 | Re-rate after spike |
| Portainer + templates alone | 4 | Insufficient |
| Full new Codespaces/DevPod replacement | 3 | Excessive scope |
| Adopt/extend an agent-task supervisor | — | **Unrated; spike first** |

## 12. Open decisions and spikes (reordered)

Ordered by what is cheap and blocks the most work.

1. **Runner scorecard** (value 10, effort 3). Target agents are Claude Code, Codex CLI and Google Antigravity; Aider, OpenHands, Goose and others are scored for reference. Claude Code ships first and Codex CLI second. Antigravity ships a CLI (`agy`) with a headless print mode, so the gate is met: spike #1 drove it headless with typed events and resume. It has no mid-run injection and no approval channel in print mode, so it is a second-tier adapter in degraded mode (§5.2). Its account requirements and vendor terms for headless use are **unverified**. Spike #1 (issue #1) measured Claude Code, and Codex CLI and Antigravity in part; the results are in §5.2, and still open are a real Codex run (usage limit until 3 October), a login that expires mid-session, what the usage-limit `status` reads once exhausted, and approvals for Codex and Antigravity. One page comparing them on: headless mode, permission/approval bypass, session-ID resume after process or VM kill, mid-run message injection (stdin vs resumed turn; a release 1 requirement, §5.2), structured event output (also required), how "blocked, needs human" is reported. Also score subscription sign-in for Claude Code and Codex CLI (all **unverified**): headless or device-code login, where the token is stored, whether it survives a container restart and a Mac reboot, refresh behaviour inside a container, what happens when two environments share one login, how an expired login or exhausted usage window is signalled, current vendor terms for this kind of use, and whether a run keeps going with no client attached. Pause via SIGSTOP or stop-after-turn is not a resumed session; most CLIs resume only between turns.
2. **Adopt-or-extend spike** (value 9, effort 3). Time-box 1–2 days on two of OpenHands, Vibe Kanban, Sculptor, Coder Tasks before committing to a build.
3. **Apple Container native spike**, merged with benchmarking. Compatibility checklist:
    - [ ] Create/start/stop/delete representative workspaces
    - [ ] Enforce explicit CPU/memory
    - [ ] Preserve project data across stop/start and rebuild
    - [ ] SSH, VS Code, selected JetBrains IDE (code-server optional)
    - [ ] Recover after runtime/manager restart and Mac reboot (LaunchAgent vs LaunchDaemon; auto-login/FileVault implications; `container system start` on boot)
    - [ ] Private registry pulls and credential handling
    - [ ] Agent auth directory and detached session survive environment stop/start and Mac reboot
    - [ ] VPN reachability, forwarding or jump host
    - [ ] **Default-deny egress and network isolation controls**
    - [ ] **Escape tests: guest cannot reach host or sockets; forbidden mounts rejected**
    - [ ] Memory behaviour at 4 then 8 instances, including pressure and swap
4. **Autonomy and approval policy** (§6) and threat model (§7): a security decision that feeds credentials and UI.
5. **Persistence semantics** (§4.4).
6. **Primary forge and login provider** for release 1.
7. **CI credentials and event handling** for Gitea/Drone (medium term).
8. Confirm stack (§3 D3, D8) and finalize the `whr` grammar.

Reboot considerations also include power-loss/UPS behaviour and macOS auto-update reboot policy.

## 13. Delivery

### Release 1: one vertical slice

- [ ] Apple Container backend, one host, native adapter
- [ ] One agent runner with observed progress and validated recovery
- [ ] Task/workspace/run/decision model with durable state, event log, reconciler
- [ ] `whr` CLI (scripting contract, completion, `doctor`)
- [ ] Web UI with inbox, live transcript, send-message, start task, pause/resume/cancel (§9.3)
- [ ] SSH access (certificates, `whr ssh --config`)
- [ ] Policy table, per-run credentials, egress proxy, resource budgets, audit log, `whr kill-all`
- [ ] Installable PWA as the phone client: web app manifest and a service worker for the app shell, so the remote-control UI (§9.3) installs to the Home Screen. Stays inside the server-rendered stack (D8), needs HTTPS on the VPN hostname, and uses per-device revocable tokens
- [ ] Single static-token login
- [ ] One forge via PAT/bot token
- [ ] Runtime and forge adapters as interfaces with one implementation each

**Explicitly out of release 1:** code-server, JetBrains validation, OAuth, editor launch and takeover in the UI, CI adapter, multi-host, scheduler beyond an admission counter.

### Medium term

- [ ] OAuth providers, Gitea/Forgejo/GitLab/GitHub adapters
- [ ] Docker/Podman backends, remote Linux hosts (host worker becomes remote-capable)
- [ ] Antigravity adapter in degraded mode (`agy` print mode, §5.2, §12) and additional runners
- [ ] Out-of-process adapter plugin loader with conformance checks (§5.5, §7.8)
- [ ] Drone CI with revision-aware feedback
- [ ] Resource-aware scheduling, recovery improvements
- [ ] code-server, UI editor launch and takeover, `whr top`
- [ ] Web onboarding wizard over the same service layer (§9.5); release 1 uses `whr login` and `whr doctor`
- [ ] Web Push alongside ntfy (§9.4) for the installed PWA. iOS Web Push needs the app installed to the Home Screen (**unverified**)

### Long term

- [ ] Other microVM/VM platforms, Kubernetes where useful
- [ ] Multiple hosts and placement policies
- [ ] Wider forge/CI coverage

**Not planned:** native iOS and Android apps. The installable PWA (release 1) is the mobile client; the JSON API stays the contract for any client.

Phases are proposals, not a schedule.

## 14. Review log

Reviewers disagreed on two points; the resolutions adopted here:

- **Forge handoff:** manual compare-URL handoff (product) vs one forge, no half-state (architect). Adopted: one forge via PAT.
- **Web UI:** defer entirely (ops) vs minimal inbox (product, architect). Adopted: minimal read-mostly UI with inbox, since decisions are answered there.
- **UI stack (D8):** `templ` + htmx (option 1 of 7 weighed) over Svelte, Preact, React and others. The cost is that the UI does not consume the JSON API directly; the shared service layer keeps the two front ends consistent.

Skipped for now: separate identity service, Kubernetes, multi-host placement, Portainer/Coder UI integration.

## 15. References

Carried over from the original summary; compatibility claims require testing against pinned versions.

- Apple Container: [README](https://github.com/apple/container), [technical overview](https://github.com/apple/container/blob/main/docs/technical-overview.md), [container machine](https://github.com/apple/container/blob/main/docs/container-machine.md), [VS Code Remote-SSH example](https://github.com/apple/container/blob/main/examples/container-machine-vscode/README.md)
- Socktainer: [repository](https://github.com/socktainer/socktainer)
- Editors: [code-server FAQ](https://github.com/coder/code-server/blob/main/docs/FAQ.md), [JetBrains remote development](https://www.jetbrains.com/help/idea/remote-development-overview.html)
- Platforms: [Coder](https://coder.com/docs/about), [external provisioners](https://coder.com/docs/admin/provisioners), [DevPod](https://devpod.sh/docs/what-is-devpod), [Eclipse Che](https://eclipse.dev/che/docs/)
- Portainer: [Add-ons](https://docs.portainer.io/admin/add-ons), [custom templates](https://docs.portainer.io/user/docker/templates/custom), [REST API](https://docs.portainer.io/api/access)
- OAuth: [GitHub](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps), [Gitea](https://docs.gitea.com/development/oauth2-provider/), [Forgejo](https://forgejo.org/docs/latest/admin/advanced/oauth2-provider/), [Codeberg](https://docs.codeberg.org/integrations/keycloak/), [GitLab](https://docs.gitlab.com/api/oauth2/)
