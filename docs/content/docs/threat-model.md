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
| T1 | Injected instructions make the agent do something harmful | A1 | Untrusted input is data, never instructions; runs on issues by untrusted authors wait for a Decision; sensitive actions on untrusted input ask (§6, §7.1) | #53 | Partly implemented (untrusted authors held for a Decision; #53) |
| T2 | The guest reads or writes host secrets through a mount | A2, A3 | The adapter rejects `$HOME` and its parents, secrets directories, sockets and system directories, after resolving symlinks (§4.4, §7.4) | #18, #50, #58; the Apple Container adapter mounts only the resolved path (#26) | Implemented |
| T3 | The host runs code planted in an agent checkout (hooks, `core.fsmonitor`, filters, `core.sshCommand`) | A2 | The host never runs git in a workspace (D42). An agent's commits leave as a `git bundle` streamed out of the environment and fetched, with `fsckObjects` and a size cap, into a scratch repository and only then into the supervisor's own repository; the fetch is the check. Cleanup and push run on that supervisor-owned repository through `hostgit` (isolated configuration, hooks and fsmonitor off), whose target comes from the forge mirror, never from a workspace. The human's own repositories are never mounted (§4.5, §7.4) | #91, #90 (per-task clones and reading an agent checkout removed), #27 | Implemented and tested: the bundle import refuses a truncated or corrupt bundle, and nothing planted in a workspace's `.git` runs on the host (spike #89) |
| T4 | The guest reaches the host, the LAN or other environments | A2, A3 | One `--internal` network per environment blocks the internet, other LAN devices and other environments, but not the host's own addresses; so supervisor listeners bind to loopback only, a `pf` rule blocks the container subnets from the host, and host services on all interfaces are turned off or hardened (§7.2, D29) | Egress conformance passes on Apple Container (#26); host reachability #69 | Contradicted for the host's own addresses: guests reach host services on the LAN address (#69); the `pf` mitigation is unmeasured |
| T5 | Data leaves through the network | A2, A3 | Default-deny allowlist by hostname in the sidecar, every decision logged; no DNS in the guest (§7.2) | #26, #78 | Implemented and conformance-tested; ports 443 and 80 only, non-public addresses refused after resolution, rebinding closed by dialling the checked address (#78) |
| T6 | The agent pushes, merges, tags or releases unreviewed code | A1, A2 | The agent never pushes (D18); the supervisor pushes after a "Ready to push?" Decision per commit SHA; the policy floor forbids merge, tag, release and deploy; a GitHub ruleset requires human review and lists no bypass for the App (§6, D15) | #4, #17, #49, #51, #27 | Partly implemented |
| T7 | An approval is granted by mistake, late or for different code | A1, A2, A4 | Approvals fail closed on timeout and when the channel is lost; they are tied to a commit SHA; input is capped and shown as untrusted; the transport is the agent's stdio control protocol, with no network path or token in the guest (§4.2, D26). A sensitive answer needs a fresh passkey assertion with Face ID or a fingerprint, bound to the Decision and its commit SHA (D45) | #17, #49, #57; transport #7, #25, #75; step-up #101 | Implemented and tested: domain logic, the stdio transport (crash and deadline cases verified, spike #7), cut input marked, prompt floods denied, step-up for a review and an egress host (#101); untested on a real phone; a a policy or preset change and revoking the forge tokens are answered on the web only behind a named step-up (#107) |
| T8 | Forge credentials leak | A2, A3 | Installation tokens last about an hour, are scoped to the installed repositories, reach git through a credential helper or the proxy, and never sit in the guest's environment, on disk or in logs (§7.3, D15) | #27, #22 | Planned |
| T9 | Secrets end up in logs, transcripts or notifications | A2, A4 | Redaction at ingest, before events, Decision inputs and audit entries are written; generic notification payloads with no content (§5.4, §9.4) | #22, #31 | Implemented: redaction at ingest in the store, generic ntfy payloads |
| T10 | The agent login is stolen from the environment | A2, A3 | Dedicated auth directory per environment, never `$HOME`; egress allowlist; revocation at the vendor when an environment is deleted (§7.3). A subscription login never touches the host: the human signs in inside the environment, and `whr` never reads, stores or relays it (D40). An API key is the only agent credential on the host: `0600`, single-linked, outside every root, read with `O_NOFOLLOW`, passed through a pipe at `/dev/fd/3`, never a file or a command line. Planned with D48 (not built): in `api-key` mode the key will sit in the sidecar's gateway and the agent will hold a per-run token revoked at run end; residual: spend up to the budgets while the run lives, and a compromised sidecar. | #26, #78, #81, #82 | Accepted risk inside the environment; the host side is implemented and tested, and a subscription token in the configuration is refused (#81); in-environment sign-in is spike #82 |
| T11 | Someone else drives the supervisor | A4 | The web UI on loopback only, reached remotely through a forwarder (`tailscale serve`, or a proxy admitted by `pf` for a router VPN), and the JSON API only on a host-only unix socket (D29); sign-in has no failure lockout, because a passkey assertion cannot be guessed and a lockout only lets whoever reaches the forwarder keep the human out; sign-in ceremonies have their own pool, separate from step-up and enrolment (which need a session or the host token), and a full pool evicts its oldest ceremony instead of refusing a new one; the begin limit is only a CPU guard far above a human's rate. A sustained flood can therefore delay a new passkey sign-in while it lasts; signed-in sessions, step-ups and the host CLI are unaffected. Per-client limits would need an identity the forwarder passes on ({{< status open >}}) (decided in the review of #101, M1); the host CLI always works; the API token on every request from the local CLI, since a forwarder on a non-loopback address is reachable by guests; on the phone and tablet a passkey with user verification instead of a token, revocable per device by the admin, and a session cookie that is HttpOnly, Secure and same-origin; CSRF protection (§7.5, D29, D45) | #24, #30, #33, #69, #101 | The JSON API is served only on a host-only unix socket and the forwarded listener carries the web UI alone (aacc1cd, D29), closing the gap the review of #101 found. API token implemented (#96); web sessions with CSRF, same-site checks and a strict CSP (#30); passkeys implemented, enrolled and revoked only from the host, and once one exists the token no longer signs in to the web UI (#101), untested on a real phone; loopback measured unreachable from guests (#69), forwarder and `pf` unmeasured |
| T12 | The record of what happened is altered | A2, A5 | Append-only audit log outside the workspace, hash-linked to transcript content; purges record themselves (§5.4, §7.7) | #21, #37, #66 | Partly implemented: audit tier, redaction before writing and self-recording purges exist; hash-linking is planned (#37); the guard bypass found in review is open (#66) |
| T13 | An agent exhausts the host or the budget | A2 | CPU and memory limits per environment, admission control, time and cost budgets, `whr kill-all` (§7.4, §8, §5.7) | #37, #48 | Measured limits; budgets planned |
| T14 | A tampered tool or plugin runs in an environment or in the supervisor | A5 | Agent CLIs come from a content-addressed, read-only store verified against the vendor's checksums; plugins run out of process with the same isolation (D19, D20, §7.8); CI actions pinned by SHA | Tool store (#74); plugins later | Implemented for Claude Code (pinned version, https only, size cap, SHA-256 of the stored bytes against the pin and the vendor manifest, read-only store); plugins later |
| T15 | The developer's editor runs planted repository config when opening an agent checkout | A2 | The editor gets a supervisor-owned copy, cloned from the supervisor's repository after a bundle export (§4.5), so it is fresh while the environment runs and never the agent's worktree. A human who opens a workspace's worktree directly does so at their own risk; the console (D43) is the place to work in workspaces | #59, #91; the CLI and UI launch in #24, #30 | Implemented in the service; front ends planned |
| T16 | SSH or IDE access into an environment is abused or left open | A4, A5 | Short-lived per-session certificates, no passwords, reachable only over the VPN; code-server never public; editor extensions treated as a supply-chain risk (§7.6). SSH and `whr console` land in the console environment, never on the host; only the admin logs in to the host (D43). The console holds no credentials, mounts workspaces read-only by default and runs git with hooks and fsmonitor off | #32, #92 | Implemented (SSH CA, `whr ssh`, the console with read-only mounts and the git wrapper; #32, #92), not measured on the real setup |
| T17 | The vendor suspends the developer's account for use outside its terms | A6 | `whr` never handles a subscription credential; one human per supervisor starts every run; forge events only fill the inbox; no scheduled or unattended runs; unmodified vendor binaries; a quota stop pauses with a Decision, no retries or account rotation (D40) | #81, #82, #71 | Decided; the configuration refuses a subscription token (#81); the reading of the terms is ours and {{< status unverified >}} |

## Accepted risks

These are accepted for a single-developer, watched personal tool. Each has a limit, and each is revisited if the tool gets more users.

- **A preview can still leak by navigation** (D33, issue #72). The proxy's CSP stops a preview's script from fetching or loading images from another site, but a link, a top-level navigation or a DNS prefetch hint (which not every browser's CSP controls) can still carry data out past the egress allowlist. Limit: only the human opens a preview, it closes with its environment, its session and its device, and the human sees where it navigates.
- **A preview shares the UI's cookies** (D33, issue #72). Browsers do not isolate cookies by port, so a web app the agent runs, previewed on another port of the same host name, can set or overwrite cookies the UI also sees. Limit: the UI's session and CSRF state are issued and kept on the server and its cookies carry the `__Host-` prefix, so a tossed cookie can sign the human out but cannot fix a session, forge a request or read one; only the human opens a preview, and it closes with its environment. A separate host name for previews would remove this ({{< status open >}}).
- **An image build has the builder's own network** (D38, issue #76). Building an environment from a repository's Dockerfile runs its `RUN` steps in Apple Container's builder VM, which the egress allowlist does not cover; `container build` 1.5.0 has DNS options but no network option ({{< status unverified >}}: from its `--help`), so it cannot be put behind the sidecar. Devcontainer features add third-party `install.sh` scripts that run as root in the same builder (D38, #108, #127): only from `ghcr.io/devcontainers/features/` or a source the human allowed per repository, each pinned by its manifest digest. Limit: only the default branch's committed Dockerfile and context, and those pinned features, are built (never a topic or a working tree), no `--ssh`, `--secret` or credentials reach the build, its arguments pass the reserved-name check, and its only output is an image tag.
- **The agents of one workspace are one trust domain** (D42). They share the agent clone's `.git` and can change each other's worktrees, branches, hooks and config. Limit: a workspace never contains the human's own repository, the host never runs git in it, and nothing leaves it except a verified bundle whose commits are approved per SHA. Agents that must not touch each other belong in separate workspaces.
- **The console runs agent-written repository config** (D43) when the human runs git or tools there. Limit: it is a VM with no host home, secrets, credentials or runtime socket; workspaces are read-only by default; hooks and fsmonitor are off. Spike #89 measured that hooks and fsmonitor stop through `GIT_CONFIG_COUNT`, while filter drivers, textconv and aliases set in a workspace's config run unless overridden by name; the console's git wrapper overrides each one it finds, and a setting it misses runs inside the console VM only.

- **Data can leave through an allowed host.** The sidecar matches on the hostname in `CONNECT`, so `api.anthropic.com` can carry data out, and domain fronting is not stopped. Limit: the allowlist is minimal, every request is logged, and the logs are reviewable.
- **The sidecar is trusted and has full egress.** Limit: it runs only `whr-proxy`, mounted read-only into the configured or built-in base image (never a repository's devcontainer image), with no access to secrets.
- **A subscription login sits inside the environment.** Any process in the guest can read it while a run is active. Limit: the human signs in there through the vendor's own flow and `whr` never sees the credential (D40), one dedicated auth directory per environment, never the host's, and revocation at the vendor when the environment is deleted. That vendor revocation works as expected is {{< status unverified >}}.
- **Host git reads agent work only as a bundle.** Since D42 the host runs no git in a workspace: an agent's commits arrive as a bundle and are fetched into the supervisor's repository with object checks (§4.5). What remains is git's own handling of a crafted bundle and of the commits and trees in it, limited by `fsckObjects`, git's path protections, a size cap and a scratch repository that holds nothing else.
- **The agent sees what it is given.** Repository contents go to the LLM vendor. That is the developer's choice per repository.

## Open items

- Guests reach the host's LAN address and every service on all interfaces (#69): the `pf` rule blocking container subnets and the hardening of macOS's own services are not yet measured. T1 is partly implemented: issues from untrusted authors are held for a Decision before any run (#53); T15's front ends come with #24 and #30.  T7's transport is verified by spike #7, including the crash and deadline cases.
- Links inside a secrets directory are followed one level and the locations found in review are rejected (#58). Mounts can also be limited to the workspace roots workharbor owns, as a second layer behind the deny-list (`CheckMountsWithin`). Accepted: a hard link to a secret inside a project, and links more than one level deep inside a secrets directory.
- Webhook signature verification and the author association used for trust tiers are part of the forge adapter, #27.
- Whether an image build can be confined to an allowlist, for example through a proxy reachable from the builder VM or a later `container build` network option ({{< status open >}}).
- This page is reviewed whenever a D-row changes a boundary, and before release 1.
