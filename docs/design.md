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

## 7. Security

Threat model and autonomy policy are written before the build.

1. **Untrusted input.** Issue text, PR comments and CI logs are untrusted. Use trust tiers by author (owner vs external); hold or flag runs on issues from unknown authors. A run that combines private data, untrusted input and outbound network requires approval.
2. **Default-deny egress** through a logging allowlist proxy: forge, package registries, LLM API only. Block LAN, host, other workspaces and cloud-metadata addresses. Add to the compatibility checklist; Apple Container network isolation controls are unverified.
3. **Credentials.** Run-scoped, short-lived, single-repo, non-extractable. GitHub App installation tokens (~1 h); per-repo bot tokens or deploy keys for Gitea/Forgejo/GitLab. Inject through a git credential helper or host-side proxy so raw tokens never reach env vars, disk or logs. Keep the LLM API key in the proxy. Revoke at run end. Redact retained transcripts. Agent and CI credentials are separate.
4. **Isolation policy, testable.** Reject mounts of `$HOME`, `~/.ssh` and runtime sockets. Non-root agents, read-only rootfs where feasible, hard CPU/memory/disk quotas, per-run timeout and token/cost budget. Escape tests (guest cannot reach host or Socktainer socket) in the conformance suite. The VM boundary does not protect what is deliberately exposed.
5. **Supervisor identity.** Login allowlist of forge users, PKCE and `state`, short-lived sessions, scoped revocable CLI tokens, CSRF protection, API bound to loopback/VPN, forge tokens encrypted at rest, webhook signature verification. Link accounts by provider instance + stable user ID, never by email.
6. **SSH/IDE access.** Short-lived per-session SSH certificates or keys, no password auth, jump host only over VPN, code-server never public and always authenticated, treat Open VSX extensions as supply-chain risk.
7. **Audit and kill switch.** Tamper-evident append-only log stored outside the workspace, linked to commit SHA. `whr kill-all` stops all runs and revokes tokens. Alert on anomalous egress or token spikes. Optional: signed bot commits.

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

v0 shows task/issue, repo, branch, PR, recent actions, test results, pending Decisions and resource use, with a single summary card per task. Sections "Harbor" (overview) and "Inbox". Writes: answering Decisions only. Pause/resume and editor launch come after v0.

### 9.4 Notifications

Push (ntfy, webhook or macOS notification) when a blocking Decision stops a task: the value of a supervisor is not having to watch it.

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

1. **Runner scorecard** (value 10, effort 3). One page comparing Claude Code, Codex CLI, Aider, OpenHands, Goose and others on: headless mode, permission/approval bypass, session-ID resume after process or VM kill, mid-run instruction injection (stdin vs resumed turn), structured event output, how "blocked, needs human" is reported. Pause via SIGSTOP or stop-after-turn is not a resumed session; most CLIs resume only between turns.
2. **Adopt-or-extend spike** (value 9, effort 3). Time-box 1–2 days on two of OpenHands, Vibe Kanban, Sculptor, Coder Tasks before committing to a build.
3. **Apple Container native spike**, merged with benchmarking. Compatibility checklist:
    - [ ] Create/start/stop/delete representative workspaces
    - [ ] Enforce explicit CPU/memory
    - [ ] Preserve project data across stop/start and rebuild
    - [ ] SSH, VS Code, selected JetBrains IDE (code-server optional)
    - [ ] Recover after runtime/manager restart and Mac reboot (LaunchAgent vs LaunchDaemon; auto-login/FileVault implications; `container system start` on boot)
    - [ ] Private registry pulls and credential handling
    - [ ] VPN reachability, forwarding or jump host
    - [ ] **Default-deny egress and network isolation controls**
    - [ ] **Escape tests: guest cannot reach host or sockets; forbidden mounts rejected**
    - [ ] Memory behaviour at 4 then 8 instances, including pressure and swap
4. **Autonomy and approval policy** (§6) and threat model (§7): a security decision that feeds credentials and UI.
5. **Persistence semantics** (§4.4).
6. **Primary forge and login provider** for release 1.
7. **CI credentials and event handling** for Gitea/Drone (medium term).
8. Confirm stack (§3 D3) and finalize the `whr` grammar.

Reboot considerations also include power-loss/UPS behaviour and macOS auto-update reboot policy.

## 13. Delivery

### Release 1: one vertical slice

- [ ] Apple Container backend, one host, native adapter
- [ ] One agent runner with observed progress and validated recovery
- [ ] Task/workspace/run/decision model with durable state, event log, reconciler
- [ ] `whr` CLI (scripting contract, completion, `doctor`)
- [ ] Read-mostly web UI with inbox
- [ ] SSH access (certificates, `whr ssh --config`)
- [ ] Policy table, per-run credentials, egress proxy, resource budgets, audit log, `whr kill-all`
- [ ] Single static-token login
- [ ] One forge via PAT/bot token
- [ ] Runtime and forge adapters as interfaces with one implementation each

**Explicitly out of release 1:** code-server, JetBrains validation, OAuth, pause/resume and editor launch in the UI, CI adapter, multi-host, scheduler beyond an admission counter.

### Medium term

- [ ] OAuth providers, Gitea/Forgejo/GitLab/GitHub adapters
- [ ] Docker/Podman backends, remote Linux hosts (host worker becomes remote-capable)
- [ ] Additional runners
- [ ] Drone CI with revision-aware feedback
- [ ] Resource-aware scheduling, recovery improvements
- [ ] code-server, UI pause/resume and editor launch, `whr top`

### Long term

- [ ] Other microVM/VM platforms, Kubernetes where useful
- [ ] Multiple hosts and placement policies
- [ ] Wider forge/CI coverage

Phases are proposals, not a schedule.

## 14. Review log

Reviewers disagreed on two points; the resolutions adopted here:

- **Forge handoff:** manual compare-URL handoff (product) vs one forge, no half-state (architect). Adopted: one forge via PAT.
- **Web UI:** defer entirely (ops) vs minimal inbox (product, architect). Adopted: minimal read-mostly UI with inbox, since decisions are answered there.

Skipped for now: separate identity service, Kubernetes, multi-host placement, Portainer/Coder UI integration.

## 15. References

Carried over from the original summary; compatibility claims require testing against pinned versions.

- Apple Container: [README](https://github.com/apple/container), [technical overview](https://github.com/apple/container/blob/main/docs/technical-overview.md), [container machine](https://github.com/apple/container/blob/main/docs/container-machine.md), [VS Code Remote-SSH example](https://github.com/apple/container/blob/main/examples/container-machine-vscode/README.md)
- Socktainer: [repository](https://github.com/socktainer/socktainer)
- Editors: [code-server FAQ](https://github.com/coder/code-server/blob/main/docs/FAQ.md), [JetBrains remote development](https://www.jetbrains.com/help/idea/remote-development-overview.html)
- Platforms: [Coder](https://coder.com/docs/about), [external provisioners](https://coder.com/docs/admin/provisioners), [DevPod](https://devpod.sh/docs/what-is-devpod), [Eclipse Che](https://eclipse.dev/che/docs/)
- Portainer: [Add-ons](https://docs.portainer.io/admin/add-ons), [custom templates](https://docs.portainer.io/user/docker/templates/custom), [REST API](https://docs.portainer.io/api/access)
- OAuth: [GitHub](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps), [Gitea](https://docs.gitea.com/development/oauth2-provider/), [Forgejo](https://forgejo.org/docs/latest/admin/advanced/oauth2-provider/), [Codeberg](https://docs.codeberg.org/integrations/keycloak/), [GitLab](https://docs.gitlab.com/api/oauth2/)
