---
title: "Spike #84: Antigravity in a container"
description: "Running Google Antigravity (agy) in an Apple Container environment: vendor terms, auth mechanisms, event shapes, egress allowlist, cancellation via whr-shim, and session resume."
weight: 9
---

> Source: `spike/agy` at `c752c41` (a local branch until it is pushed; link it then), with scripts, measurement harness, and raw results next to `RESULTS.md`. Tracks [#84](https://github.com/wstein/workharbor/issues/84). Measured on 1 October 2026.

Measured on Apple silicon macOS host with Apple Container 1.5.0 (`fedora:latest` linux-arm64 guest), Antigravity 1.2.14 (linux-arm64), `whr-shim` (static linux-arm64), and `whr-proxy` (egress allowlist proxy sidecar).

## Summary

| Test Case | Scenario | Measured Finding | Latency / Metric | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **Case 1** | Unauthenticated Run | Detects missing auth in 4.52 s, prints interactive OAuth URL and console code prompt | Detection: 4.52 s | **PASS** |
| **Case 2** | Auth Discovery | Identifies 5 binary auth modes; maps consumer OAuth and Gemini API key storage schemas | D40 / D41 mapped | **PASS** |
| **Case 3** | Event Shapes | `stream-json` emits typed `init`, `step_update`, and `result` NDJSON events; registers 58 tools | 58 tools registered | **PASS** |
| **Case 4** | Egress Control | `whr-proxy` allows Google APIs; blocks unallowed domains and raw IP literals | 403 Forbidden on deny | **PASS** |
| **Case 5** | Cancel via `whr-shim` | Process group killed via `whr-shim kill`; zero orphan processes verified via pure `/proc` | Kill: 319.67 ms; 0 orphans | **PASS** |
| **Case 6** | Session Resume | Resumes conversation via `--conversation <id>` with prompt cache hits (16,303 tokens) | Multi-turn NDJSON stream | **PASS** |
| **Case 7** | Error & Quota Signaling | Emits structured `AGY_ERROR: {...}` JSON on stderr; exits with canonical exit code 3 | Exit code 3; structured JSON | **PASS** |

---

## Detailed Findings

### 1. Vendor Terms (D40, D41)

Reading of the Google Terms of Service, Generative AI Additional Terms, and Gemini API Additional Terms (as of 1 October 2026) {{< status unverified >}}:
- **Consumer Sign-In**: Personal interactive use with an individual Google Account (including Google One AI Premium / Gemini Advanced subscriptions) is covered under standard consumer terms. Automated headless pipelines should use the Gemini API (with `GEMINI_API_KEY`) or Vertex AI rather than driving consumer account sessions.
- **Agentic Confirmation Clause**: Under the Gemini API and Generative AI Additional Terms, developers using agentic services *"will not automatically bypass any requests for human confirmation"*. This aligns directly with workharbor's policy table and blocking Decisions (design §6).
- **Seat Boundaries**: Under D40, each developer signs in to their own environment through Google's official OAuth flow. workharbor does not collect, forward, or share user tokens.

### 2. Auth Discovery & In-Environment Sign-In (D40)

Inspection of the Antigravity binary and runtime files revealed five internal auth modes:
1. `AUTH_MODE_PERSONAL_CONSUMER`: Consumer OAuth session.
2. `AUTH_MODE_GEMINI_API_KEY`: Direct API key access via `GEMINI_API_KEY` with `modelProvider: "gemini"` in `~/.gemini/antigravity-cli/settings.json`.
3. `AUTH_MODE_BUSINESS_LICENSED_ENTERPRISE`: Enterprise seat licensing.
4. `AUTH_MODE_BUSINESS_AGENT_PLATFORM_PAYGO`: Pay-as-you-go agent platform.
5. `AUTH_MODE_ENTERPRISE_GATEWAY`: Gateway proxy mode.

**Storage locations**:
- Consumer token: `~/.gemini/jetski-standalone-oauth-token` (file permission `0600`), containing `auth_method: "consumer"` and `token` with `access_token`, `refresh_token`, and `expiry`.
- Active account: `~/.gemini/google_accounts.json` (`{"active": "user@example.com", "old": []}`).
- Storage provider: `compositeTokenStorage` checks `KeyringTokenStorage` (macOS Keychain / Linux Secret Service D-Bus) and falls back to `FileTokenStorage`.
- **In-Environment Sign-in**: In headless print mode without an existing session, Antigravity prints an OAuth authorization URL and waits with a 60 s timeout for the human to paste the code (`Or, paste the authorization code here and press Enter:`). This allows the human to authenticate inside the container (D40) over terminal stdio without requiring a graphical browser inside the container.

### 3. Event Shapes (`--output-format stream-json`)

Antigravity natively emits newline-delimited JSON (NDJSON) events:
- **`init`**:
  ```json
  {
    "event": "init",
    "conversation_id": "ded8fc6d-00b3-4d6e-9f44-82d68f657446",
    "init": {
      "cwd": "/",
      "tools": ["run_command", "view_file", "replace_file_content", "multi_replace_file_content", ...],
      "permission_mode": "request-review"
    }
  }
  ```
- **`step_update`**:
  ```json
  {
    "event": "step_update",
    "step_update": {
      "conversation_id": "...",
      "step_index": 1,
      "state": "DONE",
      "step_type": "agent_response",
      "text_delta": "...",
      "duration_seconds": 2.25,
      "usage": {
        "input_tokens": 20454,
        "output_tokens": 153,
        "thinking_tokens": 121,
        "cache_read_tokens": 0,
        "total_tokens": 20607
      }
    }
  }
  ```
- **`result`**:
  ```json
  {
    "event": "result",
    "result": {
      "conversation_id": "...",
      "status": "SUCCESS",
      "response": "...",
      "duration_seconds": 2.40,
      "num_turns": 1,
      "usage": {
        "input_tokens": 20454,
        "output_tokens": 153,
        "thinking_tokens": 121,
        "cache_read_tokens": 0,
        "total_tokens": 20607
      }
    }
  }
  ```

### 4. Egress Allowlist Proxy Fit

When routed through `whr-proxy` in the sidecar, the following hosts must be allowed:
- **Gemini API Mode**: `generativelanguage.googleapis.com:443`.
- **Consumer OAuth Sign-In**: `accounts.google.com:443`, `oauth2.googleapis.com:443`, `www.googleapis.com:443`.
- **Backend / Telemetry**: `aicode.googleapis.com:443`, `cloudcode-pa.googleapis.com:443`.
- **Tool Store / Updates**: `storage.googleapis.com:443`.

Unlisted domains (`example.com:443`) and raw IP literals (`1.1.1.1:80`) are blocked by `whr-proxy` with `403 Forbidden` (`blocked by egress policy`). Feature-flag requests to `antigravity-unleash.goog:443` are safely denied without affecting agent execution.

### 5. Cancellation via `whr-shim`

Wrapping execution in `whr-shim run -pidfile /tmp/agy.pid ...` establishes a process group for Antigravity and its child processes. Calling `whr-shim kill -pidfile /tmp/agy.pid` sends `SIGINT` (followed by `SIGKILL` if grace expires) to the entire process group.
- **Kill Latency**: 319.67 ms.
- **Orphan Count**: 0 processes remained, verified via `/proc/[0-9]*/cmdline` and `/proc/[0-9]*/stat`.

### 6. Session Resume & Multi-Turn Protocol

- **Resume**: Sessions resume via `antigravity --conversation <id>` or `antigravity --continue`. Turn 2 executions reuse cached prompt tokens (16,303 tokens read from cache in test measurements) and increment turn counts cleanly.
- **Interactive Multi-Turn Driver**: Passing `--input-format stream-json --output-format stream-json` enables bi-directional NDJSON streaming over stdio, allowing a persistent driver process to execute turns sequentially.

### 7. Structured Error Reporting & Quota Signaling

When an API error occurs (e.g. invalid key, quota exhaustion, or spend cap), Antigravity exits with code `3` and emits a structured JSON line on stderr:
```json
AGY_ERROR: {
  "short_error": "agent executor error: generating and executing: Error 400, Message: API key not valid...",
  "status": "INVALID_ARGUMENT",
  "error_code": 400,
  "code_kind": "http",
  "retryable": false,
  "error_id": "83a2d209-f787-4c42-b64a-ffb0aa994963-1"
}
```
Exhausted daily quotas and billing spend caps stop immediately rather than retrying indefinitely.
