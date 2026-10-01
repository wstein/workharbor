# Spike #84: Antigravity in Container Results

Measured on macOS (Apple Silicon host) with Apple Container (`fedora:latest` linux-arm64 guest)
and `whr-proxy` sidecar egress allowlist proxy.

## Measurement Matrix

| Case | Topic | Measurement / Finding | Verdict |
| :--- | :--- | :--- | :--- |
| **Case 1** | Unauthenticated Run | Detects missing auth in 4.52 s, prints interactive OAuth URL and console code prompt | **PASS** |
| **Case 2** | Auth Discovery | Supports `AUTH_MODE_PERSONAL_CONSUMER` and `AUTH_MODE_GEMINI_API_KEY`; composite keyring/file storage | **PASS** |
| **Case 3** | Event Shapes | `stream-json` emits `init`, `step_update`, `result`; registers 58 tools (2 step events) | **PASS** |
| **Case 4** | Egress Control | `whr-proxy` permits Google APIs; blocks unallowed domains (403 (example.com -> 403 Forbidden)) and raw IPs | **PASS** |
| **Case 5** | Cancellation via `whr-shim` | Process group killed in 319.67 ms; 0 orphan processes verified via `/proc` | **PASS** |
| **Case 6** | Session Resume | `--conversation <id>` preserves context (16,303 prompt cache read tokens); multi-turn NDJSON | **PASS** |
| **Case 7** | Error & Quota Signaling | Structured `AGY_ERROR: {...}` JSON emitted on stderr; canonical exit code 3 | **PASS** |

## Contract Fit Summary (§5.2)

1. **Protocol / Stream Shape**: Antigravity print mode natively implements `--output-format stream-json` with typed NDJSON events (`init`, `step_update`, `result`). Token accounting includes `input_tokens`, `output_tokens`, `thinking_tokens`, and `cache_read_tokens`.
2. **Interactive Multi-turn**: Supports `--input-format stream-json` reading NDJSON messages from stdin, running one turn per message.
3. **Session Resume**: Resumes existing sessions via `--conversation <id>` or `--continue`.
4. **Tool Compatibility**: Exposes 58 agent tools (`run_command`, `replace_file_content`, `multi_replace_file_content`, `view_file`, `grep_search`, `browser_subagent`).
5. **Process Group Management**: Fully compatible with `whr-shim run` and `whr-shim kill`. Leaves zero orphans upon supervisor cancellation.
6. **Error Signaling**: Exits with code `3` and emits structured JSON `AGY_ERROR` on stderr for API, model, and quota failures.
