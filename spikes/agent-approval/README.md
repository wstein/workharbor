# Agent Approval Routing Spike

Reproducible measurement suite for [Issue #7](https://github.com/wstein/workharbor/issues/7) and [Issue #10](https://github.com/wstein/workharbor/issues/10). Throwaway spike code; not shipping code.

Findings are documented in [RESULTS.md](RESULTS.md); raw streams and timing results are in `results/`.

## Purpose

When an agent runs in an isolated guest environment with an `--internal` network (D18), the guest has no direct route to the supervisor host. This spike measures bidirectional approval routing using Claude Code's stdin control protocol (`container exec` with stdin pipe), evaluates failure modes (closed stdin, supervisor crash, timeout/deadline, request ID validation), and measures mid-turn process cancellation and session resume (#10 leftovers).

## Safety

- Every container, network, and volume created is prefixed with `whspike-*`.
- Only objects named `whspike-*` are ever inspected or removed (`cleanup` in `lib.sh`).
- Never runs `container rm --all` or `container system stop`.
- Egress is strictly confined to `api.anthropic.com` via the static `probe` proxy sidecar on an isolated internal network (`whspike-net`).

## Prerequisites

- macOS with Apple Container 1.5.0 (`container` CLI)
- Go toolchain (to build `whr-shim` and `probe`)
- Python 3.9+
- A 0600 env file containing `CLAUDE_CODE_OAUTH_TOKEN` (passed via `TOKEN_FILE`)
- Guest container base image: `docker.io/library/fedora:latest`

## Setup & Running

Build the necessary static binaries (`whr-shim`, `probe`) and download Claude Code 2.1.285:

```bash
cd spikes/agent-approval
./build-tools.sh
```

Run the measurement suite using a 0600 env file:

```bash
TOKEN_FILE=/path/to/token.env ./run-spike.sh
```

Or run individual test cases directly via `driver.py`:

```bash
AGENT_CONTAINER="whspike-ag1" TOOLS_DIR="/tools" TOKEN_FILE="token.env" RESULTS_DIR="results" python3 driver.py case1
```

Test cases implemented in `driver.py`:
- `case1`: Approved tool call roundtrip (`allow`)
- `case2`: Denied tool call roundtrip (`deny`)
- `case3`: Closed supervisor stdin / EOF handling
- `case4`: Supervisor crash (`kill -9` on host `container exec` client) and recovery via `whr-shim`
- `case5`: Unanswered approval deadline / timeout
- `case6`: Request ID mismatch and validation
- `case7`: Leftover #10 - Mid-tool-call cancellation with `whr-shim` and session resume inspection
- `all`: Run all 7 cases sequentially
