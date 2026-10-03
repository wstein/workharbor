---
title: "Spike #82: Agent subscription sign-in inside an environment"
description: "Subscription sign-in inside an Apple Container environment behind an egress allowlist proxy under D40: Claude Code and Codex CLI flows, required egress hosts, persistence on the agent-home volume, concurrency, and phone-first feasibility."
weight: 10
---

> Source: [`spikes/agent-signin/`](https://github.com/wstein/workharbor/tree/de923e4551ca1d7e76898bde2c2a17fca5bf6ee2/spikes/agent-signin) at commit `de923e4`, with scripts, measurement harness, and raw results next to `RESULTS.md`. The commit is in `main`'s history; the files left `main`'s tree in issue #194, and the spike has no `spike/agent-signin` branch. Tracks [#82](https://github.com/wstein/workharbor/issues/82). Measured on 1 October 2026.

D40 establishes that `whr` never handles or stores a subscription credential: the human signs in inside the environment through the vendor's own flow, and the CLI keeps the login on that environment's agent-home volume (D16); only an API key may be configured on the supervisor's side.

This spike measures how subscription sign-in works inside an Apple Container environment behind an egress allowlist proxy sidecar (`whr-proxy`) on an `--internal` network for Claude Code 2.1.285 and Codex CLI 0.159.2.

## Summary

| Topic | Claude Code 2.1.285 | Codex CLI 0.159.2 | Verdict |
| :--- | :--- | :--- | :--- |
| **In-Guest Sign-in Flow** | `claude auth login` (authorization code via console paste); `claude setup-token` (in TTY) | `codex login --device-auth` (RFC 8628 Device Code); `codex login` (local HTTP port 1455) | **PASS** |
| **Device Code Authorization** | Not supported (requires browser redirect & console code paste) | Supported natively via `--device-auth` (displays code, background polling) | **PASS** |
| **Required Egress Allowlist** | `claude.com`, `platform.claude.com`, `api.anthropic.com`, `auth.anthropic.com`, `statsig.anthropic.com` | `auth.openai.com`, `api.openai.com`, `chatgpt.com`, `cdn.oaistatic.com` | **PASS** |
| **Credential Storage Location** | `/root/.claude/.credentials.json` (mode `0600`) and `/root/.claude.json` | `/root/.codex/auth.json` (mode `0600`) | **PASS** |
| **Host Isolation** | Stored entirely on the named agent-home volume (`-v volume:/root`); zero host leakage | Stored entirely on the named agent-home volume (`-v volume:/root`); zero host leakage | **PASS** |
| **Stop / Start / Rebuild Survival** | Preserved across container `stop`, `start`, and container recreate with the same volume | Preserved across container `stop`, `start`, and container recreate with the same volume | **PASS** |
| **Concurrent Environments on One Account** | Supported if signed in separately (separate OAuth refresh tokens). Cloned volumes collide on refresh token rotation. | Supported if signed in separately (separate OAuth refresh tokens). Cloned volumes collide on refresh token rotation. | **PASS** |
| **Phone-First Flow (D35)** | **No**; requires terminal interaction (`container exec -it` or SSH) to paste auth code | **Yes**; user opens URL on phone, enters code, Codex finishes sign-in automatically | **PASS** |

---

## 1. Claude Code 2.1.285 Sign-in Flows

In a hardened container environment (Apple Container on an `--internal` network, egress through `whr-proxy` sidecar), two interactive sign-in flows were tested:

### Flow A: `claude auth login`
When run in a headless container attached via terminal (`container exec -it`), Claude Code detects the absence of a desktop browser and prints:
```text
Opening browser to sign in…
If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=org%3Acreate_api_key+user%3Aprofile+user%3Ainference+user%3Asessions%3Aclaude_code+user%3Amcp_servers+user%3Afile_upload+user%3Aplugins&code_challenge=...&code_challenge_method=S256&state=...
Paste code here if prompted >
```
The developer opens the link in their browser (Mac or mobile), logs in to Claude.ai, and is shown a one-time code on `platform.claude.com`. Pasting that code back into the attached terminal prompt completes authentication.

### Flow B: `claude setup-token`
`claude setup-token` uses an Ink React interactive terminal UI, requiring an attached TTY. It requests `scope=user:inference` and outputs a 1-year long-lived token. Like `claude auth login`, it prints an authorization URL and prompts `Paste code here if prompted >`.

### Credential Storage & Isolation
Because Linux containers lack the macOS Keychain, Claude Code writes credentials to:
- `/root/.claude/.credentials.json` with permissions `0600` (`-rw-------`), directory `/root/.claude/` permissions `0700`.
- Schema:
  ```json
  {
    "claudeAiOauth": {
      "accessToken": "sk-ant-...",
      "refreshToken": "sk-ant-...",
      "expiresAt": 1800000000000,
      "scopes": ["user:profile", "user:inference", "user:sessions:claude_code"]
    }
  }
  ```
- Metadata lands in `/root/.claude.json` (`oauthAccount`).
- The entire credential directory resides on the environment's dedicated agent-home volume. No tokens or files ever leak to the host filesystem.

---

## 2. Codex CLI 0.159.2 Sign-in Flows

### Flow A: `codex login --device-auth` (Device Code Flow)
Codex CLI natively implements RFC 8628 OAuth 2.0 Device Authorization Grant:
```text
Welcome to Codex [v0.159.2]
OpenAI's command-line coding agent

Follow these steps to sign in with ChatGPT using device code authorization:

1. Open this link in your browser and sign in to your account
  https://auth.openai.com/codex/device

2. Enter this one-time code (expires in 15 minutes)
  8W3N-RMYQ8

Continue only if you started this login in Codex. If a website or another person gave you this code, cancel.
```
Codex CLI polls `https://auth.openai.com` in the background. The user does not need to paste anything back into the terminal. Once authorized on OpenAI's website, Codex CLI saves credentials and exits `0`.

### Flow B: `codex login` (Local Callback Server)
Without `--device-auth`, Codex CLI spins up a local HTTP callback server on `http://localhost:1455`. In an isolated container without host port forwarding, browser redirects to `localhost:1455` fail. Codex prints: *"On a remote or headless machine? Use `codex login --device-auth` instead."*

### Credential Storage
Credentials land in `/root/.codex/auth.json` (mode `0600`), holding:
```json
{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "...",
    "access_token": "...",
    "refresh_token": "...",
    "account_id": "..."
  },
  "last_refresh": "..."
}
```

---

## 3. Lifecycle Survival

Both Claude Code and Codex CLI credentials were tested across:
1. `container stop` followed by `container start`
2. `container rm -f` followed by creating a fresh container VM mounting the same agent-home volume (`-v volume:/root`)

In both cases:
- File permissions (`0600`) and contents were preserved exactly.
- `claude auth status` immediately reports `"loggedIn": true, "authMethod": "claude.ai"`.
- This confirms that subscription logins persist across environment rebuilds as long as the named volume is preserved (D16).

---

## 4. Concurrency and Refresh Token Rotation

When two separate environments run concurrently under the same user account:
- **Independent Logins (D40 compliant)**: When each environment completes its own in-guest login flow, Anthropic/OpenAI generates a distinct OAuth authorization code and distinct refresh token pair. In `test-concurrency.sh`, two containers running simultaneously on internal IPs `192.168.128.3` and `192.168.128.4` performed concurrent agent calls through the shared `whr-proxy` sidecar (340 ms) without port, socket, or token conflicts.
- **Cloned Volumes (Hazard)**: If an agent-home volume is copied or cloned, both environments share the same refresh token. Because Claude Code uses **Refresh Token Rotation (RTR)**, the first environment to refresh its token revokes the previous refresh token. The second environment's subsequent refresh attempt fails with `401 unauthorized` (`refresh_token_dead`). workharbor must never clone an agent-home volume between environments.

---

## 5. Phone-First Viability (D35)

- **Claude Code**: Cannot be completed from the phone alone. Because Anthropic's flow is an authorization code grant that requires copying the code from `platform.claude.com` and pasting it into the waiting terminal prompt, the human must attach to the container terminal (`container exec -it` on the Mac, or an SSH/web terminal session).
- **Codex CLI**: Can be completed entirely from the phone. The supervisor displays the user code (e.g. `8W3N-RMYQ8`) and URL (`https://auth.openai.com/codex/device`). The user opens the URL on their phone browser, confirms the login, and Codex CLI in the guest detects the grant via background polling.
