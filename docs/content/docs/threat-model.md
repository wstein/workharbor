---
title: Threat model
description: Assets, trust boundaries, threats, controls and accepted risks for workharbor.
weight: 2
toc: true
---

**Status:** updated 1 October 2026 for release 1 (issue #11): after the review of `main` at 26ce0d4 and its fixes (#78, #79), and for the vendor terms of subscription logins (D40, T17). It refines [design §7](design/security.md#7-security), which stays the list of security rules; this page says what those rules defend against, where each is enforced and tested, and which risks are accepted. A control is **measured** when a spike or test showed it working, **planned** when an issue implements it, and **open** when nothing covers it yet.

## Scope and assumptions

workharbor runs on one developer's Apple-silicon Mac. It drives coding agents (Claude Code first, Codex CLI second) in Apple Container environments and talks to one forge, GitHub (D15). The developer reaches it from a laptop or phone over a VPN.

- **One trusted human.** The developer and the Mac's macOS account are trusted. Protecting the developer from themselves is out of scope.
- **The agent is not trusted.** Its model output follows whatever text reaches it, including issue text written by strangers, so every agent and everything it writes is treated as potentially hostile.
- **The VM boundary holds.** Apple Container runs each container in its own lightweight VM (§5.1). A hypervisor escape is out of scope; what the supervisor deliberately exposes to a guest is in scope.
- **Vendors are trusted for what they host.** The LLM vendors and GitHub see the code they are sent. Choosing to send it is the developer's decision, not a threat this page addresses.

## Assets

| Asset | Why it matters |
| --- | --- |
| The host: `$HOME`, `~/.ssh`, the login keychain, other repositories, local services | Full compromise of the developer's machine and every account on it |
| Forge credentials: the GitHub App private key and its installation tokens | Write access to the repositories the App is installed on |
| Agent logins: a subscription login or an API key | Spend, usage quota and the developer's account with the vendor |
| Repository contents and unpushed work | Source code, possibly private, and work not yet anywhere else |
| The supervisor: its database, audit log, sessions and tokens | Control over every agent, and the record of what they did |
| The developer's attention | Approvals are only as good as what the developer is shown |

## Trust boundaries

```text
phone / laptop ──VPN──▶ supervisor (host, trusted) ──▶ GitHub API, ntfy
                          │ exec, mounts
                          ▼
          ┌──── environment (guest VM, untrusted) ─────┐
          │ agent process, agent home volume,          │
          │ bind-mounted per-task checkout (writable)  │
          └───────────────┬────────────────────────────┘
                          │ --internal network only
                          ▼
              proxy sidecar (trusted, full egress) ──▶ LLM API, allowlisted hosts
```

1. **Guest to host.** Mounts, the `exec` channel, and files the host later reads from an agent-writable checkout.
2. **Guest to network.** Only through the proxy sidecar on a per-environment `--internal` network (§7.2).
3. **Supervisor to clients.** The JSON API and web UI, reachable only on loopback or the VPN (§7.5).
4. **Supervisor to the forge.** API calls and pushes with the App's tokens; webhooks coming back.
5. **Untrusted text into the agent.** Issue and PR text, comments, CI logs and anything fetched while working.

## Threat sources

| ID | Source | Typical path |
| --- | --- | --- |
| A1 | A stranger who writes an issue or comment | Prompt injection through text the agent reads |
| A2 | A compromised or misled agent | Anything the guest can reach: mounts, network, files the host reads |
| A3 | A malicious dependency or tool in the environment | Same reach as the agent |
| A4 | Someone on the LAN or holding a lost phone | The supervisor API, sessions, notifications |
| A5 | A compromised upstream: an agent CLI release, a plugin, a CI action | Code that runs with the supervisor's or the agent's rights |
| A6 | workharbor itself, used beyond a vendor's terms | A feature that handles a subscription credential or starts runs without the human |

## Threats and controls

| # | Threat | Source | Controls | Enforced and tested in | Status |
| --- | --- | --- | --- | --- | --- |
| T1 | Injected instructions make the agent do something harmful | A1 | Untrusted input is data, never instructions; runs on issues by untrusted authors wait for a Decision; sensitive actions on untrusted input ask (§6, §7.1) | #53 | Open |
| T2 | The guest reads or writes host secrets through a mount | A2, A3 | The adapter rejects `$HOME` and its parents, secrets directories, sockets and system directories, after resolving symlinks (§4.4, §7.4) | #18, #50, #58; the Apple Container adapter mounts only the resolved path (#26) | Implemented |
| T3 | The host runs code planted in an agent checkout (hooks, `core.fsmonitor`, filters, `core.sshCommand`) | A2 | Host git runs only through `hostgit`: the checkout is checked first (real `.git`, alternates naming only the cache, no `gitdir:` redirect), only read-only plumbing with exact option lists runs in the agent's tree, the configuration is isolated, and cleanup and push run on a supervisor-owned copy fetched only from a stopped environment (§4.5, §7.4). Under D42 the host never runs git in a workspace at all: commits leave as a verified bundle streamed from the environment, and the human's own repositories are never mounted | #19; enforced in the push flow (`service/publish.go`, #27); bundle export #91 | Implemented and tested for per-task clones; the workspace path (D42) is planned in #90 and #91 |
| T4 | The guest reaches the host, the LAN or other environments | A2, A3 | One `--internal` network per environment blocks the internet, other LAN devices and other environments, but not the host's own addresses; so supervisor listeners bind to loopback only, a `pf` rule blocks the container subnets from the host, and host services on all interfaces are turned off or hardened (§7.2, D29) | Egress conformance passes on Apple Container (#26); host reachability #69 | Contradicted for the host's own addresses: guests reach host services on the LAN address (#69); the `pf` mitigation is unmeasured |
| T5 | Data leaves through the network | A2, A3 | Default-deny allowlist by hostname in the sidecar, every decision logged; no DNS in the guest (§7.2) | #26, #78 | Implemented and conformance-tested; ports 443 and 80 only, non-public addresses refused after resolution, rebinding closed by dialling the checked address (#78) |
| T6 | The agent pushes, merges, tags or releases unreviewed code | A1, A2 | The agent never pushes (D18); the supervisor pushes after a "Ready to push?" Decision per commit SHA; the policy floor forbids merge, tag, release and deploy; a GitHub ruleset requires human review and lists no bypass for the App (§6, D15) | #4, #17, #49, #51, #27 | Partly implemented |
| T7 | An approval is granted by mistake, late or for different code | A1, A2, A4 | Approvals fail closed on timeout and when the channel is lost; they are tied to a commit SHA; input is capped and shown as untrusted; the transport is the agent's stdio control protocol, with no network path or token in the guest (§4.2, D26) | #17, #49, #57; transport #7 (reopened), #25 | Domain logic implemented; transport reported but not reproducible, crash and deadline cases unverified (#7) |
| T8 | Forge credentials leak | A2, A3 | Installation tokens last about an hour, are scoped to the installed repositories, reach git through a credential helper or the proxy, and never sit in the guest's environment, on disk or in logs (§7.3, D15) | #27, #22 | Planned |
| T9 | Secrets end up in logs, transcripts or notifications | A2, A4 | Redaction at ingest, before events, Decision inputs and audit entries are written; generic notification payloads with no content (§5.4, §9.4) | #22, #31 | Implemented: redaction at ingest in the store, generic ntfy payloads |
| T10 | The agent login is stolen from the environment | A2, A3 | Dedicated auth directory per environment, never `$HOME`; egress allowlist; revocation at the vendor when an environment is deleted (§7.3). A subscription login never touches the host: the human signs in inside the environment, and `whr` never reads, stores or relays it (D40). An API key is the only agent credential on the host: `0600`, single-linked, outside every root, read with `O_NOFOLLOW`, passed through a `0600` env file, never a command line | #26, #78, #81, #82 | Accepted risk inside the environment; the host side is implemented and tested, and a subscription token in the configuration is refused (#81); in-environment sign-in is spike #82 |
| T11 | Someone else drives the supervisor | A4 | API on loopback only, reached remotely through a forwarder (`tailscale serve`, or a proxy admitted by `pf` for a router VPN); the API token on every request, since a forwarder on a non-loopback address is reachable by guests; per-device revocable tokens for the phone app; CSRF protection (§7.5, D29) | #24, #33, #69 | Planned; loopback measured unreachable from guests (#69), forwarder and `pf` unmeasured |
| T12 | The record of what happened is altered | A2, A5 | Append-only audit log outside the workspace, hash-linked to transcript content; purges record themselves (§5.4, §7.7) | #21, #37, #66 | Partly implemented: audit tier, redaction before writing and self-recording purges exist; hash-linking is planned (#37); the guard bypass found in review is open (#66) |
| T13 | An agent exhausts the host or the budget | A2 | CPU and memory limits per environment, admission control, time and cost budgets, `whr kill-all` (§7.4, §8, §5.7) | #37, #48 | Measured limits; budgets planned |
| T14 | A tampered tool or plugin runs in an environment or in the supervisor | A5 | Agent CLIs come from a content-addressed, read-only store verified against the vendor's checksums; plugins run out of process with the same isolation (D19, D20, §7.8); CI actions pinned by SHA | Tool store (#74); plugins later | Implemented for Claude Code (pinned version, https only, size cap, SHA-256 of the stored bytes against the pin and the vendor manifest, read-only store); plugins later |
| T15 | The developer's editor runs planted repository config when opening an agent checkout | A2 | The editor gets a supervisor-owned copy of the topic, refreshed by hardened fetch and marked stale while the run is active; the agent's checkout is never offered (§4.5). Under D42 the copy comes from the imported bundle; a human who opens a workspace's worktree directly does so at their own risk, and the console (D43) is the place to work in workspaces | #59, #91; the CLI and UI launch in #24, #30 | Implemented in the service; front ends planned |
| T16 | SSH or IDE access into an environment is abused or left open | A4, A5 | Short-lived per-session certificates, no passwords, reachable only over the VPN; code-server never public; editor extensions treated as a supply-chain risk (§7.6). SSH and `whr console` land in the console environment, never on the host; only the admin logs in to the host (D43). The console holds no credentials, mounts workspaces read-only by default and runs git with hooks and fsmonitor off | #32, #92 | Planned, after the slice |
| T17 | The vendor suspends the developer's account for use outside its terms | A6 | `whr` never handles a subscription credential; one human per supervisor starts every run; forge events only fill the inbox; no scheduled or unattended runs; unmodified vendor binaries; a quota stop pauses with a Decision, no retries or account rotation (D40) | #81, #82, #71 | Decided; the configuration refuses a subscription token (#81); the reading of the terms is ours and {{< status unverified >}} |

## Accepted risks

These are accepted for a single-developer, watched personal tool. Each has a limit, and each is revisited if the tool gets more users.

- **The agents of one workspace are one trust domain** (D42). They share the agent clone's `.git` and can change each other's worktrees, branches, hooks and config. Limit: a workspace never contains the human's own repository, the host never runs git in it, and nothing leaves it except a verified bundle whose commits are approved per SHA. Agents that must not touch each other belong in separate workspaces.
- **The console runs agent-written repository config** (D43) when the human runs git or tools there. Limit: it is a VM with no host home, secrets, credentials or runtime socket; workspaces are read-only by default; hooks and fsmonitor are off. Filter drivers and textconv set in a workspace's config are not covered ({{< status unverified >}} until spike #89).

- **Data can leave through an allowed host.** The sidecar matches on the hostname in `CONNECT`, so `api.anthropic.com` can carry data out, and domain fronting is not stopped. Limit: the allowlist is minimal, every request is logged, and the logs are reviewable.
- **The sidecar is trusted and has full egress.** Limit: it runs only the proxy, from a pinned image, with no access to secrets.
- **A subscription login sits inside the environment.** Any process in the guest can read it while a run is active. Limit: the human signs in there through the vendor's own flow and `whr` never sees the credential (D40), one dedicated auth directory per environment, never the host's, and revocation at the vendor when the environment is deleted. That vendor revocation works as expected is {{< status unverified >}}.
- **Host git on agent trees is limited, not eliminated.** `hostgit` checks the checkout, isolates the configuration and allows only read-only plumbing there, and fetching into a supervisor-owned copy is the default. The push flow fetches only from a stopped environment (#27), so no guest process can change the files between the checks and the fetch.
- **The agent sees what it is given.** Repository contents go to the LLM vendor. That is the developer's choice per repository.

## Open items

- Guests reach the host's LAN address and every service on all interfaces (#69): the `pf` rule blocking container subnets and the hardening of macOS's own services are not yet measured. T1 has no implemented control yet (#53); T15's front ends come with #24 and #30.  T7's transport needs reproducible evidence and the crash and deadline cases (#7).
- Links inside a secrets directory are followed one level and the locations found in review are rejected (#58). Mounts can also be limited to the workspace roots workharbor owns, as a second layer behind the deny-list (`CheckMountsWithin`). Accepted: a hard link to a secret inside a project, and links more than one level deep inside a secrets directory.
- Webhook signature verification and the author association used for trust tiers are part of the forge adapter, #27.
- This page is reviewed whenever a D-row changes a boundary, and before release 1.
