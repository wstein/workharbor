---
title: Security notes
description: What you accept by running coding agents under workharbor, and how to limit it.
weight: 2
toc: true
---

The short version of the [threat model](../threat-model.md) for whoever runs workharbor. Agents are treated as untrusted: they read text strangers wrote, and they follow it.

## What workharbor is built to enforce

These are the design's guarantees. `whr serve` and the commands exist but have not run against a release, so none of this is {{< status verified >}} on a real setup unless it says so. Each item says how far it is built; the [design's status table](../design/_index.md) and the threat model track the rest.

- **Agents never push, merge, tag, release or deploy.** They commit in their own checkout. workharbor pushes a branch only after you approve its exact commit in a "Ready to push?" decision, and a GitHub ruleset makes a human review the pull request (D15, D18). *Policy table, the push flow and its per-commit approval implemented; the check against a real GitHub ruleset and the GitHub client come with issue #27.*
- **Approvals fail closed.** A request nobody answers in time, an approval for different code, or a lost connection to the agent ends in a denial (design §4.2). *Implemented in the domain; the agent's approval channel is being verified (issue #7).*
- **Each agent runs in its own lightweight VM** on its own internal network. It reaches the model provider only through a proxy that allows listed host names on ports 443 and 80 and refuses private and local addresses, and it cannot reach other LAN devices or other agents (design §7.2). It **can** reach your Mac's own services that listen on the network, even from its isolated network, so workharbor listens on loopback only and you turn off or harden the rest ([host setup, step 8](host-setup.md#8-firewall-and-ssh)). *The runtime adapter and the proxy are implemented and pass their conformance suite on Apple Container (issues #26, #78); the `pf` rules that keep guests off your Mac's services are still being measured (issue #69).*
- **Your home directory, SSH keys and secrets folders are never mounted** into an agent environment, and git on the host never runs commands an agent planted in its checkout (design §4.4, §4.5). *Mount checks and hardened host git implemented.*
- **`whr` never handles your subscription login.** You sign in inside each environment through the vendor's own flow, only you start runs, and a run that hits its quota pauses and asks you (design D40, [vendor terms](vendor-terms.md)). *Decided; the configuration refuses a subscription token, and how sign-in works inside an environment is being measured (issue #82).*

## What you accept

- **The agent's login sits inside its environment.** With a subscription login (Claude or ChatGPT sign-in) you sign in inside the environment, and `whr` never sees the credential ([vendor terms](vendor-terms.md)). Any process in that environment can read it while a run is active. Delete environments you no longer use, and revoke the login at the vendor if one is ever exposed.
- **Data can leave through the model provider.** The proxy allows the provider's API host, so an agent could send repository contents there. That is also how agents work: send only repositories you are willing to share with the provider.
- **The proxy matches host names.** It does not stop domain fronting through an allowed host.

## Your part

- **Revoke a token that was exposed**, at once: a token pasted into a chat, a terminal or a log is exposed. Agents and scripts pass tokens only through a `0600` env file, never on a command line.
- **Review before you approve.** "Ready to push?" shows the commits and the diff: approve only what you have read.
- **Treat issue text from strangers as untrusted.** workharbor holds runs on issues from unknown authors for your decision (issue #53).
- **Keep workharbor off the open network**: Tailscale or your router's VPN, never a port forward to workharbor itself ([host setup, step 7](host-setup.md#7-reach-it-from-your-phone)).
- **Encrypt an external SSD** on its own, because FileVault does not cover it, and **keep `whr`'s session logged in** (no automatic log-out), because the supervisor and the container system run in it ([host setup, step 3](host-setup.md#3-filevault-and-restarts)).

## Who can reach it

- **The JSON API is on a host-only unix socket**, `api.sock` in `whr`'s state directory (directory `0700`, socket `0600`), reached only by the CLI as the `whr` user. **The forwarder carries the web UI alone**, and the forwarded listener serves no `/v1` route, so a leaked API token cannot answer a review, allow an egress host or enrol a passkey from the phone network (D29, [design §7.5](../design/security.md#7-security)). *Implemented; the forwarder and the `pf` rules are not measured yet (issue #69).*
- **The web UI listens on loopback only.** No guest reaches a loopback listener (measured, issue #69).

## Passkeys

With a passkey enrolled, the web UI signs in with it; the API token no longer signs in to the web UI (it stays for the CLI) (D45, [design §7.5](../design/security.md#7-security), [Run the supervisor](run-the-supervisor.md#passkeys-provisional)).

- **Enrol and revoke only on the host**, with `whr passkey add` and `whr passkey rm`. The web UI has no route that adds or removes one.
- **A fresh passkey assertion is needed** for a "Ready to push?" review, an egress host, a workflow change (preset or integration branch) and revoking the forge tokens. It names exactly what you approve, including the commit.
- **There is no lockout after failed sign-ins**: a passkey assertion cannot be guessed, and a lockout would only let whoever reaches the forwarder keep you out. A flood can delay a new sign-in while it lasts; signed-in sessions, step-ups and the host CLI are unaffected.

*Implemented and tested without a real phone ({{< status unverified >}}).*

## Previews

A preview shows agent-written code in your browser ([design D33](../design/decisions.md)). Each has an origin of its own and a CSP set by the proxy, needs its own token, forwards only to the one declared port and closes with its environment and its session.

- **A preview can still leak by navigation.** The CSP stops its script from fetching or loading images from another site, but a link, a top-level navigation or a prefetch hint can carry data out past the egress allowlist. This is an accepted risk ([threat model](../threat-model.md)): only you open a preview, so look at where it navigates, and open one only for work you can read.
- **A preview shares the UI's cookies**, because browsers do not isolate cookies by port. It can sign you out of the UI but not take a session over.

## If your phone is lost

1. On the host, as `whr`: `whr passkey ls`, then `whr passkey rm <id>`. This revokes the passkey and ends its sessions.
2. In the web UI, from another device, open `/devices` and sign out any session that is still listed.
3. Enrol a new passkey on the host with `whr passkey add`; recovery is always you at the host.
4. If the phone also held an agent's subscription login, revoke it at the vendor ([vendor terms](vendor-terms.md)); `whr kill-all` cannot do that.
