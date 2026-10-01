# Spike #82: Agent Subscription Sign-in inside an Environment (D40)

This spike investigates how subscription sign-in works inside an isolated Apple Container environment behind an egress allowlist proxy sidecar, verifying decision D40 across Claude Code 2.1.285 and Codex CLI 0.159.2.

## Background & Questions (D40)

D40 establishes that `whr` never handles or stores subscription credentials. The human signs in inside the environment through the vendor's own flow, and the CLI keeps credentials on that environment's agent-home volume (D16).

Acceptance criteria:
1. **Claude Code 2.1.285**: In a hardened container (`--internal` network, egress proxy sidecar), determine which sign-in flows work from an attached terminal (`container exec -it`): `/login` (`claude auth login`), `claude setup-token`, device-code flow. Record required allowlist hosts and credential landing paths.
2. **Persistence across Lifecycle**: Verify that credentials survive container `stop`, `start`, and container rebuild/recreate on the agent-home volume.
3. **Concurrency & Refresh Tokens**: Measure two environments running simultaneously under the same account; record refresh-token rotation and collision behavior.
4. **Phone-First Flow (D35)**: Determine whether sign-in can be completed from a phone without `whr` relaying credentials, or if an attached terminal (`container exec -it` / SSH) is required.
5. **Codex CLI 0.159.2**: Measure the same questions for `codex login` and verify device-code authorization (`--device-auth`).
6. **Committed Evidence**: Reproducible harness, raw unedited output, and documentation.

## Running the Spike

Prerequisites:
- Apple Silicon macOS host with Apple Container (`container` CLI 1.5.0+)
- Go toolchain (for building `whr-proxy` and `whr-shim`)
- GitHub CLI `gh` (for fetching Codex CLI musl release)

```bash
# Build tools and run all tests:
./run-spike.sh
```

Raw outputs are saved in `results/` and summarized in `RESULTS.md`.
