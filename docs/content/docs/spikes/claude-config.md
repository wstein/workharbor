---
title: "Spike #68: planted Claude Code config"
description: "Which hooks, MCP servers, skills and settings planted in a repository or the agent home take effect."
weight: 4
---

> Source: [`spike/claude-config`](https://github.com/wstein/workharbor/tree/spike/claude-config/spikes/claude-config) at `a89724c7d99c7957ed98f660044aec34874a1af1`, with the scripts and raw output next to the results. Tracks [#68](https://github.com/wstein/workharbor/issues/68). Published as recorded on 1 October 2026.

Issue #68, design §5.2. Run on 2026-10-01 with Claude Code 2.1.285 on macOS 26.6.2. `run.sh` is the experiment and `run.out` its unedited output. The agent is not logged in (an empty `CLAUDE_CONFIG_DIR`), so no model call is made: the hooks and MCP servers start before the first request.

Planted: a `SessionStart` hook in the repository's `.claude/settings.json` and `.claude/settings.local.json` and in the agent home's `settings.json` (each also allows `Bash(*)`), a project `.mcp.json` server, and a project skill with `allowed-tools: Bash`. A hook or server that ran leaves a canary file.

| Flags | Took effect |
| --- | --- |
| none | project hook, local hook, user (agent home) hook, MCP server, skill |
| `--setting-sources user` (the adapter until #68) | **the agent home's hook** |
| `--setting-sources ""` | nothing |
| `--setting-sources "" --strict-mcp-config` | nothing |
| `--setting-sources "" --settings <file> --strict-mcp-config` | only the supervisor's own hook |
| `--setting-sources "" --settings '<json>' --strict-mcp-config --disable-slash-commands` | only the supervisor's own hook |

## What it shows

- Pinning to `user` keeps the repository out but not the agent: the agent can write its home, so a hook or permission rule planted there runs on the next start and resume.
- An empty source list loads no settings, hooks, project MCP servers or project skills at all, and the supervisor's own `--settings` (a file or an inline JSON string) still applies.
- With sources `user`, the project MCP server and skill did not load either; only the settings source decides.

## Not shown

- That a planted **permission rule** is ignored too. The same loader reads hooks and permissions, so it is inferred, but a rule's effect needs a model call that wanted the tool.
- Behaviour after a login (the credentials come from the agent home or the keychain; this run had none), `--bare`, and `--restricted` (which removes Bash and so cannot be used for a coding agent).
