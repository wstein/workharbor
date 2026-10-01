---
title: Security notes
description: What you accept by running coding agents under workharbor, and how to limit it.
weight: 2
toc: true
---

The short version of the [threat model](../threat-model.md) for whoever runs workharbor. Agents are treated as untrusted: they read text strangers wrote, and they follow it.

## What workharbor is built to enforce

workharbor has no runnable service yet: these are the design's guarantees. Each says whether it is implemented; the [design's status table](../design/_index.md) and the threat model track the rest.

- **Agents never push, merge, tag, release or deploy.** They commit in their own checkout. workharbor pushes a branch only after you approve its exact commit in a "Ready to push?" decision, and a GitHub ruleset makes a human review the pull request (D15, D18). *Policy table implemented; the push flow and the ruleset check come with issue #27.*
- **Approvals fail closed.** A request nobody answers in time, an approval for different code, or a lost connection to the agent ends in a denial (design §4.2). *Implemented in the domain; the agent's approval channel is being verified (issue #7).*
- **Each agent runs in its own lightweight VM** on its own internal network. It reaches the model provider only through a proxy that allows listed hosts, and it cannot reach other LAN devices or other agents (design §7.2). It **can** reach your Mac's own services that listen on the network, even from its isolated network, so workharbor listens on loopback only and you turn off or harden the rest ([host setup, step 8](host-setup.md#8-firewall-and-ssh)). *Measured in spike #2 and issue #69; the runtime adapter comes with issue #26.*
- **Your home directory, SSH keys and secrets folders are never mounted** into an agent environment, and git on the host never runs commands an agent planted in its checkout (design §4.4, §4.5). *Mount checks and hardened host git implemented.*

## What you accept

- **The agent's login sits inside its environment.** With a subscription login (Claude or ChatGPT sign-in), any process in that environment can read it while a run is active. Delete environments you no longer use, and revoke the login at the vendor if one is ever exposed.
- **Data can leave through the model provider.** The proxy allows the provider's API host, so an agent could send repository contents there. That is also how agents work: send only repositories you are willing to share with the provider.
- **The proxy matches host names.** It does not stop domain fronting through an allowed host.

## Your part

- **Revoke a token that was exposed**, at once: a token pasted into a chat, a terminal or a log is exposed. Agents and scripts pass tokens only through a `0600` env file, never on a command line.
- **Review before you approve.** "Ready to push?" shows the commits and the diff: approve only what you have read.
- **Treat issue text from strangers as untrusted.** workharbor holds runs on issues from unknown authors for your decision (issue #53).
- **Keep the API off the open network**: Tailscale or your router's VPN, never a port forward to workharbor itself ([host setup, step 7](host-setup.md#7-reach-it-from-your-phone)).
