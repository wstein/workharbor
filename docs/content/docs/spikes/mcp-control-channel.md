---
title: "Spike #174: MCP over the control channel"
description: "Whether the supervisor can answer an SDK-type MCP server over Claude Code's stdio control channel, whether an internal guest needs any other path, and which other agents speak MCP."
weight: 10
---

> Source: `spike/mcp-control-channel` (`spikes/mcp-control-channel/`), a local branch until it is pushed. Tracks [#174](https://github.com/wstein/workharbor/issues/174); the design question is [#129](https://github.com/wstein/workharbor/issues/129).
>
> **Status: prepared, not run.** The scripts exist and the driver was checked against a mock agent; nothing below has been measured on Claude Code yet.

## Question

#129 proposes an agent-facing workharbor MCP on exactly one transport: an SDK-type MCP server that the supervisor answers over the agent's existing stdio control channel (the channel of approvals, D26), with no listener and no token in the guest. Whether Claude Code serves SDK MCP tools in headless `stream-json` mode with `--strict-mcp-config` is {{< status unverified >}}.

## What will be measured

| Part | Measurement | Result |
| --- | --- | --- |
| A.1 | `initialize` with `sdkMcpServers` accepted under `--strict-mcp-config`; `tools/list` shows exactly the declared tools | {{< status unverified >}} |
| A.2 | `tools/call` round trip: latency, framing, 1 KB, 64 KB, over the cap | {{< status unverified >}} |
| A.3 | a blocking `ask` answered after 10 s, 5 min, never: does Claude Code time the call out, and when | {{< status unverified >}} |
| A.4 | interleaving with an approval request (D26) | {{< status unverified >}} |
| A.5 | planted `.mcp.json` and user settings stay refused; name collision | {{< status unverified >}} |
| A.6 | run identity comes from the channel, no argument selects it | {{< status unverified >}} |
| A.7 | exec killed or stdin closed mid-call fails closed | {{< status unverified >}} |
| B | vsock and exec from an `--internal` guest, firewall on and off, no new host listener | {{< status unverified >}} |
| C | Codex CLI and Antigravity: MCP over stdio, one supervisor-started server, no repository config | {{< status unverified >}} |

A.1 to A.3 are the gate: if they fail, A.4 to A.7 are not run.

## Setup

The run will use two whtmp-labelled containers on an `--internal` network: the egress probe proxy sidecar and the agent container with Claude Code 2.1.288 (linux-arm64, sha256 to be checked against the signed manifest). Werner will sign in inside the agent container (D40); no token will be written to a file. The driver will play the supervisor and log every wire line.

## Since the plan

The strings `sdkMcpServers`, `mcp_message`, `mcp_set_servers` and `mcp_status` were observed in the 2.1.288 binary during preparation; how (command, binary sha256) was not recorded, so the finding is an observation, not a measurement. If they are there, the control protocol has a surface for this; that shows the names exist, not that headless mode behaves as #129 needs {{< status unverified >}}.
