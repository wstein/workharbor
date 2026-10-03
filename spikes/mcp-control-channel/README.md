# Spike #174: SDK MCP server over the agent control channel

Throwaway measurement scripts for [#174](https://github.com/wstein/workharbor/issues/174) (design question in #129). Status: **prepared, not yet run against a signed-in session.** Nothing here was measured on Claude Code yet; the only run so far is the driver against `mock_claude.py`, which checks the driver's own logic and proves nothing about Claude Code.

## What is in here

| File | Purpose |
| --- | --- |
| `build-tools.sh` | builds `whr-shim` and `probe` (linux-arm64), downloads Claude Code 2.1.288 and checks its sha256 against the signed manifest |
| `run-env.sh` | `up` / `down` / `case <a1..a7>` / `plant` / `reach` / `agents`; `DRY_RUN=1` prints the container commands only |
| `mcpdriver.py` | plays the supervisor: declares one SDK MCP server `wh` in the control `initialize`, answers `mcp_message` requests, logs every wire line with a ms offset |
| `probe-reach.sh` | Part B: exec latency, vsock, negative controls, host listeners before and after |
| `probe-agents.sh` | Part C: MCP flags of Codex CLI and Antigravity from `--help` (needs binaries, no login) |
| `mock_claude.py` | stand-in agent for self-testing the driver without a container or login |

All containers, volumes and the network are named `whtmp-mcp-*` and carry `workharbor.temp=true`, `workharbor.lane=wh-verify`, `workharbor.purpose=mcp-control-channel`. `run-env.sh down` removes them; `make temp-clean LANE=wh-verify` does too. No token is written anywhere: the login lives on the `whtmp-mcp-ahome` volume (D40) and goes with it.

## Assumed protocol (unverified)

From strings in the 2.1.288 binary, not from a run: `initialize` takes `sdkMcpServers: [names]`; the CLI then sends control requests `{subtype: "mcp_message", server_name, message: <JSON-RPC>}` and expects `{mcp_response: <JSON-RPC>}` back; `mcp_status` reports the servers. The driver records the real shapes in `results/*-wire.jsonl`.

## Run order (on the reference Mac mini, with Apple Container running)

1. `./run-env.sh up` (starts the proxy sidecar and the agent container, prints the sign-in steps).
2. Werner signs in inside the container (see the printed commands). Before sign-in, `./run-env.sh case a1` may already answer A.1 if the CLI connects at `initialize`.
3. `./run-env.sh case a1`, `a2`, `a3`: stop if they fail (A.1 to A.3 are the gate). `a3` waits 10 s, 300 s and up to `NEVER_CAP` (900 s) for "never".
4. `a4` to `a7`. `a5` plants a `.mcp.json`, user settings and a colliding server `wh`, then checks marker files; `a7` kills the exec and closes stdin mid-call, then lists guest processes.
5. B: `FIREWALL=off ./run-env.sh reach`, Werner turns the Application Firewall on, `FIREWALL=on ./run-env.sh reach`.
6. C: `CODEX_BIN=... AGY_BIN=... ./run-env.sh agents`.
7. `./run-env.sh down`; commit `results/` unedited to this branch.

## Case to criterion

A.1 `a1`; A.2 `a2` (1 KB, 64 KB, 1 MB, 10 MB; the cap is whatever the model sees); A.3 `a3`; A.4 `a4`; A.5 `a5`; A.6 `a6` (the tool schema has no run argument, identity comes from the channel); A.7 `a7`.
