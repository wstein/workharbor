# Spike #84: Antigravity in a Container

This spike investigates running the Google Antigravity agent CLI (`antigravity` / `agy`) inside an isolated Apple Container environment behind the `whr-proxy` sidecar egress allowlist proxy.

## Scope and Research Questions

1. **Terms**: What Google's terms say about consumer accounts vs Gemini API keys vs automated use (documented in `docs/content/docs/manual/vendor-terms.md`).
2. **Auth Modes**: Discovery of authentication mechanisms:
  - Consumer account OAuth (`AUTH_MODE_PERSONAL_CONSUMER`): token schema in `~/.gemini/jetski-standalone-oauth-token`, `~/.gemini/google_accounts.json`, keyring vs file fallback.
  - Gemini API key (`AUTH_MODE_GEMINI_API_KEY`): direct headless execution against `generativelanguage.googleapis.com` without signing in.
  - Interactive login from within container (D40): OAuth code grant via console paste without requiring browser in guest.
3. **Hardened Environment**: Running pinned `antigravity` (1.2.14) on an `--internal` container network behind `whr-proxy`.
4. **Contract Fit (§5.2)**: NDJSON event stream (`stream-json`), session resume (`--conversation <id>`), multi-turn streaming (`--input-format stream-json`), cancellation via `whr-shim`, and structured error reporting (`AGY_ERROR: {...}`, exit code 3).

## Running the Spike

Prerequisites:
- Apple Silicon macOS host with Apple Container (`container` CLI)
- Go compiler (for building `whr-proxy` and `whr-shim`)
- Python 3

```bash
# Build guest tools and run the full measurement suite:
./run-spike.sh
```

Results are recorded in `results/` and summarized in `RESULTS.md`.
