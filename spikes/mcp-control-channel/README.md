# Spike #174: SDK MCP server over the agent control channel

Measurement scripts and results for [#174](https://github.com/wstein/workharbor/issues/174) (design question in [#129](https://github.com/wstein/workharbor/issues/129)).

Status:
- **Part A (Claude Code):** Initialized and tool-discovery verified (A.1); planted-configuration refusal verified (A.5). Model turns (A.2, A.3, A.4) blocked on upstream weekly quota reset (`resets Oct 6, 9am UTC`).
- **Part B (Reachability):** In-band stdio multiplexing confirmed; guest needs no network listener or port reachability to host.
- **Part C (Other Agents):** Stdio MCP round-trips verified for Codex CLI (`0.160.0`) and Antigravity (`1.2.14`). Copilot CLI (`1.0.50`) evaluated and refused with live refusal log.

## What is in here

| File | Purpose |
| --- | --- |
| `build-tools.sh` | Builds `whr-shim` and `probe` (linux-arm64), downloads Claude Code 2.1.288 and verifies sha256 (`359ab6a058fcde9741dff54979a212fd134cdf8e8cfc2f8de02bc350b9e2b9d5`) |
| `run-env.sh` | Orchestrates temporary whtmp test environment (`up`, `down`, `case <a1..a7>`, `plant`, `reach`, `agents`) |
| `mcpdriver.py` | Host supervisor driver: registers SDK MCP server `wh` on stdio control channel, answers `mcp_message` requests, logs wire traffic |
| `probe-reach.sh` | Part B: host reachability and network isolation probes |
| `probe-agents.sh` | Part C: CLI flags and MCP capabilities of Codex, Antigravity, and Copilot |
| `mock_stdio_server.py`| Minimal stdio JSON-RPC MCP server (`wh_ping`) with wire logging |
| `results/` | Minimal decisive evidence files: wire logs (`a1-wire.jsonl`, `c-codex-wire.jsonl`, `c-agy-wire.jsonl`), CLI transcripts, and marker checks |

All containers, volumes and networks carry `workharbor.temp=true`, `workharbor.lane=wh-verify`, `workharbor.purpose=mcp-control-channel`. No token is written to files; credentials stay in the agent home volume (D40) or host-side.

## Measured Results

### Part A: Claude Code (v2.1.288)

1. **A.1 (Control Protocol Initialize & Tools List): VERIFIED**
   - Headless `stream-json` with `--strict-mcp-config` accepts `sdkMcpServers: ["wh"]` in control `initialize`.
   - Supervisor exchange answers `initialize`, `notifications/initialized`, and `tools/list`.
   - Claude registers tools as `mcp__wh__echo`, `mcp__wh__ask`, `mcp__wh__whoami`.
   - `mcp_status` reports: `{"name": "wh", "status": "connected", "scope": "dynamic", "source": "sdk"}`.
   - Claude declares capabilities: `sdk_mcp_tools_list_changed`, `sdk_mcp_manifests`.
2. **A.5 (Planted Config Refusal): VERIFIED**
   - Planted `/work/.mcp.json` and `/root/.claude/settings.json` are suppressed by `--strict-mcp-config`.
   - `no planted server ran` confirmed by marker check. Only supervisor `wh` server connected.
3. **A.2, A.3, A.4 (Live Model Turns): BLOCKED**
   - Upstream Anthropic subscription hit weekly rate limit (`429: You've hit your weekly limit · resets Oct 6, 9am (UTC)`).
   - Tool registration is verified; model turns remain blocked until quota reset.

### Part C: Other Agents (Exact Versions & Live Round-Trips Measured)

1. **OpenAI Codex CLI (`0.160.0`): VERIFIED**
   - Live end-to-end tool call measured (`results/c-codex-wire.jsonl` and `results/c-codex-roundtrip.txt`).
   - Wire log records: `initialize` (`codex-mcp-client 0.160.0`), `notifications/initialized`, `tools/list` (discovered `wh_ping`), and `tools/call` (`msg="hello-codex"`).
   - Model (`gpt-6.1-sol`) completed turn with output: `wh-pong: hello-codex`.
   - CLI flags: `-c mcp_servers.<name>={...}` overrides configuration cleanly without editing user `config.toml`.
   - App-server protocol (`app-server --listen stdio://`) uses JSON-RPC over stdio (D53).
2. **Google Antigravity (`agy` `1.2.14`): VERIFIED**
   - Live end-to-end tool call measured (`results/c-agy-wire.jsonl` and `results/c-agy-roundtrip.txt`).
   - Wire log records: `initialize` (`antigravity-client v1.0.0`), `notifications/initialized`, `tools/list` (discovered `wh_ping`), and `tools/call` (`msg="hello-antigravity"`).
   - Model completed turn with output: `wh-pong: hello-antigravity`.
   - Configured via standard `mcp_config.json` stdio schema.
   - Automation support verified: non-interactive batch execution with `--dangerously-skip-permissions`.
3. **GitHub Copilot CLI (`1.0.50`): REFUSED**
   - Measured live refusal transcript in `results/c-copilot-refusal.txt` and context in `results/c-copilot-note.txt`.
   - When configured with an external MCP server, Copilot failed closed with:
     ```text
     ! Third-party MCP servers are disabled by your organization's Copilot policy. Only built-in servers are available.
     ! 1 MCP server was blocked by policy: 'wh'
     Error: Access denied by policy settings
     ```
   - Account entitlement (Werner confirmed expired Copilot subscription) and server-side policy gate external supervisor servers, and built-ins bypass supervisor guard. Refused for Workharbor.

## Case to Criterion Status

| Case | Criterion | Status | Evidence |
|---|---|---|---|
| A.1 | `initialize` + `tools/list` under `--strict-mcp-config` | **Verified** | `results/a1-wire.jsonl` |
| A.2 | `tools/call` round-trip latency & framing | **Blocked** | Upstream 429 weekly limit (`resets Oct 6`) |
| A.3 | Blocking `ask` call timeouts | **Blocked** | Waiting on A.2 unblock |
| A.4 | Interleaving with approvals (D26) | **Blocked** | Waiting on A.2 unblock |
| A.5 | Planted `.mcp.json` and settings refused | **Verified** | `results/a5.out`, `results/a5-markers.txt` |
| A.6 | Run identity from channel | **Prepared** | Schema verified without run arg |
| A.7 | Fail-closed on termination | **Prepared** | Scripts in place |
| B | Host reachability / network isolation | **Verified** | `results/b-reach-firewall-unknown.txt` |
| C.1 | Codex CLI stdio MCP round-trip | **Verified** | `results/c-codex-wire.jsonl`, `results/c-codex-roundtrip.txt` |
| C.2 | Antigravity stdio MCP round-trip | **Verified** | `results/c-agy-wire.jsonl`, `results/c-agy-roundtrip.txt` |
| C.3 | Copilot CLI MCP refusal | **Verified** | `results/c-copilot-refusal.txt`, `results/c-copilot-note.txt` |
