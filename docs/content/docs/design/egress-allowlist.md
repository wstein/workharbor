---
title: Host egress allowlist
description: "Design note: a host firewall allowlist for agent users, its limits and its doctor check."
weight: 9
toc: true
---

## Host egress allowlist for agent processes (#361)

{{< status open >}} Design only; no rule is generated or checked yet. It adds a host layer under the egress proxy of §7.2: the proxy limits what an environment reaches, this limits what an agent user's process on the host reaches if it bypasses the proxy.

**Threat addressed.** A host-side process of a dedicated agent account (an account setup would create; none exists today, and `workharbor`, the supervisor's account, is not one) opens connections to any destination. The allowlist limits agent users to known destinations, so exfiltration to an arbitrary server and reaching the LAN or cloud-metadata addresses fail at the host.

**Not covered.** Anything sent to an allowed destination (including a forge or model API account the agent controls), domain fronting, a compromised allowed host, traffic of non-agent users, and the supervisor itself. Guest and VM traffic and the `whr-proxy` sidecar are forwarded or NAT'd, so a uid-scoped `user` or `meta skuid` rule does not match them; the guest path stays with the §7.2 proxy. It filters by destination only: no deep packet inspection and no TLS interception.

### Allowed destinations (one list)

| Role | Destinations | Ports |
| --- | --- | --- |
| Agent, model API | the configured model API host (for example `api.anthropic.com`) | 443 |
| Agent, forge | the configured forge host (git and API) | 443 |
| Agent, package sources | the package registries the supervisor's built-in list names | 443 |

This table is the single documented list; setup renders the rules from the supervisor's configuration and built-in list, never from a repository's requests. Everything else is dropped and logged, including DNS to resolvers other than the host's.

### Hostname resolution

Rules hold IP addresses, so setup resolves each allowed name once when the human reviews the rules and writes the addresses into a named set (`pf` table, `nftables` set). `whr setup` can re-render the set on request; it never refreshes in the background (no daemon). Limits: a name whose addresses change (CDN rotation) breaks until the human re-renders; shared addresses admit every name behind them; a stale set is detected by the doctor only as a mismatch with a fresh resolution, which reports not verified when resolution fails.

### Rule sketch (documentation only)

```text
# pf (macOS), anchor "whr-agents"; <whr_allow> is the rendered table
pass out quick proto tcp from any to <whr_allow> port 443 user { whr-agent1 }
block out log quick user { whr-agent1 }
```

```text
# nftables (Linux), table inet whr_agents; the set holds the rendered addresses
table inet whr_agents {
  set allow4 { type ipv4_addr; elements = { 203.0.113.10 } }
  chain out {
    type filter hook output priority 0; policy accept;
    meta skuid { 1001 } ip daddr @allow4 tcp dport 443 accept
    meta skuid { 1001 } log drop
  }
}
```

The addresses are documentation examples. Loopback and the host's DNS resolver must also be allowed for the agent uid (for example `pass out quick on lo0` and `meta iifname "lo" accept`, and the resolver on port 53), or the sketches drop them. Agent users are listed by the supervisor's configuration.

### Setup and offboard

Setup prints the rules and the exact command to load them; the human reviews and applies them. Setup never loads rules silently. Offboard removes only what setup added (the named anchor or table) and reports anything else it finds without touching it.

### Doctor check

`whr doctor` reads the loaded anchor or table and compares it with the rules rendered from the documented list. Results: verified (loaded and equal), failed (absent or different), not verified (not run as a user that may read the rules, the tool missing, output unparsable or any unknown state). Not verified is never reported as ready.

### Non-goals

No deep packet inspection, no TLS interception (it adds a trust anchor and attack surface), no new daemon, no silent rule changes.
