---
title: Design
description: Architecture, security model and delivery plan for workharbor.
weight: 1
toc: true
---

**CLI:** `whr` · **Status:** Revised design, updated 1 October 2026 · The domain layer is implemented test first; there is no runnable service yet. Spikes #1 (agent contract) and #2 (Apple Container) are measured with committed scripts; #10 (cancel) and #7 (approvals) are reported in issue comments, and #7 is reopened for reproducible evidence. Results are marked in the sections they affect.

| Status | What | Where |
| --- | --- | --- |
| Decided | D1 to D37 | §3; open decisions in the [M0 milestone](https://github.com/wstein/workharbor/milestone/1) |
| Implemented | Task, run and environment state machines and their coupling rules; Decisions with fail-closed approvals; the policy table; mount checks; hardened host git with checkout checks, the repository cache and the editor copy; redaction at ingest; runtime and agent contracts with fakes and conformance suites; the SQLite store; the service layer and DB-first reconciler; the Claude Code adapter in degraded mode; the `whr-shim` launcher; `whr version` | `internal/domain`, `internal/policy`, `internal/runtime`, `internal/hostgit`, `internal/redact`, `internal/agent`, `internal/store`, `internal/service`, `cmd/whr`, `cmd/whr-shim`; issues #4, #8, #15–#23, #45, #49–#52, #55–#60, #63, #64; open follow-ups #66–#69; #25 partly |
| Spiked | Agent contract (Claude Code, Codex CLI, Antigravity); Apple Container; host cancel with `whr-shim`; approvals over stdio (evidence pending) | Issues #1, #2, #10 and #7 (reopened); results in §4.2, §4.4, §5.1 to §5.3, §5.6, §7 |
| Planned | The release 1 slice and the rest of release 1 | §13; [R1 Slice](https://github.com/wstein/workharbor/milestone/2) and [R1 Complete](https://github.com/wstein/workharbor/milestone/3) milestones |

Revision of the original discussion summary (`agent-work-supervisor-summary.md`) after a four-role team review (architecture, security, product/CLI, feasibility/ops). Review ratings are value/effort out of 10. Claims about Apple Container that spike #2 measured are stated as measured in §4.4, §5.1, §5.3, §5.6, §7 and §12. Claims about Socktainer, Coder and Portainer, and anything marked **unverified**, still come from the original sources and are unverified until the remaining spikes in §12 are done.

## 1. Goal

A self-hosted service in which AI coding agents carry out project work independently while one developer acts as human-in-the-loop (HitL): intervening, answering questions, reviewing and approving.

- Agents work repository issues, modify code, run tests and commit in their own checkouts. After a cleanup step and the developer's approval the supervisor pushes the branch and opens or updates the PR (§4.5).
- Usage is a live coding assistant, not an automation pipeline: the developer chats with an agent about the work, then detaches while it works for 15 to 60 minutes or longer, and returns when it needs them. A personal tool with one developer and a handful of concurrent sessions.
- The developer attaches via chat, SSH or an editor temporarily. Disconnecting never interrupts the agent.
- Web UI and `whr` CLI are two front ends over one service layer: the CLI calls the JSON API, the web UI is server-rendered HTML (see D8).
- First host: Apple-silicon Mac mini on Apple Container, recommended M6 with 32 GB memory and 512 GB storage (D32). Other runtimes later via adapters.

The central concept is an **agent task supervisor with managed workspaces**, not an editor-centred dev environment.

**Positioning.** workharbor is a self-hosted, agent-agnostic remote for coding agents. From a phone or a browser the developer starts a task, chats with the agent, watches progress, answers its questions and pauses or cancels it, with Claude Code, Codex CLI and later other agents behind one interface (§5.2). It runs on the developer's own machine with their existing agent logins, so there is no hosted service in the middle. Hosted remote offerings from the agent vendors cover one vendor each; where they fall short for a given agent (for example a missing mobile or remote client) is **unverified** and is checked in the §12 spike. The boundary stays: agents never merge, tag, release or deploy (§6).

## 2. Requirements

| Area | Requirement |
| --- | --- |
| Deployment | On-premises, one developer |
| Hardware | Apple-silicon Mac mini: M6 with 32 GB memory and 512 GB storage recommended; on a budget 16 GB, with 512 GB or with 256 GB plus an external SSD; 24 GB / 512 GB in between (D32, §8) |
| Runtime | Apple Container (per-container VM isolation); Docker/Podman/other microVMs later |
| Capacity | **4 concurrent instances realistic, 8 a stretch goal** (see §8) |
| Overhead | Light operational and resource cost |
| Access | VPN, temporary SSH, VS Code / JetBrains via SSH; code-server optional and later |
| Forges | Gitea, Forgejo, Codeberg, GitLab, GitHub (release 1: GitHub, D15) |
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
| D10 | **Decisions are rows in this table.** Each is proposed and settled in a GitHub issue labelled `decision`, then recorded here with its rationale. Separate decision-record pages come only if the design is split into several pages | One place to look; CONTRIBUTING already names this table as the record |
| D11 | **Cooperative pause stays an agent capability flag**, reported per adapter and never assumed. None of the measured agents has it (spike #1), so their adapters report it false; pause then means a hard interrupt followed by a resume from the agent session, and the UI says so | Keeps the contract ready for an agent that can stop after its current turn, without pretending the current ones can |
| D12 | **Release 1 starts with a CLI-only vertical slice:** `whr run <issue-url>`, then `whr logs -f`, `whr say` and `whr cancel`, and `whr approve` pushes the prepared `agent/*` branch and opens the PR, with Claude Code in Apple Container on one forge. The web UI, PWA, SSH, notifications and the Codex CLI adapter follow in the rest of release 1 | Proves the service layer, adapters and policy end to end before any UI, and gives D8 a working API to check against |
| D13 | **Task state machine** (§4.1): a terminal `failed` state, rework from `ready_for_review` back to `running`, and `awaiting_guidance` only for blocking Decisions raised by a live run, so "Ready to push?" leaves the task in `ready_for_review` | Gives exit code 10 a state, makes the rework path explicit, and keeps the review gate from looking like a stalled run in the inbox |
| D14 | **CLI framework: `spf13/cobra`**, without viper (issue #46). The root command silences cobra's own error and usage output, sets stdout and stderr explicitly, and `main` maps errors to `internal/exitcode` (§9.2) | The design needs generated shell completion with dynamic task and workspace IDs and a noun-verb grammar with aliases (§9.1, §9.2); cobra provides both and can generate the CLI reference. Rated above kong, urfave/cli and the standard library `flag` |
| D15 | **Release 1 forge: GitHub, through a GitHub App installation** (issue #6). The App is the bot identity: its installation tokens last about an hour and are scoped to the repositories it is installed on and to the permissions it asks for (contents and pull requests write, issues write, metadata read). Its private key lives in the credential service. A ruleset on the default branch requires a human review and lists no bypass for the App, so it cannot merge (§6). Gitea, Forgejo and GitLab follow behind the same forge adapter | The repository and its CI already live on GitHub, so the limits can be checked against a real ruleset at once. Short-lived, repo-scoped tokens match §7.3 better than a long-lived PAT, and an App is a separate identity that commits and audit entries can name. That an App's installation token cannot bypass a ruleset without being listed as a bypass actor is **unverified** until #27 tests it |
| D16 | **Persistence** (§4.4): repositories live on the host and are mounted into environments; the agent home (auth directory, session, caches) is one writable named volume per environment; build caches stay on that volume outside the checkout; the root filesystem is disposable | Measured in spike #2: volumes and bind mounts survive delete, the root filesystem does not, and caches on a bind mount roughly double warm builds |
| D17 | **Agent checkouts are per-task clones** of a bare cache whose objects are mounted read-only (§4.5); `git worktree` stays the developer's own tool and is never handed to an agent | A worktree's `.git` exposes the shared repository's branches, hooks and config (spike #2, item 9) |
| D18 | **Agents never push** (§4.5, §6). The agent commits in its checkout; the supervisor rebases, folds and checks the topic, opens a "Ready to push?" Decision per commit SHA, and pushes and opens the PR only after approval | Nothing leaves the host unreviewed, and policy is enforced outside the agent (D5) |
| D19 | **Stock images plus a shared read-only tool store** (§5.6): agent CLIs live once, content-addressed, on the host and are mounted read-only; versions are profiles | Spike #2: installing per container cost about 11 s and 230 MB; the store is immutable from inside and shared by several containers |
| D20 | **Adapters are built in for release 1 and out-of-process plugins later** (§5.5), never Go's in-process `plugin` package | Two built-in adapters prove the contract first; third-party code stays out of the supervisor process |
| D21 | **Task state machine, amending D13** (§4.1): a run paused by `auth_expired` or `quota_exhausted` moves its task to `awaiting_guidance`; a task fails only from `running` or `awaiting_guidance`, and a lost workspace in `ready_for_review` opens a review Decision (rework or cancel) | Settles the gaps found in the review of the D13 implementation without new transitions, so the code in `internal/domain` already agrees |
| D22 | **Build the supervision layer; adopt none of the agent-task supervisors** (issue #5, confirms D1). OpenHands, Vibe Kanban, Sculptor and Coder Agents were assessed from their docs and repositories (§11). Borrow: ACP as a candidate generic agent-adapter protocol (§5.5) and OpenHands' confirmation states; Sculptor's Claude control-protocol integration and editable message queue; Claude Remote Control's phone UX as a reference and a fallback for Claude | None meets the non-negotiable parts of release 1 together: Apple Container, default-deny egress per environment, approvals routed to a human for Claude Code and Codex under subscription logins, an agent that never pushes, more than one forge. Adapting one would replace its runtime, policy and forge layers, which is most of workharbor. Vendor remotes cover one vendor and push to GitHub only. Desk research only: the claims marked **unverified** in §11 were not tried |
| D23 | **Decisions around pauses** (§4.2): `auth_expired` and `quota_exhausted` are blocking `question` Decisions with fixed options (re-login and resume, resume now or at the reset, cancel) and no deadline; pausing a run supersedes every open Decision the run raised, questions and approvals alike, and they are raised again when the agent asks after resuming | Nothing is permitted by a login or quota answer, so it is a question, and waiting on it is safe, so it does not fail closed. A paused agent's process is gone (D11), so an answer could only reach a dead process or the wrong request; superseding reuses the restart rule |
| D24 | **Releases are human-signed tags built by GoReleaser into draft releases, distributed through a Homebrew tap** (§13, Releases). The human pushes a signed `v*` tag; a workflow checks it and has GoReleaser build `whr`, checksums, an SBOM and a build-provenance attestation into a **draft** GitHub release; you publish it, and only then is the tap updated. Versions are `0.x` until the JSON API, the adapter `contract_version` and the database migrations are stable; the first release, `v0.1.0`, is cut when the release 1 slice demo (issue #28) passes on the Mac mini | Tags and releases are already human-only (§6, AGENTS.md), and a signed tag is the trust anchor; the tag is also the Go module version, so there is no version file. GoReleaser covers cross-builds, checksums, SBOMs, signing and the tap in one pinned tool. The draft is where you check the assets before anyone can install them, so the tap must not point at a draft. A tap avoids notarizing a downloaded binary for now; whether a tap cask of an unsigned binary needs its quarantine attribute removed is **unverified** |
| D25 | **Launcher and host-initiated cancel** (§5.1, D19): agent and command processes run under `whr-shim`, a small static binary in the shared tool store. The launcher starts the command in its own process group (`setpgid`) and writes the PID to a file. Cancellation is triggered from the host via `container exec <id> /tools/whr-shim kill -pidfile <path> -grace <duration>`, which sends `SIGINT` to the whole process group and falls back to `SIGKILL` after the grace period | Signalling the `container exec` client fails in Apple Container (`missing signal in xpc message`) and leaves processes running in the guest (spike #2; issue #10, whose cancel of a real agent is pending in #7). `whr-shim` terminates the entire process tree reliably from inside the guest without leaving orphan processes (measured in spike #10: cooperative cancel exits in ~10 ms, stubborn trees killed after 500 ms grace in ~514 ms, 0 orphans) |
| D26 | **Approval channel: the stdio control protocol** (§4.2, §5.2, §12): Claude Code runs with `--permission-prompt-tool stdio` and `stream-json` in and out. Permission requests arrive as `control_request` (`can_use_tool`) messages on the `exec` stdout, and the supervisor answers with a `control_response` (allow or deny) on stdin. Whatever ends the channel, the run ends with a denial: the supervisor stops the agent with `whr-shim` (D25) and never lets a request outlive it | No network listener on the host or sidecar, no path from the internal network to the supervisor, and no approval token the guest could read. **Evidence is reported, not yet reproducible** (issue #7, reopened): issue comments report allow (4.23 s), deny (4.06 s) and a closed stdin failing closed (3.86 s) in a container, with no committed script or stream. A crashed supervisor end and an unanswered request (deadline) are **unverified**; D25 shows a dead `container exec` client can leave the guest running, hence the `whr-shim` stop |
| D27 | **Resume briefing** (§4.1, §4.2): when a run resumes after a pause, a cancel or an interruption, the supervisor's first message to the agent says what it knows: which tool call was running or waiting for approval when the process ended, that its effects are unknown and may be partial, and which open Decisions were superseded. The agent is told to check the workspace before repeating anything | Measured in spike #7, case 7 (branch `spike/agent-approval`): after a loop was killed 3 s into a Bash call, Claude Code's resumed session told the model *"The command was never executed … rejected before it could run"*, which was false. Left to the agent's own account, a resumed run may skip or repeat work |
| D28 | **Host software through Homebrew; agent CLIs only through the tool store** (manual, host setup). The Mac mini gets Apple Container, `whr` (the tap of D24) and a VPN client from a `Brewfile` installed with `brew bundle`, with `HOMEBREW_NO_AUTO_UPDATE=1` so nothing upgrades unasked. Claude Code, Codex CLI and other agent CLIs are never installed on the host for workharbor: they come from the verified, versioned tool store (D19). Nix (nix-darwin) remains possible for a host that already uses it | The host needs few packages, and D24 already distributes `whr` through a tap. Nix would be reproducible but heavy for one appliance, and it duplicates what the tool store does for agents. Whether Apple Container is packaged in nixpkgs is **unverified** |
| D29 | **The API listens on loopback only; a forwarder carries remote access; the token is always required** (§7.5). The API and web UI bind to `127.0.0.1`, which no guest reaches (issue #69). The phone reaches them through a forwarder: `tailscale serve` (the default), or, for a VPN that ends on the router such as a FRITZ!Box with WireGuard, a small proxy on the Mac's LAN address admitted by a `pf` rule to the router's VPN clients only. Every request needs the API token. A `pf` rule blocks the container subnets from the host's own addresses, and host services that listen on all interfaces are turned off or hardened | Issue #69 (branch `spike/host-reachability`, `run.out`) measured that every guest, `--internal` included, reaches host listeners on the LAN address or on all interfaces, and none reaches a loopback-only listener; spike #2's "`--internal` blocks the host" was wrong. A forwarder on any non-loopback address is reachable by guests too, so the token, not the address, is the guard there, and `pf` narrows it. Whether a guest reaches the Tailscale address, the `pf` rules themselves, and the Application Firewall's effect are **unverified** (issue #69) |
| D30 | **The forge board mirrors task state; the supervisor writes it** (issue #70). Through the forge adapter, as an optional capability, the supervisor keeps a project board current: a task awaiting guidance moves its card to "Needs you" (first), running → In progress, ready for review → Ready to push, completed → Done; Session names the agent, and the card links to the task in the web UI. Agents never write to the board. Starting a task by moving its card to an agent queue comes later, only through an "Accept this task?" Decision and the trust tiers (issue #71) | The GitHub board is a good planning dashboard and the web UI the control surface; mirroring keeps one current view without rebuilding a board in workharbor. Whether an App installation token can write fields on a **user-owned** project, and whether card-move webhooks exist for one, is **unverified**: issue #70 tests it first, and moving the repository and project into an organization is the fallback the human decides on |
| D31 | **GitHub is reached through its API from Go, with the App's installation token, never through `gh`** (§10). The forge adapter has a typed client for the REST API (issues, pull requests, rulesets) and the GraphQL API (Projects v2 fields), mints installation tokens from a JWT signed with the App key using the standard library, and handles rate limits and errors as typed values. `gh` stays a developer and agent-session tool for this repository, not part of the product (issue #27) | `gh` would carry the user's own broadly scoped token, the wrong identity for a bot, add a host dependency against D28, and leave errors and rate limits to output parsing. Libraries such as `go-github` and `githubv4` are added only if the hand-written client grows large enough to justify them (AGENTS.md) |
| D32 | **Recommended host: Mac mini M6 with 32 GB memory and 512 GB storage; on a budget 16 GB** (§2, §8), with 512 GB, or with 256 GB plus an external SSD for repositories, workspaces and backups; 24 GB / 512 GB sits in between. Spend on memory before storage, and add an external SSD rather than paying for 1 TB internal. The M5 Pro is not worth its premium for API-backed agents | US Apple Store prices on 1 October 2026: M6 16 GB / 256 GB $899, 16 / 512 $1,099, 24 / 512 $1,299, 32 / 512 $1,499, 24 GB / 1 TB $1,599, 32 GB / 1 TB $1,799; M5 Pro 24 / 512 $1,699 (Germany: M6 from €1,049). Each memory step costs $200 and buys about four more concurrent environments; memory cannot be upgraded later, while storage can be added externally. That makes 32 GB about $150 per environment against about $215 for 24 GB and $275 for 16 / 512. Environment counts are estimates until issue #39; whether Apple Container's storage can move to an external SSD is **unverified** (issue #54) |
| D33 | **Web app previews go through a preview proxy in `whr`** (§9.3, issue #72). When an agent runs a dev server in its environment, `whr` proxies a preview of one declared port through the proxy sidecar, the only container on both networks, and `tailscale serve` (or the router-VPN forwarder of D29) carries it to the developer. Each preview has its own origin, never the web UI's; it needs a per-preview token, lives only while the environment runs, forwards only to that port, and passes WebSocket upgrades for hot reload | The developer's phone or laptop cannot reach an `--internal` environment, and should not; the supervisor already knows which task, environment and port belong together. A preview serves untrusted, agent-written code in the developer's browser, so sharing the UI's origin would let it read the session and answer Decisions. Port publishing straight to the host, Traefik or Caddy, Tailscale inside each guest, and Tailscale Funnel were rejected: they bypass the supervisor, need routing data it already has, put a key in the guest, or publish unreviewed code. That the sidecar can relay inbound traffic to the internal network is **unverified** (issues #69, #72) |
| D34 | **Dogfood first: workharbor develops workharbor as early as possible** (§13). A Dogfood milestone holds the smallest set that runs one real workharbor issue through `whr` end to end: `whr serve` and the core commands (#24), the Claude Code adapter in degraded mode (#25, `dontAsk` with a fixed allowlist; host approvals follow with #7), the Apple Container adapter (#26), push after approval (#27), the reconciler fixes (#66) and the adapter's permission fix (#68), on a Mac mini M4 with 16 GB (#73). The supervisor always runs an **installed binary built from an approved commit on `main`** (`make install` until the first release, then the tap), never a topic's working tree. From the first green run, new issues start with `whr run`, and each manual workaround becomes an issue labelled `dogfood` | Today the human supervises three agent sessions by hand: relaying messages, pushing, ticking criteria, keeping the board, and catching duplicated work and a leaked token, which are all workharbor features. Degraded mode works now and takes #7 off the critical path; the push stays human-approved (D18). Agents working on workharbor edit the code that constrains them, including the policy, so the running supervisor must come from reviewed code. A host process runtime was rejected: it would be faster but would normalise unisolated agents |
| D35 | **The phone and a 12-inch tablet are the primary clients** (§9.6). The phone serves short, urgent interactions (answer, approve a tool, stop a run, glance at the harbor); the tablet replaces the laptop for reviewing a topic before push, supervising several tasks and planning with an agent. One server-rendered UI with a phone layout and a two-pane tablet layout, installed as a PWA. Approving "Ready to push?" asks for a passkey on any device | The developer detaches while agents work and returns when one needs them (§1), which happens away from a desk; a 12-inch tablet with a keyboard covers the review that the phone's screen cannot. Publishing code is the one irreversible step a lost or unlocked phone could take, so it alone needs a fresh check of who is approving. That a passkey prompt works in an installed PWA on both devices is **unverified** |
| D36 | **The autonomy table's defaults and fixed floor** (§6, issue #9). Commit in the topic's checkout: `auto`; push an `agent/*` branch: `ask`, carried out by the supervisor after the "Ready to push?" approval; open or update a PR and comment on the issue: `auto`, after the push; merge, tag, release and deploy: `forbid`. Whatever a repository's table says, merge, tag, release and deploy stay `forbid` and push stays at most `ask`; an override may only tighten; an unknown action or mode is `forbid` | Implemented and tested in `internal/policy` (issues #4, #51); this row records it as decided. Sensitive actions triggered by untrusted input asking (§6) follow with the trust tiers (issue #53) |
| D37 | **The CLI grammar of the dogfood slice is stable** (§9.1, issue #9): `whr serve`, `run`, `ls`, `logs -f`, `say`, `cancel`, `inbox`, `approve`, `reject` and `answer <decision> <option>` (for a question's fixed options, D23), with the scripting contract of §9.2. The other commands stay provisional until they are built | These are the commands the first dogfood run uses (D34); fixing their names now lets #24, scripts and the manual (#65) rely on them. `answer` is separate from `approve` and `reject` because a question has its own options, not allow or deny |

## 4. Domain model

| Object | Responsibility | Lifetime |
| --- | --- | --- |
| Task | Issue, instructions, decisions, progress, results, PR link | Until completed, cancelled or failed |
| Workspace | Checkout/worktree, branch, files, tool config, caches | May span several runs |
| Run | One execution of an agent in an environment | Start, pause, resume, terminate |
| Environment (`env`) | Container or VM backing a run/workspace | Stopped or recreated independently of workspace data |
| **Decision** | A question, approval or review request raised to the human | Until answered |
| **ReviewCandidate** | Task → branch → commit SHA → PR → CI results, keyed by SHA | One per prepared revision: created when cleanup pins the SHA, before the push (§4.5) |
| Event | Append-only record of instructions, observations, decisions, actions | Permanent (audit trail and UI feed) |

### 4.1 State machines

Task, run and environment each get their own small FSM with explicit legal transitions; these are specified before coding.

- **Task** (D13):

    | From | To |
    | --- | --- |
    | `queued` | `running`, `cancelled` |
    | `running` | `awaiting_guidance`, `ready_for_review`, `failed`, `cancelled` |
    | `awaiting_guidance` | `running`, `failed`, `cancelled` |
    | `ready_for_review` | `running` (rework), `completed`, `cancelled` |
    | `completed`, `cancelled`, `failed` | none (terminal) |

    - `awaiting_guidance` is for blocking Decisions raised by a run: a question or a tool approval from a live run, or `auth_expired` or `quota_exhausted` for a paused one (D23). The review Decisions of `ready_for_review` ("Ready to push?", §4.5) leave the task in `ready_for_review`.
    - **Rework** (`ready_for_review → running`) happens when the push is declined or the PR needs changes. It starts a new run on the same workspace and topic.
    - **`completed`** is set when the pushed PR is merged on the forge.
    - **`failed`** is terminal and maps to exit code 10 (§9.2). A failed run does not fail its task: it opens a blocking Decision (retry or cancel). A task fails only when a hard limit ends it (time or cost budget, a lost workspace) or the human answers that Decision with "give up". Both happen while the task is `running` or `awaiting_guidance`. In `ready_for_review` a lost workspace opens a review Decision instead (rework or cancel), so no other state needs a way into `failed` (D21).
    - A run paused by the human leaves its task `running`: pause is a run state. A run paused by `auth_expired` or `quota_exhausted` opens a blocking Decision, which moves the task to `awaiting_guidance` like any blocking Decision raised by the run (D21).
- **Run** (issue #15):

    | From | To |
    | --- | --- |
    | `starting` | `running`, `stopped`, `failed`, `interrupted` |
    | `running` | `paused`, `stopped`, `failed`, `interrupted` |
    | `paused` | `starting` (relaunch), `running` (cooperative pause), `stopped`, `interrupted` |
    | `interrupted` | `starting` (resume), `stopped`, `failed` |
    | `stopped`, `failed` | none (terminal) |

    - **Pause is a run state.** Pausing never changes the environment (§4.3).
    - **Resuming a paused run.** None of the measured agents has a cooperative pause (D11), so pause is a hard interrupt: the agent process is gone while the run is paused, and resume relaunches it from the session. That is `paused → starting`, like a resume from `interrupted`, so a failed relaunch can end in `failed`. `paused → running` is for an agent that reports cooperative pause, whose process stays alive. If the process of a paused run is lost anyway, the run goes to `interrupted`. Every resume after a pause, a cancel or an interruption starts with a briefing from the supervisor (D27).
    - **`stopped`** is a normal end: the agent finished, or the run was cancelled. **`failed`** is an agent crash, a failed start or a failed resume.
    - **Into `interrupted`:** the reconciler (§5.3) marks a `starting`, `running` or `paused` run `interrupted` when its process or environment is gone: a supervisor or host restart, or a lost environment. Nothing else sets it, and a pending approval is raised again on resume (§4.2).
    - **Out of `interrupted`:** `starting` when the reconciler resumes the agent from its session in a running environment (§4.3); `stopped` when the task is cancelled; `failed` when resuming is impossible or its attempts are used up.
    - **Terminal runs are never reused.** A retry or rework starts a new run on the same workspace and topic (§4.1 task rules).
- **Environment** (issue #15):

    | From | To |
    | --- | --- |
    | `provisioning` | `stopped` (created), `deleted` (provisioning failed or abandoned) |
    | `stopped` | `running`, `deleted` |
    | `running` | `stopped` |
    | `deleted` | none (terminal) |

    - **Start and stop** move between `stopped` and `running`. After a service or host restart every environment is `stopped` (spike #2, §5.3), so the reconciler observes `running → stopped` and starts the ones that should run.
    - **Delete only from `stopped`.** A running environment is stopped first.
    - **Recycling** (§4.3) deletes an environment and provisions a new one, so it is a new Environment with a new ID. An environment is never reprovisioned.
- **Coupling rules** (issue #16). The three machines are not independent; a task aggregate (the task with its runs, their environments and its ReviewCandidates) checks these guards before a change is made:
  - **One live run.** A task has at most one run that is not `stopped` or `failed`, and a run is only created or started (from `paused` or `interrupted`) in an environment that is `running`.
  - **A new run is new.** `StartRun` takes a run that has not started (no state) and a non-empty ID that no run of the task has; a stopped or failed run is never passed back in. It is refused unless the task is `queued`, `running` or `ready_for_review` (rework, §4.1); a `completed`, `cancelled`, `failed` or `awaiting_guidance` task starts no run. Once every guard has passed, `StartRun` moves a `queued` or `ready_for_review` task to `running` in the same change, so a rework run never stops under a `ready_for_review` task; a refused start moves nothing.
  - **Pause never stops the environment.** Pausing a run changes the run only. An environment is not stopped while a run in it is `starting` or `running`; a run is stopped or interrupted first.
  - **`ready_for_review` needs a stopped run and a pinned SHA.** The task's latest run is `stopped` (a `failed` run does not qualify), and a ReviewCandidate exists whose SHA is pinned (§4.5). The current revision is the most recent ReviewCandidate, and it records the run that produced it: it must be the latest run. Only the latest run may pin a revision, so a late pin from an older run cannot become the current one. After rework, a new run that stops without pinning a new commit leaves the task not ready, so the old SHA and its old CI result never carry over.
  - **CI belongs to a SHA.** A pipeline result is recorded on the ReviewCandidate of its own commit. A pass on an earlier SHA never counts for the current revision: where CI is required, `ready_for_review` is refused until the current SHA has passed.
  - **Violations are conflicts.** A broken coupling rule and an illegal transition are reported as a conflict, exit code 5 (§9.2). An unknown run, environment or SHA is "not found", exit code 3.

### 4.2 Decision object

Fields: ID, task, run (empty for a review Decision, which no live run raised), kind (`question | approval | review`), blocking flag, subject, input (untrusted, capped), commit SHA, options, status, created and answered timestamps, deadline, answer, reason and answering actor (issue #17). The inbox, `whr inbox`, notifications and the audit trail hang off it. "Awaiting guidance" is the state a task enters while a blocking Decision raised by a live run is open; review Decisions belong to `ready_for_review` (§4.1).

**Approvals are live and blocking.** Spike #1 showed the pattern with Claude Code: the agent's permission prompt is routed to the supervisor, which opens an `approval` Decision carrying the tool name and a capped copy of its input. The agent stays blocked until a human answers allow or deny, with an optional reason that is passed back to the agent. Rules:

- **Fail closed.** Deny on timeout (the spike used 10 minutes) and when the supervisor is unreachable.
- **Plan approval.** In plan mode the agent's `ExitPlanMode` arrives as an approval whose subject is the plan.
- **Capped input.** Tool inputs in a Decision are capped (the spike used 2,000 characters); the full input stays with the agent. All of it is untrusted data.
- **Status.** `open` becomes `answered`, `expired` (the deadline passed) or `superseded` (the supervisor restarted or the run was paused); each is terminal. Only `answered` with `allow` ever permits anything: an open, expired or superseded approval is a denial.
- **Approval channel transport (D26).** Approvals travel over the agent's stdio control protocol (`--permission-prompt-tool stdio`, `stream-json` in and out) on the runtime's interactive exec channel (`container exec -i`): `control_request` (`can_use_tool`) on stdout, `control_response` (allow or deny) on stdin, matched by `request_id`. An answer for an unknown `request_id` is denied, and open requests are denied when they are superseded (D23). A closed stdin was reported to fail closed without running the tool; a crashed supervisor end and an unanswered request are **unverified** (issue #7), so when the channel is lost the supervisor stops the agent with `whr-shim` (D25) and records the request as denied. No guest-to-host network path, no listener and no approval token in the guest.
- **Deadline.** An approval always has one (default 10 minutes). An answer that arrives after it is refused and the Decision expires, so a late "allow" does not count. An approval that has none (a lost value, a damaged row) counts as past it: an answer expires it, and `Allows` is false. The store refuses to write such an approval and refuses to load one. Only a question or a review Decision may have no deadline.
- **A refusal can still change the Decision.** A late answer expires the Decision, and an allow for another commit is recorded as a denial, while both return an error. The store's `RespondDecision` loads, answers and saves in one transaction and saves the change even when the answer is refused, so a refusal is never lost to a rollback. The caller still receives the error (exit code 5).
- **Bad input is a usage error.** A missing actor or time, an answer that is not offered and a malformed new Decision are not conflicts: they exit with code 2 (§9.2). A conflict (code 5) depends on the Decision's state; a usage error does not.
- **Commit SHA.** A review Decision such as "Ready to push?" carries the pinned SHA of its ReviewCandidate (§4.5, §6). An allow is tied to that SHA: an allow given for a different SHA is recorded as a denial, and the supervisor asks `Allows(sha)` against the commit it is about to push, so an approval for an earlier revision never covers a later one.
- **Pause and restart** (D23). A Decision raised by a live run (a question or an approval) is `superseded` when the supervisor restarts or the run is paused: pause is a hard interrupt (D11), so the agent process that asked is gone and could not receive the answer. The reconciler, or the resume, raises a new one when the agent asks again (§5.3). An answer sent to a superseded Decision is refused as a conflict (§9.2, exit code 5). A review Decision belongs to no run, so it survives both, and so do the login and quota questions below: they are raised for a run that is already paused, and nothing waits on them. This is the one restart rule; §4.1 and §5.3 refer to it.
- **Expired auth and quota** (D23). `auth_expired` and `quota_exhausted` are blocking `question` Decisions raised for the paused run, not approvals, because nothing is being permitted. Their options are fixed: for `auth_expired`, "Signed in again, resume" or "Cancel"; for `quota_exhausted`, "Resume now", "Resume at reset" (with the reset time when the agent reports it) or "Cancel". The run stays paused and the task moves to `awaiting_guidance` (§4.1, D21). They have no deadline: waiting is safe, because nothing runs until they are answered.
- **Task state (D13, D23).** Only a blocking Decision raised by a run moves its task to `awaiting_guidance`: a question or approval from a live run, or a login or quota question for a paused one. A review Decision belongs to no run and leaves the task in `ready_for_review`.

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

### 4.4 Persistence semantics

Decided from spike #2 (Apple Container, issue #2; confirm on other backends):

- **Repositories live on the host**, outside containers and volumes, and are mounted into an environment read-write. They survive any environment, a developer's editor can open them at any time, and one object store serves every task. How checkouts are laid out is §4.5. This replaces the earlier idea of keeping the checkout on a volume.
- **The agent home** (auth directory, session, caches) is one writable named volume per environment. A volume survives stop, start, delete and rebuild.
- **A writable volume is exclusive.** While one container has it read-write, no other container can attach it, not even read-only (the second start fails with "The storage device attachment is invalid"). A rebuild stops the old container before the new one starts. A volume can be shared read-only by several containers.
- **A bind-mounted checkout is slower but usable.** For 5000 files with 300 edits, the first `git status` took 0.3 to 1.1 s on a bind mount against 0.12 s in a volume, later runs about 100 to 130 ms against 75 to 118 ms, `git add` 363 ms against 120 ms, and `commit` 667 ms against 334 ms. Git tuning (untracked cache, `feature.manyFiles`, preloaded index) changed steady-state `status` by almost nothing, because the cost is the cold first stat of every file. Very large repositories were not measured; the cold pass grows with the file count.
- **Caches and build output stay on the volume, outside the checkout.** A real build (this repository's `make check`, including golangci-lint and editorconfig-checker compiled from source, 4 CPUs) took 45 s cold and 2.3 s warm with everything on a volume; with only the checkout on a bind mount it was 41 s and 2.7 s, no measurable cost; with the Go caches (about 25,700 module files and 7,800 build-cache files) on the bind mount as well it was 56 s and 4.7 s, about twice as slow warm. The adapter sets `GOMODCACHE`, `GOCACHE` and the package-manager and build-directory equivalents to paths on the environment's volume. A tree with many small files of its own, such as `node_modules`, will behave like the cache case and was not measured.
- **Bind mounts sync both ways at once**, and files the guest root writes appear owned by the host user.
- **The root filesystem is disposable.** It survives stop and start and is lost on delete, so nothing that matters lives there.
- **Mounts are rejected by the adapter, not the runtime.** The runtime accepts any host path. The adapter calls `runtime.CheckMount` before every mount: it resolves symlinks first, then rejects `$HOME` and its parents, `~/.ssh`, other secrets directories and runtime sockets (§7.4), and the conformance suite checks it.
- **Tools are not part of the workspace.** Agent CLIs come from a shared read-only store (§5.6).

Still open: retention and garbage collection of completed tasks, topics and workspaces on the host disk (§8), quotas (a named volume is a sparse image with a virtual size of 512 GiB), and bind-mount speed on very large repositories.

### 4.5 Topics, checkouts and cleanup before push

A **topic** is one line of work: one branch (`agent/<topic>`) with its own checkout on the host. Several topics are in flight at once, each with its own environment and agent, and the developer can open any checkout in their editor at the same time. That is the worktree idea: parallel topics that are merged and tidied locally before anything leaves the machine.

**Checkout layout** (spike #2, item 9):

- **A per-task clone with a read-only object cache is the default for agent topics.** The supervisor keeps a bare cache per repository on the host. Each topic is a `git clone --shared` of it, with its own `.git` (hooks, config, refs). The cache's objects are mounted read-only at their host path, because the clone's alternates point there, so the agent cannot write the cache and cannot see other topics. New objects go to the clone's own object directory.
- **Clone depth is a per-repository setting, `clone_depth`, default 0 (full history).** A positive value fetches the cache with `--depth` for very large repositories, at a cost measured with git 2.56 on a test repository: `git clone --shared` is silently ignored for a shallow source, so each topic gets its own copy of the shallow history (no alternates; the cache needs no mount); refreshing the cache needs a forced refspec, and a topic takes the refreshed target only with `fetch --update-shallow`; once the target moves further than the depth past the topic's fork point there is no merge base, and a rebase replays the whole shallow history. The supervisor therefore checks for a merge base before a rebase and deepens the cache and the topic (`--deepen`) until it finds one. A partial clone (`--filter=blob:none`) keeps the full history and may be the better option; it was not tested.
- **Worktrees of one repository** (`git worktree`) are the developer's tool on the host for their own parallel topics. They are not handed to agents. A worktree's `.git` file points at the shared repository by host path: mounting only the worktree fails (`not a git repository`), and mounting the shared repository exposes every branch, the shared hooks and config, and the other worktrees' metadata. `git worktree add --relative-paths` makes the pointer work if the mounts keep the relative layout, but the exposure is the same.

**Cleanup before push:**

- **Agents do not push.** The agent commits in its topic's checkout. Nothing leaves the host until a cleanup step has run and been approved.
- **Prepare for push.** The supervisor, on its own copy of the topic (`hostgit`, below), rebases it onto its target, folds attempts into one commit per finished change (fixup and autosquash of unpushed commits only), checks the commit messages (conventional commits and the repository's commit linter), signs the commits with the bot key (§7.7) and runs the repository's checks. Several finished topics can be merged into one integration branch first.
- **Approval.** The result is a ReviewCandidate (a pinned commit SHA) and a Decision, "Ready to push?", that shows the commit list and the diff stat. Approval is per commit SHA, as for any review (§6).
- **Push and PR.** On approval the supervisor pushes the prepared `agent/*` branch with the run's scoped credentials and opens or updates the PR. Merging stays human, on the forge.
- **Pushed commits are never rewritten.** After a push a topic is only extended; rewriting pushed history needs an explicit request.
- **Done or cancelled topics.** The checkout and its branch are removed by the retention rules (§4.4, issue #54) after the push is merged or the topic is cancelled. Unpushed work is kept until the owner discards it.

**Prepare and push in code** (issue #27). The steps above run in this order, on the supervisor's own copy: the environment is stopped (not only the run), the branch is fetched with `FetchBranch`, the target comes from the cache, `EnsureMergeBase` finds a base, and `Prepare` rebases the topic onto the target in a temporary worktree with `--autosquash` (hooks off, the bot as committer, commits signed with the bot key), then lints every commit message and returns the new tip. Running the repository's own checks is not done by `hostgit`: they are the repository's code, so a `Checker` runs them in an environment, never on the host. The tip is pinned (`PinRevision`) and a review Decision "Ready to push?" is raised for that SHA. `Push` sends one `agent/*` branch, fast-forward only, as the exact approved commit (`<sha>:refs/heads/<branch>`), never with force and never tags.

**The editor copy** (issue #59). The developer's editor is never pointed at an agent's checkout: opening it runs planted repository config on the host (VS Code and its git extension, direnv, `.vscode/tasks.json`; threat model T15). `EditorCopy` clones the supervisor's own bare copy of the topic (which `FetchBranch` filled from the stopped environment) into a directory outside the workspace root, with an empty template and none of the agent's config, hooks or alternates, and refreshes it later with a fast-forward only, so the developer's edits are never overwritten. It refuses a destination inside the workspace root, so the agent's checkout cannot be handed out by mistake. The commits themselves are the agent's work and may contain files an editor acts on (`.vscode/tasks.json`, `.envrc`, `.devcontainer`), so the copy lists them and the UI warns before the folder is trusted. The copy is refreshed only from a stopped environment; while the environment runs, the last copy is offered as stale.

**The forge contract** (§10) separates Git transport from the forge API. The adapter is wrapped in a `Guard` that holds the autonomy table: `OpenPR` and `UpdatePR` take an `Approval` (the Decision and the commit SHA) and are refused unless the Decision allows that SHA and the branch at the forge points at it; `Push` needs the same proof; `Merge`, `Tag`, `Release` and `Deploy` are refused whatever the table says (the table's ceilings), and the inner adapter is never called for them. An `Issue` carries the author association for trust tiers (#53), and `VerifyWebhook` takes the request's `http.Header`.

**The host treats every agent-writable checkout as hostile** (§7.4). It never runs plain git there: cleanup and push use hardened git, or fetch the branch into a supervisor-owned repository first.

**`hostgit`** (issue #19) is the only way the host runs git on anything an agent can write:

- **Fetch, then work on the copy.** `FetchBranch` fetches one `agent/*` branch from the agent's checkout into a bare repository the supervisor owns, with no tags, no submodules, object checks on (`fsckObjects`) and only the file transport. Cleanup (rebase, fold, sign) and push run only on that supervisor-owned repository; the agent's checkout is never rebased, committed in or pushed from.
- **Fetch only from a stopped environment.** The checks on a checkout (a real `.git` directory, alternates naming only the cache) and the fetch that follows read the same files at different times. While the environment runs, a process in it, even one the agent left in the background after its run stopped, could rewrite them in between. So the supervisor fetches an agent's branch only after the environment is stopped, not merely the run; preparing for push stops it first.
- **The checkout is checked before git touches it.** `hostgit` knows a workspace root and the read-only caches that agent checkouts may borrow objects from. A checkout is refused unless its resolved path lies under the root, its `.git` is a real directory (not a `gitdir:` file or a symlink, which would redirect git to another host repository), `objects` and `refs` are real directories, there is no `commondir`, and `objects/info/alternates` is absent or names only the listed caches (a regular file, no quoting; `http-alternates` must not exist). An alternates file pointing at another repository would let a commit name that repository's secret blobs by hash, and the next push would send them to the forge. Git is then pointed at the verified `.git` directory explicitly (`GIT_DIR`, with `GIT_CEILING_DIRECTORIES` so it never walks up), and a fetch uses only the file protocol (`GIT_ALLOW_PROTOCOL=file`; every other command allows none). The checks and the fetch run only once the environment is stopped (below); the supervisor enforces that in the push flow (issue #27).
- **No git commands in the agent's tree, except read-only plumbing.** A handle on an agent's checkout (`Untrusted`) accepts only `rev-parse`, `rev-list`, `cat-file`, `for-each-ref`, `ls-tree`, `merge-base` and `show-ref`. Each command has a list of the options it accepts, matched exactly: git accepts an unambiguous prefix of a long option, so a list of refused options is not enough (`--textc` ran a textconv driver). `status`, `add`, `commit`, `checkout`, `diff`, `log -p`, `rebase`, `push` and `fetch` are refused, as are `--textconv` and `--filters`: they can run filters, textconv drivers, hooks or the file-system monitor from the agent's repository config, which no command-line override can list in advance.
- **A hardened floor on every host git command.** Git starts with an empty environment (no `GIT_*` from the host, a minimal `PATH`, an empty `HOME`, no system or global config, no terminal prompt, no optional locks, no replace objects) and `-c` overrides that disable hooks (`core.hooksPath`), the file-system monitor, the pager and editor, credential helpers, signing, submodule recursion, and every transport except `file` for a fetch from an agent.
- **The cache and the topic clone** (issue #45). `Cache` is the bare repository per repository, with the repository's `clone_depth` (default 0). `Refresh` fetches one branch from the forge with the forced refspec `+refs/heads/<b>:refs/heads/<b>`, no tags, objects checked, and `--depth` for a positive depth (an existing shallow cache is made complete with `--unshallow` when the depth is 0). The source is an absolute path or an `https://` URL without credentials; every other scheme (`ssh`, `ext::`, `file://`) and anything that starts with a dash is refused, and an authenticated fetch comes with the forge adapter (issue #27). `CloneTopic` creates the checkout with an empty template (no hooks copied) and the topic branch: with depth 0 it is `git clone --shared`, whose alternates name the cache's `objects` directory, which is the one directory the runtime mounts read-only and the one `hostgit` lists as an allowed cache; with a positive depth it is a `--depth` clone through a `file://` URL, because `--shared` is ignored for a shallow source, so the topic is self-contained and nothing is mounted. A shallow cache with depth 0 configured is refused instead of silently producing a copy.
- **One writer per cache** (issue #67). Every operation on a cache (refresh, deepen, clone, merge-base search, maintenance) runs under a per-cache lock: a mutex shared by every handle of the same path and a file lock (`whr.lock`) for other processes, waited for with the caller's context. Git's own lock files (`shallow.lock`, `packed-refs.lock`, ref locks) are never left to block the next refresh: while the cache lock is held no other supervisor process runs git on it, so a lock file found then is stale and is removed first. Three parallel shallow refreshes had failed 40 times in 60 on `shallow.lock` without it.
- **A topic keeps its objects.** After a force-push on the forge the cache drops the old tip, and a garbage collection would expire it, corrupting a `--shared` topic cloned from it. `CloneTopic` therefore pins the commit it cloned with a keep ref (`refs/whr/keep/<id>`), `ReleaseTopic` removes it when the topic is removed (§4.4), the cache sets `gc.pruneExpire=never`, and `Maintain` (the only explicit maintenance; background gc is off) runs under the lock.
- **Names map to directories by a strict rule.** `CachePath(repo)` accepts only `owner/name` made of ASCII letters, digits, `.`, `_` and `-` (each part starting with a letter or digit, at most 100 characters), lowercases it, and appends a short hash, so `Foo/x` and `foo/x` are one cache on a case-insensitive disk and no name, path separator or Unicode form can leave the cache root. A cache path must be absolute and inside the cache root when one is set (`WithCacheRoot`), a relative path is refused instead of being created under `/`, and a local source inside the workspace root (agent-writable) is refused.
- **Merge base before a rebase.** `EnsureMergeBase` looks for a merge base between the target and the topic in the supervisor's copy. While there is none and either side is shallow it deepens the cache (`fetch --deepen`) and then the copy (`fetch --update-shallow --deepen`) by a step, up to a limit, and reports `ErrNoMergeBase` when the limit is reached or both sides are complete, so the caller opens a Decision instead of rebasing blindly. `FetchBranch` takes `--update-shallow`, because a shallow topic would otherwise not be fetched at all.
- **Tested.** Tests plant a hook, `core.fsmonitor`, `core.sshCommand`, `core.pager`, a clean and smudge filter and `uploadpack.packObjectsHook` in an agent checkout. Control runs of plain git prove each one fires; none runs through `hostgit`.

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

Covers provision, start/stop/delete, inspect, resource limits, logs, exec, storage and endpoint discovery. Capabilities are explicit and never assumed:

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
- **`Exec`** streams: separate stdout and stderr readers, `Wait` for the exit code, and cancellation through the context. Exec in an environment that is not running is `ErrNotRunning`. `ExecRequest.Stdin` is an optional reader the adapter copies into the process and closes at its end; with a pipe the caller keeps writing while the command runs, which is how the Claude Code adapter sends `stream-json` input and mid-run messages (§5.2). Without a reader the command's stdin is closed. Whether `container exec` carries stdin through to the guest was reported working by issue #7's comments but has no reproducible evidence yet; the conformance suite checks it once the Apple Container adapter (#26) runs it.
- **Cancel kills the process in the guest.** Cancelling the context ends the stream, and the process inside the environment must be gone, not only the client (spike #2: SIGINT was not forwarded, and the agent kept running). The suite proves it with a second `exec` that looks for the process, so a backend that only returns from `Wait` fails.
- **Lifecycle is idempotent.** Start of a running and stop of a stopped environment succeed, so a reconciler can retry (§5.3).
- **Hardening is required, not possible** (issue #26). `Spec.Validate` is not enough on its own: a spec with the default network, a writable root, another task's volume and a bind of `~/.ssh` validated. `runtime.Prepare` is the one checked step. It runs `Validate`, requires `Network.Internal` and `ReadOnlyRoot`, checks every bind mount with `CheckMount` (and `CheckMountsWithin` the workspace roots), checks that every named volume belongs to this environment (`Owns`), and returns a `PreparedSpec` with unexported fields. `Provision` takes only a `PreparedSpec`, so nothing unchecked can reach the runtime. `CheckMount` returns the resolved path and `Prepare` puts that path in the prepared spec, so the adapter mounts exactly what was checked: a source swapped for a symlink after the check is not followed.
- **The contract owns the surroundings.** Provisioning creates the per-environment `--internal` network, the agent-home volume and the egress sidecar (`Spec.Egress`: the proxy's image, its allowlist and the host binary of the proxy), and deleting the environment removes all three, by exact name. `Resources` reports them, so the conformance suite can check that they exist and that they are gone. A writable volume is exclusive (§4.4): starting a second environment whose volume another running environment holds read-write is refused with `ErrVolumeBusy`.
- **Cache objects are read-only.** For a `--shared` topic, `Spec.Alternates` lists the cache's `objects` directory (issue #45), and `Prepare` turns each into a read-only bind mount at the same host path, because the clone's alternates file names that path. They are checked like any bind mount and must lie under the cache root.
- **Fakes and conformance.** `runtimetest` has an in-memory fake that can simulate a service restart (every environment becomes `stopped`, as measured below) and the conformance suite every backend must pass; the real Apple Container adapter (#26) runs the same suite on the Mac.

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

Specified as explicitly as the runtime contract, and versioned: the contract carries a `contract_version`, and an adapter declares which version it implements. Release 1 ships Claude Code and Codex CLI as built-in adapters against it. Codex CLI lacks mid-run injection and host-routed approvals in what spike #1 could test, so in release 1 it runs in the degraded mode below, labelled in the UI. *Full mode* needs every capability marked so; an agent without them runs degraded (D12 requires full mode only of Claude Code, the first agent). Capability flags:

- headless / unattended operation
- **mid-run message injection (required for full mode)**: send a user message into a running session and report how it was delivered (injected now, or at the next turn). Without it an agent cannot be a remote-controlled assistant (§1); an agent that lacks it may only run in a degraded mode that the UI labels
- **structured event stream (required for every adapter)**: messages, tool calls, diffs and test results as typed events, which feed the live transcript (§9.3)
- cooperative pause (e.g. stop after current turn); reported false by every measured agent (D11)
- session persistence and resume
- PR/issue tooling
- "awaiting guidance" signal (how the agent raises a blocking Decision)
- **approval prompts routed to the host (required for full mode)**: the agent blocks on a permission request and the supervisor answers it with a human's allow or deny and a reason (§4.2). An agent without it can only run with a fixed allowlist and every other action denied
- **auth modes**, reported explicitly and never assumed:
    - `api-key`: the key stays in the host-side proxy and is issued per run (§7.3).
    - `subscription`: a consumer-plan login (for example Claude or ChatGPT sign-in) kept in a dedicated per-environment auth directory. The CLI refreshes the token itself, so it cannot sit behind the proxy.
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
- **Medium term:** a plugin loader, once two built-in adapters have proved the contract. Whether an existing agent-client protocol (for example Zed's ACP) already covers part of the contract is **unverified**; check it in the §12 scorecard and reuse it if it fits.
- **Conformance.** A plugin declares its capabilities and must pass the same conformance suite as a built-in adapter, so a capability flag is a verified claim (§5.1).
- **Wire types.** Everything that crosses the seam has a stable JSON name (snake case): capabilities, the start spec, events, results, approval requests and answers. Times are RFC 3339 and a zero time is left out. An error crosses as a string code (`unsupported`, `unsupported_auth`, `no_approver`, `no_session`, `not_running`, `bad_spec`), and `agent.ErrorFor` maps a code back to the sentinel, so `errors.Is` works on the supervisor's side of the seam.
- **The approver is a reverse call.** The approver cannot be a Go callback across a process. The plugin sends an `approve` request (an approval request) to the supervisor on the same connection and waits for the answer. The supervisor applies the approval timeout and the fail-closed rule (§5.2), so a lost connection, a plugin that dies or a late answer is a denial, and the plugin never decides.
- **Trust.** See §7.8: plugins are installed explicitly and run isolated.

### 5.6 Tool store

Environments run **stock images**. The agent CLIs (Claude Code, Codex CLI, later others) and the supervisor's own helpers live once in a versioned, immutable **tool store** on the host and are mounted read-only into each environment, in the manner of a Nix store. This replaces installing an agent in every container, which took about 11 s and 230 MB each in spike #2.

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

**Building the store** (issue #74, `internal/toolstore`, `whr tools build -store <dir> -shim <whr-shim>`). The pins live in the repository (`internal/toolstore/pins.json`: name, version, platform, release URL and SHA-256), so a version change is a reviewed commit and never a download-time decision. A download must match the pin **and** the SHA-256 the vendor's own `manifest.json` lists for the platform (the manifest of Claude Code 2.1.285 does, and its linux-arm64 entry equals the hash spike #7 measured); a mismatch in either is refused before anything is stored. The binary is placed content-addressed at `store/<hash8>-<name>-<version>-<platform>/bin/<name>` through a staging directory and a rename, and the entry, its `bin` and the tool are made read-only (`Verify` re-hashes every entry and reports a changed or writable one). `whr-shim` is added from the launcher built from the same commit, and a profile `profiles/<name>-<version>/` links to the entries with relative links, replaced in one move, so an upgrade or a rollback is a profile change. The vendor's manifest signature (`manifestSignatureEnforcement`) is not verified here: **unverified**.

**The configuration file** (issue #74, `internal/config`) is JSON, decoded strictly (an unknown or misspelt key is an error), so the standard library is enough; TOML would add a dependency for comments only. It holds the repositories (`owner/name` and `clone_depth`), the three roots (cache, workspaces, tool store), the GitHub App ID and its key file, the agent-login env file, the API token file and the listen address. Secrets are paths to files, never values: each must be a regular file, not a link, owned by the user, with mode `0600` and some content, and outside the workspace root, where an agent could read it. The listen address must be a loopback IP (D29: a guest reaches every other address of the host); the roots must exist and must not overlap, since the workspace root is where an agent writes. `whr serve` validates all of it at start and reports every problem with its key. The full onboarding stays in #29.

Open: how new versions are discovered (a developer bumps a pin in a commit), and how the store is garbage collected.

### 5.7 Usage and cost

A remote for agents that run detached for an hour has to say what they used. The supervisor records usage per run and reports it; it never meters the model traffic itself.

- **Source: the agent's own reports.** Each turn becomes a `usage` event with a typed payload: model, token counts, the cost with its source, and the usage windows (name, utilization from 0 to 1, reset time). The token counts are optional: a missing block means the agent did not report them, which is not the same as zero, so a token budget never reads an unknown count as 0 tokens. An adapter that reports usage sets `ReportsUsage`, and the suite checks that the events are well formed. Claude Code's `result` event carries `total_cost_usd` and `usage` with input, output, cache-read and cache-creation tokens (recorded in spike #7). Antigravity sends `usage` in `step_update` and `result`; Codex CLI is **unverified**.
- **Reported or estimated.** Every cost carries its source. `reported` is the agent's figure; `estimated` is computed by workharbor from a pinned, dated price table and labelled as an estimate everywhere it is shown.
- **Auth mode decides what the number means** (§5.2). In `api-key` mode cost is real spend. In `subscription` mode it is notional: the plan is paid flat, and the usage-window utilization (five-hour and seven-day, §5.2) is what limits the developer, so the UI leads with that.
- **Kept as audit entries.** Usage rows are small and survive a transcript purge (§5.4), so totals stay correct after history is deleted.
- **Reports.** Totals per run, task, repository and day or month: `whr usage [--task <task>] [--since <time>] [--json]`, a usage line in `whr show`, and per-task usage plus a usage-window meter in the web UI (§9.3).
- **Budgets read the same counters.** The per-run and per-task token and cost budgets of §7.4 compare against these totals: a soft threshold notifies (§9.4), and a hard limit ends the task as `failed` (D13).

## 6. Policy and autonomy

Autonomy is a per-repo/per-task policy table: **action → `auto | ask | forbid`**.

| Action | Default |
| --- | --- |
| Commit in the topic's own checkout | auto |
| Push an `agent/*` branch | **after cleanup**: the supervisor pushes the prepared branch once you approve it (§4.5); the agent never pushes |
| Open/update PR, comment on issue | auto, after the push |
| Merge, tag, release, deploy | **forbid** for the agent; human-gated |
| Sensitive actions triggered by untrusted input | ask |

Two limits hold whatever a repository's table says (issue #51). Merge, tag, release and deploy are `forbid`, and an agent push is at most `ask`: the supervisor pushes only after approval (§4.5), so an override can tighten these but never loosen them. A mode other than `auto`, `ask` or `forbid`, and an action the table does not list, is `forbid`.

Enforcement is outside the agent: forge branch protection, required human review, and a bot identity that cannot bypass them. Approval is per commit SHA (ties to ReviewCandidate). Every approval is a Decision record.

**Agent permission modes.** The agent CLIs have their own coarse modes. In the spike with Claude Code (a fixed allowlist of `Read` and a few harmless shell prefixes) they behaved as follows for a file write:

| Mode | Behaviour |
| --- | --- |
| `manual` | Asks (an approval Decision, §4.2) |
| `acceptEdits` | Writes without asking |
| `dontAsk` | Denies silently |
| `plan` | Plans read-only, then asks the human to approve the plan |
| `auto` | Identical to `manual` in the test: read-only commands such as `pwd` and `git status` ran, and a file write, `touch`, `curl` and `rm` were all asked |

Rules for using them:

- Offer them as per-session presets over the action table, never as the policy itself. The table is per action and enforced outside the agent; the modes are coarser and live inside it.
- Do not count on `auto` to reduce prompts: in headless mode it asked exactly as often as `manual`. Whatever it is meant to do is not visible there.
- `bypassPermissions`, which switches every prompt off, is never offered by default and never outside an isolated environment.
- A mode is fixed when the agent process starts. Changing it on a running session restarts the process with `--resume` and keeps the session and transcript.
- Prefix allow rules such as `Bash(ls:*)` do not match a compound command like `a && b`; the CLI asks about the whole command. An allowlist needs a rule for compound commands (match each part, or ask).

## 7. Security

The rules below are the security requirements. The [threat model](threat-model.md) says what each defends against, where it is enforced and tested, and which risks are accepted (issue #11).

1. **Untrusted input.** Issue text, PR comments and CI logs are untrusted. Use trust tiers by author (owner vs external); hold or flag runs on issues from unknown authors. A run that combines private data, untrusted input and outbound network requires approval.
2. **Default-deny egress** through a logging allowlist proxy: forge, package registries, LLM API only. Block LAN, host, other workspaces and cloud-metadata addresses.

    Verified on Apple Container in spike #2 (issue #2). The default network gives none of this: a guest reaches the internet, the LAN, other containers and any host service bound to all interfaces. What works:

    - **One `--internal` network per environment.** It blocks the internet, DNS (names do not resolve), other LAN devices, IPv6 and containers on other networks. It does **not** block the host: issue #69 measured that an internal guest reaches host listeners bound to the Mac's LAN address or to all interfaces, through the network's gateway; spike #2's earlier "blocks the host through every address" was wrong. Listeners bound only to loopback stayed unreachable. Agents on the same internal network can reach each other, so a network is never shared between tasks.
    - **The proxy runs in a sidecar container**, attached to the default and the internal network (`--network` repeats). The host cannot serve an internal network because it gets no interface on it, so a host-side proxy cannot bind to its gateway.
    - **Allowlist by hostname, with the name resolved by the proxy.** Allowed hosts returned 200, denied hosts and a raw-IP CONNECT got 403, and every decision was logged with time, verdict, method, host and source. The guest needs no DNS, which closes DNS exfiltration. Limits: the match is on the name in CONNECT, so it does not defeat domain fronting, and the sidecar has full egress and is trusted.
    - **Minimal allowlist for Claude Code:** `api.anthropic.com` alone. In an authenticated run inside a container the proxy also saw a telemetry host (`http-intake.logs.us5.datadoghq.com`) and denied it; nothing broke. Installing needs `claude.ai` and `downloads.claude.ai`, which the tool store (§5.6) removes. A client that obeys proxy variables, such as `curl`, tests the proxy and not the network; test the direct path with the proxy variables ignored.
3. **Credentials.** Run-scoped, short-lived, single-repo, non-extractable. GitHub App installation tokens (~1 h); per-repo bot tokens or deploy keys for Gitea/Forgejo/GitLab. Inject through a git credential helper or host-side proxy so raw tokens never reach env vars, disk or logs. In `api-key` mode the LLM API key stays in the proxy. In `subscription` mode the consumer-plan login lives inside the environment (§5.2), is long-lived and not scoped to a repo, and leaks if the agent is compromised. **Accepted risk** for a single-developer, watched personal tool; limit it with a dedicated auth directory per environment (never `$HOME`), the egress allowlist, and revocation at the vendor when an environment is deleted. Revoke run-scoped credentials at run end. Redact secrets at ingest, before anything is stored (§5.4). Agent and CI credentials are separate.
4. **Isolation policy, testable.** Reject mounts of `$HOME`, `~/.ssh` and runtime sockets. Non-root agents, read-only rootfs where feasible, hard CPU/memory/disk quotas, per-run timeout and token/cost budget. Escape tests (guest cannot reach host or Socktainer socket) in the conformance suite. The VM boundary does not protect what is deliberately exposed.

    Measured in spike #2:

    - **The runtime does not reject mounts.** It mounted `/etc` without complaint, so the adapter enforces the deny-list. It resolves symlinks first (a symlink to `$HOME` is rejected), then rejects `$HOME`, its parents, secrets directories, unix sockets and runtime socket directories. The spike's `check_mount` was the seed; the rules now live in `runtime.CheckMount` (issue #18):
        - A source must be an absolute path that resolves; otherwise it is rejected. Read-only makes no difference.
        - Rejected: a unix socket; the home directory and every parent of it (`/Users`, `/`); the secrets under the home directory (`.ssh`, `.gnupg`, `.aws`, `.azure`, `.kube`, `.docker`, `.config/gh`, `.config/gcloud`, `.config/op`, `.netrc`, `.gitconfig`, `.git-credentials`, `.npmrc`, `.pypirc`, `.password-store`, `.claude`, `.codex`, `Library/Keychains`, `Library/Containers`, `Library/Group Containers` (the 1Password agent socket) and `Library/Application Support` (browser cookies)) **and any directory that contains one**, such as `~/.config` or `~/Library`; the runtime socket directories (`~/.socktainer`, `~/.docker/run`, `~/.orbstack`, `~/.colima`, `~/.lima`, `~/.local/share/containers`, `/var/run`, `/private/var/run`, `/run`) and their parents; the system roots `/`, `/Users`, `/Users/Shared`, `/home`, `/private`, `/var`, `/private/var`, `/private/var/folders`, `/tmp`, `/private/tmp`, `/Volumes` and `/Library`; and the system trees `/etc`, `/private/etc`, `/System`, `/dev`, `/proc`, `/sys`, `/boot`, `/root`, `/private/var/root`, `/Library/Keychains` and `/private/var/db`. Everything below `/Volumes` (another disk can hold a copy of the home directory) and below `/private/var/folders` (the user's `$TMPDIR`) is rejected too, unless it lies inside the home directory. A subdirectory of `/tmp` is not rejected.
        - **A secrets path is resolved too.** `~/.config/gh` or `~/.ssh` is often a symlink into a dotfiles directory (stow, chezmoi). Each secrets path is checked as written and after resolving it, so a mount that equals or contains either is rejected, and mounting the dotfiles directory is refused.
        - **Links inside a secrets directory are followed one level.** GNU stow links `~/.ssh/id_ed25519` to `~/keys/id_ed25519` when `~/.ssh` is a real directory. The direct entries of each secrets directory that are symbolic links are resolved and their targets are protected like the secrets themselves, so mounting `~/keys` is refused. Links deeper in the tree, and hard links, are out of scope.
        - **Mounts below workspace roots (decided, #58).** As a second layer, the supervisor passes the workspace roots it owns (the directories it creates checkouts in), and `CheckMountsWithin` accepts a bind mount only if it lies inside one of them. With no roots configured the deny-list above is all there is. A mount outside every root is refused with the reason `outside the workspace roots`. The deny-list stays first, so a root cannot widen it.
        - **Paths are compared by file identity, not by string.** A path equals, lies below or contains another when the filesystem says it is the same file, so case differences on a case-insensitive volume and a precomposed against a decomposed Unicode name (a home such as `jürgen` is stored decomposed on macOS) are the same path. A path that cannot be examined (it does not exist) is compared by its lower-cased string.
        - A missing home directory fails closed with an error that is not a forbidden-mount error, because it is a caller bug.
    - **Mounted unix sockets are unusable.** A host socket in a bind-mounted directory could not be listed or connected to, including the real Socktainer socket, and the host listener saw no connection.
    - **Hardening flags work:** `--read-only --cap-drop ALL --user 1000:1000 --tmpfs /tmp` gave an empty capability set, a read-only root filesystem, a writable `/tmp` and a failing `mount`. Use them where the agent allows.
    - **Never `--ssh`**, which forwards the host ssh-agent. Never `container rm --all`.
    - **Host services are reachable from every guest**, default-network and `--internal` alike, when bound to the LAN address or to all interfaces; a service bound only to loopback was not (issue #69). That includes macOS's own services (Remote Login, Screen Sharing, File Sharing). So: supervisor listeners bind to loopback only (D29); host services that listen on all interfaces are turned off or hardened (SSH without passwords); and a `pf` rule blocks the container subnets from the host's addresses (to be measured, issue #69). The macOS Application Firewall's effect was not measured.
    - **Not probed:** the vsock and vfio device nodes in the guest, `--publish-socket`, `--virtualization` and Rosetta.
    - **Agent-writable repositories are hostile input to the host** (spike #2, item 9). A pre-commit hook and a `core.fsmonitor` command planted from inside a guest ran on the host when the host later ran plain `git commit` and `git status`. The host therefore runs git on agent-writable trees only through `hostgit` (§4.5): read-only plumbing in the agent's tree, an isolated configuration, and cleanup and push only on a supervisor-owned copy fetched from it. The alternates and `.git` redirect checks are still open (issue #19).
    - **Do not mount a shared `.git` read-write into an environment.** It exposes every branch, the shared hooks and config and the other worktrees' metadata. Agent topics get a per-task clone whose object cache is mounted read-only (§4.5).
5. **Supervisor identity.** Login allowlist of forge users, PKCE and `state`, short-lived sessions, scoped revocable CLI tokens, CSRF protection, API bound to loopback and one configured VPN or LAN address, never to all interfaces (D29), forge tokens encrypted at rest, webhook signature verification. Link accounts by provider instance + stable user ID, never by email.
6. **SSH/IDE access.** Short-lived per-session SSH certificates or keys, no password auth, jump host only over VPN, code-server never public and always authenticated, treat Open VSX extensions as supply-chain risk.
7. **Audit and kill switch.** Tamper-evident append-only log stored outside the workspace, linked to commit SHA. `whr kill-all` stops all runs and revokes tokens. Alert on anomalous egress or token spikes. Bot commits are signed with the bot key before push (§4.5).
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

## 9. Interfaces

### 9.1 CLI grammar

Noun-verb with short aliases. Nouns: `task`, `ws`, `run`, `env`, `inbox`. `whr task create` is the canonical long form; common verbs are hoisted.

```bash
whr serve                                  # the supervisor: JSON API, web UI, reconciler
whr login --server <url>
whr run <issue-url> [--backend apple]     # create + start; the core demo
whr ls [--json]
whr show <task>                            # review card: diff stat, tests, CI for current SHA, agent notes, open decisions
whr logs <task> -f
whr watch                                  # live event stream
whr say <task> "msg"                       # or -f guidance.md, or - for stdin
whr pause|resume|cancel <task>
whr purge <task> --transcript [--before <time>]   # delete transcript content, keep audit entries
whr usage [--task <task>] [--since <time>]       # tokens and cost per run, task, repo and period (§5.7)
whr inbox [--watch]
whr approve|reject <decision>
whr answer <decision> <option> [--text "..."]  # a question's fixed option, or a free answer (D23)
whr diff <task>
whr ssh <ws> [--takeover]                  # --takeover pauses the agent and takes the lock
whr ssh --config                           # emit ~/.ssh/config snippet (ProxyJump) for VS Code/JetBrains
whr open <ws> --editor vscode
whr wait <task> --for decision|done
whr kill-all
whr doctor                                 # server, auth, runtime capabilities, SSH config
```

Avoid `review` as a verb (ambiguous). Task refs accept a short ID, a prefix or `repo#42`. **Stable** (D37): `serve`, `run`, `ls`, `logs`, `say`, `cancel`, `inbox`, `approve`, `reject`, `answer`. The other names are provisional until they are built.

### 9.2 Scripting contract

- **Exit codes:** 0 ok · 1 error · 2 usage · 3 not found · 4 auth · 5 conflict/wrong state · 6 needs human input (`whr wait`) · 7 timeout · 10 task failed.
- `--json` returns a stable envelope with `schema_version`; `--jsonl` for streams.
- Streams via SSE, resumable with `--since <event-id>`.
- `Idempotency-Key` on mutations; `--dry-run` for destructive actions.
- Stdout is data, stderr is human text; honour `NO_COLOR` and TTY detection.
- Shell completion generated by the CLI framework (cobra, D14), with dynamic task/workspace ID completion.

### 9.3 Web UI

v0 shows task/issue, repo, branch, PR, recent actions, test results, pending Decisions and resource use, with a single summary card per task. Sections "Harbor" (overview) and "Inbox".

Because the app is a remote for coding agents (§1), v0 also carries the core remote-control loop:

- **Live transcript.** A structured, streamed view of the agent session (messages, tool calls, diffs, test results) over SSE. Not a raw terminal mirror, which reads badly on a phone.
- **Send a message** to the running agent (mid-run instruction injection, §5.2). Delivery is reported honestly: injected now, delivered at the next turn, or, for an agent in degraded mode, as a resumed turn.
- **Start a task** from an issue or a repo, choosing the agent.
- **Pause, resume and cancel** a run.
- **Answer Decisions**, as before.
- **History controls.** *Clear view* hides older events and loads them on request, without deleting anything. *Purge transcript* deletes the stored transcript content after a confirmation that states what goes (event count and size), what stays (the audit entries and a record of the purge) and that it cannot be undone. A chat is paged and virtualized, so a very long one stays usable on a phone.

Writes in v0 are therefore: answer Decisions, send messages, start tasks, pause/resume/cancel, and purge a transcript. Editor launch and takeover (`whr ssh --takeover`) come after v0.

**Previews (D33).** A web app the agent runs is previewed through `whr`'s preview proxy on its own origin, never the UI's, opened on request and closed with its environment (issue #72).

**Stack (D8).** `templ` templates rendered by the Go server, htmx for partial updates and form posts, and the htmx SSE extension for live event and inbox updates. htmx is vendored and version-pinned; styling is plain CSS with design tokens shared with the documentation site (navy and teal, light and dark). Pages are semantic HTML first, so they work without JavaScript for reading. Handlers stay thin: they call the same service layer as the JSON API. Diffs are server-rendered (or use a small library such as diff2html); an interactive terminal (xterm.js) is out of scope for v0.

### 9.4 Notifications

Push when a blocking Decision stops a task: the value of a supervisor is not having to watch it. This includes `auth_expired` and `quota_exhausted` (§5.2), which are the most likely reasons a detached run stalls.

**Default channel: ntfy** (phone and desktop apps; self-hosted or ntfy.sh). Generic webhook and macOS notification are secondary channels; Pushover, Telegram and Web Push are later options. ntfy's iOS delivery through a self-hosted server is **unverified**.

- **Events:** a new blocking Decision (question, approval, review), `auth_expired`, `quota_exhausted`, and a run that ended or failed. Deduplicate and rate-limit per task so a stalled run does not notify repeatedly.
- **Generic payload.** Task ID, event kind and a link only. Never issue text, code, logs, transcripts or tokens: the message leaves the host, may pass a public relay, and issue text is untrusted input.
- **Link, not action.** The notification opens the task in the web UI behind the supervisor login. No approve or answer buttons in the push. Approvals stay per commit SHA (§6).
- **Reachability.** The API listens on loopback and one configured address (D29), so the link uses that address's name and the phone needs the VPN to open it.
- **Topic protection.** A long random topic, or an access token on a self-hosted server. Keep the topic and token in the credential service, never in the repo or logs.
- **Best effort.** The inbox stays the source of truth; a push can arrive late or be lost.

### 9.5 Onboarding

First run is a guided sequence of six steps. The steps are the contract; the surface differs by phase. Release 1 delivers them through `whr login`, `whr doctor` and a config file, because the v0 web UI is scoped to remote control of running tasks (§9.3). A web wizard over the same service layer is a medium-term item (§13). Each step can be skipped and re-run later.

1. **Sign in.** Server URL (reached over the VPN, never public) and the single static access token, stored encrypted. OAuth sign-in comes later (§10).
2. **Connect the forge.** GitHub in release 1 (D15): install the workharbor GitHub App on the chosen repositories; Gitea, Forgejo and GitLab later. Verify the limits the forge enforces, not prompts (§6): the bot can push `agent/*` branches and open PRs, branch protection requires a human review, the bot cannot bypass it, and merge, tag, release and deploy stay forbidden.
3. **Choose the agent login.** `subscription` (device-code sign-in; nothing typed into the web page) or `api-key` (kept in the host proxy), per §5.2. The subscription option states the accepted risk of §7.3.
4. **Check the host.** The checks of `whr doctor`: server and token, container runtime, forbidden mounts rejected, default-deny egress, agent session surviving a reboot, capacity (plan for 4 concurrent environments, §8). A check that has not been verified is reported as not verified, never as passed (spike #2 measured Apple Container isolation and egress; reboot survival is still unverified, §12).
5. **Set up phone notifications.** ntfy provider (self-hosted or ntfy.sh), a generated random topic stored in the credential service, and a test push that carries the generic payload of §9.4. Remind that the link needs the VPN.
6. **Ready.** Summary of what was configured and what is not yet verified, then the first command: `whr run <issue-url>`.

### 9.6 Mobile clients: phone and 12-inch tablet

The phone and a 12-inch tablet are the **primary** clients (D35); a laptop browser is a larger tablet. Both run the installed PWA (§13) over the VPN and its forwarder (D29). The phone is for short, urgent interactions; the tablet replaces the laptop for reviewing and longer supervision.

**Phone** (about 6 inches, one hand, seconds to a few minutes):

| # | Use case | Needs | When |
| --- | --- | --- | --- |
| P1 | A push says a task needs you; open it straight in the inbox | ntfy link to the Decision (§9.4), deep links, fast cold start | R1 |
| P2 | Answer a question: pick a fixed option or type or dictate a short answer | Option buttons, a text field, idempotent submit (§9.2) | R1 |
| P3 | Approve or deny a tool request before its deadline | Tool name, capped input, countdown to the fail-closed deadline (§4.2), one tap each | R1 |
| P4 | Handle an expired login or exhausted quota | "Signed in again, resume", "Resume at reset" or "Cancel" (D23); the vendor's sign-in link or device code | R1 |
| P5 | Check the harbor at a glance | Counts by state, tasks needing you first, usage-window meter (§5.7) | R1 |
| P6 | Send the running agent a short instruction | Message box, delivery shown as injected, next turn or resumed turn | R1 |
| P7 | Stop a runaway run | Pause or cancel per task; `kill-all` behind a confirmation | R1 |
| P8 | Start a task from an issue seen elsewhere | Share an issue link to the PWA (Web Share Target), pick the agent | Later |
| P9 | Follow the live transcript for a minute | Tail mode, the last events only, collapsed tool output | R1 |

**12-inch tablet** (landscape, often with a keyboard, minutes to an hour):

| # | Use case | Needs | When |
| --- | --- | --- | --- |
| T1 | Review a topic and answer "Ready to push?" | Commit list, side-by-side diff in landscape, checks and CI for the pinned SHA (§4.5) | R1 |
| T2 | Supervise several tasks at once | Two panes: task list and transcript; switch tasks without losing scroll | R1 |
| T3 | Plan with the agent, approve its plan | Long messages with a hardware keyboard, plan approval (§6) | R1 |
| T4 | Preview the web app the agent builds next to its transcript | The preview on its own origin (D33) in a second window or Split View | R1 Complete |
| T5 | Start tasks deliberately | Pick an issue, the agent, the permission mode and instructions | R1 |
| T6 | Watch usage and budgets, and the planning board | Usage per task and window (§5.7), a link to the forge board (D30) | R1 Complete |
| T7 | Audit and housekeeping | Event log, purge a transcript with its confirmation (§5.4) | R1 Complete |
| T8 | Take over or edit by hand | Editor launch on the supervisor's copy (§4.5) | Later |

**What follows for the web UI and the PWA:**

- **Two layouts from one server-rendered page** (D8): a single column with actions in thumb reach on the phone; two panes in landscape on the tablet, with keyboard shortcuts and pointer hover. No separate mobile app (§13).
- **Touch first:** targets of at least 44 points, no hover-only controls, readable with large text settings, light and dark.
- **Flaky networks:** live views resume with `Last-Event-ID` (§9.2), and every answer carries an idempotency key, so a double tap or a retry never answers twice.
- **Approving a push needs a fresh check of who you are:** on any device, "Ready to push?" asks for a passkey (WebAuthn) before it accepts the approval, because a phone left unlocked must not be able to publish code. Tool approvals and questions do not, as their deadlines are short and they publish nothing.
- **Per-device tokens**, revocable from the other device (§13), and a short idle timeout on the phone.
- **Nothing sensitive leaves the server:** notifications stay generic (§9.4); diffs and transcripts are rendered on demand and not cached for offline use by the service worker.

## 10. Forge, CI and identity integrations

Keep Git transport separate from forge API operations. The forge API is called from Go with the App's installation token, never through `gh` (D31), and a project board can mirror task state (D30). Release 1 ships **one forge, GitHub** (D15), through a GitHub App installation, with no manual-handoff half-state.

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
| **Agent-task supervisors** (OpenHands, Vibe Kanban, Sculptor, Coder Agents) | Assessed by desk research (issue #5, D22): none fits release 1 | See the scorecard below |

**Agent-task supervisors and vendor remotes** (desk research from docs and repositories, 1 October 2026, issue #5; nothing was installed, so every capability below is as documented, and the marked ones are **unverified**). Fit is against release 1: phone or browser remote, detached runs, mid-run messages, approvals routed to a human, Apple Container, default-deny egress, supervisor-only push, more than one forge.

| Candidate | Runtime | Agents | Approvals to a human | Push | Forges | Fit | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| OpenHands 1.24 (MIT) | Docker or a plain process; no Apple Container | Own agent; Claude Code and Codex over ACP | Confirmation mode for its own agent; for ACP agents **unverified** | Agent holds forge tokens | GitHub, GitLab, Bitbucket | 5 | Borrow: ACP as an adapter protocol, the confirmation states |
| Vibe Kanban 0.1.44 (Apache-2.0) | Host worktrees, no isolation | Claude Code, Codex, Gemini and others | Plan approval cards | The UI pushes, opens and can merge PRs | GitHub, Azure Repos | 3 | Reject: the company shut down and its phone pairing went with its cloud service |
| Sculptor 0.48 (MIT, research preview) | Desktop app; Docker backend experimental | Claude Code, Pi | None: tool permissions are auto-approved | Pushes and opens a PR on a click | GitHub | 3 | Borrow: its Claude control-protocol integration and editable message queue |
| Coder Agents 2.36 (AGPL; Coder Tasks was removed) | Terraform templates; no Apple Container provisioner | Its own loop on API keys; no Claude Code or Codex subscription | Plan mode only | Agent pushes as the user | Through templates (**unverified**) | 2 | Reject |
| Claude Code Remote Control and on the web | Local CLI relayed by the vendor, or vendor VMs | Claude only | Permission modes; approval from the phone **unverified** | The cloud agent pushes itself | GitHub only | 3 | Complement: a reference phone UX and a fallback for Claude |
| Codex cloud | Vendor containers | Codex only | **Unverified** | User-initiated PR | GitHub only | 2 | Complement |

The original rating table missed agent-task supervisors; it is re-rated here (D22).

| Strategy | Original fit | Revised note |
| --- | --- | --- |
| Existing runner + thin supervisor + native Apple Container | 9 | Preferred; Claude Code first, Codex CLI second (D12, §5.2) |
| Same supervisor via Portainer/Socktainer | 7 | → ~4–5; optional shim only |
| Coder workspace layer + task supervisor | 7 | → ~3: Coder Tasks was removed and Coder Agents runs its own loop on API keys (D22) |
| Portainer + templates alone | 4 | Insufficient |
| Full new Codespaces/DevPod replacement | 3 | Excessive scope |
| Adopt/extend an agent-task supervisor | — | 2–3: none supports Apple Container, default-deny egress, host-routed approvals under subscription logins and supervisor-only push together (D22) |

## 12. Open decisions and spikes (reordered)

Ordered by what is cheap and blocks the most work.

1. **Runner scorecard** (value 10, effort 3). Target agents are Claude Code, Codex CLI and Google Antigravity; Aider, OpenHands, Goose and others are scored for reference. Claude Code ships first and Codex CLI second. Antigravity ships a CLI (`agy`) with a headless print mode, so the gate is met: spike #1 drove it headless with typed events and resume. It has no mid-run injection and no approval channel in print mode, so it is a second-tier adapter in degraded mode (§5.2). Its account requirements and vendor terms for headless use are **unverified**. Spike #1 (issue #1) measured Claude Code, and Codex CLI and Antigravity in part; the results are in §5.2, and still open are a real Codex run (usage limit until 3 October), a login that expires mid-session, what the usage-limit `status` reads once exhausted, and approvals for Codex and Antigravity. One page comparing them on: headless mode, permission/approval bypass, session-ID resume after process or VM kill, mid-run message injection (stdin vs resumed turn; a release 1 requirement, §5.2), structured event output (also required), how "blocked, needs human" is reported. Also score subscription sign-in for Claude Code and Codex CLI (all **unverified**): headless or device-code login, where the token is stored, whether it survives a container restart and a Mac reboot, refresh behaviour inside a container, what happens when two environments share one login, how an expired login or exhausted usage window is signalled, current vendor terms for this kind of use, and whether a run keeps going with no client attached. Pause via SIGSTOP or stop-after-turn is not a resumed session; most CLIs resume only between turns.
2. **Adopt-or-extend** (value 9, effort 3): decided by desk research, build (D22, §11). Hands-on checks of the unverified claims are optional and only worth doing if a candidate adds Apple Container support or host-routed approvals.
3. **Apple Container native spike**, merged with benchmarking. Run as spike #2 (issue #2, branch `spike/apple-container`, `RESULTS.md`); measured on Apple Container 1.5.0, macOS 26.6.2. Compatibility checklist:
    - [x] Create/start/stop/delete representative workspaces (start about 1.1 s; use `--init`, §5.1)
    - [x] Enforce explicit CPU/memory (vCPU count and a cgroup limit inside the VM)
    - [x] Preserve project data across stop/start and rebuild (volumes and bind mounts; §4.4)
    - [ ] SSH, VS Code, selected JetBrains IDE (code-server optional). Not tested; `exec` works
    - [x] Recover after runtime/manager restart (§5.3): every container comes back `stopped` with its data
    - [ ] Recover after a Mac reboot (LaunchAgent vs LaunchDaemon; auto-login/FileVault implications). Not triggered; no plist for the services exists on disk, so `container system start` has to run after login
    - [ ] Private registry pulls and credential handling. Public pulls from Docker Hub worked; private registries not tested
    - [x] Agent session and its auth directory survive environment stop/start and rebuild: after `container stop` and `start`, the same agent session resumed from the home volume and remembered an earlier instruction, with the login supplied per exec. Surviving a Mac reboot was not tested
    - [ ] VPN reachability, forwarding or jump host. Not tested
    - [x] **Default-deny egress and network isolation controls** (`--internal` network plus a proxy sidecar; §7.2)
    - [x] **Escape tests: guest cannot reach host or sockets; forbidden mounts rejected** (mounted unix sockets unusable; mount rejection is the adapter's job; §7.4)
    - [ ] Memory behaviour at 1 then 4 instances, including pressure and swap; 8 only once 4 is measured (#39). Not started; an idle agent in a container used about 277 MiB
    - [x] Stock images with a shared read-only tool store (§5.6)
    - [x] Agent run inside a container with a real login (spike #2, `05c-agent-run.sh`): a stock image on an `--internal` network, tools from the store, the model reached only through the proxy sidecar, the spike #1 harness on the host driving it with live events, token deltas, a mid-run message and a resume after a container restart. The agent container used about 290 MiB
    - [ ] Approvals from inside the container (§4.2, D26): the stdio control protocol over `container exec -i` is chosen; reported in issue #7's comments, reproducible evidence and the crash and deadline cases pending (#7, reopened)
    - [x] A reliable cancel from the host (§5.1, D25): measured in spike #10 using the `whr-shim` launcher from the tool store (§5.6, D19). Signalling the host exec client fails; signalling the process group via `container exec <id> /tools/whr-shim kill` terminates cooperative processes in ~10 ms and stubborn trees after a 500 ms grace in ~514 ms, with 0 orphans left
    - [ ] Repositories mounted from the host (§4.5): bind-mount speed with `node_modules`-style trees and much larger repositories
4. **Autonomy and approval policy** (§6) and threat model (§7): decided (D36); the policy table is implemented with a fixed floor (issues #4, #51), and the [threat model](threat-model.md) is written (issue #11). Trust tiers for untrusted input remain (issue #53).
5. **Persistence semantics** (§4.4): decided (D16).
6. **Primary forge** for release 1: decided, GitHub through a GitHub App (D15). A login provider is not needed before OAuth; release 1 signs in with a static token (§9.5).
7. **CI credentials and event handling** for Gitea/Drone (medium term).
8. The `whr` grammar of the slice is decided (D37); the stack too (D3, D8, D14).

Reboot considerations also include power-loss/UPS behaviour and macOS auto-update reboot policy.

## 13. Delivery

### Release 1: one vertical slice

Built CLI first (D12): the slice is the core loop through `whr`; the web UI and the phone client come after it. The [Dogfood milestone](https://github.com/wstein/workharbor/milestone/5) comes first: the subset that lets workharbor run its own issues (D34).

- [ ] Apple Container backend, one host, native adapter
- [ ] Built-in agent adapters for Claude Code (first) and Codex CLI (§5.2, §5.5), with observed progress and validated recovery
- [ ] Task/workspace/run/decision model with durable state, event log, reconciler
- [ ] `whr` CLI (scripting contract, completion, `doctor`)
- [ ] Web UI with inbox, live transcript, send-message, start task, pause/resume/cancel, transcript purge (§9.3, §5.4)
- [ ] SSH access (certificates, `whr ssh --config`)
- [ ] Policy table, per-run credentials, egress proxy, resource budgets, audit log, `whr kill-all`
- [ ] Installable PWA as the phone client: web app manifest and a service worker for the app shell, so the remote-control UI (§9.3) installs to the Home Screen. Stays inside the server-rendered stack (D8), needs HTTPS on the VPN hostname, and uses per-device revocable tokens
- [ ] Single static-token login
- [ ] GitHub through a GitHub App installation (D15)
- [ ] Runtime and forge adapters as interfaces with one implementation each

**Explicitly out of release 1:** code-server, JetBrains validation, OAuth, editor launch and takeover in the UI, CI adapter, multi-host, scheduler beyond an admission counter.

### Releases (D24)

- **When.** The pipeline is built during release 1 and stays dormant; the first release, `v0.1.0`, is cut when the slice demo (issue #28) passes, so the Mac mini runs `whr serve` from a released binary under launchd (issue #38). Then one `0.x` release per milestone. `v1.0.0` waits until the JSON API (OpenAPI, D3), the adapter `contract_version` and the database migrations are stable and an upgrade with a backup has been tested.
- **Version.** The tag is the only source: `git describe` is stamped into the binary with `-ldflags`, built with `-trimpath`; `whr version` prints the version, commit and whether the tree was dirty, and `whr version --json` gives the same as data on stdout with a `schema_version`. Without a tag the version is `v0.0.0-<commits>-g<sha>`, never empty, and a build that was not stamped (plain `go build`) reads it from the Go build info. No version file.
- **Prepare.** An ordinary commit, which an agent may make: `chore(release): prepare vX.Y.Z` regenerates CHANGELOG.md with git-cliff for that version. CI must pass on it.
- **Tag.** Only the human, signed and annotated: `git tag -s vX.Y.Z`. A tag ruleset lets only the repository admin create `v*` tags and forbids updating or deleting them.
- **Build.** A workflow triggered by the tag checks that the tag is annotated and signed by a known key, that the tagged commit is on `main` and that CI passed on it. It then runs GoReleaser (pinned): `darwin/arm64` first, `linux/arm64` and `linux/amd64` for later remote hosts, checksums, an SBOM, a build-provenance attestation and release notes from git-cliff, into a **draft** release. Only that job gets `contents: write` and the attestation permissions.
- **Publish.** The human checks the draft and publishes it. A second workflow, triggered by the publication, updates the Homebrew tap (`brew install wstein/tap/whr`), so the tap never points at a draft.
- **macOS distribution.** The tap is the supported install path. Signing and notarizing a downloaded binary need an Apple Developer ID and are deferred until someone other than the developer installs it from a download.

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

Reviewers disagreed on three points; the resolutions adopted here:

- **Forge handoff:** manual compare-URL handoff (product) vs one forge, no half-state (architect). Adopted: one forge, no half-state; since D15 that forge is GitHub through a GitHub App, not a PAT.
- **Web UI:** defer entirely (ops) vs minimal inbox (product, architect). Adopted then: a minimal read-mostly UI with inbox, since decisions are answered there. Since superseded by the remote-control scope of §9.3 (D8, D12).
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
