# Spike #82: Agent Subscription Sign-in Results

Measured on macOS (Apple Silicon host, 16 GiB) with Apple Container 1.5.0, `fedora:latest` linux-arm64 guest,
`whr-proxy` sidecar egress allowlist proxy, Claude Code 2.1.285 (glibc linux-arm64), and Codex CLI 0.159.2 (static musl linux-arm64).

## Summary Matrix

| Question / Area | Claude Code 2.1.285 | Codex CLI 0.159.2 |
| :--- | :--- | :--- |
| **Supported Sign-in Flows in Container** | `claude auth login` (OAuth code with terminal paste); `claude setup-token` (1-year token in TTY with terminal paste) | `codex login --device-auth` (RFC 8628 Device Code); `codex login` (local HTTP callback server on port 1455) |
| **Device Code Authorization** | Not supported (no RFC 8628 device flow; requires redirect & terminal paste) | Supported natively via `--device-auth` (displays code, polls server) |
| **Required Egress Allowlist** | `claude.com`, `platform.claude.com`, `api.anthropic.com`, `auth.anthropic.com`, `statsig.anthropic.com` | `auth.openai.com`, `api.openai.com`, `chatgpt.com`, `cdn.oaistatic.com` |
| **Credential Storage Location** | `/root/.claude/.credentials.json` (0600) & `/root/.claude.json` | `/root/.codex/auth.json` (0600) |
| **Host Leakage** | None. All credentials reside strictly on the named agent-home volume (`-v volume:/root`) | None. Credentials reside strictly on the named agent-home volume (`-v volume:/root`) |
| **Lifecycle Survival (Stop/Start/Rebuild)** | Survives `container stop`/`start` and `container rm -f`/`run` with same volume | Survives `container stop`/`start` and `container rm -f`/`run` with same volume |
| **Concurrent Environments on One Account** | Runs concurrently if signed in separately (separate OAuth refresh tokens). Cloned volumes collide on token rotation. | Runs concurrently if signed in separately (separate OAuth refresh tokens). Cloned volumes collide on token rotation. |
| **Phone-First Flow (D35)** | **No**; requires terminal interaction (`container exec -it` on Mac or SSH) to paste auth code. | **Yes**; user opens URL on phone, types code, Codex polls and completes sign-in automatically. |

## Detailed Findings

### 1. Claude Code 2.1.285

- **Flow 1 (`claude auth login`)**:
  When run in a container without a local browser, Claude detects the headless environment, outputs the authorization URL:
  `https://claude.com/cai/oauth/authorize?code=true&client_id=...&redirect_uri=https://platform.claude.com/oauth/code/callback&scope=...`
  and prompts on stdout:
  `Paste code here if prompted > `
  The developer navigates to the URL on their browser (Mac or phone), authenticates to Claude.ai, and is shown an authorization code on `platform.claude.com`. Pasting this code back into the attached terminal finishes sign-in.
- **Flow 2 (`claude setup-token`)**:
  Requires an attached TTY (Ink React UI). Renders:
  `Welcome to Claude Code v2.1.285`
  `This will guide you through long-lived (1-year) auth token setup for your Claude account. Claude subscription required.`
  `Browser didn't open? Use the url below to sign in (c to copy)`
  `Paste code here if prompted > `
  Requests `scope=user:inference` and outputs a long-lived bearer token.
- **Storage**:
  In a Linux container (no macOS Keychain), Claude writes to `/root/.claude/.credentials.json` with permissions `0600` (`-rw-------`), containing:
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
  Metadata lands in `/root/.claude.json` (`oauthAccount`).
- **Persistence**:
  After `container stop` and `start`, or deleting the container and recreating it with the same volume, `claude auth status` immediately reports `"loggedIn": true, "authMethod": "claude.ai"`.

### 2. Codex CLI 0.159.2

- **Flow 1 (`codex login --device-auth`)**:
  Implements standard RFC 8628 Device Code authorization:
  ```
  Welcome to Codex [v0.159.2]
  Follow these steps to sign in with ChatGPT using device code authorization:
  1. Open this link in your browser and sign in to your account
    https://auth.openai.com/codex/device
  2. Enter this one-time code (expires in 15 minutes)
    8W3N-RMYQ8
  ```
  Codex polls `https://auth.openai.com` in the background until the user signs in. No terminal input or paste is required.
- **Flow 2 (`codex login`)**:
  Starts a local HTTP server on `http://localhost:1455` for browser redirect. Fails in headless containers unless port 1455 is forwarded.
- **Storage**:
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
- **Persistence**:
  Survives container recreation on the agent-home volume.

### 3. Concurrency and Refresh Token Rotation

- When two environments are signed in separately (each environment completes its own authorization flow), the OAuth provider issues a unique refresh token to each environment.
- In `test-concurrency.sh`, two containers running simultaneously on internal IPs `192.168.128.3` and `192.168.128.4` executed agent calls through the shared `whr-proxy` sidecar concurrently (340 ms) without connection or proxy interference.
- **Important Warning**: If an environment volume is duplicated or cloned, both environments share the same refresh token. Because Claude Code uses refresh token rotation, whichever environment refreshes first invalidates the other environment's token (resulting in `refresh_token_dead` and 401 failure). Separate sign-in per environment (D40) prevents this.

### 4. Phone-First Viability (D35)

- **Claude Code**: Cannot be completed purely from the phone without an attached terminal. Because Claude Code uses an authorization code grant that prints a prompt and waits for user paste, the human needs an attached terminal (`container exec -it` from the host Mac or an SSH session).
- **Codex CLI**: Can be completed from the phone. The phone user opens `https://auth.openai.com/codex/device`, enters the code displayed in the supervisor's log/UI, and Codex finishes sign-in on its own.
