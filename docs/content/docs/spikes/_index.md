---
title: Spikes
description: "The measured results behind the design: what each spike ran, on what, and what it showed."
weight: 5
---

Each spike measured one question on the Mac mini (Apple silicon, macOS 26.6.2; Apple `container` 1.5.0 where a container was involved) and wrote its results, scripts and raw output on a `spike/*` branch. They are published here as recorded; where a later measurement or decision changed a conclusion, a note at the top of the page says so, and the [design](../design/_index.md) is authoritative.

| Spike | Question | Issue |
| --- | --- | --- |
| [Agent contract](agent-contract.md) | Claude Code, Codex CLI and Antigravity as headless agents: events, injection, approvals, resume and quota. | [#1](https://github.com/wstein/workharbor/issues/1) |
| [Apple Container](apple-container.md) | Lifecycle, storage, isolation, egress, an agent inside a container, recovery and the tool store on Apple Container 1.5.0. | [#2](https://github.com/wstein/workharbor/issues/2) |
| [Approvals and cancel](agent-approval.md) | Routing Claude Code's permission prompts over the stdio control protocol from a container, fail-closed cases, and cancel and resume mid-turn. | [#7](https://github.com/wstein/workharbor/issues/7), [#10](https://github.com/wstein/workharbor/issues/10) |
| [Planted Claude Code config](claude-config.md) | Which hooks, MCP servers, skills and settings planted in a repository or the agent home take effect. | [#68](https://github.com/wstein/workharbor/issues/68) |
| [Host reachability](host-reachability.md) | What a guest on Apple Container can reach on the host's addresses, `--internal` networks included. | [#69](https://github.com/wstein/workharbor/issues/69) |
| [Bind mount versus volume](bind-mount-perf.md) | `git status`, copy and build times for a `node_modules`-style tree and a 153 000-file repository on a bind mount and on a volume. | [#40](https://github.com/wstein/workharbor/issues/40) |
| [A volume over a bind mount](volume-over-bind.md) | Whether a volume mounted over a subdirectory of a bind-mounted checkout works, and what its mount point does in the guest. | [#80](https://github.com/wstein/workharbor/issues/80) |
| [Workspaces, bundles and the console](workspaces.md) | Streaming a git bundle out of a running environment, one agent against another in a clone, how far `GIT_CONFIG_COUNT` protects a console, and Fedora and Ubuntu LTS bases. | [#89](https://github.com/wstein/workharbor/issues/89) |
