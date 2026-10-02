---
title: Security
description: "Policy and autonomy, and the security rules: isolation, egress, credentials, host git and access."
weight: 4
toc: true
---

## 6. Policy and autonomy

Autonomy is a per-repo/per-task policy table: **action → `auto | ask | forbid`**.

| Action | Default |
| --- | --- |
| Commit in the topic's own checkout | auto |
| Push an `agent/*` branch | **after cleanup**: the supervisor pushes the prepared branch once you approve it (§4.5); the agent never pushes |
| Open/update PR, comment on issue | auto, after the push |
| Write the project board (a task's card status, session and link; the supervisor's own action, never an agent's, D30) | auto |
| Merge, tag, release, deploy | **forbid** for the agent; human-gated |
| Sensitive actions triggered by untrusted input | ask |

Two limits hold whatever a repository's table says (issue #51). Merge, tag, release and deploy are `forbid`, and an agent push is at most `ask`: the supervisor pushes only after approval (§4.5), so an override can tighten these but never loosen them. A mode other than `auto`, `ask` or `forbid`, and an action the table does not list, is `forbid`.

**Starting a run is not an agent action** and is not in the table: only the human starts a run (`whr run`, the UI, or accepting a card's Decision), and a forge event or webhook only adds to the inbox (D40). No setting turns an event into a start.

Enforcement is outside the agent: forge branch protection, required human review, and a bot identity that cannot bypass them. Approval is per commit SHA (ties to ReviewCandidate). Every approval is a Decision record.

**Workflow presets (D47, issue #105).** Each repository names one in the supervisor's configuration (`repositories[].workflow`); the repository never chooses it. A preset sets the rows above, within the two limits, and the forge flow around them:

| | `prototype` | `integration` (default) | `published` |
| --- | --- | --- | --- |
| Approved commits go to | the integration branch, fast-forwarded to the approved SHA; no PR | a PR into the integration branch (`develop`) | a PR into the default branch |
| Approval | "Ready to push?" per head SHA; one Decision may cover a topic's series | per SHA, plus the forge's review | per SHA, the forge's review and required checks |
| Promotion to `main` | the human | the human merges and promotes | not applicable |
| Agent permission mode | `dontAsk` with the allowlist | `dontAsk` with the allowlist | `manual` (host approvals, §4.2) |
| Egress requests (§4.2) | asked once per repository | asked once per repository | asked again when their source changes; lockfile suggestions ignored |
| Ruleset `whr doctor` expects | force push blocked on the integration branch | the integration branch protected, PR required | the default branch requires review, checks and signed commits; no bypass actor |

Three rules close the gaps between the presets and the rest of the design:

- **The supervisor never moves the default branch**, in any preset. `prototype` needs an explicitly configured `integration_branch` that is not the default branch; without one the configuration is refused. Promotion to the default branch is always the human's.
- **The environment comes from the default branch only** (D38): the devcontainer file, the Dockerfile, `postCreateCommand` and the egress requests are read at the default branch's commit, never at the integration branch's, so an approved agent commit on the integration branch cannot configure the agent's next environment.
- **A task never runs looser than the repository.** A task keeps the branch it started with, and publishes under the stricter of its own preset and the repository's current one; changing `integration_branch` is a policy change like changing the preset (refused at start until accepted, and audited).

The fast-forward of `prototype` is never forced: a branch that moved is refused and the topic is rebased (§4.2). It is the human's approval carried out by the supervisor, so the floor holds. Changing a preset is a policy change: it is audited, it needs a passkey step-up when confirmed on the web (`/changes`, #107), and a looser preset never applies to a run already started.

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

The rules below are the security requirements. The [threat model](../threat-model.md) says what each defends against, where it is enforced and tested, and which risks are accepted (issue #11).

1. **Untrusted input.** Issue text, PR comments and CI logs are untrusted. Use trust tiers by author (owner vs external); hold or flag runs on issues from unknown authors. A run that combines private data, untrusted input and outbound network requires approval.
2. **Default-deny egress** through a logging allowlist proxy: forge, package registries, LLM API only. Block LAN, host, other workspaces and cloud-metadata addresses.

    Verified on Apple Container in spike #2 (issue #2). The default network gives none of this: a guest reaches the internet, the LAN, other containers and any host service bound to all interfaces. What works:

    - **One `--internal` network per environment.** It blocks the internet, DNS (names do not resolve), other LAN devices, IPv6 and containers on other networks. It does **not** block the host: issue #69 measured that an internal guest reaches host listeners bound to the Mac's LAN address or to all interfaces, through the network's gateway; spike #2's earlier "blocks the host through every address" was wrong. Listeners bound only to loopback stayed unreachable. Agents on the same internal network can reach each other, so a network is never shared between tasks.
    - **The proxy runs in a sidecar container**, attached to the default and the internal network (`--network` repeats). The host cannot serve an internal network because it gets no interface on it, so a host-side proxy cannot bind to its gateway.
    - **The LAN is refused over IPv6 too.** Besides the private and reserved ranges, the proxy refuses every global IPv6 prefix assigned to the host's own interfaces, read when the sidecar starts and passed to it, so an allowlisted name whose DNS points at a LAN address over IPv6 is refused like one over IPv4 (issue #124).
    - **An entry matches exactly one host name.** `github.com` admits `github.com`, not `api.github.com`. A wildcard `*.example.com` (every subdomain, not the bare name) is allowed only in the supervisor's own configuration and its built-in list, never through a repository's request: an egress request from `devcontainer.json` or a lockfile is one exact host, a request containing `*` is refused, and the Decision the human answers names exactly that host (§4.2). Approving `github.io` therefore never opens every user's `*.github.io`.
    - **Allowlist by hostname, with the name resolved by the proxy.** Allowed hosts returned 200, denied hosts and a raw-IP CONNECT got 403, and every decision was logged with time, verdict, method, host and source. The guest needs no DNS, which closes DNS exfiltration. The proxy allows only port 443 for CONNECT and port 80 for plain HTTP, refuses a name if any of its addresses is loopback, private, link-local, CGNAT, multicast or otherwise reserved, and dials the address it checked, so a rebinding DNS answer is never used (#78). A tunnel closes after five idle minutes, and the sidecar runs with one CPU and 256 MB. Limits: the match is on the name in CONNECT, so it does not defeat domain fronting, and the sidecar has full egress and is trusted.
    - **Image builds are outside the allowlist.** Building an environment from a repository's Dockerfile (D38) runs in the builder VM with its own network; `container build` 1.5.0 offers no network option. Only the default branch is built, with no credentials, so this is an accepted risk in the threat model.
    - **Minimal allowlist for Claude Code:** `api.anthropic.com` alone. In an authenticated run inside a container the proxy also saw a telemetry host (`http-intake.logs.us5.datadoghq.com`) and denied it; nothing broke. Installing needs `claude.ai` and `downloads.claude.ai`, which the tool store (§5.6) removes. A client that obeys proxy variables, such as `curl`, tests the proxy and not the network; test the direct path with the proxy variables ignored.
    ```mermaid
    flowchart LR
        phone["Phone or tablet"] -->|"tailnet, API token"| fwd
        subgraph host["Mac mini"]
            fwd["tailscale serve"] -->|"loopback"| whr["whr serve on 127.0.0.1"]
            pf["pf: container subnets blocked from the host's addresses"]
        end
        subgraph internal["--internal network, one per environment"]
            env["Environment: agent, no DNS"]
        end
        whr -->|"container exec, stdio"| env
        env -->|"HTTPS_PROXY"| sidecar["Egress sidecar: whr-proxy, on both networks"]
        sidecar -->|"allowlisted names, ports 443 and 80, public addresses"| net["Forge, LLM API, registries"]
        env -.->|"no route"| lan["Internet, LAN, other environments"]
        env -.->|"loopback not reachable"| whr
    ```

3. **Credentials.** Run-scoped, short-lived, single-repo, non-extractable. GitHub App installation tokens (~1 h); per-repo bot tokens or deploy keys for Gitea/Forgejo/GitLab. Inject through a git credential helper or host-side proxy so raw tokens never reach env vars, disk or logs. In `api-key` mode the LLM API key stays in the sidecar's model-API gateway and the agent holds only a per-run token, revoked at run end (D48; release 1 still hands the key to the agent's process, through a pipe at `/dev/fd/3`, never a file or a command line). In `subscription` mode the consumer-plan login lives inside the environment (§5.2), is long-lived and not scoped to a repo, and leaks if the agent is compromised. The human signs in there through the vendor's own flow; `whr` never reads, stores, relays or logs that credential (D40). **Accepted risk** for a single-developer, watched personal tool; limit it with a dedicated auth directory per environment (never `$HOME`), the egress allowlist, and revocation at the vendor when an environment is deleted. Revoke run-scoped credentials at run end. Redact secrets at ingest, before anything is stored (§5.4). Agent and CI credentials are separate.
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
    - **Agent-writable repositories are hostile input to the host** (spike #2, item 9). A pre-commit hook and a `core.fsmonitor` command planted from inside a guest ran on the host when the host later ran plain `git commit` and `git status`. The host therefore never runs git in an agent-writable tree (D42): an agent's commits leave as a verified bundle streamed from the environment, and cleanup and push run through `hostgit` (isolated configuration, hooks and fsmonitor off) on the supervisor's own repository (§4.5).
    - **Never mount the human's own repository's `.git` into an environment.** A shared `.git` exposes every branch, the shared hooks and config and the other worktrees' metadata. Agents work in a workspace's own agent clone (D42); its agents share that `.git` as one trust domain, and the host reads their commits only as a verified bundle (§4.5), never by running git in the workspace.
4a. **Images the supervisor builds are never pulled.** Every image `whr` builds (environments, the base image, the console) is named under the reserved registry host `whr.invalid/`, which no registry answers (RFC 6761), never under a bare name that normalises to `docker.io`; creating an environment from such an image uses the local copy only, and a missing one is built, never fetched. A name an attacker could publish on a public registry would otherwise supply the agent's or the console's image on a local miss (review of #108, {{< status unverified >}} whether `container` pulls on a miss: wh/verify).
5. **Supervisor identity.** Sign-in with a passkey (or, before one is enrolled, the API token on the host's own browser), short-lived sessions, scoped revocable CLI tokens, CSRF protection, the web UI bound to loopback only and reached remotely through a forwarder, never on all interfaces, and the JSON API on a host-only unix socket that no forwarder carries (D29), forge tokens encrypted at rest, webhook signature verification. Link accounts by provider instance + stable user ID, never by email. **On the phone and tablet the human signs in with a passkey that requires user verification, and a sensitive Decision ("Ready to push?", an egress host, a policy change, a secret operation) needs a fresh assertion whose challenge names the Decision and its commit SHA (D45)**; the static API token is for the local CLI only, there is no TOTP, and the admin on the host enrolls and revokes passkeys. **Two listeners (D29, D45):** the JSON API and its token exist only on the host's unix socket; the forwarded loopback listener carries the web UI alone, where every sensitive answer needs a passkey step-up. A request that reaches `/v1` through the forwarder is impossible by construction, not refused by a header check. **Passkey rules (D45, issue #101):** a web session alone never answers a review or an egress host; a step-up assertion names the Decision and its commit SHA or host, is single-use, lives two minutes on the server, is bound to the session, and must still match the Decision as it is when it is checked; passkeys are enrolled and revoked only from the host CLI (`whr passkey`), never from the web UI; once a passkey exists, the API token no longer signs in to the web UI. Sign-in ceremonies live in their own pool that evicts its oldest entry when full, apart from step-up and enrolment ceremonies, and there is no failure lockout: an unauthenticated flood can delay a new sign-in for as long as it lasts (the begin guard still refuses when full), but signed-in sessions, step-ups and the host CLI are unaffected. **HTTPS is decided by the configured `public_url`**, never by a request header such as `X-Forwarded-Proto`: a request counts as HTTPS only over TLS or when it arrives for the `public_url` host, and then the UI sets `Secure`, `__Host-` cookies; plain loopback HTTP stays plain. **Previews (D33, issue #72):** the preview proxy replaces the app's Content-Security-Policy with `default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'`: every fetch directive stays at `'self'`, so a preview's script cannot send data to another site by fetch, image or form, while inline scripts and `eval` stay allowed because dev servers need them and they open no path out; a preview opened from the web closes when the last web session that owns it ends (sign-out, revoke or expiry); a preview opened from the host CLI is the host's and stays until its environment closes. A policy or preset change confirms in the web UI only with a step-up that names the change; the supervisor keeps the recorded workflow until then and applies the change at its next start. The one secret operation on the web is revoking the forge tokens, with a step-up that names it; no secret value is ever entered, shown or stored through the web UI (issue #107).
6. **SSH/IDE access.** Short-lived per-session SSH certificates or keys, no password auth, jump host only over VPN, code-server never public and always authenticated, treat Open VSX extensions as supply-chain risk. SSH and `whr console` land in the console environment (D43), never on the host; logging in to the host is for the admin. The console mounts workspaces read-only by default, holds no credentials, and runs git with hooks and fsmonitor off.
7. **Audit and kill switch.** Tamper-evident append-only log stored outside the workspace, linked to commit SHA. `whr kill-all` stops all runs and revokes tokens. Alert on anomalous egress or token spikes. Bot commits are signed with the bot key before push (§4.5).
8. **Plugins.** A plugin handles sessions, credentials and workspace access, so it is a supply-chain risk. Default deny: plugins are installed only by explicit developer action, from a pinned version or hash, and run out of process with the same isolation as any agent environment. They never receive host credentials, `$HOME`, `~/.ssh` or runtime sockets; they get only the per-run credentials a built-in adapter would. Their capabilities are checked by the conformance suite, and every plugin action appears in the audit log.

Separate identities: login identity, connected forge accounts, agent (bot) identity, supervisor sessions.
