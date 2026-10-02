---
title: Roadmap
description: "Existing platforms, open decisions and spikes, and the delivery plan."
weight: 6
toc: true
---

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

**Agent-task supervisors and vendor remotes** (desk research from docs and repositories, 1 October 2026, issue #5; nothing was installed, so every capability below is as documented, and the marked ones are {{< status unverified >}}). Fit is against release 1: phone or browser remote, detached runs, mid-run messages, approvals routed to a human, Apple Container, default-deny egress, supervisor-only push, more than one forge.

| Candidate | Runtime | Agents | Approvals to a human | Push | Forges | Fit | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| OpenHands 1.24 (MIT) | Docker or a plain process; no Apple Container | Own agent; Claude Code and Codex over ACP | Confirmation mode for its own agent; for ACP agents {{< status unverified >}} | Agent holds forge tokens | GitHub, GitLab, Bitbucket | 5 | Borrow: ACP as an adapter protocol, the confirmation states |
| Vibe Kanban 0.1.44 (Apache-2.0) | Host worktrees, no isolation | Claude Code, Codex, Gemini and others | Plan approval cards | The UI pushes, opens and can merge PRs | GitHub, Azure Repos | 3 | Reject: the company shut down and its phone pairing went with its cloud service |
| Sculptor 0.48 (MIT, research preview) | Desktop app; Docker backend experimental | Claude Code, Pi | None: tool permissions are auto-approved | Pushes and opens a PR on a click | GitHub | 3 | Borrow: its Claude control-protocol integration and editable message queue |
| Coder Agents 2.36 (AGPL; Coder Tasks was removed) | Terraform templates; no Apple Container provisioner | Its own loop on API keys; no Claude Code or Codex subscription | Plan mode only | Agent pushes as the user | Through templates ({{< status unverified >}}) | 2 | Reject |
| Claude Code Remote Control and on the web | Local CLI relayed by the vendor, or vendor VMs | Claude only | Permission modes; approval from the phone {{< status unverified >}} | The cloud agent pushes itself | GitHub only | 3 | Complement: a reference phone UX and a fallback for Claude |
| Codex cloud | Vendor containers | Codex only | {{< status unverified >}} | User-initiated PR | GitHub only | 2 | Complement |

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

Ordered by what is cheap and blocks the most work. The measured results are on the [spike pages](../spikes/_index.md).

1. **Runner scorecard** (value 10, effort 3). Target agents are Claude Code, Codex CLI and Google Antigravity; Aider, OpenHands, Goose and others are scored for reference. Claude Code ships first and Codex CLI second. Antigravity ships a CLI (`agy`) with a headless print mode, so the gate is met: spike #1 drove it headless with typed events and resume. It has no mid-run injection and no approval channel in print mode, so it is a second-tier adapter in degraded mode (§5.2). Its account requirements and vendor terms for headless use are {{< status unverified >}}. Spike #1 (issue #1) measured Claude Code, and Codex CLI and Antigravity in part; the results are in §5.2, and still open are a real Codex run (usage limit until 3 October), a login that expires mid-session, what the usage-limit `status` reads once exhausted, and approvals for Codex and Antigravity. One page comparing them on: headless mode, permission/approval bypass, session-ID resume after process or VM kill, mid-run message injection (stdin vs resumed turn; a release 1 requirement, §5.2), structured event output (also required), how "blocked, needs human" is reported. Also score subscription sign-in for Claude Code and Codex CLI (all {{< status unverified >}}): headless or device-code login, where the token is stored, whether it survives a container restart and a Mac reboot, refresh behaviour inside a container, what happens when two environments share one login, how an expired login or exhausted usage window is signalled, current vendor terms for this kind of use, and whether a run keeps going with no client attached. Pause via SIGSTOP or stop-after-turn is not a resumed session; most CLIs resume only between turns.
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
4. **Autonomy and approval policy** (§6) and threat model (§7): decided (D36); the policy table is implemented with a fixed floor (issues #4, #51), and the [threat model](../threat-model.md) is written (issue #11). Trust tiers for untrusted input remain (issue #53).
5. **Persistence semantics** (§4.4): decided (D16, D39). Open: a checkout on a volume for repositories that track more than about 50 000 files. It needs the branch exported from inside the guest for hostgit, for example as a bundle written by a disposable helper container that mounts the volume read-only; measured cost of seeding such a volume: 68 s for 153 000 files (spike #40).
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
- [ ] Workspaces with named agents (D42) in two steps: the dogfood slice runs one named agent per workspace, with its agent clone and the bundle export (#90, #91); several agents in one environment at once follow in R1 Complete (#94)
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
- **Build.** A workflow triggered by the tag checks that the tag is annotated and signed by a known key (an SSH key listed in `.github/release-signers`), that the tagged commit is on `main` and that CI passed on it. It then runs GoReleaser (pinned): `darwin/arm64` first, `linux/arm64` and `linux/amd64` for later remote hosts, checksums, an SBOM, a build-provenance attestation and release notes from git-cliff, into a **draft** release. Only that job gets `contents: write` and the attestation permissions.
- **Publish.** The human checks the draft and publishes it. A second workflow, triggered by the publication, updates the Homebrew tap (`brew install wstein/tap/whr`), so the tap never points at a draft.
- **macOS distribution.** The tap is the supported install path. Signing and notarizing a downloaded binary need an Apple Developer ID and are deferred until someone other than the developer installs it from a download.

### Medium term

**Auth-mode phasing** (D41): subscription logins first, with the guardrails of D40 (quota stops, the shared usage window, the agent's permission mode; #36, #48, #82); API keys as a full peer in the medium term (#83); an enterprise offering in the long term.

- [ ] API-key mode as a full peer (D41, #83): the host-side key proxy that issues a key per run (§7.3) instead of the configuration's key file, spend budgets (#37) and per-key usage (#48)
- [ ] OAuth providers, Gitea/Forgejo/GitLab/GitHub adapters
- [ ] Docker/Podman backends, remote Linux hosts (host worker becomes remote-capable)
- [ ] Alpine as a first-class console base (#93): a small image and a fast package manager for the human's console (D43); no agent runs there, so musl only affects the human's own tools
- [ ] Firecracker as a runtime target (#85): a microVM with its own guest kernel per environment, the isolation model of Apple Container, on a Linux host with KVM (remote, or a Linux VM on the Mac if nested virtualization allows it, {{< status unverified >}}). It boots a kernel and a root filesystem rather than an OCI image, so stock and devcontainer images need a conversion or Kata Containers or firecracker-containerd underneath; the spike decides, and the runtime conformance suite is the gate
- [ ] Antigravity adapter in degraded mode (`agy` print mode, §5.2, §12) and additional runners
- [ ] Out-of-process adapter plugin loader with conformance checks (§5.5, §7.8)
- [ ] Drone CI with revision-aware feedback
- [ ] Resource-aware scheduling, recovery improvements
- [ ] code-server, UI editor launch and takeover, `whr top`
- [ ] Web onboarding wizard over the same service layer (§9.5); release 1 uses `whr login` and `whr doctor`
- [ ] Web Push alongside ntfy (§9.4) for the installed PWA. iOS Web Push needs the app installed to the Home Screen ({{< status unverified >}})

### Long term

- [ ] An enterprise offering: several developers on one supervisor under the vendors' commercial terms (Team or Enterprise plans, API keys or a cloud provider), with per-user identity and audit. D40's one-human rule is for consumer subscriptions; this needs its own decision (D41)
- [ ] Proxmox VE and VMware vSphere/ESXi as runtime targets (#86): environments as full VMs on an existing virtualization cluster, each with its own kernel (a Proxmox LXC container shares the host kernel and does not meet §7's bar), driven through their APIs with credentials scoped to one pool or folder, an isolated network per environment with the egress sidecar, and the runtime conformance suite as the gate. A full VM boots far slower than Apple Container's 1.1 s (spike #2), which may call for pooled environments
- [ ] Alpine as a first-class base for agent environments (#93), once each agent CLI has a musl build that passes `agenttest` and the common toolchains work on musl (Python wheels, Node native modules); the tool store then pins musl builds next to the glibc ones (D19)
- [ ] A Windows host with Hyper-V VMs (#87): the supervisor on Windows, each environment a Hyper-V VM with its own kernel. A host port, not only a runtime: the Windows counterparts of Homebrew (D28), the `pf` rules (D29), launchd and `0600` secret files are needed, and whether Firecracker (#85) can run inside WSL2 is {{< status unverified >}}
- [ ] Other microVM/VM platforms, Kubernetes where useful
- [ ] Multiple hosts and placement policies
- [ ] Wider forge/CI coverage

**Not planned:**

- Native iOS and Android apps. The installable PWA (release 1) is the mobile client; the JSON API stays the contract for any client.
- VirtualBox as a runtime. One VM per environment would meet §7, but it adds little next to Apple Container on the Mac and Firecracker, Proxmox or VMware on Linux; it is driven only through `VBoxManage`, boots full VMs slowly, and its shared folders are slow where spike #40 already found bind mounts costly. Whether it runs on Apple-silicon Macs is {{< status unverified >}}.
- WSL2 as a runtime. Its distributions share one utility VM and one Linux kernel, so environments would be separated by namespaces only, which fails §7's bar of a kernel per environment. Windows is a host platform instead (#87).

Phases are proposals, not a schedule.
