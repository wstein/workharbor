# workharbor — Design

**CLI:** `whr` · **Status:** Revised design, 30 September 2026 · No implementation or compatibility testing yet.

Revision of the original discussion summary (`agent-work-supervisor-summary.md`) after a four-role team review (architecture, security, product/CLI, feasibility/ops). Review ratings are value/effort out of 10. Claims about Apple Container, Socktainer, Coder and Portainer come from the original sources and are **unverified** until the spikes in §12 are done.

## 1. Goal

A self-hosted service in which AI coding agents carry out project work independently while one developer acts as human-in-the-loop (HitL): intervening, answering questions, reviewing and approving.

- Agents work repository issues, modify code, run tests, update issues and open or update PRs.
- The developer attaches via SSH or an editor temporarily. Disconnecting never interrupts the agent.
- Web UI and `whr` CLI are two clients of one API.
- First host: Apple-silicon Mac mini (16 GB, ~1 TB) on Apple Container. Other runtimes later via adapters.

The central concept is an **agent task supervisor with managed workspaces**, not an editor-centred dev environment.

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
| D3 | **Go, single static binary** (server, host worker and `whr` as subcommands), **SQLite in WAL mode**, embedded web UI, OpenAPI as the single source for CLI and UI types, SSE for live events | One host, one user; easy launchd packaging, later Linux cross-compile |
| D4 | Agent runner chosen **first**, by scorecard (§12), preferring one with a structured headless protocol | Constrains the whole model |
| D5 | Approval boundaries are **data** (a policy table), enforced at the forge adapter, never by prompts | Prompt rules are not a security boundary |
| D6 | Supervisor is a **DB-first reconciler**, not a process tree | Only realistic answer to reboot/restart gaps |
| D7 | Harbor metaphor is for branding and UI section names only; CLI and API use plain nouns | Guessable, searchable commands |

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

### 4.3 Lifecycle rules

- IDE/SSH disconnect does not pause the agent or stop the environment.
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
| Web UI | v0: read-only task list, event log and inbox; answering Decisions is the only write |
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

Specified as explicitly as the runtime contract. Capability flags:

- headless / unattended operation
- mid-run instruction injection
- cooperative pause (e.g. stop after current turn)
- session persistence and resume
- structured event stream
- PR/issue tooling
- "awaiting guidance" signal (how the agent raises a blocking Decision)

### 5.3 Reconciler

Desired state lives in the database. A loop compares it with actual runtime state, marks orphaned runs `interrupted`, and resumes from the agent session rather than the VM. This is the answer to Apple Container's missing restart-policy recovery.

### 5.4 Events and idempotency

Per-task append-only event log doubles as audit trail, UI feed and CLI stream. Every mutating command accepts an idempotency key.

## 6. Policy and autonomy

Autonomy is a per-repo/per-task policy table: **action → `auto | ask | forbid`**.

| Action | Default |
| --- | --- |
| Push to `agent/*` branches | auto |
| Open/update PR, comment on issue | auto |
| Merge, tag, release, deploy | **forbid** for the agent; human-gated |
| Sensitive actions triggered by untrusted input | ask |

Enforcement is outside the agent: forge branch protection, required human review, and a bot identity that cannot bypass them. Approval is per commit SHA (ties to ReviewCandidate). Every approval is a Decision record.
